package keeper

import (
	"context"
	"fmt"

	"github.com/cosmos/evm/x/pqauth/types"
	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/log/v2"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AccountKeeper is the subset of the auth keeper used to load the signing account.
type AccountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}

// Keeper stores ML-DSA-87 public keys and EVM authorisation windows.
type Keeper struct {
	storeKey      storetypes.StoreKey
	accountKeeper AccountKeeper
	authz         AuthzKeeper
	fees          FeeAllowanceRevoker
	staking       StakingKeeper
	evm           EVMCodeKeeper
	erc20         ERC20AllowanceKeeper
	valset        ValidatorSetKeeper
}

// NewKeeper returns a pqauth keeper.
func NewKeeper(storeKey storetypes.StoreKey, accountKeeper AccountKeeper) Keeper {
	if accountKeeper == nil {
		panic("pqauth account keeper is nil")
	}
	return Keeper{
		storeKey:      storeKey,
		accountKeeper: accountKeeper,
	}
}

// Logger returns a module logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", types.ModuleName)
}

// GetParams returns the stored module parameters.
// A missing or zero duration is the default of 100 blocks.
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	bz := ctx.KVStore(k.storeKey).Get(types.ParamsKey())
	if len(bz) == 0 {
		return types.DefaultParams()
	}
	var params types.Params
	if err := proto.Unmarshal(bz, &params); err != nil {
		panic(fmt.Errorf("pqauth params in state are corrupt: %w", err))
	}
	if params.EvmWindowDuration == 0 {
		params.EvmWindowDuration = types.DefaultEVMWindowDuration
	}
	return params
}

// SetParams stores the module parameters.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	if params.EvmWindowDuration == 0 {
		params.EvmWindowDuration = types.DefaultEVMWindowDuration
	}
	if err := params.Validate(); err != nil {
		return err
	}
	bz, err := proto.Marshal(&params)
	if err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set(types.ParamsKey(), bz)
	return nil
}

// HasMLDSAPubKey reports whether the account already has a registered key.
func (k Keeper) HasMLDSAPubKey(ctx sdk.Context, addr sdk.AccAddress) bool {
	return ctx.KVStore(k.storeKey).Has(types.MLDSAPubKeyStoreKey(addr))
}

// GetMLDSAPubKey returns the registered key and true when one is stored.
func (k Keeper) GetMLDSAPubKey(ctx sdk.Context, addr sdk.AccAddress) ([]byte, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.MLDSAPubKeyStoreKey(addr))
	if len(bz) == 0 {
		return nil, false
	}
	out := make([]byte, len(bz))
	copy(out, bz)
	return out, true
}

// SetMLDSAPubKey stores a validated ML-DSA-87 public key for the account.
// A second call for the same account fails. The caller has already checked
// that the account exists.
func (k Keeper) SetMLDSAPubKey(ctx sdk.Context, addr sdk.AccAddress, pubKey []byte) error {
	if err := types.ValidateMLDSAPubKey(pubKey); err != nil {
		return err
	}
	if k.HasMLDSAPubKey(ctx, addr) {
		return types.ErrMLDSAKeyExists
	}

	stored := make([]byte, len(pubKey))
	copy(stored, pubKey)
	ctx.KVStore(k.storeKey).Set(types.MLDSAPubKeyStoreKey(addr), stored)
	return nil
}

// ReplaceMLDSAPubKey overwrites the account's registered ML-DSA-87 public key.
// The account must already have one.
func (k Keeper) ReplaceMLDSAPubKey(ctx sdk.Context, addr sdk.AccAddress, pubKey []byte) error {
	if err := types.ValidateMLDSAPubKey(pubKey); err != nil {
		return err
	}
	if !k.HasMLDSAPubKey(ctx, addr) {
		return types.ErrMLDSAKeyNotFound
	}
	stored := make([]byte, len(pubKey))
	copy(stored, pubKey)
	ctx.KVStore(k.storeKey).Set(types.MLDSAPubKeyStoreKey(addr), stored)
	return nil
}

// GetValidatorMLDSAPubKey returns the key bound to a consensus address.
func (k Keeper) GetValidatorMLDSAPubKey(ctx sdk.Context, consAddr sdk.ConsAddress) ([]byte, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.ValidatorMLDSAStoreKey(consAddr))
	if len(bz) == 0 {
		return nil, false
	}
	out := make([]byte, len(bz))
	copy(out, bz)
	return out, true
}

// SetValidatorMLDSAPubKey stores a validator key. A second call for the same
// consensus address fails.
func (k Keeper) SetValidatorMLDSAPubKey(ctx sdk.Context, consAddr sdk.ConsAddress, pubKey []byte) error {
	if len(consAddr) != types.ConsensusAddressSize {
		return types.ErrInvalidValidator
	}
	if err := types.ValidateMLDSAPubKey(pubKey); err != nil {
		return err
	}
	if _, found := k.GetValidatorMLDSAPubKey(ctx, consAddr); found {
		return types.ErrMLDSAKeyExists
	}
	stored := make([]byte, len(pubKey))
	copy(stored, pubKey)
	ctx.KVStore(k.storeKey).Set(types.ValidatorMLDSAStoreKey(consAddr), stored)
	return nil
}

