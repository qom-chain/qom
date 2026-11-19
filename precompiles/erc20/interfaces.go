package erc20

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// revive:disable-next-line exported
type Erc20Keeper interface {
	GetAllowance(ctx sdk.Context, erc20 common.Address, owner common.Address, spender common.Address) (*big.Int, error)
	SetAllowance(ctx sdk.Context, erc20 common.Address, owner common.Address, spender common.Address, value *big.Int) error
	DeleteAllowance(ctx sdk.Context, erc20 common.Address, owner common.Address, spender common.Address) error
	// CheckOwnerEVMWindow rejects approve and transferFrom for an owner with
	// a registered ML-DSA-87 key whose EVM authorisation window is closed.
	CheckOwnerEVMWindow(ctx sdk.Context, owner common.Address) error
}
