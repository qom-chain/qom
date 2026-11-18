package backend

import (
	"context"
	"fmt"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ErrPQAuthEVMWindowClosed is returned by eth_sendRawTransaction when the
// sender has a registered ML-DSA-87 key and its EVM authorisation window is
// closed.
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
func (b *Backend) checkPQAuthEVMWindow(ctx context.Context, sender sdk.AccAddress) error {
	if sender.Empty() {
		return nil
	}
	qc := pqauthtypes.NewQueryClient(b.ClientCtx)
	account := sender.String()

	keyRes, err := qc.MLDSAPubKey(ctx, &pqauthtypes.QueryMLDSAPubKeyRequest{Account: account})
	if err != nil {
		b.Logger.Debug("pqauth key query failed; skipping EVM window pre-check", "account", account, "error", err.Error())
		return nil
	}
	if keyRes == nil || !keyRes.Registered {
		return nil
	}

	winRes, err := qc.EVMWindow(ctx, &pqauthtypes.QueryEVMWindowRequest{Account: account})
	if err != nil {
		b.Logger.Debug("pqauth window query failed; skipping EVM window pre-check", "account", account, "error", err.Error())
		return nil
	}
	if winRes != nil && winRes.Open {
		return nil
	}

	var expiry int64
	if winRes != nil {
		expiry = winRes.ExpiryHeight
	}
	return fmt.Errorf(
		"pq-auth: EVM window closed for ML-DSA registered account %s (window expiry height %d); submit MsgOpenEVMWindow before sending an Ethereum transaction",
		account, expiry,
	)
}