// ReplaceValidatorMLDSAPubKey overwrites a validator key that is already stored.
func (k Keeper) ReplaceValidatorMLDSAPubKey(ctx sdk.Context, consAddr sdk.ConsAddress, pubKey []byte) error {
	if _, found := k.GetValidatorMLDSAPubKey(ctx, consAddr); !found {
		return types.ErrMLDSAKeyNotFound
	}
	if err := types.ValidateMLDSAPubKey(pubKey); err != nil {
		return err
	}
	stored := make([]byte, len(pubKey))
	copy(stored, pubKey)
	ctx.KVStore(k.storeKey).Set(types.ValidatorMLDSAStoreKey(consAddr), stored)
	return nil
}

// RestrictCosmosPrecompiles reports whether an open EVM window still rejects
// top-level bank, staking, and gov precompile calls from registered accounts.
func (k Keeper) RestrictCosmosPrecompiles(ctx sdk.Context) bool {
	return k.GetParams(ctx).RestrictCosmosPrecompiles
}

// GetEVMWindowExpiry returns the stored exclusive expiry height.
func (k Keeper) GetEVMWindowExpiry(ctx sdk.Context, addr sdk.AccAddress) (int64, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.EVMWindowStoreKey(addr))
	if len(bz) == 0 {
		return 0, false
	}
	expiry, err := types.DecodeEVMWindowExpiry(bz)
	if err != nil {
		panic(err)
	}
	return expiry, true
}

// SetEVMWindowExpiry stores an exclusive expiry height for the account.
// A later call replaces the stored height, which is how a window is refreshed.
func (k Keeper) SetEVMWindowExpiry(ctx sdk.Context, addr sdk.AccAddress, expiry int64) error {
	bz, err := types.EncodeEVMWindowExpiry(expiry)
	if err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set(types.EVMWindowStoreKey(addr), bz)
	return nil
}

// IsEVMWindowOpen reports whether the account's window is open at this height.
// The window is open while the current height is strictly less than the stored expiry.
func (k Keeper) IsEVMWindowOpen(ctx sdk.Context, addr sdk.AccAddress) bool {
	expiry, found := k.GetEVMWindowExpiry(ctx, addr)
	return found && types.EVMWindowOpen(ctx.BlockHeight(), expiry)
}

// InitGenesis stores genesis parameters and bindings. Each account must already exist.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) error {
	if err := k.SetParams(ctx, gs.Params); err != nil {
		return err
	}

	for _, key := range gs.Keys {
		addr, err := sdk.AccAddressFromBech32(key.Account)
		if err != nil {
			return err
		}
		if k.accountKeeper.GetAccount(ctx, addr) == nil {
			return types.ErrAccountNotFound
		}
		if err := k.SetMLDSAPubKey(ctx, addr, key.PubKey); err != nil {
			return err
		}
	}

	seenWindows := make(map[string]struct{}, len(gs.Windows))
	for _, window := range gs.Windows {
		addr, err := sdk.AccAddressFromBech32(window.Account)
		if err != nil {
			return err
		}
		if k.accountKeeper.GetAccount(ctx, addr) == nil {
			return types.ErrAccountNotFound
		}
		id := string(addr)
		if _, ok := seenWindows[id]; ok {
			return types.ErrInvalidEVMWindow
		}
		seenWindows[id] = struct{}{}
		if err := k.SetEVMWindowExpiry(ctx, addr, window.ExpiryHeight); err != nil {
			return err
		}
	}

	seenValidators := make(map[string]struct{}, len(gs.ValidatorKeys))
	for _, key := range gs.ValidatorKeys {
		id := string(key.ConsensusAddress)
		if _, ok := seenValidators[id]; ok {
			return types.ErrMLDSAKeyExists
		}
		seenValidators[id] = struct{}{}
		if err := k.SetValidatorMLDSAPubKey(ctx, sdk.ConsAddress(key.ConsensusAddress), key.PubKey); err != nil {
			return err
		}
	}
	return nil
}

// IterateMLDSAPubKeys walks every registered binding. The callback's key and
// address arguments must not be retained after it returns.
func (k Keeper) IterateMLDSAPubKeys(ctx sdk.Context, cb func(addr sdk.AccAddress, pubKey []byte) bool) {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, []byte{types.KeyPrefixMLDSAPubKey})
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		rawKey := iterator.Key()
		addr := sdk.AccAddress(rawKey[1:])
		pubKey := iterator.Value()
		if cb(addr, pubKey) {
			break
		}
	}
}

// IterateEVMWindows walks every stored window. The callback's address argument
// must not be retained after it returns.
func (k Keeper) IterateEVMWindows(ctx sdk.Context, cb func(addr sdk.AccAddress, expiry int64) bool) {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, []byte{types.KeyPrefixEVMWindow})
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		rawKey := iterator.Key()
		addr := sdk.AccAddress(rawKey[1:])
		expiry, err := types.DecodeEVMWindowExpiry(iterator.Value())
		if err != nil {
			panic(err)
		}
		if cb(addr, expiry) {
			break
		}
	}
}

// IterateValidatorMLDSAPubKeys walks every validator binding. The callback
// must not retain pubKey. consAddr is a copy.
func (k Keeper) IterateValidatorMLDSAPubKeys(ctx sdk.Context, cb func(consAddr sdk.ConsAddress, pubKey []byte) bool) {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, []byte{types.KeyPrefixValidatorMLDSA})
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		rawKey := iterator.Key()
		consAddr := make(sdk.ConsAddress, len(rawKey)-1)
		copy(consAddr, rawKey[1:])
		pubKey := iterator.Value()
		if cb(consAddr, pubKey) {
			break
		}
	}
}
