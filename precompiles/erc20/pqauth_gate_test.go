package erc20

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	cmn "github.com/cosmos/evm/precompiles/common"
	erc20types "github.com/cosmos/evm/x/erc20/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var errWindowClosed = errors.New("owner window closed")

type allowanceKey struct{ owner, spender common.Address }

// gateKeeper is an in-memory Erc20Keeper whose CheckOwnerEVMWindow rejects
// the owners in closed.
type gateKeeper struct {
	allowances map[allowanceKey]*big.Int
	closed     map[common.Address]bool
	checked    []common.Address
	writes     int
}

func (g *gateKeeper) GetAllowance(_ sdk.Context, _ common.Address, owner, spender common.Address) (*big.Int, error) {
	if v, ok := g.allowances[allowanceKey{owner, spender}]; ok {
		return new(big.Int).Set(v), nil
	}
	return big.NewInt(0), nil
}

func (g *gateKeeper) SetAllowance(_ sdk.Context, _ common.Address, owner, spender common.Address, value *big.Int) error {
	g.writes++
	g.allowances[allowanceKey{owner, spender}] = new(big.Int).Set(value)
	return nil
}

func (g *gateKeeper) DeleteAllowance(_ sdk.Context, _ common.Address, owner, spender common.Address) error {
	g.writes++
	delete(g.allowances, allowanceKey{owner, spender})
	return nil
}

func (g *gateKeeper) CheckOwnerEVMWindow(_ sdk.Context, owner common.Address) error {
	g.checked = append(g.checked, owner)
	if g.closed[owner] {
		return errWindowClosed
	}
	return nil
}

// logDB records logs; any other StateDB method panics (nil embedded).
type logDB struct {
	vm.StateDB
	logs []*ethtypes.Log
}

func (l *logDB) AddLog(log *ethtypes.Log) { l.logs = append(l.logs, log) }

// stubBank is not a bank BaseKeeper, so MsgServer.Send returns
// "invalid keeper type" after the allowance logic has run.
type stubBank struct{ cmn.BankKeeper }

func gateFixture(t *testing.T) (sdk.Context, *Precompile, *gateKeeper) {
	t.Helper()
	key := storetypes.NewKVStoreKey("erc20gate")
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("erc20gate_t"))
	pair := erc20types.NewTokenPair(common.HexToAddress("0xD4949664cD82660AaE99bEdc034a0deA8A0bd517"), "aqomx", erc20types.OWNER_MODULE)
	gk := &gateKeeper{allowances: map[allowanceKey]*big.Int{}, closed: map[common.Address]bool{}}
	return ctx, NewPrecompile(pair, stubBank{}, gk, nil), gk
}

func a(b byte) common.Address { return common.BytesToAddress(bytes.Repeat([]byte{b}, 20)) }

func contractFrom(caller, precompile common.Address) *vm.Contract {
	return vm.NewContract(caller, precompile, uint256.NewInt(0), 1_000_000, nil)
}

func TestApproveRequiresOwnerWindow(t *testing.T) {
	ctx, p, gk := gateFixture(t)
	owner, spender := a(0x01), a(0x02)
	method := p.Methods[ApproveMethod]

	// Owner window closed: rejected, nothing written, no event.
	gk.closed[owner] = true
	db := &logDB{}
	_, err := p.Approve(ctx, contractFrom(owner, p.Address()), db, &method, []interface{}{spender, big.NewInt(5)})
	require.ErrorIs(t, err, errWindowClosed)
	require.Zero(t, gk.writes)
	require.Empty(t, db.logs)
	require.Equal(t, []common.Address{owner}, gk.checked)

	// Revoking (amount 0) is also gated: the owner acts only with an open window.
	_, err = p.Approve(ctx, contractFrom(owner, p.Address()), db, &method, []interface{}{spender, big.NewInt(0)})
	require.ErrorIs(t, err, errWindowClosed)

	// Owner window open (or owner unregistered): approve works as before.
	gk.closed[owner] = false
	_, err = p.Approve(ctx, contractFrom(owner, p.Address()), db, &method, []interface{}{spender, big.NewInt(5)})
	require.NoError(t, err)
	require.Equal(t, int64(5), gk.allowances[allowanceKey{owner, spender}].Int64())
	require.Len(t, db.logs, 1)
}

func TestTransferFromRequiresOwnerWindow(t *testing.T) {
	ctx, p, gk := gateFixture(t)
	owner, spender, to := a(0x11), a(0x12), a(0x13)
	method := p.Methods[TransferFromMethod]
	gk.allowances[allowanceKey{owner, spender}] = big.NewInt(100)

	// Owner window closed: rejected before the allowance is read or spent.
	gk.closed[owner] = true
	_, err := p.TransferFrom(ctx, contractFrom(spender, p.Address()), &logDB{}, &method, []interface{}{owner, to, big.NewInt(10)})
	require.ErrorIs(t, err, errWindowClosed)
	require.Zero(t, gk.writes)
	require.Equal(t, int64(100), gk.allowances[allowanceKey{owner, spender}].Int64())
	// The gate checks the owner (from), not the caller (spender).
	require.Equal(t, []common.Address{owner}, gk.checked)

	// Window open: the gate passes and the normal allowance logic runs. The
	// stub bank then fails the send, which shows the gate was not the error.
	gk.closed[owner] = false
	_, err = p.TransferFrom(ctx, contractFrom(spender, p.Address()), &logDB{}, &method, []interface{}{owner, to, big.NewInt(10)})
	require.Error(t, err)
	require.NotErrorIs(t, err, errWindowClosed)
	require.Contains(t, err.Error(), "invalid keeper type")
	require.Equal(t, 1, gk.writes)

	// Window open, allowance too small: the usual ERC-20 error.
	_, err = p.TransferFrom(ctx, contractFrom(spender, p.Address()), &logDB{}, &method, []interface{}{owner, to, big.NewInt(1_000)})
	require.ErrorIs(t, err, ErrInsufficientAllowance)
}

func TestTransferIsNotGated(t *testing.T) {
	ctx, p, gk := gateFixture(t)
	from, to := a(0x21), a(0x22)
	method := p.Methods[TransferMethod]
	gk.closed[from] = true
	// transfer moves the caller's own balance; the caller is the tx sender
	// (gated in the ante handler) or a contract. It does not consult the gate.
	_, err := p.Transfer(ctx, contractFrom(from, p.Address()), &logDB{}, &method, []interface{}{to, big.NewInt(1)})
	require.NotErrorIs(t, err, errWindowClosed)
	require.Empty(t, gk.checked)
}
