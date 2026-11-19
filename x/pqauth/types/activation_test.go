package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHardeningV2DefaultIsMainnetHeight(t *testing.T) {
	// The source default (no -X override) must be the approved mainnet height.
	require.Equal(t, MainnetHardeningV2ActivationHeight, mustParseActivationHeight("33000"))
	require.Equal(t, "33000", hardeningV2ActivationHeightValue)
	require.Equal(t, int64(33000), MainnetHardeningV2ActivationHeight)
	require.False(t, HardeningV2Active(32999))
	require.True(t, HardeningV2Active(33000))
	require.NoError(t, CheckHardeningV2ActivationForChain(MainnetChainID))
}

func TestCheckHardeningV2ActivationForChain(t *testing.T) {
	restore := SetHardeningV2ActivationHeightForTesting(20)
	defer restore()
	require.Error(t, CheckHardeningV2ActivationForChain(MainnetChainID))
	require.NoError(t, CheckHardeningV2ActivationForChain("qom_9997-1"))
	SetHardeningV2ActivationHeightForTesting(HardeningV2Disabled)
	require.Error(t, CheckHardeningV2ActivationForChain(MainnetChainID))
}

func TestMustParseActivationHeight(t *testing.T) {
	require.Equal(t, HardeningV2Disabled, mustParseActivationHeight("disabled"))
	require.Equal(t, HardeningV2Disabled, mustParseActivationHeight(""))
	require.Equal(t, int64(7), mustParseActivationHeight(" 7 "))
	require.Panics(t, func() { mustParseActivationHeight("-1") })
	require.Panics(t, func() { mustParseActivationHeight("abc") })
}
