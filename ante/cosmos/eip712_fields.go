package cosmos

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/cosmos/evm/ethereum/eip712"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	errorsmod "cosmossdk.io/errors"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"
)

// EIP712SignDocFieldsDecorator applies the upstream EIP-712 sign doc field
// check from the upstream-fixes activation height.
//
// Upstream rejects, on the EIP-712 signature path of eth_secp256k1 keys, a
// Protobuf sign doc that sets TimeoutTimestamp, Unordered, or a fee
// granter/payer, which the EIP-712 typed data does not include.
// That check sits in a function without a context, so it cannot be gated by
// height there. This decorator gives the same result with a context: for such
// sign docs it requires the signature to verify with standard ECDSA, which is
// what the upstream VerifySignature reduces to for them. It consumes no gas.
//
// It must run after SigVerificationDecorator and before
// IncrementSequenceDecorator, so that it sees the same signer data.
type EIP712SignDocFieldsDecorator struct {
	ak              accountKeeper
	signModeHandler *txsigning.HandlerMap
}

// NewEIP712SignDocFieldsDecorator returns a new EIP712SignDocFieldsDecorator.
func NewEIP712SignDocFieldsDecorator(ak accountKeeper, signModeHandler *txsigning.HandlerMap) EIP712SignDocFieldsDecorator {
	return EIP712SignDocFieldsDecorator{ak: ak, signModeHandler: signModeHandler}
}

// AnteHandle implements sdk.AnteDecorator.
func (d EIP712SignDocFieldsDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !pqauthtypes.UpstreamFixesActive(ctx.BlockHeight()) ||
		simulate || ctx.IsReCheckTx() || !ctx.IsSigverifyTx() {
		return next(ctx, tx, simulate)
	}

	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "transaction does not implement the auth signing interface")
	}
	adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
	if !ok {
		return ctx, fmt.Errorf("expected tx to implement V2AdaptableTx, got %T", tx)
	}
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, err
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}
	if len(sigs) != len(signers) {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid number of signer;  expected: %d, got %d", len(signers), len(sigs))
	}

	txData := adaptableTx.GetSigningTxData()
	// Account reads here must not change the gas used by the transaction.
	readCtx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	genesis := ctx.BlockHeight() == 0
	for i, sig := range sigs {
		acc := d.ak.GetAccount(readCtx, signers[i])
		if acc == nil || acc.GetPubKey() == nil {
			// SigVerificationDecorator has already rejected this case.
			continue
		}
		pubKey := acc.GetPubKey()
		var accNum uint64
		if !genesis {
			accNum = acc.GetAccountNumber()
		}
		anyPk, err := codectypes.NewAnyWithValue(pubKey)
		if err != nil {
			return ctx, err
		}
		signerData := txsigning.SignerData{
			Address:       acc.GetAddress().String(),
			ChainID:       ctx.ChainID(),
			AccountNumber: accNum,
			Sequence:      sig.Sequence,
			PubKey:        &anypb.Any{TypeUrl: anyPk.TypeUrl, Value: anyPk.Value},
		}
		if err := checkEIP712SignDocFields(readCtx, pubKey, sig.Data, func(mode signingtypes.SignMode) ([]byte, error) {
			apiMode, err := internalToAPISignMode(mode)
			if err != nil {
				return nil, err
			}
			return d.signModeHandler.GetSignBytes(readCtx, apiMode, signerData, txData)
		}); err != nil {
			return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, err.Error())
		}
	}

	return next(ctx, tx, simulate)
}

// checkEIP712SignDocFields walks the signature data the same way as the SDK
// signature verification and, for every eth_secp256k1 signature in
// SIGN_MODE_DIRECT whose sign doc sets a field not covered by EIP-712,
// requires standard ECDSA verification.
func checkEIP712SignDocFields(
	ctx context.Context,
	pubKey cryptotypes.PubKey,
	data signingtypes.SignatureData,
	signBytes func(signingtypes.SignMode) ([]byte, error),
) error {
	switch data := data.(type) {
	case *signingtypes.SingleSignatureData:
		ethPk, ok := pubKey.(*ethsecp256k1.PubKey)
		if !ok || data.SignMode != signingtypes.SignMode_SIGN_MODE_DIRECT {
			return nil
		}
		bz, err := signBytes(data.SignMode)
		if err != nil {
			return err
		}
		if eip712.ProtobufSignDocHasUncoveredFields(bz) && !ethPk.VerifySignatureECDSA(bz, data.Signature) {
			return fmt.Errorf("EIP-712 signatures do not support TimeoutTimestamp, Unordered, or fee granter/payer")
		}
		return nil

	case *signingtypes.MultiSignatureData:
		multiPK, ok := pubKey.(multisig.PubKey)
		if !ok || data.BitArray == nil {
			return nil
		}
		pubKeys := multiPK.GetPubKeys()
		sigIndex := 0
		for i := 0; i < len(pubKeys) && i < data.BitArray.Count(); i++ {
			if !data.BitArray.GetIndex(i) {
				continue
			}
			if sigIndex >= len(data.Signatures) {
				return nil
			}
			if err := checkEIP712SignDocFields(ctx, pubKeys[i], data.Signatures[sigIndex], signBytes); err != nil {
				return err
			}
			sigIndex++
		}
		return nil

	default:
		return nil
	}
}

func internalToAPISignMode(mode signingtypes.SignMode) (signingv1beta1.SignMode, error) {
	switch mode {
	case signingtypes.SignMode_SIGN_MODE_DIRECT:
		return signingv1beta1.SignMode_SIGN_MODE_DIRECT, nil
	case signingtypes.SignMode_SIGN_MODE_LEGACY_AMINO_JSON:
		return signingv1beta1.SignMode_SIGN_MODE_LEGACY_AMINO_JSON, nil
	case signingtypes.SignMode_SIGN_MODE_TEXTUAL:
		return signingv1beta1.SignMode_SIGN_MODE_TEXTUAL, nil
	case signingtypes.SignMode_SIGN_MODE_DIRECT_AUX:
		return signingv1beta1.SignMode_SIGN_MODE_DIRECT_AUX, nil
	default:
		return signingv1beta1.SignMode_SIGN_MODE_UNSPECIFIED, fmt.Errorf("unsupported sign mode %s", mode)
	}
}
