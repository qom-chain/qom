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

func TestValidatorKeyGuardDefaultIsMainnetHeight(t *testing.T) {
	// The source default (no -X override) must be the approved mainnet height,
	// so a plain build passes the mainnet start guard.
	require.Equal(t, MainnetValidatorKeyGuardActivationHeight, mustParseActivationHeight("34000"))
	require.Equal(t, "34000", validatorKeyGuardActivationHeightValue)
	require.Equal(t, int64(34000), MainnetValidatorKeyGuardActivationHeight)
	require.False(t, ValidatorKeyGuardActive(33999))
	require.True(t, ValidatorKeyGuardActive(34000))
	require.Equal(t, MainnetValidatorKeyGuardActivationHeight, mustParseActivationHeight(validatorKeyGuardActivationHeightValue))
	require.Equal(t, MainnetValidatorKeyGuardActivationHeight, ValidatorKeyGuardActivationHeight())
	require.NoError(t, CheckValidatorKeyGuardActivationForChain(MainnetChainID))
	h := MainnetValidatorKeyGuardActivationHeight
	require.False(t, ValidatorKeyGuardActive(h-1))
	require.True(t, ValidatorKeyGuardActive(h))
	// Independent of the hardening v2 height.
	require.NotEqual(t, MainnetHardeningV2ActivationHeight, MainnetValidatorKeyGuardActivationHeight)
}

func TestCheckValidatorKeyGuardActivationForChain(t *testing.T) {
	restore := SetValidatorKeyGuardActivationHeightForTesting(20)
	defer restore()
	require.Error(t, CheckValidatorKeyGuardActivationForChain(MainnetChainID))
	require.NoError(t, CheckValidatorKeyGuardActivationForChain("qom_9997-1"))
	require.True(t, ValidatorKeyGuardActive(20))
	require.False(t, ValidatorKeyGuardActive(19))
	SetValidatorKeyGuardActivationHeightForTesting(HardeningV2Disabled)
	require.Error(t, CheckValidatorKeyGuardActivationForChain(MainnetChainID))
	require.False(t, ValidatorKeyGuardActive(1<<62))
	// The hardening v2 guard is not affected by this override.
	require.NoError(t, CheckHardeningV2ActivationForChain(MainnetChainID))
}

func TestUpstreamFixesDefaultIsMainnetHeight(t *testing.T) {
	require.Equal(t, MainnetUpstreamFixesActivationHeight, mustParseActivationHeight(upstreamFixesActivationHeightValue))
	require.Equal(t, MainnetUpstreamFixesActivationHeight, UpstreamFixesActivationHeight())
	require.NoError(t, CheckUpstreamFixesActivationForChain(MainnetChainID))
	h := MainnetUpstreamFixesActivationHeight
	require.False(t, UpstreamFixesActive(h-1))
	require.True(t, UpstreamFixesActive(h))
	// Later than the earlier activations, which are already live.
	require.Greater(t, h, MainnetValidatorKeyGuardActivationHeight)
	require.Greater(t, h, MainnetHardeningV2ActivationHeight)
}

func TestCheckUpstreamFixesActivationForChain(t *testing.T) {
	restore := SetUpstreamFixesActivationHeightForTesting(40)
	defer restore()
	require.Error(t, CheckUpstreamFixesActivationForChain(MainnetChainID))
	require.NoError(t, CheckUpstreamFixesActivationForChain("qom_9997-1"))
	require.True(t, UpstreamFixesActive(40))
	require.False(t, UpstreamFixesActive(39))
	SetUpstreamFixesActivationHeightForTesting(HardeningV2Disabled)
	require.Error(t, CheckUpstreamFixesActivationForChain(MainnetChainID))
	require.False(t, UpstreamFixesActive(1<<62))
	// The other guards are not affected by this override.
	require.NoError(t, CheckHardeningV2ActivationForChain(MainnetChainID))
	require.NoError(t, CheckValidatorKeyGuardActivationForChain(MainnetChainID))
}
