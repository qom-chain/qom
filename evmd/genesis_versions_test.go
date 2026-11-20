package evmd

import (
	"testing"

	"github.com/stretchr/testify/require"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
)

func TestGenesisModuleVersionMap(t *testing.T) {
	current := module.VersionMap{feegrant.ModuleName: 3, "bank": 4}

	mainnet := genesisModuleVersionMap(pqauthtypes.MainnetChainID, current)
	require.Equal(t, uint64(2), mainnet[feegrant.ModuleName])
	require.Equal(t, uint64(4), mainnet["bank"])
	require.Equal(t, uint64(3), current[feegrant.ModuleName], "input map must not be modified")

	other := genesisModuleVersionMap("qom_9000-1", current)
	require.Equal(t, current, other)

	// Entries for modules that are not registered are not added.
	require.Len(t, genesisModuleVersionMap(pqauthtypes.MainnetChainID, module.VersionMap{"bank": 4}), 1)
}
