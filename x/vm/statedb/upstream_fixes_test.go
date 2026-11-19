package statedb_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	"github.com/cosmos/evm/x/vm/statedb"
	"github.com/cosmos/evm/x/vm/types/mocks"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const upstreamFixesTestHeight = 10

func newHeightTestCtx(name string, height int64) (sdk.Context, *storetypes.KVStoreKey) {
	key := storetypes.NewKVStoreKey(name)
	tkey := storetypes.NewTransientStoreKey(name + "_t")
	ctx := testutil.DefaultContext(key, tkey).WithEventManager(sdk.NewEventManager()).WithBlockHeight(height)
	return ctx, key
}

// atomicTestKeeper routes writes through ctx's real KVStore, so a write
// discarded via CacheContext is observable as absent.
type atomicTestKeeper struct {
	key     *storetypes.KVStoreKey
	errAddr common.Address
}

var _ statedb.Keeper = &atomicTestKeeper{}

func (k *atomicTestKeeper) store(ctx sdk.Context) storetypes.KVStore { return ctx.KVStore(k.key) }

func (k *atomicTestKeeper) GetAccount(ctx sdk.Context, addr common.Address) *statedb.Account {
	bz := k.store(ctx).Get(addr.Bytes())
	if bz == nil {
		return nil
	}
	return &statedb.Account{Balance: new(uint256.Int).SetBytes(bz)}
}

func (k *atomicTestKeeper) SetAccount(ctx sdk.Context, addr common.Address, acc statedb.Account) error {
	if addr == k.errAddr {
		return errors.New("blocked")
	}
	k.store(ctx).Set(addr.Bytes(), acc.Balance.Bytes())
	return nil
}

func (k *atomicTestKeeper) DeleteAccount(ctx sdk.Context, addr common.Address) error {
	if addr == k.errAddr {
		return errors.New("blocked")
	}
	k.store(ctx).Delete(addr.Bytes())
	return nil
}

func (k *atomicTestKeeper) GetState(sdk.Context, common.Address, common.Hash) common.Hash {
	return common.Hash{}
}
func (k *atomicTestKeeper) GetCode(sdk.Context, common.Hash) []byte             { return nil }
func (k *atomicTestKeeper) GetCodeHash(sdk.Context, common.Address) common.Hash { return common.Hash{} }
func (k *atomicTestKeeper) ForEachStorage(sdk.Context, common.Address, func(common.Hash, common.Hash) bool) {
}
func (k *atomicTestKeeper) DeleteState(sdk.Context, common.Address, common.Hash)      {}
func (k *atomicTestKeeper) SetState(sdk.Context, common.Address, common.Hash, []byte) {}
func (k *atomicTestKeeper) DeleteCode(sdk.Context, []byte)                            {}
func (k *atomicTestKeeper) SetCode(sdk.Context, []byte, []byte)                       {}

func (k *atomicTestKeeper) KVStoreKeys() map[string]storetypes.StoreKey {
	return map[string]storetypes.StoreKey{k.key.Name(): k.key}
}

