package backend

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/rpc/backend/mocks"
	rpctypes "github.com/cosmos/evm/rpc/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// fakePQLookup is an in-memory pqAuthLookup that counts calls.
type fakePQLookup struct {
	registered map[string]bool
	open       map[string]bool
	err        error
	calls      int
}

func (f *fakePQLookup) Registered(_ context.Context, account string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.registered[account], nil
}

func (f *fakePQLookup) EVMWindow(_ context.Context, account string) (bool, int64, error) {
	f.calls++
	if f.err != nil {
		return false, 0, f.err
	}
	return f.open[account], 42, nil
}

func pqTestBackend(t *testing.T, balance string, balanceErr error) *Backend {
	t.Helper()
	qc := mocks.NewEVMQueryClient(t)
	if balance != "" || balanceErr != nil {
		var res *evmtypes.QueryBalanceResponse
		if balanceErr == nil {
			res = &evmtypes.QueryBalanceResponse{Balance: balance}
		}
		qc.On("Balance", mock.Anything, mock.Anything).Return(res, balanceErr).Maybe()
	}
	return &Backend{
		Logger:      log.NewNopLogger(),
		QueryClient: &rpctypes.QueryClient{QueryClient: qc},
	}
}

func signedSetCode(t *testing.T, key *ecdsa.PrivateKey, target common.Address) ethtypes.SetCodeAuthorization {
	t.Helper()
	auth, err := ethtypes.SignSetCode(key, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.NewInt(9000),
		Address: target,
	})
	require.NoError(t, err)
	return auth
}

func setCodeTx(auths ...ethtypes.SetCodeAuthorization) *ethtypes.Transaction {
	return ethtypes.NewTx(&ethtypes.SetCodeTx{
		ChainID:   uint256.NewInt(9000),
		Gas:       100_000,
		GasFeeCap: uint256.NewInt(10),
		GasTipCap: uint256.NewInt(1),
		To:        common.HexToAddress("0x00000000000000000000000000000000000000aa"),
		Value:     uint256.NewInt(5),
		AuthList:  auths,
	})
}

func TestPQAuthSetCodePreCheckTargets(t *testing.T) {
	regKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	unregKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	regAcc := sdk.AccAddress(crypto.PubkeyToAddress(regKey.PublicKey).Bytes()).String()
	target := common.HexToAddress("0x0000000000000000000000000000000000001234")
	from := common.HexToAddress("0x00000000000000000000000000000000000000f0")

	b := pqTestBackend(t, "1000000000000000000", nil)
	ctx := context.Background()

	// Registered authority, non-zero target, window open: rejected.
	lookup := &fakePQLookup{registered: map[string]bool{regAcc: true}, open: map[string]bool{regAcc: true}}
	err = b.checkPQAuthSetCodeAuthoritiesWith(ctx, lookup, from, setCodeTx(signedSetCode(t, regKey, target)), true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "may only sign a SetCode authorisation that clears delegation")

	// Registered authority, clearing target, window open: allowed.
	err = b.checkPQAuthSetCodeAuthoritiesWith(ctx, lookup, from, setCodeTx(signedSetCode(t, regKey, common.Address{})), true)
	require.NoError(t, err)

	// Registered authority, clearing target, window closed: rejected.
	closed := &fakePQLookup{registered: map[string]bool{regAcc: true}}
	err = b.checkPQAuthSetCodeAuthoritiesWith(ctx, closed, from, setCodeTx(signedSetCode(t, regKey, common.Address{})), true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "EVM window closed")

	// Unregistered authority, any target: allowed.
	err = b.checkPQAuthSetCodeAuthoritiesWith(ctx, lookup, from, setCodeTx(signedSetCode(t, unregKey, target)), true)
	require.NoError(t, err)

	// Below activation the target rule is off: same as 32ee354, a registered
	// authority with an open window may delegate.
	err = b.checkPQAuthSetCodeAuthoritiesWith(ctx, lookup, from, setCodeTx(signedSetCode(t, regKey, target)), false)
	require.NoError(t, err)
	// ...but a closed window is still rejected below activation.
	err = b.checkPQAuthSetCodeAuthoritiesWith(ctx, closed, from, setCodeTx(signedSetCode(t, regKey, target)), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "EVM window closed")

	// Lookup failure fails open (ante still enforces).
	failing := &fakePQLookup{err: errors.New("grpc down")}
	err = b.checkPQAuthSetCodeAuthoritiesWith(ctx, failing, from, setCodeTx(signedSetCode(t, regKey, target)), true)
	require.NoError(t, err)

	// Non-SetCode tx: no work at all.
	none := &fakePQLookup{}
	plain := ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(9000), Gas: 21000, GasFeeCap: big.NewInt(1), GasTipCap: big.NewInt(1)})
	require.NoError(t, b.checkPQAuthSetCodeAuthoritiesWith(ctx, none, from, plain, true))
	require.Zero(t, none.calls)
}

