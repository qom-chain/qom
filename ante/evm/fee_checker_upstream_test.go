package evm_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/evm/ante/evm"
	"github.com/cosmos/evm/ante/types"
	"github.com/cosmos/evm/encoding"
	testconstants "github.com/cosmos/evm/testutil/constants"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
)

// From the activation height a tip cap near LegacyDec's upper limit is clamped
// to the fee cap; below it the v1.0.1 arithmetic (which overflows) is kept.
func TestDynamicFeeCheckerNearMaxTip(t *testing.T) {
	chainID := uint64(testconstants.EighteenDecimalsChainID)
	encodingConfig := encoding.MakeConfig(chainID) //nolint:staticcheck // this is used

	configurator := evmtypes.NewEVMConfigurator()
	configurator.ResetTestConfig()
	require.NoError(t, configurator.
		WithExtendedEips(evmtypes.DefaultCosmosEVMActivators).
		WithEVMCoinInfo(testconstants.ChainsCoinInfo[chainID]).
		Configure())
	require.NoError(t, evmtypes.SetChainConfig(evmtypes.DefaultChainConfig(chainID)))

	// tip 1 below LegacyDec's upper limit: in range at base fee 1,
	// overflows LegacyDec at base fee 2.
	nearMaxTip := math.LegacyMustNewDecFromStr(new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256),
		big.NewInt(1),
	).String() + ".999999999999999999").Sub(math.LegacyNewDec(1))

	buildTx := func() sdk.FeeTx {
		txBuilder := encodingConfig.TxConfig.NewTxBuilder().(authtx.ExtensionOptionsTxBuilder)
		txBuilder.SetGasLimit(1)
		txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(testconstants.ExampleAttoDenom, math.NewInt(10))))

		option, err := codectypes.NewAnyWithValue(&types.ExtensionOptionDynamicFeeTx{
			MaxPriorityPrice: nearMaxTip,
		})
		require.NoError(t, err)
		txBuilder.SetExtensionOptions(option)
		return txBuilder.GetTx()
	}

	const height = 7
	ctx := sdk.NewContext(nil, tmproto.Header{Height: height}, false, log.NewNopLogger())
	acceptParams := feemarkettypes.Params{BaseFee: math.LegacyNewDec(1)}
	recheckParams := feemarkettypes.Params{BaseFee: math.LegacyNewDec(2)}

	t.Run("post-activation", func(t *testing.T) {
		defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height)()
		fees, _, err := evm.NewDynamicFeeChecker(&acceptParams)(ctx, buildTx())
		require.NoError(t, err)
		require.Equal(t, "10aatom", fees.String())

		fees, _, err = evm.NewDynamicFeeChecker(&recheckParams)(ctx, buildTx())
		require.NoError(t, err)
		require.Equal(t, "10aatom", fees.String())
	})

	t.Run("pre-activation keeps v1.0.1 arithmetic", func(t *testing.T) {
		defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height + 1)()
		fees, _, err := evm.NewDynamicFeeChecker(&acceptParams)(ctx, buildTx())
		require.NoError(t, err)
		require.Equal(t, "10aatom", fees.String())

		require.Panics(t, func() {
			_, _, _ = evm.NewDynamicFeeChecker(&recheckParams)(ctx, buildTx())
		})
	})
}

// For ordinary values the fee and priority are the same before and after the
// activation height.
func TestDynamicFeeCheckerSameResultAcrossActivation(t *testing.T) {
	chainID := uint64(testconstants.EighteenDecimalsChainID)
	encodingConfig := encoding.MakeConfig(chainID) //nolint:staticcheck // this is used

	configurator := evmtypes.NewEVMConfigurator()
	configurator.ResetTestConfig()
	require.NoError(t, configurator.
		WithExtendedEips(evmtypes.DefaultCosmosEVMActivators).
		WithEVMCoinInfo(testconstants.ChainsCoinInfo[chainID]).
		Configure())
	require.NoError(t, evmtypes.SetChainConfig(evmtypes.DefaultChainConfig(chainID)))

	const height = 7
	ctx := sdk.NewContext(nil, tmproto.Header{Height: height}, false, log.NewNopLogger())

	for _, tip := range []int64{-1, 0, 1, 5, 1_000_000_000, 10_000_000_000_000} {
		for _, fee := range []int64{10_000_000_000, 25_000_000_000_000, 1_000_000_000_000_000_000} {
			for _, base := range []int64{0, 1_000_000_000, 10_000_000_000} {
				txBuilder := encodingConfig.TxConfig.NewTxBuilder().(authtx.ExtensionOptionsTxBuilder)
				txBuilder.SetGasLimit(21000)
				txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(testconstants.ExampleAttoDenom, math.NewInt(fee))))
				if tip >= 0 {
					option, err := codectypes.NewAnyWithValue(&types.ExtensionOptionDynamicFeeTx{MaxPriorityPrice: math.LegacyNewDec(tip)})
					require.NoError(t, err)
					txBuilder.SetExtensionOptions(option)
				}
				params := feemarkettypes.Params{BaseFee: math.LegacyNewDec(base)}

				restore := pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height + 1)
				feesPre, prioPre, errPre := evm.NewDynamicFeeChecker(&params)(ctx, txBuilder.GetTx())
				restore()
				restore = pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height)
				feesPost, prioPost, errPost := evm.NewDynamicFeeChecker(&params)(ctx, txBuilder.GetTx())
				restore()

				require.Equal(t, errPre == nil, errPost == nil, "tip=%d fee=%d base=%d", tip, fee, base)
				require.Equal(t, feesPre.String(), feesPost.String(), "tip=%d fee=%d base=%d", tip, fee, base)
				require.Equal(t, prioPre, prioPost, "tip=%d fee=%d base=%d", tip, fee, base)
			}
		}
	}
}
