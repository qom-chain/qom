package backend

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// The pqauth pre-checks below run in eth_sendRawTransaction. They reject a
// transaction whose sender, or an EIP-7702 authority, has a registered
// ML-DSA-87 key and would be rejected by the ante handler.
//
// The EVMWindowDecorator in the EVM ante handler already rejects such a
// transaction, but the app-side EVM mempool only runs the ante handler during
// asynchronous promotion (legacypool promoteExecutables), after Insert has
// already returned success. Without this pre-check the RPC returned a tx hash
// and the transaction was then silently dropped.
//
// This check is node-local and read-only (RPC layer only). It does not change
// CheckTx, DeliverTx, or any consensus state. It evaluates the window at the
// latest committed height, which is never stricter than the ante check, so it
// cannot reject a transaction the ante handler would accept. If the pqauth
// query fails for any reason it fails open and keeps the previous behaviour.

// pqAuthLookup answers the two pqauth questions the pre-check asks. An error
// means the answer is unknown; callers fail open.
type pqAuthLookup interface {
	Registered(ctx context.Context, account string) (bool, error)
	EVMWindow(ctx context.Context, account string) (open bool, expiry int64, err error)
}

// grpcPQAuthLookup queries the node's pqauth module over the client context.
type grpcPQAuthLookup struct {
	qc pqauthtypes.QueryClient
}

func (l grpcPQAuthLookup) Registered(ctx context.Context, account string) (bool, error) {
	res, err := l.qc.MLDSAPubKey(ctx, &pqauthtypes.QueryMLDSAPubKeyRequest{Account: account})
	if err != nil {
		return false, err
	}
	return res != nil && res.Registered, nil
}

func (l grpcPQAuthLookup) EVMWindow(ctx context.Context, account string) (bool, int64, error) {
	res, err := l.qc.EVMWindow(ctx, &pqauthtypes.QueryEVMWindowRequest{Account: account})
	if err != nil {
		return false, 0, err
	}
	if res == nil {
		return false, 0, nil
	}
	return res.Open, res.ExpiryHeight, nil
}

func (b *Backend) pqAuthLookup() pqAuthLookup {
	return grpcPQAuthLookup{qc: pqauthtypes.NewQueryClient(b.ClientCtx)}
}

// checkPQAuthEVMWindow rejects a registered sender whose window is closed.
func (b *Backend) checkPQAuthEVMWindow(ctx context.Context, sender sdk.AccAddress) error {
	return b.checkPQAuthEVMWindowWith(ctx, b.pqAuthLookup(), sender)
}

func (b *Backend) checkPQAuthEVMWindowWith(ctx context.Context, lookup pqAuthLookup, sender sdk.AccAddress) error {
	if sender.Empty() {
		return nil
	}
	account := sender.String()

	registered, err := lookup.Registered(ctx, account)
	if err != nil {
		b.Logger.Debug("pqauth key query failed; skipping EVM window pre-check", "account", account, "error", err.Error())
		return nil
	}
	if !registered {
		return nil
	}

	open, expiry, err := lookup.EVMWindow(ctx, account)
	if err != nil {
		b.Logger.Debug("pqauth window query failed; skipping EVM window pre-check", "account", account, "error", err.Error())
		return nil
	}
	if open {
		return nil
	}
	return fmt.Errorf(
		"pq-auth: EVM window closed for ML-DSA registered account %s (window expiry height %d); submit MsgOpenEVMWindow before sending an Ethereum transaction",
		account, expiry,
	)
}

// checkPQAuthSetCodeAuthorities applies the node-local pre-check to every
// recoverable EIP-7702 authority in a SetCode (type 0x04) transaction. The
// EVMWindowDecorator enforces the same rules in the ante handler; this only
// gives the client an immediate error instead of a silent mempool drop.
//
// The sender's balance is checked against the transaction cost first, so an
// unfunded sender cannot make the node recover signatures and run pqauth
// queries for a long authorisation list.
func (b *Backend) checkPQAuthSetCodeAuthorities(ctx context.Context, from common.Address, tx *ethtypes.Transaction) error {
	if tx == nil || len(tx.SetCodeAuthorizations()) == 0 {
		return nil
	}
	return b.checkPQAuthSetCodeAuthoritiesWith(ctx, b.pqAuthLookup(), from, tx, b.hardeningV2ActiveForNextBlock(ctx))
}

