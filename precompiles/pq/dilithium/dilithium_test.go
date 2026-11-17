package dilithium_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/precompiles/pq/dilithium"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

func TestDilithiumPrecompileIsRegistered(t *testing.T) {
	addr := dilithium.Precompile{}.Address()
	require.Equal(t, evmtypes.DilithiumPrecompileAddress, addr.Hex())
	require.Equal(t, evmtypes.DilithiumPrecompileAddress, addr.String())
	require.Contains(t, evmtypes.AvailableStaticPrecompiles, evmtypes.DilithiumPrecompileAddress)
	require.True(t, slices.IsSorted(evmtypes.AvailableStaticPrecompiles))
}
