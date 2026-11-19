package evm_test

import (
	"bytes"
	"crypto/ecdsa"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
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

func signedAuth(t *testing.T, key *ecdsa.PrivateKey, chainID uint64, target common.Address, nonce uint64) ethtypes.SetCodeAuthorization {
	t.Helper()
	auth, err := ethtypes.SignSetCode(key, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.NewInt(chainID),
		Address: target,
		Nonce:   nonce,
	})
	require.NoError(t, err)
	return auth
}

// setCodeMsg builds a type-4 MsgEthereumTx sent by sender carrying auths.
func setCodeMsg(sender sdk.AccAddress, auths ...ethtypes.SetCodeAuthorization) *evmtypes.MsgEthereumTx {
	to := common.Address{}
	tx := ethtypes.NewTx(&ethtypes.SetCodeTx{
		ChainID:   uint256.NewInt(9999),
		Gas:       100_000,
		GasFeeCap: uint256.NewInt(1),
		GasTipCap: uint256.NewInt(1),
		To:        to,
		Value:     uint256.NewInt(0),
		AuthList:  auths,
	})
	msg := &evmtypes.MsgEthereumTx{From: sender.Bytes()}
	msg.FromEthereumTx(tx)
	return msg
}

func TestEVMWindowDecoratorSetCodeAuthorities(t *testing.T) {
	regKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	unregKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	registered := sdk.AccAddress(crypto.PubkeyToAddress(regKey.PublicKey).Bytes())
	unregistered := sdk.AccAddress(crypto.PubkeyToAddress(unregKey.PublicKey).Bytes())
	relayer := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	target := common.HexToAddress("0x0000000000000000000000000000000000001234")

	pub, _, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)
	pubBz, err := pub.MarshalBinary()
	require.NoError(t, err)

	ak := windowAccountKeeper{accs: map[string]sdk.AccountI{
		registered.String():   authtypes.NewBaseAccountWithAddress(registered),
		unregistered.String(): authtypes.NewBaseAccountWithAddress(unregistered),
		relayer.String():      authtypes.NewBaseAccountWithAddress(relayer),
	}}
	storeKey := storetypes.NewKVStoreKey(pqauthtypes.StoreKey)
	ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("transient-setcode")).WithBlockHeight(50)
	pq := keeper.NewKeeper(storeKey, ak)
	require.NoError(t, pq.SetMLDSAPubKey(ctx, registered, pubBz))
	dec := evm.NewEVMWindowDecorator(ak, pq)

	regAuth := signedAuth(t, regKey, 9999, target, 0)
	unregAuth := signedAuth(t, unregKey, 9999, target, 0)

	// Relayer-submitted type-4 with a registered authority, window closed: reject.
	called, err := runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{setCodeMsg(relayer, regAuth)}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Contains(t, err.Error(), "EIP-7702")
	require.False(t, called)

	// Mixed list: one bad authority rejects the whole tx.
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{setCodeMsg(relayer, unregAuth, regAuth)}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)

	// Unregistered authority: allowed.
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{setCodeMsg(relayer, unregAuth)}})
	require.NoError(t, err)
	require.True(t, called)

	// Unrecoverable signature is skipped (the EVM never applies it).
	bad := regAuth
	bad.R = *uint256.NewInt(0)
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{setCodeMsg(relayer, bad)}})
	require.NoError(t, err)
	require.True(t, called)

	// Open the registered account's window: allowed.
	require.NoError(t, pq.SetEVMWindowExpiry(ctx, registered, ctx.BlockHeight()+10))
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{setCodeMsg(relayer, regAuth)}})
	require.NoError(t, err)
	require.True(t, called)
}
