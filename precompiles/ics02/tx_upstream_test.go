package ics02

import (
	"testing"

	"github.com/stretchr/testify/require"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// From the activation height updateClient returns an error, instead of
// panicking, when the client message decodes to nil (empty bytes or an Any
// with an empty type URL). Below it the v1.0.1 behaviour is kept.
func TestUpdateClientEmptyClientMessage(t *testing.T) {
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	// The client keeper is nil on purpose: the call must fail before reaching it.
	p := NewPrecompile(cdc, nil)

	emptyTypeURL, err := (&codectypes.Any{Value: []byte{0x01, 0x02}}).Marshal()
	require.NoError(t, err)

	testCases := []struct {
		name     string
		updateBz []byte
	}{
		{name: "empty bytes", updateBz: []byte{}},
		{name: "any with empty type url", updateBz: emptyTypeURL},
	}

	const height = 5
	ctx := sdk.Context{}.WithBlockHeight(height)
	for _, tc := range testCases {
		t.Run("post: "+tc.name, func(t *testing.T) {
			defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height)()
			var (
				bz      []byte
				callErr error
			)
			require.NotPanics(t, func() {
				bz, callErr = p.UpdateClient(ctx, nil, nil, nil, []interface{}{"07-tendermint-0", tc.updateBz})
			})
			require.ErrorIs(t, callErr, clienttypes.ErrInvalidClientType)
			require.Nil(t, bz)
		})
		t.Run("pre: "+tc.name, func(t *testing.T) {
			defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height + 1)()
			require.Panics(t, func() {
				_, _ = p.UpdateClient(ctx, nil, nil, nil, []interface{}{"07-tendermint-0", tc.updateBz})
			})
		})
	}
}
