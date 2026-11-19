package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

func TestCheckOwnerEVMWindowActivationHeight(t *testing.T) {
	restore := pqauthtypes.SetHardeningV2ActivationHeightForTesting(100)
	defer restore()

	ctx, k := newAllowanceTestKeeper(t)
	owner := addr(0x01)
	k.SetPQAuthKeeper(fakePQAuth{registered: map[string]bool{sdk.AccAddress(owner.Bytes()).String(): true}})

	// Below activation: 32ee354 behaviour, no gate.
	require.NoError(t, k.CheckOwnerEVMWindow(ctx.WithBlockHeight(99), owner))
	// At activation: closed window rejected.
	require.ErrorIs(t, k.CheckOwnerEVMWindow(ctx.WithBlockHeight(100), owner), errortypes.ErrUnauthorized)
}
