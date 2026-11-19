package keeper_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/evm/x/pqauth/keeper"
	"github.com/cosmos/evm/x/pqauth/types"

	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// memValset is an in-memory power index: vals is kept highest power first and
// the iterator returns the non-jailed entries, like the staking power index.
type memValset struct {
	vals   []stakingtypes.Validator
	max    uint32
	jailed []sdk.ConsAddress
}

type sliceIterator struct {
	keys [][]byte
	pos  int
}

func (it *sliceIterator) Domain() ([]byte, []byte) { return nil, nil }
func (it *sliceIterator) Valid() bool              { return it.pos < len(it.keys) }
func (it *sliceIterator) Next()                    { it.pos++ }
func (it *sliceIterator) Key() []byte              { return it.keys[it.pos] }
func (it *sliceIterator) Value() []byte            { return it.keys[it.pos] }
func (it *sliceIterator) Error() error             { return nil }
func (it *sliceIterator) Close() error             { return nil }

func (m *memValset) ValidatorsPowerStoreIterator(context.Context) (corestore.Iterator, error) {
	it := &sliceIterator{}
	for _, v := range m.vals {
		if v.Jailed {
			continue
		}
		bz, err := sdk.ValAddressFromBech32(v.OperatorAddress)
		if err != nil {
			return nil, err
		}
		it.keys = append(it.keys, bz)
	}
	return it, nil
}

func (m *memValset) GetValidator(_ context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	for _, v := range m.vals {
		if v.OperatorAddress == addr.String() {
			return v, nil
		}
	}
	return stakingtypes.Validator{}, stakingtypes.ErrNoValidatorFound
}

func (m *memValset) MaxValidators(context.Context) (uint32, error) { return m.max, nil }
func (m *memValset) PowerReduction(context.Context) math.Int       { return sdk.DefaultPowerReduction }

func (m *memValset) Jail(_ context.Context, consAddr sdk.ConsAddress) error {
	for i, v := range m.vals {
		ca, err := v.GetConsAddr()
		if err != nil {
			return err
		}
		if sdk.ConsAddress(ca).Equals(consAddr) {
			if v.Jailed {
				return errors.New("already jailed")
			}
			m.vals[i].Jailed = true
			m.jailed = append(m.jailed, consAddr)
			return nil
		}
	}
	return stakingtypes.ErrNoValidatorFound
}

type testVal struct {
	val      stakingtypes.Validator
	consAddr sdk.ConsAddress
}

// newTestVal returns an unjailed validator with power (in consensus units).
func newTestVal(t *testing.T, power int64) testVal {
	t.Helper()
	pk := ed25519.GenPrivKey().PubKey()
	op := sdk.ValAddress(pk.Address())
	v, err := stakingtypes.NewValidator(op.String(), pk, stakingtypes.Description{})
	require.NoError(t, err)
	v.Tokens = sdk.TokensFromConsensusPower(power, sdk.DefaultPowerReduction)
	v.DelegatorShares = math.LegacyNewDecFromInt(v.Tokens)
	return testVal{val: v, consAddr: sdk.ConsAddress(pk.Address())}
}

func veEnabled(ctx sdk.Context, enableHeight int64) sdk.Context {
	return ctx.WithConsensusParams(cmtproto.ConsensusParams{
		Abci: &cmtproto.ABCIParams{VoteExtensionsEnableHeight: enableHeight},
	})
}

func setupGuard(t *testing.T, height int64, max uint32, vals ...testVal) (sdk.Context, keeper.Keeper, *memValset) {
	t.Helper()
	ctx, k := setupKeeper(t)
	ctx = veEnabled(ctx, 1).WithBlockHeight(height).WithEventManager(sdk.NewEventManager())
	vs := &memValset{max: max}
	for _, v := range vals {
		vs.vals = append(vs.vals, v.val)
	}
	k.SetValidatorSetKeeper(vs)
	return ctx, k, vs
}

func bindKey(t *testing.T, ctx sdk.Context, k keeper.Keeper, v testVal) {
	t.Helper()
	require.NoError(t, k.SetValidatorMLDSAPubKey(ctx, v.consAddr, newMLDSAPubKey(t)))
}

func TestValidatorKeyGuardInactiveBelowHeight(t *testing.T) {
	restore := types.SetValidatorKeyGuardActivationHeightForTesting(100)
	defer restore()

	keyed, unkeyed := newTestVal(t, 400), newTestVal(t, 800)
	ctx, k, vs := setupGuard(t, 99, 100, unkeyed, keyed)
	bindKey(t, ctx, k, keyed)

	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx))
	require.Empty(t, vs.jailed)
	require.Empty(t, ctx.EventManager().Events())

	// Not even a missing staking keeper matters below the height.
	k.SetValidatorSetKeeper(nil)
	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx))

	// The build default (mainnet height) does not apply one block early.
	types.SetValidatorKeyGuardActivationHeightForTesting(types.MainnetValidatorKeyGuardActivationHeight)
	k.SetValidatorSetKeeper(vs)
	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx.WithBlockHeight(types.MainnetValidatorKeyGuardActivationHeight-1)))
	require.Empty(t, vs.jailed)
}

