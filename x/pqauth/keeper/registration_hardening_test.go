package keeper_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/cosmos/evm/x/pqauth/keeper"
	"github.com/cosmos/evm/x/pqauth/types"

	"cosmossdk.io/core/address"

	"github.com/cosmos/cosmos-sdk/baseapp"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
)

// delegationCodeHash is an arbitrary non-empty code hash, standing in for the
// hash of an EIP-7702 designator 0xef0100||target.
var delegationCodeHash = common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")

type regFixture struct {
	addr    sdk.AccAddress
	ethPub  []byte
	pubKey  []byte
	pop     []byte
	account sdk.AccountI
}

func newRegFixture(t *testing.T) regFixture {
	t.Helper()
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	ethPub := ethPriv.PubKey()
	addr := sdk.AccAddress(ethPub.Address())
	mldsaPriv, pubKey := newMLDSAKey(t)
	return regFixture{
		addr:    addr,
		ethPub:  ethPub.Bytes(),
		pubKey:  pubKey,
		pop:     accountPop(t, mldsaPriv, addr, ethPub.Bytes(), pubKey),
		account: authtypes.NewBaseAccount(addr, ethPub, 1, 0),
	}
}

func (f regFixture) msg() *types.MsgRegisterMLDSAKey {
	return &types.MsgRegisterMLDSAKey{Signer: f.addr.String(), PubKey: f.pubKey, PopSignature: f.pop}
}

func TestRegisterMLDSAKeyRejectsAccountWithEVMCode(t *testing.T) {
	f := newRegFixture(t)
	ctx, k := setupKeeper(t, f.account)
	owner := common.BytesToAddress(f.addr)
	evm := memEVM{codeHashes: map[common.Address]common.Hash{owner: delegationCodeHash}}
	erc20 := &memERC20{allowances: map[common.Address]int{owner: 2}}
	k.SetEVMKeepers(evm, erc20)

	gasBefore := ctx.GasMeter().GasConsumed()
	_, err := k.RegisterMLDSAKey(ctx, f.msg())
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
	require.Contains(t, err.Error(), "has EVM code")
	require.Contains(t, err.Error(), "EIP-7702")
	_, found := k.GetMLDSAPubKey(ctx, f.addr)
	require.False(t, found)
	// Rejected before the ML-DSA PoP verification is charged, and before any
	// allowance is touched.
	require.Less(t, ctx.GasMeter().GasConsumed()-gasBefore, uint64(250_000))
	require.Empty(t, erc20.deleted)

	// A zero code hash is treated as no code.
	evm.codeHashes[owner] = common.Hash{}
	_, err = k.RegisterMLDSAKey(ctx, f.msg())
	require.NoError(t, err)
	_, found = k.GetMLDSAPubKey(ctx, f.addr)
	require.True(t, found)
}

func TestRegisterMLDSAKeyAfterDelegationCleared(t *testing.T) {
	f := newRegFixture(t)
	ctx, k := setupKeeper(t, f.account)
	owner := common.BytesToAddress(f.addr)
	evm := memEVM{codeHashes: map[common.Address]common.Hash{owner: delegationCodeHash}}
	k.SetEVMKeepers(evm, &memERC20{})

	_, err := k.RegisterMLDSAKey(ctx, f.msg())
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)

	// Clearing the delegation (code hash back to empty) allows registration.
	delete(evm.codeHashes, owner)
	_, err = k.RegisterMLDSAKey(ctx, f.msg())
	require.NoError(t, err)
}

func TestRegisterMLDSAKeyOtherAccountCodeDoesNotMatter(t *testing.T) {
	f := newRegFixture(t)
	ctx, k := setupKeeper(t, f.account)
	other := common.BytesToAddress(bytes20())
	k.SetEVMKeepers(memEVM{codeHashes: map[common.Address]common.Hash{other: delegationCodeHash}}, &memERC20{})
	_, err := k.RegisterMLDSAKey(ctx, f.msg())
	require.NoError(t, err)
}

func TestRegisterMLDSAKeyFailsClosedWithoutEVMKeepers(t *testing.T) {
	f := newRegFixture(t)

	ctx, k := setupKeeper(t, f.account)
	k.SetEVMKeepers(nil, &memERC20{})
	_, err := k.RegisterMLDSAKey(ctx, f.msg())
	require.ErrorIs(t, err, sdkerrors.ErrLogic)
	require.Contains(t, err.Error(), "EVM keeper")
	_, found := k.GetMLDSAPubKey(ctx, f.addr)
	require.False(t, found)

	ctx, k = setupKeeper(t, f.account)
	k.SetEVMKeepers(memEVM{}, nil)
	_, err = k.RegisterMLDSAKey(ctx, f.msg())
	require.ErrorIs(t, err, sdkerrors.ErrLogic)
	require.Contains(t, err.Error(), "ERC-20 keeper")
	_, found = k.GetMLDSAPubKey(ctx, f.addr)
	require.False(t, found)
}

func TestRegisterMLDSAKeyDeletesOwnedERC20Allowances(t *testing.T) {
	f := newRegFixture(t)
	ctx, k := setupKeeper(t, f.account)
	owner := common.BytesToAddress(f.addr)
	other := common.BytesToAddress(bytes20())
	erc20 := &memERC20{allowances: map[common.Address]int{owner: 3, other: 1}}
	k.SetEVMKeepers(memEVM{}, erc20)

	_, err := k.RegisterMLDSAKey(ctx, f.msg())
	require.NoError(t, err)
	require.Equal(t, []common.Address{owner}, erc20.deleted)
	require.NotContains(t, erc20.allowances, owner)
	require.Equal(t, 1, erc20.allowances[other])
}

