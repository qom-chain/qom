package bech32

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func runBech32ToHex(t *testing.T, p *Precompile, height int64, address string) ([]byte, error) {
	t.Helper()
	input, err := p.Pack(Bech32ToHexMethod, address)
	require.NoError(t, err)
	evm := &vm.EVM{Context: vm.BlockContext{BlockNumber: big.NewInt(height)}}
	return p.Run(evm, &vm.Contract{Input: input}, true)
}

// From the activation height bech32ToHex rejects addresses that are not 20
// bytes long; below it the v1.0.1 behaviour (truncate or left-pad) is kept.
func TestBech32ToHexAddressLength(t *testing.T) {
	p, err := NewPrecompile(6000)
	require.NoError(t, err)
	const height = 5
	defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(height)()

	bz32 := make([]byte, 32)
	for i := range bz32 {
		bz32[i] = byte(i + 1)
	}
	addr32 := sdk.AccAddress(bz32).String()
	addr19 := sdk.AccAddress(make([]byte, 19)).String()
	bz20 := common.HexToAddress("0x4bf7a202cD56FC6c4D84a862a43DDFa9F7AaEF3e").Bytes()
	addr20 := sdk.AccAddress(bz20).String()

	// post-activation
	_, err = runBech32ToHex(t, p, height, addr32)
	require.ErrorContains(t, err, "expected 20 bytes, got 32")
	_, err = runBech32ToHex(t, p, height, addr19)
	require.ErrorContains(t, err, "expected 20 bytes, got 19")
	out, err := runBech32ToHex(t, p, height, addr20)
	require.NoError(t, err)
	res, err := p.Unpack(Bech32ToHexMethod, out)
	require.NoError(t, err)
	require.Equal(t, common.BytesToAddress(bz20), res[0].(common.Address))

	// pre-activation: v1.0.1 behaviour
	out, err = runBech32ToHex(t, p, height-1, addr32)
	require.NoError(t, err)
	res, err = p.Unpack(Bech32ToHexMethod, out)
	require.NoError(t, err)
	require.Equal(t, common.BytesToAddress(bz32), res[0].(common.Address))
	out, err = runBech32ToHex(t, p, height-1, addr20)
	require.NoError(t, err)
	res, err = p.Unpack(Bech32ToHexMethod, out)
	require.NoError(t, err)
	require.Equal(t, common.BytesToAddress(bz20), res[0].(common.Address))

	// the exported method keeps the v1.0.1 behaviour
	method := p.Methods[Bech32ToHexMethod]
	_, err = p.Bech32ToHex(&method, []interface{}{addr32})
	require.NoError(t, err)
}
