package keeper

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/cosmos/evm/x/pqauth/types"

	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ValidatorSetKeeper is the subset of the staking keeper used by the
// validator ML-DSA key guard. The staking keeper implements it.
type ValidatorSetKeeper interface {
	ValidatorsPowerStoreIterator(ctx context.Context) (corestore.Iterator, error)
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	MaxValidators(ctx context.Context) (uint32, error)
	PowerReduction(ctx context.Context) math.Int
	Jail(ctx context.Context, consAddr sdk.ConsAddress) error
}

// SetValidatorSetKeeper attaches the staking keeper used by the validator
// ML-DSA key guard. The application sets it before the keeper is copied into
// the module manager.
func (k *Keeper) SetValidatorSetKeeper(valset ValidatorSetKeeper) {
	if k == nil {
		return
	}
	k.valset = valset
}

// EnforceValidatorMLDSAKeys jails every validator that the staking end blocker
// would bond but that has no ML-DSA-87 key bound to its consensus address.
// It must run before the staking end blocker.
//
// Vote extensions carry an ML-DSA-87 signature that every node checks against
// the validator's on-chain key. A bonded validator without a key has all of
// its precommits rejected, so with more than 1/3 of the voting power it halts
// the chain, and the registration that would fix it can no longer be included.
// Jailing removes the validator from the staking power index, so it is never
// bonded and never gets voting power. A validator created without a key in
// the same transaction can still join: register the key
// (MsgRegisterValidatorMLDSAKey), then unjail (slashing MsgUnjail), in one
// transaction or two. Unjailing without a key is undone at the end of that
// block.
//
// The walk follows staking's ApplyAndReturnValidatorSetUpdates: highest power
// first, skipping nothing staking would bond, and stopping at the first
// zero-power validator or once MaxValidators keyed validators are seen. Only
// validators that could be bonded at this block are read.
//
// Nothing happens below ValidatorKeyGuardActivationHeight, or when vote
// extensions are not enabled in the consensus parameters (ML-DSA keys are only
// needed for vote extensions).
func (k Keeper) EnforceValidatorMLDSAKeys(ctx sdk.Context) error {
	if !types.ValidatorKeyGuardActive(ctx.BlockHeight()) {
		return nil
	}
	if cp := ctx.ConsensusParams(); cp.Abci == nil || cp.Abci.VoteExtensionsEnableHeight == 0 {
		return nil
	}
	if k.valset == nil {
		return fmt.Errorf("pqauth validator set keeper is not set")
	}

	maxValidators, err := k.valset.MaxValidators(ctx)
	if err != nil {
		return err
	}
	powerReduction := k.valset.PowerReduction(ctx)

	type unkeyed struct {
		consAddr sdk.ConsAddress
		operator string
	}
	var toJail []unkeyed

	iterator, err := k.valset.ValidatorsPowerStoreIterator(ctx)
	if err != nil {
		return err
	}
	keyed := uint32(0)
	for ; iterator.Valid() && keyed < maxValidators; iterator.Next() {
		valAddr := sdk.ValAddress(iterator.Value())
		validator, err := k.valset.GetValidator(ctx, valAddr)
		if err != nil {
			_ = iterator.Close()
			return fmt.Errorf("validator record not found for address %X: %w", valAddr, err)
		}
		if validator.Jailed {
			// Never in the power index; staking itself rejects this case.
			continue
		}
		if validator.PotentialConsensusPower(powerReduction) == 0 {
			break
		}
		consAddr, err := validator.GetConsAddr()
		if err != nil {
			_ = iterator.Close()
			return fmt.Errorf("validator %s consensus address: %w", validator.GetOperator(), err)
		}
		if _, found := k.GetValidatorMLDSAPubKey(ctx, consAddr); found {
			keyed++
			continue
		}
		toJail = append(toJail, unkeyed{consAddr: consAddr, operator: validator.GetOperator()})
	}
	if err := iterator.Close(); err != nil {
		return err
	}

	for _, v := range toJail {
		if err := k.valset.Jail(ctx, v.consAddr); err != nil {
			return fmt.Errorf("jail validator %s without ML-DSA key: %w", v.operator, err)
		}
		k.Logger(ctx).Info("jailed validator without an ML-DSA key", "validator", v.operator, "consensus_address", hex.EncodeToString(v.consAddr))
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeJailValidatorMissingMLDSAKey,
			sdk.NewAttribute(types.AttributeKeyValidator, v.operator),
			sdk.NewAttribute(types.AttributeKeyConsensusAddress, hex.EncodeToString(v.consAddr)),
		))
	}
	return nil
}