func TestRegisterMLDSAKeyPropagatesERC20AllowanceError(t *testing.T) {
	f := newRegFixture(t)
	ctx, k := setupKeeper(t, f.account)
	k.SetEVMKeepers(memEVM{}, &memERC20{err: errors.New("boom")})

	_, err := k.RegisterMLDSAKey(ctx, f.msg())
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete ERC-20 allowances")
	_, found := k.GetMLDSAPubKey(ctx, f.addr)
	require.False(t, found)
}

func TestRegisterMLDSAKeyBadProofDoesNotDeleteAllowances(t *testing.T) {
	f := newRegFixture(t)
	ctx, k := setupKeeper(t, f.account)
	owner := common.BytesToAddress(f.addr)
	erc20 := &memERC20{allowances: map[common.Address]int{owner: 1}}
	k.SetEVMKeepers(memEVM{}, erc20)

	msg := f.msg()
	msg.PopSignature = dummyPop()
	_, err := k.RegisterMLDSAKey(ctx, msg)
	require.ErrorIs(t, err, types.ErrInvalidProof)
	require.Empty(t, erc20.deleted)
}

type authzAccountStub struct {
	accountKeeperStub
	codec address.Codec
}

func (s authzAccountStub) AddressCodec() address.Codec { return s.codec }

func (s authzAccountStub) NewAccountWithAddress(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return authtypes.NewBaseAccountWithAddress(addr)
}

func (s authzAccountStub) SetAccount(context.Context, sdk.AccountI) {}

// TestRegisterMLDSAKeyRevokesGrantsWithSDKAuthzKeeper runs registration
// against the real SDK authz keeper, so the bounded GranterGrants lookup is
// checked against the SDK's store layout and pagination. The registrant has
// more grants than one page; other granters' grants must survive.
func TestRegisterMLDSAKeyRevokesGrantsWithSDKAuthzKeeper(t *testing.T) {
	f := newRegFixture(t)

	pqKey := storetypes.NewKVStoreKey(types.StoreKey)
	authzKey := storetypes.NewKVStoreKey(authzkeeper.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{types.StoreKey: pqKey, authzkeeper.StoreKey: authzKey},
		map[string]*storetypes.TransientStoreKey{"transient_test": storetypes.NewTransientStoreKey("transient_test")},
		nil,
	).WithChainID(testChainID).WithBlockTime(time.Unix(1_700_000_000, 0))

	accounts := accountKeeperStub{accounts: map[string]sdk.AccountI{f.addr.String(): f.account}}
	encCfg := moduletestutil.MakeTestEncodingConfig(authzmodule.AppModuleBasic{})
	ak := authzAccountStub{
		accountKeeperStub: accounts,
		codec:             addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
	}
	authzK := authzkeeper.NewKeeper(runtime.NewKVStoreService(authzKey), encCfg.Codec, baseapp.NewMsgServiceRouter(), ak)

	k := keeper.NewKeeper(pqKey, accounts)
	k.SetDelegationKeepers(authzK, nopFees{}, nil)
	k.SetEVMKeepers(memEVM{}, &memERC20{})

	msgTypes := []string{
		sdk.MsgTypeURL(&types.MsgOpenEVMWindow{}),
		sdk.MsgTypeURL(&types.MsgRotateMLDSAKey{}),
	}
	expiry := time.Unix(1_800_000_000, 0)
	grantee := func(i int) sdk.AccAddress {
		b := make([]byte, 20)
		b[0] = 0x77
		b[18] = byte(i >> 8)
		b[19] = byte(i)
		return b
	}
	const registrantGrantees = 130 // 260 grants: three pages of 100
	for i := 0; i < registrantGrantees; i++ {
		for _, mt := range msgTypes {
			require.NoError(t, authzK.SaveGrant(ctx, grantee(i), f.addr, authz.NewGenericAuthorization(mt), &expiry))
		}
	}
	otherGranter := sdk.AccAddress(bytes20())
	require.NoError(t, authzK.SaveGrant(ctx, grantee(1), otherGranter, authz.NewGenericAuthorization(msgTypes[0]), nil))
	// A grant to the registrant (registrant is grantee) is not revoked.
	require.NoError(t, authzK.SaveGrant(ctx, f.addr, otherGranter, authz.NewGenericAuthorization(msgTypes[1]), nil))

	before, err := authzK.GranterGrants(ctx, &authz.QueryGranterGrantsRequest{Granter: f.addr.String()})
	require.NoError(t, err)
	require.Len(t, before.Grants, 100) // default page

	_, err = k.RegisterMLDSAKey(ctx, f.msg())
	require.NoError(t, err)

	after, err := authzK.GranterGrants(ctx, &authz.QueryGranterGrantsRequest{Granter: f.addr.String()})
	require.NoError(t, err)
	require.Empty(t, after.Grants)

	others, err := authzK.GranterGrants(ctx, &authz.QueryGranterGrantsRequest{Granter: otherGranter.String()})
	require.NoError(t, err)
	require.Len(t, others.Grants, 2)

	// Expiry-queue entries were removed with the grants: pruning finds nothing
	// left to delete for the registrant and does not error.
	require.NoError(t, authzK.DequeueAndDeleteExpiredGrants(ctx.WithBlockTime(expiry.Add(time.Hour))))
}
