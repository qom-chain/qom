package evm

import (
	"context"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	anteinterfaces "github.com/cosmos/evm/ante/interfaces"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	errorsmod "cosmossdk.io/errors"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// accountKeeper is the account lookup used to recognise module accounts.
type accountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}

// EVMWindowDecorator rejects an Ethereum transaction when its sender, or any
// EIP-7702 SetCode authority it carries, has a registered ML-DSA-87 key and
// the EVM authorisation window is closed.
// Senders without a registered key keep normal ethereum behaviour.
// Module accounts and IBC messages are exempt.
//
// An open window authorises the Ethereum transaction with the account's
// ethsecp256k1 key alone. Bank (0x0804), staking (0x0800), and gov (0x0805)
// precompiles move Cosmos-module state, so a forged secp256k1 signature during
// the window can spend balances, stake, or vote. restrict_cosmos_precompiles
// rejects a top-level call to those three addresses. A call that reaches the
// same precompile through another contract is not visible here. The parameter
// defaults to false so existing contracts keep working.
type EVMWindowDecorator struct {
	ak accountKeeper
	pq anteinterfaces.PQAuthKeeper
}

// NewEVMWindowDecorator returns a decorator that runs after ethereum signature
// verification in the EVM ante handler.
func NewEVMWindowDecorator(ak accountKeeper, pq anteinterfaces.PQAuthKeeper) EVMWindowDecorator {
	return EVMWindowDecorator{ak: ak, pq: pq}
}

// AnteHandle checks each Ethereum sender. A registered account may spend only
// while its authorisation window is open.
func (d EVMWindowDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if d.pq == nil {
		return ctx, errorsmod.Wrap(errortypes.ErrLogic, "EVM authorisation window check: pqauth keeper is nil")
	}

	for _, msg := range tx.GetMsgs() {
		if isIBCMessage(msg) {
			continue
		}
		ethMsg, ok := msg.(*evmtypes.MsgEthereumTx)
		if !ok {
			continue
		}

		sender := ethMsg.GetFrom()
		if sender.Empty() {
			return ctx, errorsmod.Wrap(errortypes.ErrInvalidRequest, "Ethereum transaction has an empty sender, so the EVM authorisation window cannot be checked")
		}
		if err := d.checkSender(ctx, sender, ethMsg); err != nil {
			return ctx, err
		}
		// EIP-7702: every SetCode authority follows the same window rule as
		// the sender.
		if err := d.checkSetCodeAuthorities(ctx, ethMsg); err != nil {
			return ctx, err
		}
	}

	return next(ctx, tx, simulate)
}

// checkSender applies the window rule to the Ethereum transaction sender.
func (d EVMWindowDecorator) checkSender(ctx sdk.Context, sender sdk.AccAddress, ethMsg *evmtypes.MsgEthereumTx) error {
	if d.exemptAccount(ctx, sender) {
		return nil
	}
	if _, registered := d.pq.GetMLDSAPubKey(ctx, sender); !registered {
		return nil
	}
	if d.pq.IsEVMWindowOpen(ctx, sender) {
		if d.pq.RestrictCosmosPrecompiles(ctx) && targetsRestrictedPrecompile(ethMsg) {
			return errorsmod.Wrapf(
				errortypes.ErrUnauthorized,
				"EVM authorisation window is open for %s, but restrict_cosmos_precompiles rejects a top-level bank, staking, or gov precompile call from an ML-DSA-registered account",
				sender,
			)
		}
		return nil
	}
	return errorsmod.Wrapf(
		errortypes.ErrUnauthorized,
		"EVM authorisation window is closed for %s; this account has a registered ML-DSA-87 key, so send MsgOpenEVMWindow before an Ethereum transaction",
		sender,
	)
}

// checkSetCodeAuthorities applies the window rule to every recoverable
// EIP-7702 authority in a SetCode (type 0x04) transaction. An authorisation
// whose signature cannot be recovered is skipped: the EVM never applies it.
func (d EVMWindowDecorator) checkSetCodeAuthorities(ctx sdk.Context, ethMsg *evmtypes.MsgEthereumTx) error {
	ethTx := ethMsg.AsTransaction()
	if ethTx == nil {
		return nil
	}
	for _, auth := range ethTx.SetCodeAuthorizations() {
		authority, err := auth.Authority()
		if err != nil {
			continue
		}
		addr := sdk.AccAddress(authority.Bytes())
		if d.exemptAccount(ctx, addr) {
			continue
		}
		if _, registered := d.pq.GetMLDSAPubKey(ctx, addr); !registered {
			continue
		}
		if d.pq.IsEVMWindowOpen(ctx, addr) {
			continue
		}
		return errorsmod.Wrapf(
			errortypes.ErrUnauthorized,
			"EVM authorisation window is closed for %s; this account has a registered ML-DSA-87 key, so send MsgOpenEVMWindow before signing an EIP-7702 SetCode authorisation",
			addr,
		)
	}
	return nil
}

func (d EVMWindowDecorator) exemptAccount(ctx sdk.Context, sender sdk.AccAddress) bool {
	if d.ak == nil {
		return false
	}
	_, ok := d.ak.GetAccount(ctx, sender).(sdk.ModuleAccountI)
	return ok
}

// isIBCMessage reports whether msg is an IBC protocol message.
// Those messages are not subject to the EVM authorisation window.
func isIBCMessage(msg sdk.Msg) bool {
	return strings.HasPrefix(codectypes.MsgTypeURL(msg), "/ibc.")
}

// targetsRestrictedPrecompile reports whether the Ethereum transaction's
// top-level To address is the bank, staking, or gov precompile. A nil
// transaction or a contract-creation transaction has no such target.
func targetsRestrictedPrecompile(msg *evmtypes.MsgEthereumTx) bool {
	tx := msg.AsTransaction()
	if tx == nil {
		return false
	}
	return isRestrictedPrecompile(tx.To())
}

func isRestrictedPrecompile(to *common.Address) bool {
	if to == nil {
		return false
	}
	switch *to {
	case common.HexToAddress(evmtypes.BankPrecompileAddress),
		common.HexToAddress(evmtypes.StakingPrecompileAddress),
		common.HexToAddress(evmtypes.GovPrecompileAddress):
		return true
	default:
		return false
	}
}
