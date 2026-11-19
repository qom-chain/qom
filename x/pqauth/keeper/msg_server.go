package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"strconv"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/cosmos/evm/x/pqauth/types"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// mldsaVerifyGas is charged for each ML-DSA-87 verification performed by a
// message handler. It matches ante.MLDSA87VerifyGas.
const mldsaVerifyGas uint64 = 250_000

var _ types.MsgServer = Keeper{}

// RegisterMLDSAKey binds an ML-DSA-87 public key to the signing account.
// The transaction is authorized by the account's ethsecp256k1 signature.
// pop_signature proves the account holds the matching ML-DSA private key.
// An account with EVM code (including an EIP-7702 delegation) is refused.
// Existing authz grants and fee allowances where this account is the granter,
// and ERC-20 precompile allowances it owns, are deleted, so a grant or
// approval created before registration cannot bypass ML-DSA.
func (k Keeper) RegisterMLDSAKey(goCtx context.Context, msg *types.MsgRegisterMLDSAKey) (*types.MsgRegisterMLDSAKeyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	addr := sdk.MustAccAddressFromBech32(msg.Signer)
	secpPub, err := k.requireEthSecp256k1Account(ctx, addr, msg.Signer)
	if err != nil {
		return nil, err
	}
	if k.HasMLDSAPubKey(ctx, addr) {
		return nil, errorsmod.Wrapf(types.ErrMLDSAKeyExists, "account %s already has an ML-DSA-87 public key; rotate it with MsgRotateMLDSAKey", msg.Signer)
	}
	if err := k.requireNoEVMCode(ctx, addr, msg.Signer); err != nil {
		return nil, err
	}
	if err := k.verifyAccountPop(ctx, addr, secpPub, msg.PubKey, msg.PopSignature); err != nil {
		return nil, errorsmod.Wrapf(err, "account %s", msg.Signer)
	}
	if err := k.revokeGranterAuthorisations(ctx, addr); err != nil {
		return nil, err
	}
	if err := k.revokeERC20Allowances(ctx, addr); err != nil {
		return nil, err
	}
	if err := k.SetMLDSAPubKey(ctx, addr, msg.PubKey); err != nil {
		return nil, errorsmod.Wrapf(err, "account %s: ML-DSA-87 public key was rejected", msg.Signer)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRegisterMLDSAKey,
		sdk.NewAttribute(sdk.AttributeKeySender, msg.Signer),
		sdk.NewAttribute(types.AttributeKeyAccount, msg.Signer),
		sdk.NewAttribute(types.AttributeKeyPubKey, hex.EncodeToString(msg.PubKey)),
	))

	return &types.MsgRegisterMLDSAKeyResponse{}, nil
}

// RotateMLDSAKey replaces an account's ML-DSA-87 public key. The current key
// and the new key must both sign the rotation payload.
func (k Keeper) RotateMLDSAKey(goCtx context.Context, msg *types.MsgRotateMLDSAKey) (*types.MsgRotateMLDSAKeyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	addr := sdk.MustAccAddressFromBech32(msg.Signer)
	secpPub, err := k.requireEthSecp256k1Account(ctx, addr, msg.Signer)
	if err != nil {
		return nil, err
	}
	oldPub, found := k.GetMLDSAPubKey(ctx, addr)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrMLDSAKeyNotFound, "account %s has no ML-DSA-87 public key to rotate", msg.Signer)
	}
	payload, err := types.RotateSignBytes(addr, ctx.ChainID(), secpPub, oldPub, msg.NewPubKey)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "account %s", msg.Signer)
	}
	if err := consumeVerifyGas(ctx); err != nil {
		return nil, err
	}
	if err := types.VerifyMLDSA(oldPub, payload, msg.OldSignature); err != nil {
		return nil, errorsmod.Wrapf(err, "account %s current ML-DSA key", msg.Signer)
	}
	if err := consumeVerifyGas(ctx); err != nil {
		return nil, err
	}
	if err := types.VerifyMLDSA(msg.NewPubKey, payload, msg.NewSignature); err != nil {
		return nil, errorsmod.Wrapf(err, "account %s new ML-DSA key", msg.Signer)
	}
	if err := k.ReplaceMLDSAPubKey(ctx, addr, msg.NewPubKey); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRotateMLDSAKey,
		sdk.NewAttribute(sdk.AttributeKeySender, msg.Signer),
		sdk.NewAttribute(types.AttributeKeyAccount, msg.Signer),
		sdk.NewAttribute(types.AttributeKeyPubKey, hex.EncodeToString(msg.NewPubKey)),
	))
	return &types.MsgRotateMLDSAKeyResponse{}, nil
}

