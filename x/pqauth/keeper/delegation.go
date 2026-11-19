package keeper

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuthzKeeper lists and deletes grants where an account is the granter.
// GranterGrants reads only the granter's key prefix, so the cost of a lookup
// is bounded by that account's own grants, not by every grant on chain.
type AuthzKeeper interface {
	GranterGrants(ctx context.Context, req *authz.QueryGranterGrantsRequest) (*authz.QueryGranterGrantsResponse, error)
	DeleteGrant(ctx context.Context, grantee, granter sdk.AccAddress, msgType string) error
}

// EVMCodeKeeper reads an account's EVM code hash. The EVM keeper implements it.
type EVMCodeKeeper interface {
	GetCodeHash(ctx sdk.Context, addr common.Address) common.Hash
}

// ERC20AllowanceKeeper deletes the ERC-20 precompile allowances an account
// owns. The erc20 keeper implements it.
type ERC20AllowanceKeeper interface {
	DeleteOwnerAllowances(ctx sdk.Context, owner common.Address) (int, error)
}

// FeeAllowanceRevoker deletes fee allowances granted by an account.
type FeeAllowanceRevoker interface {
	IterateAllFeeAllowances(ctx context.Context, cb func(grant feegrant.Grant) bool) error
	RevokeAllowance(ctx context.Context, granter, grantee sdk.AccAddress) error
}

// StakingKeeper loads a validator by consensus address.
type StakingKeeper interface {
	GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error)
}

// SetDelegationKeepers attaches the keepers used to revoke grants on
// registration and to check validator operators. A nil keeper skips that
// check only when the message does not need it. The application sets all three.
func (k *Keeper) SetDelegationKeepers(authzKeeper AuthzKeeper, fees FeeAllowanceRevoker, staking StakingKeeper) {
	if k == nil {
		return
	}
	k.authz = authzKeeper
	k.fees = fees
	k.staking = staking
}

// SetEVMKeepers attaches the EVM code reader and the ERC-20 allowance keeper
// used by registration. Registration refuses an account that has EVM code and
// deletes ERC-20 precompile allowances the account owns. The application sets
// both; registration fails while either is nil.
func (k *Keeper) SetEVMKeepers(evm EVMCodeKeeper, erc20 ERC20AllowanceKeeper) {
	if k == nil {
		return
	}
	k.evm = evm
	k.erc20 = erc20
}

// feegrantRevoker adapts the SDK feegrant keeper. RevokeAllowance on that
// keeper is unexported, so revocation goes through the public message server.
type feegrantRevoker struct {
	k feegrantkeeper.Keeper
}

// NewFeegrantRevoker wraps the SDK feegrant keeper.
func NewFeegrantRevoker(k feegrantkeeper.Keeper) FeeAllowanceRevoker {
	return feegrantRevoker{k: k}
}

func (r feegrantRevoker) IterateAllFeeAllowances(ctx context.Context, cb func(grant feegrant.Grant) bool) error {
	return r.k.IterateAllFeeAllowances(ctx, cb)
}

func (r feegrantRevoker) RevokeAllowance(ctx context.Context, granter, grantee sdk.AccAddress) error {
	_, err := feegrantkeeper.NewMsgServerImpl(r.k).RevokeAllowance(ctx, &feegrant.MsgRevokeAllowance{
		Granter: granter.String(),
		Grantee: grantee.String(),
	})
	return err
}
