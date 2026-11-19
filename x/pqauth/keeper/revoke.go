package keeper

import (
	"github.com/ethereum/go-ethereum/common"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
)

// granterGrantsPageLimit is the page size used to read a granter's authz
// grants. It only bounds memory per page; every page is read.
const granterGrantsPageLimit = 100

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
// the grantee are left in place. Deletion happens after reading so the store
// iterator is not invalidated.
//
// Authz grants are read with GranterGrants, which walks only the granter's
// key prefix (0x01 | len | granter) in store order. Fee allowances are keyed
// grantee first, so the SDK has no granter index; they are still read with a
// full scan of the feegrant store.
func (k Keeper) revokeGranterAuthorisations(ctx sdk.Context, granter sdk.AccAddress) error {
	if k.authz == nil || k.fees == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "pqauth authz and feegrant keepers must be set before registration")
	}

	grants, err := k.granterGrants(ctx, granter)
	if err != nil {
		return err
	}
	for _, ref := range grants {
		if err := k.authz.DeleteGrant(ctx, ref.grantee, ref.granter, ref.msgType); err != nil {
			return errorsmod.Wrapf(err, "delete authz grant %s from %s to %s", ref.msgType, ref.granter, ref.grantee)
		}
	}

	var allowances []feeGrantRef
	var grantErr error
	err = k.fees.IterateAllFeeAllowances(ctx, func(grant feegrant.Grant) bool {
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

// granterGrants returns every authz grant whose granter is granter, page by
// page, in store order.
func (k Keeper) granterGrants(ctx sdk.Context, granter sdk.AccAddress) ([]authzGrantRef, error) {
	var refs []authzGrantRef
	var nextKey []byte
	for {
		res, err := k.authz.GranterGrants(ctx, &authz.QueryGranterGrantsRequest{
			Granter:    granter.String(),
			Pagination: &query.PageRequest{Key: nextKey, Limit: granterGrantsPageLimit},
		})
		if err != nil {
			return nil, errorsmod.Wrap(err, "read authz grants")
		}
		if res == nil {
			return refs, nil
		}
		for _, grant := range res.Grants {
			if grant == nil {
				return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "read authz grant: nil grant")
			}
			if grant.Granter != granter.String() {
				return nil, errorsmod.Wrapf(sdkerrors.ErrLogic, "read authz grant: granter %s does not match %s", grant.Granter, granter)
			}
			grantee, err := sdk.AccAddressFromBech32(grant.Grantee)
			if err != nil {
				return nil, errorsmod.Wrap(err, "read authz grant grantee")
			}
			if grant.Authorization == nil {
				return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "read authz grant: missing authorization")
			}
			authorization, ok := grant.Authorization.GetCachedValue().(authz.Authorization)
			if !ok {
				return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidType, "read authz grant: cannot decode authorization %s", grant.Authorization.TypeUrl)
			}
			refs = append(refs, authzGrantRef{
				granter: append(sdk.AccAddress(nil), granter...),
				grantee: grantee,
				msgType: authorization.MsgTypeURL(),
			})
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			return refs, nil
		}
		nextKey = res.Pagination.NextKey
	}
}

// requireNoEVMCode rejects registration of an account that has EVM code,
// including an EIP-7702 delegation designator. Code on a registered account
// would run with the account's authority without an open EVM window.
func (k Keeper) requireNoEVMCode(ctx sdk.Context, addr sdk.AccAddress, signer string) error {
	if k.evm == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "pqauth EVM keeper must be set before registration")
	}
	codeHash := k.evm.GetCodeHash(ctx, common.BytesToAddress(addr))
	if codeHash == (common.Hash{}) || evmtypes.IsEmptyCodeHash(codeHash.Bytes()) {
		return nil
	}
	return errorsmod.Wrapf(
		sdkerrors.ErrInvalidRequest,
		"account %s has EVM code (code hash %s), for example an EIP-7702 delegation; clear it with a SetCode authorisation to 0x0000000000000000000000000000000000000000 before registering an ML-DSA-87 key",
		signer, codeHash.Hex(),
	)
}

// revokeERC20Allowances deletes ERC-20 precompile allowances owned by addr,
// so an approval made with the classical key alone cannot be spent after
// registration.
func (k Keeper) revokeERC20Allowances(ctx sdk.Context, addr sdk.AccAddress) error {
	if k.erc20 == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "pqauth ERC-20 keeper must be set before registration")
	}
	if _, err := k.erc20.DeleteOwnerAllowances(ctx, common.BytesToAddress(addr)); err != nil {
		return errorsmod.Wrapf(err, "delete ERC-20 allowances owned by %s", addr)
	}
	return nil
}