// RegisterValidatorMLDSAKey binds an ML-DSA-87 public key to the consensus
// address of a validator operated by the signer. The new key proves possession.
// Replacing a stored key also requires the current key's signature.
func (k Keeper) RegisterValidatorMLDSAKey(goCtx context.Context, msg *types.MsgRegisterValidatorMLDSAKey) (*types.MsgRegisterValidatorMLDSAKeyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	if k.staking == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "pqauth staking keeper is not set")
	}

	addr := sdk.MustAccAddressFromBech32(msg.Signer)
	secpPub, err := k.requireEthSecp256k1Account(ctx, addr, msg.Signer)
	if err != nil {
		return nil, err
	}
	consAddr := sdk.ConsAddress(msg.ConsensusAddress)
	validator, err := k.staking.GetValidatorByConsAddr(ctx, consAddr)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidValidator, "no validator for consensus address %X: %s", consAddr, err.Error())
	}
	operator, err := sdk.ValAddressFromBech32(validator.GetOperator())
	if err != nil {
		return nil, errorsmod.Wrapf(err, "validator operator address")
	}
	if !bytes.Equal(operator.Bytes(), addr.Bytes()) {
		return nil, errorsmod.Wrapf(types.ErrInvalidValidator, "account %s is not the operator of consensus address %X", msg.Signer, consAddr)
	}

	payload, err := types.ValidatorPoPSignBytes(ctx.ChainID(), consAddr, secpPub, msg.PubKey)
	if err != nil {
		return nil, err
	}
	if err := consumeVerifyGas(ctx); err != nil {
		return nil, err
	}
	if err := types.VerifyMLDSA(msg.PubKey, payload, msg.PopSignature); err != nil {
		return nil, errorsmod.Wrapf(err, "validator %X", consAddr)
	}

	if current, found := k.GetValidatorMLDSAPubKey(ctx, consAddr); found {
		if len(msg.OldSignature) == 0 {
			return nil, errorsmod.Wrapf(types.ErrInvalidProof, "consensus address %X already has an ML-DSA key; sign the replacement with that key", consAddr)
		}
		if err := consumeVerifyGas(ctx); err != nil {
			return nil, err
		}
		if err := types.VerifyMLDSA(current, payload, msg.OldSignature); err != nil {
			return nil, errorsmod.Wrapf(err, "current validator key %X", consAddr)
		}
		if err := k.ReplaceValidatorMLDSAPubKey(ctx, consAddr, msg.PubKey); err != nil {
			return nil, err
		}
	} else if err := k.SetValidatorMLDSAPubKey(ctx, consAddr, msg.PubKey); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRegisterValidatorMLDSAKey,
		sdk.NewAttribute(sdk.AttributeKeySender, msg.Signer),
		sdk.NewAttribute(types.AttributeKeyConsensusAddress, hex.EncodeToString(consAddr)),
		sdk.NewAttribute(types.AttributeKeyPubKey, hex.EncodeToString(msg.PubKey)),
	))
	return &types.MsgRegisterValidatorMLDSAKeyResponse{}, nil
}

// OpenEVMWindow opens or refreshes the signer's EVM authorisation window.
// The transaction itself is authorized by the account's ethsecp256k1 signature
// in the SDK ante handler. The expiry is the current height plus a fixed duration.
func (k Keeper) OpenEVMWindow(goCtx context.Context, msg *types.MsgOpenEVMWindow) (*types.MsgOpenEVMWindowResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	addr := sdk.MustAccAddressFromBech32(msg.Signer)
	if _, err := k.requireEthSecp256k1Account(ctx, addr, msg.Signer); err != nil {
		return nil, err
	}

	duration := k.GetParams(ctx).WindowDuration()
	expiry, err := types.EVMWindowExpiryHeight(ctx.BlockHeight(), duration)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "cannot open an EVM window for %s at height %d", msg.Signer, ctx.BlockHeight())
	}
	if err := k.SetEVMWindowExpiry(ctx, addr, expiry); err != nil {
		return nil, errorsmod.Wrapf(err, "cannot store the EVM window for %s", msg.Signer)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeOpenEVMWindow,
		sdk.NewAttribute(sdk.AttributeKeySender, msg.Signer),
		sdk.NewAttribute(types.AttributeKeyAccount, msg.Signer),
		sdk.NewAttribute(types.AttributeKeyExpiry, strconv.FormatInt(expiry, 10)),
	))

	return &types.MsgOpenEVMWindowResponse{ExpiryHeight: expiry}, nil
}

// requireEthSecp256k1Account returns the account's compressed public key.
// A missing account or a non-ethsecp256k1 key is an error, not a panic.
func (k Keeper) requireEthSecp256k1Account(ctx sdk.Context, addr sdk.AccAddress, signer string) ([]byte, error) {
	acc := k.accountKeeper.GetAccount(ctx, addr)
	if acc == nil {
		return nil, errorsmod.Wrapf(types.ErrAccountNotFound, "account %s does not exist; create it before using pqauth", signer)
	}
	pub := acc.GetPubKey()
	if pub == nil {
		return nil, errorsmod.Wrapf(types.ErrNotEthSecp256k1, "account %s has no public key; pqauth requires ethsecp256k1", signer)
	}
	if _, ok := pub.(*ethsecp256k1.PubKey); !ok {
		return nil, errorsmod.Wrapf(types.ErrNotEthSecp256k1, "account %s has public key type %q; pqauth requires ethsecp256k1", signer, pub.Type())
	}
	return pub.Bytes(), nil
}

func (k Keeper) verifyAccountPop(ctx sdk.Context, addr sdk.AccAddress, secpPub, mldsaPub, signature []byte) error {
	payload, err := types.AccountPoPSignBytes(addr, ctx.ChainID(), secpPub, mldsaPub)
	if err != nil {
		return err
	}
	if err := consumeVerifyGas(ctx); err != nil {
		return err
	}
	return types.VerifyMLDSA(mldsaPub, payload, signature)
}

func consumeVerifyGas(ctx sdk.Context) error {
	ctx.GasMeter().ConsumeGas(mldsaVerifyGas, "pqauth verify: mldsa87")
	return nil
}
