package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/x/pqauth/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type countingEVM struct {
	memEVM
	calls *int
}

func (c countingEVM) GetCodeHash(ctx sdk.Context, addr common.Address) common.Hash {
	*c.calls++
	return c.memEVM.GetCodeHash(ctx, addr)
}

// Below the activation height registration is the 32ee354 path: no code
// check, no ERC-20 allowance wipe, the full authz scan, and no dependency on
// the EVM keepers. From the activation height the new rules apply.
func TestRegisterMLDSAKeyActivationHeight(t *testing.T) {
	const activation = 100
	restore := types.SetHardeningV2ActivationHeightForTesting(activation)
	defer restore()

	// Below activation, with code and allowances, and even with no EVM keepers.
	f := newRegFixture(t)
	ctx, k := setupKeeper(t, f.account)
	ctx = ctx.WithBlockHeight(activation - 1)
	owner := common.BytesToAddress(f.addr)
	evmCalls := 0
	erc20 := &memERC20{allowances: map[common.Address]int{owner: 2}}
	grants := &memAuthz{}
	k.SetDelegationKeepers(grants, nopFees{}, nil)
	k.SetEVMKeepers(countingEVM{memEVM{codeHashes: map[common.Address]common.Hash{owner: delegationCodeHash}}, &evmCalls}, erc20)
	_, err := k.RegisterMLDSAKey(ctx, f.msg())
	require.NoError(t, err)
	require.Zero(t, evmCalls)
	require.Empty(t, erc20.deleted)
	require.Equal(t, 2, erc20.allowances[owner])
	require.Equal(t, 1, grants.fullScans)
	require.Zero(t, grants.granterQueries)

	f2 := newRegFixture(t)
	ctx2, k2 := setupKeeper(t, f2.account)
	k2.SetEVMKeepers(nil, nil)
	_, err = k2.RegisterMLDSAKey(ctx2.WithBlockHeight(activation-1), f2.msg())
	require.NoError(t, err)

	// At activation: code refused; after clearing, allowances wiped and the
	// bounded authz lookup used.
	f3 := newRegFixture(t)
	ctx3, k3 := setupKeeper(t, f3.account)
	ctx3 = ctx3.WithBlockHeight(activation)
	owner3 := common.BytesToAddress(f3.addr)
	evm3 := memEVM{codeHashes: map[common.Address]common.Hash{owner3: delegationCodeHash}}
	erc203 := &memERC20{allowances: map[common.Address]int{owner3: 2}}
	grants3 := &memAuthz{}
	k3.SetDelegationKeepers(grants3, nopFees{}, nil)
	k3.SetEVMKeepers(evm3, erc203)
	_, err = k3.RegisterMLDSAKey(ctx3, f3.msg())
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
	delete(evm3.codeHashes, owner3)
	_, err = k3.RegisterMLDSAKey(ctx3, f3.msg())
	require.NoError(t, err)
	require.Equal(t, []common.Address{owner3}, erc203.deleted)
	require.Zero(t, grants3.fullScans)
	require.Equal(t, 1, grants3.granterQueries)

	// At activation without EVM keepers: fail closed.
	f4 := newRegFixture(t)
	ctx4, k4 := setupKeeper(t, f4.account)
	k4.SetEVMKeepers(nil, nil)
	_, err = k4.RegisterMLDSAKey(ctx4.WithBlockHeight(activation), f4.msg())
	require.ErrorIs(t, err, sdkerrors.ErrLogic)
}
