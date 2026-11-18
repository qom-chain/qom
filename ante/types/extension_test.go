package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	antetypes "github.com/cosmos/evm/ante/types"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

func TestAcceptsCosmosExtensionOption(t *testing.T) {
	require.False(t, antetypes.AcceptsCosmosExtensionOption(nil))
	require.False(t, antetypes.AcceptsCosmosExtensionOption(&codectypes.Any{
		TypeUrl: "/cosmos.evm.vm.v1.ExtensionOptionsEthereumTx",
	}))

	mldsa, err := codectypes.NewAnyWithValue(&pqauthtypes.ExtensionOptionMLDSASig{})
	require.NoError(t, err)
	require.True(t, antetypes.AcceptsCosmosExtensionOption(mldsa))
	require.True(t, antetypes.AcceptsCosmosExtensionOption(&codectypes.Any{
		TypeUrl: mldsa.GetTypeUrl(),
		Value:   mldsa.GetValue(),
	}))

	dynamicFee, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{})
	require.NoError(t, err)
	require.True(t, antetypes.AcceptsCosmosExtensionOption(dynamicFee))
}