// hardeningV2ActiveForNextBlock reports whether the pqauth hardening v2 rules
// will apply to the next block. If the height is unknown it returns false, so
// the pre-check is never stricter than the ante handler.
func (b *Backend) hardeningV2ActiveForNextBlock(ctx context.Context) bool {
	if pqauthtypes.HardeningV2ActivationHeight() == pqauthtypes.HardeningV2Disabled {
		return false
	}
	if b.QueryClient == nil || b.QueryClient.QueryClient == nil {
		return false
	}
	latest, err := b.BlockNumber(ctx)
	if err != nil {
		return false
	}
	return pqauthtypes.HardeningV2Active(int64(latest) + 1) //#nosec G115 -- block height fits int64
}

// checkPQAuthSetCodeAuthoritiesWith runs the SetCode pre-check. hardened
// selects the hardening v2 target rule (registered authority may only clear).
func (b *Backend) checkPQAuthSetCodeAuthoritiesWith(ctx context.Context, lookup pqAuthLookup, from common.Address, tx *ethtypes.Transaction, hardened bool) error {
	if tx == nil || len(tx.SetCodeAuthorizations()) == 0 {
		return nil
	}
	// Cheap affordability check before any signature recovery or lookup.
	if err := b.checkSenderAffordsTx(ctx, from, tx); err != nil {
		return err
	}
	for _, auth := range tx.SetCodeAuthorizations() {
		authority, err := auth.Authority()
		if err != nil {
			continue
		}
		addr := sdk.AccAddress(authority.Bytes())
		account := addr.String()
		registered, err := lookup.Registered(ctx, account)
		if err != nil {
			b.Logger.Debug("pqauth key query failed; skipping EIP-7702 authority pre-check", "account", account, "error", err.Error())
			continue
		}
		if !registered {
			continue
		}
		if hardened && auth.Address != (common.Address{}) {
			return fmt.Errorf(
				"EIP-7702 authority: pq-auth: ML-DSA registered account %s may only sign a SetCode authorisation that clears delegation (target 0x0000000000000000000000000000000000000000), not delegate to %s",
				account, auth.Address.Hex(),
			)
		}
		if err := b.checkPQAuthEVMWindowWith(ctx, lookup, addr); err != nil {
			return fmt.Errorf("EIP-7702 authority: %w", err)
		}
	}
	return nil
}

// checkSenderAffordsTx rejects a transaction whose cost (gas limit times fee
// cap plus value) exceeds the sender's spendable balance at the latest height.
// It uses the same balance and cost as the ante handler's CheckSenderBalance,
// so it cannot reject a transaction the ante handler would accept at that
// height. A failed or malformed balance query fails open.
func (b *Backend) checkSenderAffordsTx(ctx context.Context, from common.Address, tx *ethtypes.Transaction) error {
	if b.QueryClient == nil || b.QueryClient.QueryClient == nil {
		return nil
	}
	res, err := b.QueryClient.Balance(ctx, &evmtypes.QueryBalanceRequest{Address: from.Hex()})
	if err != nil || res == nil {
		if err != nil {
			b.Logger.Debug("balance query failed; skipping EIP-7702 cost pre-check", "address", from.Hex(), "error", err.Error())
		}
		return nil
	}
	balance, ok := sdkmath.NewIntFromString(res.Balance)
	if !ok {
		return nil
	}
	return senderAffordsTx(balance.BigInt(), tx)
}

// senderAffordsTx reports an error when balance is below tx.Cost().
func senderAffordsTx(balance *big.Int, tx *ethtypes.Transaction) error {
	cost := tx.Cost()
	if balance == nil || balance.Sign() < 0 || balance.Cmp(cost) < 0 {
		return fmt.Errorf("insufficient funds for gas * price + value: balance %v, tx cost %v", balance, cost)
	}
	return nil
}
