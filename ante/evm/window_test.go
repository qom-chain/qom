package evm_test

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

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

type windowAccountKeeper struct {
	accs map[string]sdk.AccountI
}

func (k windowAccountKeeper) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return k.accs[addr.String()]
}

type msgsTx struct {
	msgs []sdk.Msg
}

func (m msgsTx) GetMsgs() []sdk.Msg { return m.msgs }

func (m msgsTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

// namedMsg is a proto message whose type URL is derived from name.
type namedMsg struct {
	name string
}

func (namedMsg) Reset()         {}
func (namedMsg) String() string { return "" }
func (namedMsg) ProtoMessage()  {}

func (m namedMsg) XXX_MessageName() string { return m.name }

func ethTx(addr sdk.AccAddress) *evmtypes.MsgEthereumTx {
	return &evmtypes.MsgEthereumTx{From: addr.Bytes()}
}

func runWindow(dec evm.EVMWindowDecorator, ctx sdk.Context, tx sdk.Tx) (bool, error) {
	called := false
	_, err := dec.AnteHandle(ctx, tx, false, func(newCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return newCtx, nil
	})
	return called, err
}

func TestEVMWindowDecorator(t *testing.T) {
	user := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	other := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	mod := authtypes.NewEmptyModuleAccount("transfer")
	modAddr := mod.GetAddress()

	pub, _, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)
	pubBz, err := pub.MarshalBinary()
	require.NoError(t, err)

	ak := windowAccountKeeper{accs: map[string]sdk.AccountI{
		user.String():    authtypes.NewBaseAccountWithAddress(user),
		other.String():   authtypes.NewBaseAccountWithAddress(other),
		modAddr.String(): mod,
	}}
	storeKey := storetypes.NewKVStoreKey(pqauthtypes.StoreKey)
	ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("transient-window")).WithBlockHeight(50)
	pq := keeper.NewKeeper(storeKey, ak)
	require.NoError(t, pq.SetMLDSAPubKey(ctx, user, pubBz))
	require.NoError(t, pq.SetMLDSAPubKey(ctx, modAddr, pubBz))

	dec := evm.NewEVMWindowDecorator(ak, pq, memCode{})

	called, err := runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethTx(user)}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)

	require.NoError(t, pq.SetEVMWindowExpiry(ctx, user, ctx.BlockHeight()))
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethTx(user)}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)

	require.NoError(t, pq.SetEVMWindowExpiry(ctx, user, ctx.BlockHeight()+1))
	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethTx(user)}})
	require.NoError(t, err)
	require.True(t, called)

	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethTx(other)}})
	require.NoError(t, err)
	require.True(t, called)

	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethTx(modAddr)}})
	require.NoError(t, err)
	require.True(t, called)

	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{namedMsg{name: "ibc.core.channel.v1.MsgRecvPacket"}}})
	require.NoError(t, err)
	require.True(t, called)

	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{namedMsg{name: "cosmos.bank.v1beta1.MsgSend"}}})
	require.NoError(t, err)
	require.True(t, called)

	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethTx(nil)}})
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
	require.False(t, called)
}

func TestRestrictCosmosPrecompilesRejectsTopLevelBankStakingGov(t *testing.T) {
	user := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	pub, _, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)
	pubBz, err := pub.MarshalBinary()
	require.NoError(t, err)

	ak := windowAccountKeeper{accs: map[string]sdk.AccountI{
		user.String(): authtypes.NewBaseAccountWithAddress(user),
	}}
	storeKey := storetypes.NewKVStoreKey(pqauthtypes.StoreKey)
	ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("transient-restrict")).WithBlockHeight(50)
	pq := keeper.NewKeeper(storeKey, ak)
	require.NoError(t, pq.SetMLDSAPubKey(ctx, user, pubBz))
	require.NoError(t, pq.SetEVMWindowExpiry(ctx, user, ctx.BlockHeight()+10))
	require.NoError(t, pq.SetParams(ctx, pqauthtypes.Params{
		EvmWindowDuration:         100,
		RestrictCosmosPrecompiles: true,
	}))
	dec := evm.NewEVMWindowDecorator(ak, pq, memCode{})

	for _, target := range []string{
		evmtypes.BankPrecompileAddress,
		evmtypes.StakingPrecompileAddress,
		evmtypes.GovPrecompileAddress,
	} {
		called, err := runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethCall(user, target)}})
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
		require.False(t, called)
	}

	called, err := runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethCall(user, evmtypes.DistributionPrecompileAddress)}})
	require.NoError(t, err)
	require.True(t, called)

	called, err = runWindow(dec, ctx, msgsTx{msgs: []sdk.Msg{ethTx(user)}})
	require.NoError(t, err)
	require.True(t, called)
}

func ethCall(from sdk.AccAddress, to string) *evmtypes.MsgEthereumTx {
	target := common.HexToAddress(to)
	msg := evmtypes.NewTx(&evmtypes.EvmTxArgs{
		To:       &target,
		Amount:   big.NewInt(0),
		GasLimit: 21_000,
		GasPrice: big.NewInt(1),
	})
	msg.From = from.Bytes()
	return msg
}