func TestPQAuthSetCodePreCheckBalanceFirst(t *testing.T) {
	regKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := common.HexToAddress("0x00000000000000000000000000000000000000f0")
	tx := setCodeTx(signedSetCode(t, regKey, common.Address{}), signedSetCode(t, regKey, common.Address{}))
	cost := tx.Cost() // 100_000*10 + 5
	require.Equal(t, big.NewInt(1_000_005), cost)
	ctx := context.Background()

	// Balance below cost: rejected before any pqauth lookup.
	b := pqTestBackend(t, "1000004", nil)
	lookup := &fakePQLookup{}
	err = b.checkPQAuthSetCodeAuthoritiesWith(ctx, lookup, from, tx, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient funds")
	require.Zero(t, lookup.calls)

	// Balance exactly the cost: proceeds to the authority checks.
	b = pqTestBackend(t, "1000005", nil)
	lookup = &fakePQLookup{}
	require.NoError(t, b.checkPQAuthSetCodeAuthoritiesWith(ctx, lookup, from, tx, true))
	require.Equal(t, 2, lookup.calls) // one Registered per authority, both unregistered

	// Balance query error: fail open and proceed.
	b = pqTestBackend(t, "", errors.New("query failed"))
	lookup = &fakePQLookup{}
	require.NoError(t, b.checkPQAuthSetCodeAuthoritiesWith(ctx, lookup, from, tx, true))
	require.Equal(t, 2, lookup.calls)

	// Malformed balance: fail open.
	b = pqTestBackend(t, "not-a-number", nil)
	lookup = &fakePQLookup{}
	require.NoError(t, b.checkPQAuthSetCodeAuthoritiesWith(ctx, lookup, from, tx, true))

	// No query client: fail open.
	b = &Backend{Logger: log.NewNopLogger()}
	lookup = &fakePQLookup{}
	require.NoError(t, b.checkPQAuthSetCodeAuthoritiesWith(ctx, lookup, from, tx, true))
}

func TestPQAuthSenderWindowPreCheck(t *testing.T) {
	b := &Backend{Logger: log.NewNopLogger()}
	ctx := context.Background()
	sender := sdk.AccAddress(common.HexToAddress("0x00000000000000000000000000000000000000f1").Bytes())

	require.NoError(t, b.checkPQAuthEVMWindowWith(ctx, &fakePQLookup{}, sender))
	err := b.checkPQAuthEVMWindowWith(ctx, &fakePQLookup{registered: map[string]bool{sender.String(): true}}, sender)
	require.Error(t, err)
	require.Contains(t, err.Error(), "window expiry height 42")
	require.NoError(t, b.checkPQAuthEVMWindowWith(ctx, &fakePQLookup{
		registered: map[string]bool{sender.String(): true},
		open:       map[string]bool{sender.String(): true},
	}, sender))
	require.NoError(t, b.checkPQAuthEVMWindowWith(ctx, &fakePQLookup{err: errors.New("x")}, sender))
	require.NoError(t, b.checkPQAuthEVMWindowWith(ctx, &fakePQLookup{}, nil))
}

func TestSenderAffordsTx(t *testing.T) {
	tx := setCodeTx()
	require.Error(t, senderAffordsTx(nil, tx))
	require.Error(t, senderAffordsTx(big.NewInt(-1), tx))
	require.Error(t, senderAffordsTx(new(big.Int).Sub(tx.Cost(), common.Big1), tx))
	require.NoError(t, senderAffordsTx(tx.Cost(), tx))
}
