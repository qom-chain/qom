package keeper

import (
	"context"

	"github.com/cosmos/evm/x/pqauth/types"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var _ types.QueryServer = Keeper{}

// MLDSAPubKey returns the ML-DSA-87 public key registered to an account.
func (k Keeper) MLDSAPubKey(goCtx context.Context, req *types.QueryMLDSAPubKeyRequest) (*types.QueryMLDSAPubKeyResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	addr, err := sdk.AccAddressFromBech32(req.Account)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid account address")
	}

	pubKey, found := k.GetMLDSAPubKey(ctx, addr)
	return &types.QueryMLDSAPubKeyResponse{
		PubKey:     pubKey,
		Registered: found,
	}, nil
}

// EVMWindow returns the account's stored expiry and whether it is open now.
func (k Keeper) EVMWindow(goCtx context.Context, req *types.QueryEVMWindowRequest) (*types.QueryEVMWindowResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	addr, err := sdk.AccAddressFromBech32(req.Account)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid account address")
	}

	expiry, found := k.GetEVMWindowExpiry(ctx, addr)
	if !found {
		return &types.QueryEVMWindowResponse{}, nil
	}
	return &types.QueryEVMWindowResponse{
		ExpiryHeight: expiry,
		Open:         types.EVMWindowOpen(ctx.BlockHeight(), expiry),
	}, nil
}

// Params returns the pqauth module parameters.
func (k Keeper) Params(goCtx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryParamsResponse{Params: k.GetParams(ctx)}, nil
}
