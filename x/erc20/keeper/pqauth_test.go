package keeper

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/x/erc20/types"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

type fakePQAuth struct {
	registered map[string]bool
	open       map[string]bool
}

func (f fakePQAuth) GetMLDSAPubKey(_ sdk.Context, addr sdk.AccAddress) ([]byte, bool) {
	if f.registered[addr.String()] {
		return []byte{1}, true
	}
	return nil, false
}

func (f fakePQAuth) IsEVMWindowOpen(_ sdk.Context, addr sdk.AccAddress) bool {
	return f.open[addr.String()]
}

func newAllowanceTestKeeper(t *testing.T) (sdk.Context, Keeper) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient_erc20_pq"))
	k := Keeper{storeKey: key, cdc: codec.NewProtoCodec(codectypes.NewInterfaceRegistry())}
	return ctx, k
}

func addPair(ctx sdk.Context, k Keeper, erc20 common.Address, denom string) types.TokenPair {
	pair := types.NewTokenPair(erc20, denom, types.OWNER_MODULE)
	k.SetTokenPair(ctx, pair)
	k.SetERC20Map(ctx, erc20, pair.GetID())
	k.SetDenomMap(ctx, denom, pair.GetID())
	return pair
}

func addr(b byte) common.Address { return common.BytesToAddress(bytes.Repeat([]byte{b}, 20)) }

func TestDeleteOwnerAllowances(t *testing.T) {
	ctx, k := newAllowanceTestKeeper(t)
	tokenA := addr(0xa1)
	tokenB := addr(0xb1)
	tokenUnregistered := addr(0xc1)
	addPair(ctx, k, tokenA, "aqomx")
	pairB := addPair(ctx, k, tokenB, "ibc/TEST")

	owner := addr(0x10)
	// Shares 19 of 20 bytes with owner; must not be affected.
	neighbour := common.BytesToAddress(append(bytes.Repeat([]byte{0x10}, 19), 0x11))
	other := addr(0x20)
	s1, s2 := addr(0x31), addr(0x32)
	one := big.NewInt(1)

	require.NoError(t, k.SetAllowance(ctx, tokenA, owner, s1, one))
	require.NoError(t, k.SetAllowance(ctx, tokenA, owner, s2, big.NewInt(7)))
	require.NoError(t, k.SetAllowance(ctx, tokenB, owner, s1, one))
	require.NoError(t, k.SetAllowance(ctx, tokenA, neighbour, s1, one))
	require.NoError(t, k.SetAllowance(ctx, tokenA, other, owner, one)) // owner is only the spender here
	require.NoError(t, k.SetAllowance(ctx, tokenB, other, s2, one))

	// Disable pair B after the allowance exists: still deleted.
	pairB.Enabled = false
	k.SetTokenPair(ctx, pairB)

	// An allowance-shaped key under an address with no token pair is out of
	// scope (no pair can reach it) and is left alone.
	strayStore := ctx.KVStore(k.storeKey)
	strayKey := append(append([]byte{}, types.KeyPrefixAllowance...), types.AllowanceKey(tokenUnregistered, owner, s1)...)
	strayStore.Set(strayKey, []byte{0x01})

	n, err := k.DeleteOwnerAllowances(ctx, owner)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	for _, c := range []struct {
		erc20, owner, spender common.Address
		want                  int64
	}{
		{tokenA, owner, s1, 0},
		{tokenA, owner, s2, 0},
		{tokenB, owner, s1, 0},
		{tokenA, neighbour, s1, 1},
		{tokenA, other, owner, 1},
		{tokenB, other, s2, 1},
	} {
		got, err := k.GetAllowance(ctx, c.erc20, c.owner, c.spender)
		require.NoError(t, err)
		require.Equal(t, c.want, got.Int64(), "allowance %s/%s/%s", c.erc20, c.owner, c.spender)
	}
	require.True(t, strayStore.Has(strayKey))

	// Idempotent.
	n, err = k.DeleteOwnerAllowances(ctx, owner)
	require.NoError(t, err)
	require.Zero(t, n)

	_, err = k.DeleteOwnerAllowances(ctx, common.Address{})
	require.ErrorIs(t, err, errortypes.ErrInvalidAddress)
}

func TestCheckOwnerEVMWindow(t *testing.T) {
	ctx, k := newAllowanceTestKeeper(t)
	registeredClosed := addr(0x01)
	registeredOpen := addr(0x02)
	unregistered := addr(0x03)

	// No pqauth keeper attached: gate disabled.
	require.NoError(t, k.CheckOwnerEVMWindow(ctx, registeredClosed))

	k.SetPQAuthKeeper(fakePQAuth{
		registered: map[string]bool{
			sdk.AccAddress(registeredClosed.Bytes()).String(): true,
			sdk.AccAddress(registeredOpen.Bytes()).String():   true,
		},
		open: map[string]bool{sdk.AccAddress(registeredOpen.Bytes()).String(): true},
	})
	err := k.CheckOwnerEVMWindow(ctx, registeredClosed)
	require.ErrorIs(t, err, errortypes.ErrUnauthorized)
	require.Contains(t, err.Error(), "window is closed for ERC-20 owner")
	require.NoError(t, k.CheckOwnerEVMWindow(ctx, registeredOpen))
	require.NoError(t, k.CheckOwnerEVMWindow(ctx, unregistered))
}
