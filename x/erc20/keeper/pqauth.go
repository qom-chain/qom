package keeper

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/evm/x/erc20/types"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/store/v2/prefix"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// SetPQAuthKeeper attaches the pqauth keeper that gates ERC-20 precompile
// allowances. Call it once during app construction, before the keeper value
// is copied into other keepers or modules.
func (k *Keeper) SetPQAuthKeeper(pq types.PQAuthKeeper) {
	k.pqAuthKeeper = pq
}

// CheckOwnerEVMWindow rejects an ERC-20 precompile allowance operation
// (approve by the owner, or transferFrom out of the owner's balance) when
// the owner has a registered ML-DSA-87 key and its EVM authorisation window
// is closed. Unregistered owners, and chains without x/pqauth, are not
// affected. Below the pqauth hardening v2 activation height it always returns
// nil (32ee354 behaviour).
func (k Keeper) CheckOwnerEVMWindow(ctx sdk.Context, owner common.Address) error {
	if k.pqAuthKeeper == nil || !pqauthtypes.HardeningV2Active(ctx.BlockHeight()) {
		return nil
	}
	addr := sdk.AccAddress(owner.Bytes())
	if _, registered := k.pqAuthKeeper.GetMLDSAPubKey(ctx, addr); !registered {
		return nil
	}
	if k.pqAuthKeeper.IsEVMWindowOpen(ctx, addr) {
		return nil
	}
	return errorsmod.Wrapf(
		errortypes.ErrUnauthorized,
		"EVM authorisation window is closed for ERC-20 owner %s; this account has a registered ML-DSA-87 key, so its allowances cannot be set or spent until it sends MsgOpenEVMWindow",
		owner.Hex(),
	)
}

// DeleteOwnerAllowances deletes every ERC-20 precompile allowance owned by
// owner, across all registered token pairs, including disabled pairs. The
// work is bounded: one prefix scan of erc20||owner per token pair, in store
// order. It returns the number of allowances deleted.
func (k Keeper) DeleteOwnerAllowances(ctx sdk.Context, owner common.Address) (int, error) {
	if (owner == common.Address{}) {
		return 0, errorsmod.Wrap(errortypes.ErrInvalidAddress, "owner address is empty")
	}
	var contracts []common.Address
	k.IterateTokenPairs(ctx, func(pair types.TokenPair) bool {
		contracts = append(contracts, pair.GetERC20Contract())
		return false
	})

	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixAllowance)
	deleted := 0
	for _, contract := range contracts {
		ownerPrefix := append(append([]byte{}, contract.Bytes()...), owner.Bytes()...)
		var keys [][]byte
		iterator := storetypes.KVStorePrefixIterator(store, ownerPrefix)
		for ; iterator.Valid(); iterator.Next() {
			keys = append(keys, append([]byte{}, iterator.Key()...))
		}
		if err := iterator.Close(); err != nil {
			return deleted, err
		}
		for _, key := range keys {
			store.Delete(key)
			deleted++
		}
	}
	return deleted, nil
}