// TestCommitAtomicity commits a dirty set sorted [credit, blocked, debit]. From
// the activation height a late failure on blocked discards the whole commit;
// below it the v1.0.1 behaviour (credit already written) is kept.
func TestCommitAtomicity(t *testing.T) {
	defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(upstreamFixesTestHeight)()

	credit := common.BigToAddress(big.NewInt(10))
	blocked := common.BigToAddress(big.NewInt(50))
	debit := common.BigToAddress(big.NewInt(90))
	precompileAddr := common.BigToAddress(big.NewInt(1))

	setup := func(name string, errAddr common.Address, height int64) *statedb.StateDB {
		ctx, key := newHeightTestCtx(name, height)
		return statedb.New(ctx, &atomicTestKeeper{key: key, errAddr: errAddr}, emptyTxConfig)
	}
	seed := func(db *statedb.StateDB) {
		db.AddBalance(credit, uint256.NewInt(1_000_000), tracing.BalanceChangeUnspecified)
		db.AddBalance(blocked, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
		db.AddBalance(debit, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
	}
	persisted := func(db *statedb.StateDB, addr common.Address) *statedb.Account {
		return db.Keeper().GetAccount(db.GetContext(), addr)
	}
	stageViaPrecompile := func(t *testing.T, db *statedb.StateDB) {
		t.Helper()
		cacheCtx, err := db.GetCacheContext()
		require.NoError(t, err)
		require.NoError(t, db.Keeper().SetAccount(cacheCtx, precompileAddr, statedb.Account{Balance: uint256.NewInt(7)}))
	}

	post := int64(upstreamFixesTestHeight)
	pre := post - 1

	t.Run("post: late failure discards the whole commit", func(t *testing.T) {
		db := setup("fail", blocked, post)
		seed(db)
		require.Error(t, db.Commit())
		require.Nil(t, persisted(db, credit))
	})
	t.Run("post: late failure discards precompile-staged writes too", func(t *testing.T) {
		db := setup("fail_precompile", blocked, post)
		stageViaPrecompile(t, db)
		seed(db)
		require.Error(t, db.Commit())
		require.Nil(t, persisted(db, credit))
		require.Nil(t, persisted(db, precompileAddr))
	})
	t.Run("post: success persists everything", func(t *testing.T) {
		db := setup("ok", common.Address{}, post)
		seed(db)
		require.NoError(t, db.Commit())
		require.Equal(t, uint256.NewInt(1_000_000), persisted(db, credit).Balance)
		require.Equal(t, uint256.NewInt(1), persisted(db, debit).Balance)
	})
	t.Run("post: success persists precompile-staged writes too", func(t *testing.T) {
		db := setup("ok_precompile", common.Address{}, post)
		stageViaPrecompile(t, db)
		seed(db)
		require.NoError(t, db.Commit())
		require.Equal(t, uint256.NewInt(1_000_000), persisted(db, credit).Balance)
		require.Equal(t, uint256.NewInt(7), persisted(db, precompileAddr).Balance)
	})
	t.Run("pre: v1.0.1 behaviour, writes before the failure stay", func(t *testing.T) {
		db := setup("pre_fail", blocked, pre)
		seed(db)
		require.Error(t, db.Commit())
		require.NotNil(t, persisted(db, credit))
		require.Equal(t, uint256.NewInt(1_000_000), persisted(db, credit).Balance)
		require.Nil(t, persisted(db, debit))
	})
	t.Run("pre: v1.0.1 behaviour with precompile-staged writes", func(t *testing.T) {
		db := setup("pre_fail_precompile", blocked, pre)
		stageViaPrecompile(t, db)
		seed(db)
		require.Error(t, db.Commit())
		require.NotNil(t, persisted(db, credit))
		require.NotNil(t, persisted(db, precompileAddr))
	})
	t.Run("pre: success persists everything", func(t *testing.T) {
		db := setup("pre_ok", common.Address{}, pre)
		stageViaPrecompile(t, db)
		seed(db)
		require.NoError(t, db.Commit())
		require.Equal(t, uint256.NewInt(1_000_000), persisted(db, credit).Balance)
		require.Equal(t, uint256.NewInt(7), persisted(db, precompileAddr).Balance)
	})
}

func TestAddBalanceOverflow(t *testing.T) {
	defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(upstreamFixesTestHeight)()
	maxU := new(uint256.Int).SetAllOne()

	t.Run("post: overflow panics", func(t *testing.T) {
		ctx, _ := newHeightTestCtx("ovf_post", upstreamFixesTestHeight)
		db := statedb.New(ctx, mocks.NewEVMKeeper(), emptyTxConfig)
		db.AddBalance(address, maxU, tracing.BalanceChangeUnspecified)
		require.PanicsWithValue(t,
			"state balance overflow for "+address.Hex()+": have="+maxU.String()+" add=1",
			func() { db.AddBalance(address, uint256.NewInt(1), tracing.BalanceChangeUnspecified) })
		require.Equal(t, maxU, db.GetBalance(address))
		// no overflow: still fine
		db.SubBalance(address, uint256.NewInt(5), tracing.BalanceChangeUnspecified)
		db.AddBalance(address, uint256.NewInt(5), tracing.BalanceChangeUnspecified)
		require.Equal(t, maxU, db.GetBalance(address))
	})
	t.Run("pre: v1.0.1 behaviour, the addition wraps", func(t *testing.T) {
		ctx, _ := newHeightTestCtx("ovf_pre", upstreamFixesTestHeight-1)
		db := statedb.New(ctx, mocks.NewEVMKeeper(), emptyTxConfig)
		db.AddBalance(address, maxU, tracing.BalanceChangeUnspecified)
		require.NotPanics(t, func() { db.AddBalance(address, uint256.NewInt(1), tracing.BalanceChangeUnspecified) })
		require.True(t, db.GetBalance(address).IsZero())
	})
}
