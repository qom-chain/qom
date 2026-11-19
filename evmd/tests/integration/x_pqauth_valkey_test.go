package integration

import (
	"crypto/rand"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/cosmos/evm/evmd"
	"github.com/cosmos/evm/testutil/integration/evm/network"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type valkeyEnv struct {
	t   *testing.T
	nw  *network.UnitTestNetwork
	app *evmd.EVMD
	ctx sdk.Context
}

type valkeyOperator struct {
	acc     sdk.AccAddress
	secpPub []byte
	consPK  cryptotypes.PubKey
}

func newValkeyEnv(t *testing.T) *valkeyEnv {
	t.Helper()
	nw := network.NewUnitTestNetwork(CreateEvmd)
	app, ok := nw.App.(*evmd.EVMD)
	require.True(t, ok)
	ctx := nw.GetContext()
	cp := ctx.ConsensusParams()
	cp.Abci = &cmtproto.ABCIParams{VoteExtensionsEnableHeight: 1}
	ctx = ctx.WithConsensusParams(cp).WithEventManager(sdk.NewEventManager())

	// Bind ML-DSA keys to the genesis validators, as mainnet genesis does.
	vals, err := app.StakingKeeper.GetBondedValidatorsByPower(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, vals)
	for _, v := range vals {
		ca, err := v.GetConsAddr()
		require.NoError(t, err)
		_, pub := valkeyMLDSAKey(t)
		require.NoError(t, app.PQAuthKeeper.SetValidatorMLDSAPubKey(ctx, ca, pub))
	}
	return &valkeyEnv{t: t, nw: nw, app: app, ctx: ctx}
}

func valkeyMLDSAKey(t *testing.T) (*mldsa87.PrivateKey, []byte) {
	t.Helper()
	pub, priv, err := mldsa87.GenerateKey(rand.Reader)
	require.NoError(t, err)
	bz, err := pub.MarshalBinary()
	require.NoError(t, err)
	return priv, bz
}

func (e *valkeyEnv) bondDenom() string {
	d, err := e.app.StakingKeeper.BondDenom(e.ctx)
	require.NoError(e.t, err)
	return d
}

func (e *valkeyEnv) totalBondedTokens() math.Int {
	vals, err := e.app.StakingKeeper.GetBondedValidatorsByPower(e.ctx)
	require.NoError(e.t, err)
	total := math.ZeroInt()
	for _, v := range vals {
		total = total.Add(v.Tokens)
	}
	return total
}

// newOperator funds an ethsecp256k1 account (with its pubkey on chain).
func (e *valkeyEnv) newOperator(funds math.Int) valkeyOperator {
	priv, err := ethsecp256k1.GenerateKey()
	require.NoError(e.t, err)
	pub := priv.PubKey()
	addr := sdk.AccAddress(pub.Address())
	acc := e.app.AccountKeeper.NewAccountWithAddress(e.ctx, addr)
	require.NoError(e.t, acc.SetPubKey(pub))
	e.app.AccountKeeper.SetAccount(e.ctx, acc)
	coins := sdk.NewCoins(sdk.NewCoin(e.bondDenom(), funds))
	require.NoError(e.t, e.app.BankKeeper.MintCoins(e.ctx, "mint", coins))
	require.NoError(e.t, e.app.BankKeeper.SendCoinsFromModuleToAccount(e.ctx, "mint", addr, coins))
	return valkeyOperator{acc: addr, secpPub: pub.Bytes(), consPK: ed25519.GenPrivKey().PubKey()}
}

func (e *valkeyEnv) createValidator(op valkeyOperator, stake math.Int) {
	msg, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(op.acc).String(), op.consPK, sdk.NewCoin(e.bondDenom(), stake),
		stakingtypes.NewDescription("v", "", "", "", ""),
		stakingtypes.NewCommissionRates(math.LegacyNewDecWithPrec(1, 1), math.LegacyNewDecWithPrec(2, 1), math.LegacyNewDecWithPrec(1, 2)),
		math.OneInt(),
	)
	require.NoError(e.t, err)
	_, err = stakingkeeper.NewMsgServerImpl(e.app.StakingKeeper).CreateValidator(e.ctx, msg)
	require.NoError(e.t, err)
}

func (e *valkeyEnv) registerKey(op valkeyOperator) {
	priv, pub := valkeyMLDSAKey(e.t)
	consAddr := sdk.ConsAddress(op.consPK.Address())
	payload, err := pqauthtypes.ValidatorPoPSignBytes(e.ctx.ChainID(), consAddr, op.secpPub, pub)
	require.NoError(e.t, err)
	sig, err := priv.Sign(rand.Reader, payload, nil)
	require.NoError(e.t, err)
	_, err = e.app.PQAuthKeeper.RegisterValidatorMLDSAKey(e.ctx, &pqauthtypes.MsgRegisterValidatorMLDSAKey{
		Signer:           op.acc.String(),
		ConsensusAddress: consAddr,
		PubKey:           pub,
		PopSignature:     sig,
	})
	require.NoError(e.t, err)
}

func (e *valkeyEnv) unjail(op valkeyOperator) error {
	_, err := slashingkeeper.NewMsgServerImpl(e.app.SlashingKeeper).Unjail(e.ctx, &slashingtypes.MsgUnjail{ValidatorAddr: sdk.ValAddress(op.acc).String()})
	return err
}

