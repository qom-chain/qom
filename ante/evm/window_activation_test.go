package evm_test

import (
	"bytes"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/ante/evm"
	"github.com/cosmos/evm/x/pqauth/keeper"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Below the activation height the decorator must behave exactly like 32ee354:
// a registered authority with an open window may delegate anywhere, and a
// delegated registered recipient is not checked. From the activation height
// both are rejected.
func TestEVMWindowDecoratorActivationHeight(t *testing.T) {
	const activation = 100
	restore := pqauthtypes.SetHardeningV2ActivationHeightForTesting(activation)
	defer restore()

	regKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	registered := sdk.AccAddress(crypto.PubkeyToAddress(regKey.PublicKey).Bytes())
	relayer := sdk.AccAddress(bytes.Repeat([]byte{0x51}, 20))
	delegated := sdk.AccAddress(bytes.Repeat([]byte{0x52}, 20))
	target := common.HexToAddress("0x0000000000000000000000000000000000004321")

	pub, _, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)
	pubBz, err := pub.MarshalBinary()
	require.NoError(t, err)

	ak := windowAccountKeeper{accs: map[string]sdk.AccountI{
		registered.String(): authtypes.NewBaseAccountWithAddress(registered),
		relayer.String():    authtypes.NewBaseAccountWithAddress(relayer),
		delegated.String():  authtypes.NewBaseAccountWithAddress(delegated),
	}}
	storeKey := storetypes.NewKVStoreKey(pqauthtypes.StoreKey)
	base := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("transient-activation"))
	pq := keeper.NewKeeper(storeKey, ak)
	require.NoError(t, pq.SetMLDSAPubKey(base, registered, pubBz))
	require.NoError(t, pq.SetMLDSAPubKey(base, delegated, pubBz))
	code := memCode{code: map[common.Address][]byte{
		common.BytesToAddress(delegated): ethtypes.AddressToDelegation(target),
	}}
	dec := evm.NewEVMWindowDecorator(ak, pq, code)
	auth := signedAuth(t, regKey, 9999, target, 0)
	to := common.BytesToAddress(delegated)

	below := base.WithBlockHeight(activation - 1)
	at := base.WithBlockHeight(activation)

	// Window closed: rejected on both sides (32ee354 window rule below; the
	// target rule fires first at activation).
	for _, ctx := range []sdk.Context{below, at} {
		called, err := runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{setCodeMsg(relayer, auth)}})
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
		require.False(t, called)
	}
	called, err := runWindow(dec, below, msgsTx{msgs: []sdk.Msg{setCodeMsg(relayer, auth)}})
	require.Contains(t, err.Error(), "window is closed")
	require.False(t, called)

	// Window open, non-zero target: allowed below, rejected at activation.
	require.NoError(t, pq.SetEVMWindowExpiry(base, registered, activation+10))
	called, err = runWindow(dec, below, msgsTx{msgs: []sdk.Msg{setCodeMsg(relayer, auth)}})
	require.NoError(t, err)
	require.True(t, called)
	called, err = runWindow(dec, at, msgsTx{msgs: []sdk.Msg{setCodeMsg(relayer, auth)}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Contains(t, err.Error(), "may only clear delegation")
	require.False(t, called)

	// Direct call to a delegated registered account with a closed window:
	// allowed below, rejected at activation.
	called, err = runWindow(dec, below, msgsTx{msgs: []sdk.Msg{callMsg(relayer, &to)}})
	require.NoError(t, err)
	require.True(t, called)
	called, err = runWindow(dec, at, msgsTx{msgs: []sdk.Msg{callMsg(relayer, &to)}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)
}
