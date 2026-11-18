package keeper

import (
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/cosmos/cosmos-sdk/x/feegrant"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type authzGrantRef struct {
	granter sdk.AccAddress
	grantee sdk.AccAddress
	msgType string
}

type feeGrantRef struct {
	granter sdk.AccAddress
	grantee sdk.AccAddress
}

// revokeGranterAuthorisations deletes authz grants and fee allowances that
// spend this account's authority or funds. Grants where the account is only
// the grantee are left in place. Deletion happens after iteration so the
// store iterator is not invalidated.
func (k Keeper) revokeGranterAuthorisations(ctx sdk.Context, granter sdk.AccAddress) error {
	if k.authz == nil || k.fees == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "pqauth authz and feegrant keepers must be set before registration")
	}

	var grants []authzGrantRef
	var grantErr error
	k.authz.IterateGrants(ctx, func(granterAddr, granteeAddr sdk.AccAddress, grant authz.Grant) bool {
		if !granterAddr.Equals(granter) {
			return false
		}
		authorization, err := grant.GetAuthorization()
		if err != nil {
			grantErr = err
			return true
		}
		grants = append(grants, authzGrantRef{
			granter: append(sdk.AccAddress(nil), granterAddr...),
			grantee: append(sdk.AccAddress(nil), granteeAddr...),
			msgType: authorization.MsgTypeURL(),
		})
		return false
	})
	if grantErr != nil {
		return errorsmod.Wrap(grantErr, "read authz grant")
	}
	for _, ref := range grants {
		if err := k.authz.DeleteGrant(ctx, ref.grantee, ref.granter, ref.msgType); err != nil {
			return errorsmod.Wrapf(err, "delete authz grant %s from %s to %s", ref.msgType, ref.granter, ref.grantee)
		}
	}

	var allowances []feeGrantRef
	err := k.fees.IterateAllFeeAllowances(ctx, func(grant feegrant.Grant) bool {
		if grant.Granter != granter.String() {
			return false
		}
		grantee, err := sdk.AccAddressFromBech32(grant.Grantee)
		if err != nil {
			grantErr = err
			return true
		}
		allowances = append(allowances, feeGrantRef{
			granter: append(sdk.AccAddress(nil), granter...),
			grantee: grantee,
		})
		return false
	})
	if err != nil {
		return errorsmod.Wrap(err, "read fee allowances")
	}
	if grantErr != nil {
		return errorsmod.Wrap(grantErr, "fee allowance grantee")
	}
	for _, ref := range allowances {
		if err := k.fees.RevokeAllowance(ctx, ref.granter, ref.grantee); err != nil {
			return errorsmod.Wrapf(err, "revoke fee allowance from %s to %s", ref.granter, ref.grantee)
		}
	}
	return nil
}
