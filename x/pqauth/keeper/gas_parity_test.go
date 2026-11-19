package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/x/pqauth/keeper"
	"github.com/cosmos/evm/x/pqauth/types"
)

// baselineRegistrationGas32ee354 is the gas registrationGasScenario consumes
// on commit 32ee354 (measured by running the same scenario file there).
const baselineRegistrationGas32ee354 = 411712

// TestRegistrationGasParity: below the activation height, registration gas
// equals 32ee354 exactly; from the activation height it differs.
func TestRegistrationGasParity(t *testing.T) {
	restore := types.SetHardeningV2ActivationHeightForTesting(51)
	below := registrationGasScenario(t, func(k *keeper.Keeper) { k.SetEVMKeepers(memEVM{}, &memERC20{}) })
	restore()
	require.Equal(t, uint64(baselineRegistrationGas32ee354), below)

	restore = types.SetHardeningV2ActivationHeightForTesting(50)
	defer restore()
	above := registrationGasScenario(t, func(k *keeper.Keeper) { k.SetEVMKeepers(memEVM{}, &memERC20{}) })
	require.NotEqual(t, below, above)
	t.Logf("below=%d above=%d", below, above)
}