func TestValidatorKeyGuardJailsUnkeyedAtHeight(t *testing.T) {
	restore := types.SetValidatorKeyGuardActivationHeightForTesting(100)
	defer restore()

	// The halt scenario: an unkeyed validator with 2/3 of the stake.
	keyed, unkeyed := newTestVal(t, 400), newTestVal(t, 800)
	ctx, k, vs := setupGuard(t, 100, 100, unkeyed, keyed)
	bindKey(t, ctx, k, keyed)

	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx))
	require.Equal(t, []sdk.ConsAddress{unkeyed.consAddr}, vs.jailed)
	require.True(t, vs.vals[0].Jailed)
	require.False(t, vs.vals[1].Jailed)

	evs := ctx.EventManager().Events()
	require.Len(t, evs, 1)
	require.Equal(t, types.EventTypeJailValidatorMissingMLDSAKey, evs[0].Type)
	attr, ok := evs[0].GetAttribute(types.AttributeKeyValidator)
	require.True(t, ok)
	require.Equal(t, unkeyed.val.OperatorAddress, attr.Value)

	// A second run is a no-op: jailed validators are out of the power index.
	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx.WithBlockHeight(101)))
	require.Len(t, vs.jailed, 1)
}

func TestValidatorKeyGuardKeyedValidatorsUntouched(t *testing.T) {
	restore := types.SetValidatorKeyGuardActivationHeightForTesting(0)
	defer restore()

	a, b, c := newTestVal(t, 300), newTestVal(t, 200), newTestVal(t, 100)
	ctx, k, vs := setupGuard(t, 5, 100, a, b, c)
	bindKey(t, ctx, k, a)
	bindKey(t, ctx, k, b)
	bindKey(t, ctx, k, c)

	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx))
	require.Empty(t, vs.jailed)
	require.Empty(t, ctx.EventManager().Events())
}

func TestValidatorKeyGuardWalksLikeStaking(t *testing.T) {
	restore := types.SetValidatorKeyGuardActivationHeightForTesting(0)
	defer restore()

	// MaxValidators 2. Unkeyed validators ranked inside the set are jailed;
	// the walk continues until two keyed validators are found, so the
	// validator promoted into the set is checked too. Validators below that
	// (and zero-power ones) cannot be bonded this block and are not touched.
	u1 := newTestVal(t, 900)
	k1 := newTestVal(t, 500)
	u2 := newTestVal(t, 400)
	k2 := newTestVal(t, 300)
	u3 := newTestVal(t, 100)
	ctx, k, vs := setupGuard(t, 1, 2, u1, k1, u2, k2, u3)
	bindKey(t, ctx, k, k1)
	bindKey(t, ctx, k, k2)

	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx))
	require.Equal(t, []sdk.ConsAddress{u1.consAddr, u2.consAddr}, vs.jailed)
	require.False(t, vs.vals[4].Jailed)

	// If k2 drops out (e.g. jailed for downtime), u3 would be promoted and
	// is jailed at that block.
	vs.vals[3].Jailed = true
	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx.WithBlockHeight(2)))
	require.Equal(t, []sdk.ConsAddress{u1.consAddr, u2.consAddr, u3.consAddr}, vs.jailed)
}

func TestValidatorKeyGuardStopsAtZeroPower(t *testing.T) {
	restore := types.SetValidatorKeyGuardActivationHeightForTesting(0)
	defer restore()

	keyed := newTestVal(t, 10)
	zero := newTestVal(t, 0)
	ctx, k, vs := setupGuard(t, 1, 100, keyed, zero)
	bindKey(t, ctx, k, keyed)

	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx))
	require.Empty(t, vs.jailed)
}

func TestValidatorKeyGuardNeverLeavesAnUnkeyedSoleValidator(t *testing.T) {
	restore := types.SetValidatorKeyGuardActivationHeightForTesting(0)
	defer restore()

	// Even with no keyed validator left in the power index, an unkeyed one is
	// jailed: it must never be the only signer.
	unkeyed := newTestVal(t, 10)
	ctx, k, vs := setupGuard(t, 1, 100, unkeyed)
	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx))
	require.Equal(t, []sdk.ConsAddress{unkeyed.consAddr}, vs.jailed)
}

func TestValidatorKeyGuardNeedsVoteExtensions(t *testing.T) {
	restore := types.SetValidatorKeyGuardActivationHeightForTesting(0)
	defer restore()

	unkeyed := newTestVal(t, 10)
	ctx, k, vs := setupGuard(t, 1, 100, unkeyed)

	require.NoError(t, k.EnforceValidatorMLDSAKeys(ctx.WithConsensusParams(cmtproto.ConsensusParams{})))
	require.NoError(t, k.EnforceValidatorMLDSAKeys(veEnabled(ctx, 0)))
	require.Empty(t, vs.jailed)

	// Enabled at a later height still counts: keys are needed once VEs start.
	require.NoError(t, k.EnforceValidatorMLDSAKeys(veEnabled(ctx, 50)))
	require.Len(t, vs.jailed, 1)
}

func TestValidatorKeyGuardRequiresStakingKeeperWhenActive(t *testing.T) {
	restore := types.SetValidatorKeyGuardActivationHeightForTesting(0)
	defer restore()

	ctx, k, _ := setupGuard(t, 1, 100)
	k.SetValidatorSetKeeper(nil)
	require.Error(t, k.EnforceValidatorMLDSAKeys(ctx))
}
