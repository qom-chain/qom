package evm_test

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/ante/evm"
	"github.com/cosmos/evm/x/pqauth/keeper"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// callMsg builds a dynamic-fee MsgEthereumTx from sender to to.
func callMsg(sender sdk.AccAddress, to *common.Address) *evmtypes.MsgEthereumTx {
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   big9999(),
		Gas:       100_000,
		GasFeeCap: common.Big1,
		GasTipCap: common.Big1,
		To:        to,
		Value:     common.Big0,
	})
	msg := &evmtypes.MsgEthereumTx{From: sender.Bytes()}
	msg.FromEthereumTx(tx)
	return msg
}

func TestEVMWindowDecoratorDelegatedRecipient(t *testing.T) {
	registered := sdk.AccAddress(bytes.Repeat([]byte{0x41}, 20))
	plainRegistered := sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20))
	unregDelegated := sdk.AccAddress(bytes.Repeat([]byte{0x43}, 20))
	contractRegistered := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	caller := sdk.AccAddress(bytes.Repeat([]byte{0x45}, 20))
	delegateTarget := common.HexToAddress("0x000000000000000000000000000000000000beef")

	pub, _, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)
	pubBz, err := pub.MarshalBinary()
	require.NoError(t, err)

	ak := windowAccountKeeper{accs: map[string]sdk.AccountI{
		registered.String():         authtypes.NewBaseAccountWithAddress(registered),
		plainRegistered.String():    authtypes.NewBaseAccountWithAddress(plainRegistered),
		unregDelegated.String():     authtypes.NewBaseAccountWithAddress(unregDelegated),
		contractRegistered.String(): authtypes.NewBaseAccountWithAddress(contractRegistered),
		caller.String():             authtypes.NewBaseAccountWithAddress(caller),
	}}
	storeKey := storetypes.NewKVStoreKey(pqauthtypes.StoreKey)
	ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("transient-delegated")).WithBlockHeight(50)
	pq := keeper.NewKeeper(storeKey, ak)
	for _, a := range []sdk.AccAddress{registered, plainRegistered, contractRegistered} {
		require.NoError(t, pq.SetMLDSAPubKey(ctx, a, pubBz))
	}

	code := memCode{code: map[common.Address][]byte{
		common.BytesToAddress(registered):         ethtypes.AddressToDelegation(delegateTarget),
		common.BytesToAddress(unregDelegated):     ethtypes.AddressToDelegation(delegateTarget),
		common.BytesToAddress(contractRegistered): {0x60, 0x00, 0x60, 0x00}, // not a designator
	}}
	dec := evm.NewEVMWindowDecorator(ak, pq, code)

	to := func(a sdk.AccAddress) *common.Address {
		addr := common.BytesToAddress(a)
		return &addr
	}

	// Registered + delegated + window closed: reject.
	called, err := runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{callMsg(caller, to(registered))}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Contains(t, err.Error(), "EIP-7702 delegation")
	require.False(t, called)

	// Registered without code: a normal transfer to it is allowed.
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{callMsg(caller, to(plainRegistered))}})
	require.NoError(t, err)
	require.True(t, called)

	// Unregistered + delegated: allowed (normal EIP-7702 behaviour).
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{callMsg(caller, to(unregDelegated))}})
	require.NoError(t, err)
	require.True(t, called)

	// Registered with non-designator code: not this rule's concern.
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{callMsg(caller, to(contractRegistered))}})
	require.NoError(t, err)
	require.True(t, called)

	// Contract creation has no recipient.
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{callMsg(caller, nil)}})
	require.NoError(t, err)
	require.True(t, called)

	// Window open: the owner has authorised Ethereum activity, allowed.
	require.NoError(t, pq.SetEVMWindowExpiry(ctx, registered, ctx.BlockHeight()+5))
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{callMsg(caller, to(registered))}})
	require.NoError(t, err)
	require.True(t, called)

	// Window expired again: reject.
	called, err = runWindow(dec, ctx.WithBlockHeight(55), msgsTx{msgs: []sdk.Msg{callMsg(caller, to(registered))}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)
}

func TestEVMWindowDecoratorNilCodeKeeper(t *testing.T) {
	user := sdk.AccAddress(bytes.Repeat([]byte{0x46}, 20))
	ak := windowAccountKeeper{accs: map[string]sdk.AccountI{user.String(): authtypes.NewBaseAccountWithAddress(user)}}
	storeKey := storetypes.NewKVStoreKey(pqauthtypes.StoreKey)
	ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("transient-nilcode"))
	pq := keeper.NewKeeper(storeKey, ak)
	dec := evm.NewEVMWindowDecorator(ak, pq, nil)
	called, err := runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethTx(user)}})
	require.ErrorIs(t, err, sdkerrors.ErrLogic)
	require.False(t, called)
}

func big9999() *big.Int { return big.NewInt(9999) }
