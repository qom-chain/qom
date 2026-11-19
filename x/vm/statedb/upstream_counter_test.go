package statedb

import (
	"testing"

	"github.com/stretchr/testify/require"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	"github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const testUpstreamFixesHeight = 10

func newCounterTestDB(height int64) (*StateDB, *mockSnapshotter) {
	mockSnap := newMockSnapshotter()
	db := &StateDB{
		validRevisions: []revision{},
		nextRevisionID: 0,
		journal:        newJournal(),
		snapshotter:    mockSnap,
	}
	db.ctx = sdk.Context{}.WithEventManager(sdk.NewEventManager()).WithBlockHeight(height)
	db.cacheCtx = sdk.Context{}.WithEventManager(sdk.NewEventManager()).WithBlockHeight(height)
	db.writeCache = func() {}
	return db, mockSnap
}

// From the activation height, calls rejected by the precompile call limit do
// not advance the counter, and a reverted
// precompile call still counts against the limit.
func TestAddPrecompileFn_CounterPostActivation(t *testing.T) {
	defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(testUpstreamFixesHeight)()

	db, snap := newCounterTestDB(testUpstreamFixesHeight)
	accepted := 0
	for i := 0; i < 512; i++ {
		err := db.AddPrecompileFn(snap.Snapshot())
		if i < int(types.MaxPrecompileCalls) {
			require.NoError(t, err, "call %d should be accepted", i)
			accepted++
		} else {
			require.Error(t, err, "call %d beyond the limit should be rejected", i)
		}
	}
	require.Equal(t, int(types.MaxPrecompileCalls), accepted)

	db2, snap2 := newCounterTestDB(testUpstreamFixesHeight)
	for i := 0; i < int(types.MaxPrecompileCalls)-1; i++ {
		require.NoError(t, db2.AddPrecompileFn(snap2.Snapshot()))
	}
	s := db2.Snapshot()
	require.NoError(t, db2.AddPrecompileFn(snap2.Snapshot()))
	require.Error(t, db2.AddPrecompileFn(snap2.Snapshot()), "limit reached")
	db2.RevertToSnapshot(s)
	require.Error(t, db2.AddPrecompileFn(snap2.Snapshot()), "a reverted call must still count")
}

// Below the activation height the counter behaves exactly as in v1.0.1: every
// call is journaled and counted, and the uint8 counter wraps after 256 calls.
func TestAddPrecompileFn_CounterPreActivation(t *testing.T) {
	defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(testUpstreamFixesHeight)()

	db, snap := newCounterTestDB(testUpstreamFixesHeight - 1)
	accepted := 0
	for i := 0; i < 512; i++ {
		if err := db.AddPrecompileFn(snap.Snapshot()); err == nil {
			accepted++
		}
	}
	// v1.0.1 behaviour: the counter values 1..20 are accepted, and so is 0
	// after each wrap (calls 256 and 512): 20 + 1 + 20 + 1.
	require.Equal(t, 2*int(types.MaxPrecompileCalls)+2, accepted)
	require.Len(t, db.journal.entries, 512, "every call is journaled before the limit check")
}
