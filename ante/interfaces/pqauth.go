package interfaces

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PQAuthKeeper is the subset of x/pqauth used by the ante handlers.
type PQAuthKeeper interface {
	GetMLDSAPubKey(ctx sdk.Context, addr sdk.AccAddress) ([]byte, bool)
	IsEVMWindowOpen(ctx sdk.Context, addr sdk.AccAddress) bool
	RestrictCosmosPrecompiles(ctx sdk.Context) bool
}
