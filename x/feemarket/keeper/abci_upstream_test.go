package keeper_test

import (
	gomath "math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/x/feemarket/keeper"
	"github.com/cosmos/evm/x/feemarket/types"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	sdkmath "cosmossdk.io/math"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
)

// From the activation height EndBlock clamps block gas values above MaxInt64
// instead of returning an error; below it the v1.0.1 error is kept.
func TestEndBlockGasClamp(t *testing.T) {
	const height = 12
	key := storetypes.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("t_feemarket")).
		WithBlockHeight(height).
		WithBlockGasMeter(storetypes.NewInfiniteGasMeter())
	encCfg := moduletestutil.MakeTestEncodingConfig()
	k := keeper.NewKeeper(encCfg.Codec, sdk.AccAddress("authority___________"), key)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	big := uint64(gomath.MaxInt64) + 1
	cases := []struct {
		name         string
		wanted, used uint64
	}{
		{"gas wanted above MaxInt64", big, 100},
		{"gas used above MaxInt64", 100, big},
		{"both above MaxInt64", big, big},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ctx.WithBlockGasWanted(tc.wanted).WithBlockGasUsed(tc.used)

			restore := pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height + 1)
			k.SetBlockGasWanted(c, 7)
			require.ErrorContains(t, k.EndBlock(c), "integer overflow")
			require.Equal(t, uint64(7), k.GetBlockGasWanted(c), "state unchanged on error")
			restore()

			restore = pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height)
			require.NoError(t, k.EndBlock(c))
			// gasWanted = max(clamp(wanted) * MinGasMultiplier, clamp(used))
			clamp := func(v uint64) int64 {
				if v > gomath.MaxInt64 {
					return gomath.MaxInt64
				}
				return int64(v)
			}
			exp := sdkmath.LegacyMaxDec(
				sdkmath.LegacyNewDec(clamp(tc.wanted)).Mul(types.DefaultParams().MinGasMultiplier),
				sdkmath.LegacyNewDec(clamp(tc.used)),
			).TruncateInt().Uint64()
			require.Equal(t, exp, k.GetBlockGasWanted(c))
			restore()
		})
	}

	t.Run("ordinary values are the same before and after", func(t *testing.T) {
		c := ctx.WithBlockGasWanted(1_000_000).WithBlockGasUsed(300_000)
		restore := pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height + 1)
		require.NoError(t, k.EndBlock(c))
		pre := k.GetBlockGasWanted(c)
		restore()
		restore = pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height)
		require.NoError(t, k.EndBlock(c))
		require.Equal(t, pre, k.GetBlockGasWanted(c))
		restore()
	})
}