func (e *valkeyEnv) delegate(from sdk.AccAddress, op valkeyOperator, amt math.Int) {
	_, err := stakingkeeper.NewMsgServerImpl(e.app.StakingKeeper).Delegate(e.ctx, &stakingtypes.MsgDelegate{
		DelegatorAddress: from.String(), ValidatorAddress: sdk.ValAddress(op.acc).String(), Amount: sdk.NewCoin(e.bondDenom(), amt),
	})
	require.NoError(e.t, err)
}

// endBlock runs the application end blocker (pqauth, then staking, ...) and
// returns the validator updates handed to CometBFT.
func (e *valkeyEnv) endBlock() []abci.ValidatorUpdate {
	res, err := e.app.EndBlocker(e.ctx)
	require.NoError(e.t, err)
	e.ctx = e.ctx.WithBlockHeight(e.ctx.BlockHeight() + 1).WithEventManager(sdk.NewEventManager())
	return res.ValidatorUpdates
}

func (e *valkeyEnv) validator(op valkeyOperator) stakingtypes.Validator {
	v, err := e.app.StakingKeeper.GetValidator(e.ctx, sdk.ValAddress(op.acc))
	require.NoError(e.t, err)
	return v
}

func hasUpdateFor(updates []abci.ValidatorUpdate, pk cryptotypes.PubKey) bool {
	for _, u := range updates {
		if ed := u.PubKey.GetEd25519(); ed != nil && string(ed) == string(pk.Bytes()) && u.Power > 0 {
			return true
		}
	}
	return false
}

func TestValidatorKeyGuardRealStaking(t *testing.T) {
	restore := pqauthtypes.SetValidatorKeyGuardActivationHeightForTesting(0)
	defer restore()
	e := newValkeyEnv(t)

	// The halt attempt: an unkeyed validator with twice the bonded stake.
	big := e.totalBondedTokens().MulRaw(2)
	attacker := e.newOperator(big.MulRaw(3))
	e.createValidator(attacker, big)
	updates := e.endBlock()
	require.False(t, hasUpdateFor(updates, attacker.consPK), "unkeyed validator must not get voting power")
	v := e.validator(attacker)
	require.True(t, v.Jailed)
	require.True(t, v.IsUnbonded())

	// Heavy delegation to the unkeyed validator does not bond it.
	whale := e.newOperator(big.MulRaw(3))
	e.delegate(whale.acc, attacker, big)
	require.False(t, hasUpdateFor(e.endBlock(), attacker.consPK))
	require.True(t, e.validator(attacker).IsUnbonded())

	// Unjailing without a key is undone in the same block.
	require.NoError(t, e.unjail(attacker))
	require.False(t, hasUpdateFor(e.endBlock(), attacker.consPK))
	require.True(t, e.validator(attacker).Jailed)
	require.True(t, e.validator(attacker).IsUnbonded())

	// Two-step join: register the key, then unjail; bonded at that block.
	e.registerKey(attacker)
	require.NoError(t, e.unjail(attacker))
	updates = e.endBlock()
	require.True(t, hasUpdateFor(updates, attacker.consPK))
	require.True(t, e.validator(attacker).IsBonded())
	require.False(t, e.validator(attacker).Jailed)

	// One-tx join (create-validator + key registration before the end
	// blocker) is never jailed.
	joiner := e.newOperator(math.NewInt(1_000_000_000_000_000_000).MulRaw(10))
	e.createValidator(joiner, math.NewInt(1_000_000_000_000_000_000))
	e.registerKey(joiner)
	updates = e.endBlock()
	require.True(t, hasUpdateFor(updates, joiner.consPK))
	require.True(t, e.validator(joiner).IsBonded())

	// Genesis (keyed) validators are still bonded.
	vals, err := e.app.StakingKeeper.GetBondedValidatorsByPower(e.ctx)
	require.NoError(t, err)
	require.Len(t, vals, len(e.nw.GetValidators())+2)
}

func TestValidatorKeyGuardRealStakingBelowHeight(t *testing.T) {
	e := newValkeyEnv(t)
	restore := pqauthtypes.SetValidatorKeyGuardActivationHeightForTesting(e.ctx.BlockHeight() + 2)
	defer restore()

	// Below the height: the old behaviour, an unkeyed validator is bonded.
	op := e.newOperator(math.NewInt(1_000_000_000_000_000_000).MulRaw(10))
	e.createValidator(op, math.NewInt(1_000_000_000_000_000_000))
	require.True(t, hasUpdateFor(e.endBlock(), op.consPK))
	require.True(t, e.validator(op).IsBonded())
	require.False(t, e.validator(op).Jailed)

	// No-op block, still below.
	e.endBlock()
	require.True(t, e.validator(op).IsBonded())

	// At the height the already-bonded unkeyed validator is jailed and
	// removed from the set (power 0 update).
	updates := e.endBlock()
	require.True(t, e.validator(op).Jailed)
	found := false
	for _, u := range updates {
		if ed := u.PubKey.GetEd25519(); ed != nil && string(ed) == string(op.consPK.Bytes()) {
			require.Equal(t, int64(0), u.Power)
			found = true
		}
	}
	require.True(t, found)
}
