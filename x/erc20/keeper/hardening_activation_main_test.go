package keeper

import (
	"os"
	"testing"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
)

// TestMain activates the pqauth hardening v2 rules from height 0 for this
// package. Tests of the pre-activation (32ee354) behaviour override it.
func TestMain(m *testing.M) {
	restore := pqauthtypes.SetHardeningV2ActivationHeightForTesting(0)
	code := m.Run()
	restore()
	os.Exit(code)
}
