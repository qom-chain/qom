package pqauth

import (
	"github.com/cosmos/evm/x/pqauth/keeper"
	"github.com/cosmos/evm/x/pqauth/types"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis stores the genesis account → ML-DSA public key bindings.
// Auth genesis must already have created the accounts.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, gs types.GenesisState) {
	if err := k.InitGenesis(ctx, gs); err != nil {
		panic(errorsmod.Wrap(err, "could not init pqauth genesis"))
	}
}

// ExportGenesis returns the registered bindings in iteration order.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	gs := types.DefaultGenesisState()
	gs.Params = k.GetParams(ctx)
	k.IterateMLDSAPubKeys(ctx, func(addr sdk.AccAddress, pubKey []byte) bool {
		copied := make([]byte, len(pubKey))
		copy(copied, pubKey)
		gs.Keys = append(gs.Keys, types.MLDSAKey{
			Account: addr.String(),
			PubKey:  copied,
		})
		return false
	})
	k.IterateEVMWindows(ctx, func(addr sdk.AccAddress, expiry int64) bool {
		gs.Windows = append(gs.Windows, types.EVMWindow{
			Account:      addr.String(),
			ExpiryHeight: expiry,
		})
		return false
	})
	k.IterateValidatorMLDSAPubKeys(ctx, func(consAddr sdk.ConsAddress, pubKey []byte) bool {
		copied := make([]byte, len(pubKey))
		copy(copied, pubKey)
		address := make([]byte, len(consAddr))
		copy(address, consAddr)
		gs.ValidatorKeys = append(gs.ValidatorKeys, types.ValidatorMLDSAKey{
			ConsensusAddress: address,
			PubKey:           copied,
		})
		return false
	})
	return gs
}
