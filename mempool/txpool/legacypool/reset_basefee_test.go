//go:build !test

// This test uses the production reset (reset_production.go); the test-tag
// reset fills in a default base fee and would not exercise the guard.

package legacypool

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// A reset to a head without a base fee (the placeholder header the chain
// returns when the latest context is not available) must not panic in the
// reorg loop; a later head with a base fee is handled as before.
func TestResetToHeadWithoutBaseFee(t *testing.T) {
	pool, _, _ := setupPool()
	defer pool.Close()

	old := pool.chain.CurrentBlock()
	noBaseFee := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(0)} // same shape as the chain placeholder header
	require.Nil(t, noBaseFee.BaseFee)
	require.NotPanics(t, func() { pool.Reset(old, noBaseFee) })

	withBaseFee := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(0), GasLimit: 10_000_000, BaseFee: big.NewInt(1_000_000_000)}
	require.NotPanics(t, func() { pool.Reset(noBaseFee, withBaseFee) })

	pending, queued := pool.Stats()
	require.Equal(t, 0, pending)
	require.Equal(t, 0, queued)
}
