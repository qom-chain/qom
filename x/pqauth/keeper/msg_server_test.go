package keeper_test

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
	pqauth "github.com/cosmos/evm/x/pqauth"
	"github.com/cosmos/evm/x/pqauth/keeper"
	"github.com/cosmos/evm/x/pqauth/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
)

const testChainID = "qomx-test"

type nopAuthz struct{}

func (nopAuthz) GranterGrants(context.Context, *authz.QueryGranterGrantsRequest) (*authz.QueryGranterGrantsResponse, error) {
	return &authz.QueryGranterGrantsResponse{}, nil
}

func (nopAuthz) DeleteGrant(context.Context, sdk.AccAddress, sdk.AccAddress, string) error {
	return nil
}

type nopFees struct{}

func (nopFees) IterateAllFeeAllowances(context.Context, func(feegrant.Grant) bool) error {
	return nil
}

func (nopFees) RevokeAllowance(context.Context, sdk.AccAddress, sdk.AccAddress) error {
	return nil
}

type storedGrant struct {
	granter sdk.AccAddress
	grantee sdk.AccAddress
	msgType string
	grant   authz.Grant
}

type memAuthz struct {
	grants         []storedGrant
	granterQueries int
}

// GranterGrants returns the granter's grants one at a time, so the keeper's
// pagination loop is exercised. NextKey is the index of the next grant.
func (m *memAuthz) GranterGrants(_ context.Context, req *authz.QueryGranterGrantsRequest) (*authz.QueryGranterGrantsResponse, error) {
	m.granterQueries++
	start := 0
	if req.Pagination != nil && len(req.Pagination.Key) > 0 {
		start = int(req.Pagination.Key[0])
	}
	for i := start; i < len(m.grants); i++ {
		grant := m.grants[i]
		if grant.granter.String() != req.Granter {
			continue
		}
		authorization, err := grant.grant.GetAuthorization()
		if err != nil {
			return nil, err
		}
		anyAuth, err := codectypes.NewAnyWithValue(authorization)
		if err != nil {
			return nil, err
		}
		res := &authz.QueryGranterGrantsResponse{
			Grants: []*authz.GrantAuthorization{{
				Granter:       grant.granter.String(),
				Grantee:       grant.grantee.String(),
				Authorization: anyAuth,
			}},
			Pagination: &query.PageResponse{},
		}
		if i+1 < len(m.grants) {
			res.Pagination.NextKey = []byte{byte(i + 1)}
		}
		return res, nil
	}
	return &authz.QueryGranterGrantsResponse{Pagination: &query.PageResponse{}}, nil
}

func (m *memAuthz) DeleteGrant(_ context.Context, grantee, granter sdk.AccAddress, msgType string) error {
	kept := m.grants[:0]
	for _, grant := range m.grants {
		if grant.granter.Equals(granter) && grant.grantee.Equals(grantee) && grant.msgType == msgType {
			continue
		}
		kept = append(kept, grant)
	}
	m.grants = kept
	return nil
}

type storedAllowance struct {
	granter sdk.AccAddress
	grantee sdk.AccAddress
}

type memFees struct {
	allowances []storedAllowance
}

func (m *memFees) IterateAllFeeAllowances(_ context.Context, cb func(feegrant.Grant) bool) error {
	for _, allowance := range m.allowances {
		if cb(feegrant.Grant{Granter: allowance.granter.String(), Grantee: allowance.grantee.String()}) {
			return nil
		}
	}
	return nil
}

func (m *memFees) RevokeAllowance(_ context.Context, granter, grantee sdk.AccAddress) error {
	kept := m.allowances[:0]
	for _, allowance := range m.allowances {
		if allowance.granter.Equals(granter) && allowance.grantee.Equals(grantee) {
			continue
		}
		kept = append(kept, allowance)
	}
	m.allowances = kept
	return nil
}

// memEVM returns a stored code hash per address; absent means no code.
type memEVM struct {
	codeHashes map[common.Address]common.Hash
}

func (m memEVM) GetCodeHash(_ sdk.Context, addr common.Address) common.Hash {
	if h, ok := m.codeHashes[addr]; ok {
		return h
	}
	return common.BytesToHash(evmtypes.EmptyCodeHash)
}

// memERC20 records allowance deletions by owner.
type memERC20 struct {
	allowances map[common.Address]int
	deleted    []common.Address
	err        error
}

func (m *memERC20) DeleteOwnerAllowances(_ sdk.Context, owner common.Address) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	m.deleted = append(m.deleted, owner)
	n := m.allowances[owner]
	delete(m.allowances, owner)
	return n, nil
}

type accountKeeperStub struct {
	accounts map[string]sdk.AccountI
}

func (s accountKeeperStub) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return s.accounts[addr.String()]
}

func newMLDSAKey(t *testing.T) (*mldsa87.PrivateKey, []byte) {
	t.Helper()
	pub, priv, err := mldsa87.GenerateKey(rand.Reader)
	require.NoError(t, err)
	bz, err := pub.MarshalBinary()
	require.NoError(t, err)
	require.Len(t, bz, types.MLDSA87PubKeySize)
	return priv, bz
}

func newMLDSAPubKey(t *testing.T) []byte {
	t.Helper()
	_, pub := newMLDSAKey(t)
	return pub
}

func accountPop(t *testing.T, priv *mldsa87.PrivateKey, addr sdk.AccAddress, secpPub, mldsaPub []byte) []byte {
	t.Helper()
	payload, err := types.AccountPoPSignBytes(addr, testChainID, secpPub, mldsaPub)
	require.NoError(t, err)
	sig, err := priv.Sign(rand.Reader, payload, nil)
	require.NoError(t, err)
	return sig
}

func dummyPop() []byte {
	return make([]byte, types.MLDSA87SignatureSize)
}

func setupKeeper(t *testing.T, accs ...sdk.AccountI) (sdk.Context, keeper.Keeper) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	tkey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(key, tkey).WithChainID(testChainID)

	accounts := make(map[string]sdk.AccountI, len(accs))
	for _, acc := range accs {
		accounts[acc.GetAddress().String()] = acc
	}
	k := keeper.NewKeeper(key, accountKeeperStub{accounts: accounts})
	k.SetDelegationKeepers(nopAuthz{}, nopFees{}, nil)
	k.SetEVMKeepers(memEVM{}, &memERC20{})
	return ctx, k
}

func TestRegisterMLDSAKey(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	ethPub := ethPriv.PubKey()
	addr := sdk.AccAddress(ethPub.Address())
	acc := authtypes.NewBaseAccount(addr, ethPub, 1, 0)
	mldsaPriv, pubKey := newMLDSAKey(t)

	ctx, k := setupKeeper(t, acc)

	_, err = k.RegisterMLDSAKey(ctx, &types.MsgRegisterMLDSAKey{
		Signer:       addr.String(),
		PubKey:       pubKey,
		PopSignature: accountPop(t, mldsaPriv, addr, ethPub.Bytes(), pubKey),
	})
	require.NoError(t, err)

	got, found := k.GetMLDSAPubKey(ctx, addr)
	require.True(t, found)
	require.Equal(t, pubKey, got)

	res, err := k.MLDSAPubKey(ctx, &types.QueryMLDSAPubKeyRequest{Account: addr.String()})
	require.NoError(t, err)
	require.True(t, res.Registered)
	require.Equal(t, pubKey, res.PubKey)

	_, err = k.RegisterMLDSAKey(ctx, &types.MsgRegisterMLDSAKey{
		Signer:       addr.String(),
		PubKey:       newMLDSAPubKey(t),
		PopSignature: dummyPop(),
	})
	require.ErrorIs(t, err, types.ErrMLDSAKeyExists)
}

func TestRegisterMLDSAKeyRejectsMissingAndClassicalAccounts(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())
	pubKey := newMLDSAPubKey(t)

	ctx, k := setupKeeper(t)
	_, err = k.RegisterMLDSAKey(ctx, &types.MsgRegisterMLDSAKey{
		Signer:       addr.String(),
		PubKey:       pubKey,
		PopSignature: dummyPop(),
	})
	require.ErrorIs(t, err, types.ErrAccountNotFound)

	secpPub := secp256k1.GenPrivKey().PubKey()
	secpAddr := sdk.AccAddress(secpPub.Address())
	secpAcc := authtypes.NewBaseAccount(secpAddr, secpPub, 2, 0)
	ctx, k = setupKeeper(t, secpAcc)
	_, err = k.RegisterMLDSAKey(ctx, &types.MsgRegisterMLDSAKey{
		Signer:       secpAddr.String(),
		PubKey:       pubKey,
		PopSignature: dummyPop(),
	})
	require.ErrorIs(t, err, types.ErrNotEthSecp256k1)
}

func TestRegisterMLDSAKeyRejectsBadPubKey(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())
	acc := authtypes.NewBaseAccount(addr, ethPriv.PubKey(), 1, 0)
	ctx, k := setupKeeper(t, acc)

	_, err = k.RegisterMLDSAKey(ctx, &types.MsgRegisterMLDSAKey{
		Signer: addr.String(),
		PubKey: make([]byte, types.MLDSA87PubKeySize-1),
	})
	require.ErrorIs(t, err, types.ErrInvalidMLDSAPubKey)
}

func TestMsgSignersUseTheEthAccount(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())
	pubKey := newMLDSAPubKey(t)

	encCfg := moduletestutil.MakeTestEncodingConfig(pqauth.AppModuleBasic{})
	builder := encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(&types.MsgRegisterMLDSAKey{
		Signer: addr.String(),
		PubKey: pubKey,
	}))

	sdkTx := builder.GetTx()
	signerTx, ok := sdkTx.(interface {
		GetSigners() ([][]byte, error)
	})
	require.True(t, ok)
	signers, err := signerTx.GetSigners()
	require.NoError(t, err)
	require.Equal(t, [][]byte{addr}, signers)
}

func TestGenesisRoundTrip(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())
	acc := authtypes.NewBaseAccount(addr, ethPriv.PubKey(), 1, 0)
	pubKey := newMLDSAPubKey(t)

	ctx, k := setupKeeper(t, acc)
	gs := types.GenesisState{Keys: []types.MLDSAKey{{
		Account: addr.String(),
		PubKey:  pubKey,
	}}}
	require.NoError(t, gs.Validate())
	require.NoError(t, k.InitGenesis(ctx, gs))

	exported := pqauth.ExportGenesis(ctx, k)
	require.Equal(t, gs.Keys, exported.Keys)
	require.Equal(t, types.DefaultParams(), exported.Params)
	require.Equal(t, types.DefaultEVMWindowDuration, k.GetParams(ctx).WindowDuration())
}

func TestQueryUnknownAccount(t *testing.T) {
	ctx, k := setupKeeper(t)
	addr := sdk.AccAddress(bytes20())
	res, err := k.MLDSAPubKey(ctx, &types.QueryMLDSAPubKeyRequest{Account: addr.String()})
	require.NoError(t, err)
	require.False(t, res.Registered)
	require.Empty(t, res.PubKey)
}

func bytes20() []byte {
	return []byte("0123456789abcdefghij")
}

func TestEVMWindowDurationParam(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())
	acc := authtypes.NewBaseAccount(addr, ethPriv.PubKey(), 1, 0)
	ctx, k := setupKeeper(t, acc)
	ctx = ctx.WithBlockHeight(4)

	require.NoError(t, k.SetParams(ctx, types.Params{EvmWindowDuration: 25}))
	res, err := k.OpenEVMWindow(ctx, &types.MsgOpenEVMWindow{Signer: addr.String()})
	require.NoError(t, err)
	require.Equal(t, int64(29), res.ExpiryHeight)

	params, err := k.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, int64(25), params.Params.EvmWindowDuration)

	require.Error(t, k.SetParams(ctx, types.Params{EvmWindowDuration: -1}))
	require.Error(t, types.Params{EvmWindowDuration: types.MaxEVMWindowDuration + 1}.Validate())
	require.NoError(t, types.DefaultGenesisState().Validate())
}

func TestOpenEVMWindow(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())
	acc := authtypes.NewBaseAccount(addr, ethPriv.PubKey(), 1, 0)
	ctx, k := setupKeeper(t, acc)
	ctx = ctx.WithBlockHeight(10)

	res, err := k.OpenEVMWindow(ctx, &types.MsgOpenEVMWindow{Signer: addr.String()})
	require.NoError(t, err)
	require.Equal(t, ctx.BlockHeight()+types.EVMWindowDuration, res.ExpiryHeight)
	require.True(t, k.IsEVMWindowOpen(ctx, addr))

	expiry, found := k.GetEVMWindowExpiry(ctx, addr)
	require.True(t, found)
	require.Equal(t, res.ExpiryHeight, expiry)

	query, err := k.EVMWindow(ctx, &types.QueryEVMWindowRequest{Account: addr.String()})
	require.NoError(t, err)
	require.True(t, query.Open)
	require.Equal(t, res.ExpiryHeight, query.ExpiryHeight)

	again, err := k.OpenEVMWindow(ctx, &types.MsgOpenEVMWindow{Signer: addr.String()})
	require.NoError(t, err)
	require.Equal(t, res.ExpiryHeight, again.ExpiryHeight)

	lastOpen := ctx.WithBlockHeight(res.ExpiryHeight - 1)
	require.True(t, k.IsEVMWindowOpen(lastOpen, addr))

	closed := ctx.WithBlockHeight(res.ExpiryHeight)
	require.False(t, k.IsEVMWindowOpen(closed, addr))
	closedQuery, err := k.EVMWindow(closed, &types.QueryEVMWindowRequest{Account: addr.String()})
	require.NoError(t, err)
	require.False(t, closedQuery.Open)
	require.Equal(t, res.ExpiryHeight, closedQuery.ExpiryHeight)

	refreshed, err := k.OpenEVMWindow(closed, &types.MsgOpenEVMWindow{Signer: addr.String()})
	require.NoError(t, err)
	require.Equal(t, closed.BlockHeight()+types.EVMWindowDuration, refreshed.ExpiryHeight)
	require.True(t, k.IsEVMWindowOpen(closed, addr))
	require.False(t, k.IsEVMWindowOpen(closed.WithBlockHeight(refreshed.ExpiryHeight), addr))
}

func TestOpenEVMWindowRejectsMissingAndClassicalAccounts(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())

	ctx, k := setupKeeper(t)
	_, err = k.OpenEVMWindow(ctx, &types.MsgOpenEVMWindow{Signer: addr.String()})
	require.ErrorIs(t, err, types.ErrAccountNotFound)

	secpPub := secp256k1.GenPrivKey().PubKey()
	secpAddr := sdk.AccAddress(secpPub.Address())
	secpAcc := authtypes.NewBaseAccount(secpAddr, secpPub, 2, 0)
	ctx, k = setupKeeper(t, secpAcc)
	_, err = k.OpenEVMWindow(ctx, &types.MsgOpenEVMWindow{Signer: secpAddr.String()})
	require.ErrorIs(t, err, types.ErrNotEthSecp256k1)
}

func TestOpenEVMWindowSignersUseTheEthAccount(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())

	encCfg := moduletestutil.MakeTestEncodingConfig(pqauth.AppModuleBasic{})
	builder := encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(&types.MsgOpenEVMWindow{Signer: addr.String()}))

	sdkTx := builder.GetTx()
	signerTx, ok := sdkTx.(interface {
		GetSigners() ([][]byte, error)
	})
	require.True(t, ok)
	signers, err := signerTx.GetSigners()
	require.NoError(t, err)
	require.Equal(t, [][]byte{addr}, signers)
}

func TestEVMWindowGenesisRoundTrip(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())
	acc := authtypes.NewBaseAccount(addr, ethPriv.PubKey(), 1, 0)
	pubKey := newMLDSAPubKey(t)

	ctx, k := setupKeeper(t, acc)
	gs := types.GenesisState{
		Keys: []types.MLDSAKey{{
			Account: addr.String(),
			PubKey:  pubKey,
		}},
		Windows: []types.EVMWindow{{
			Account:      addr.String(),
			ExpiryHeight: 50,
		}},
	}
	require.NoError(t, gs.Validate())
	require.NoError(t, k.InitGenesis(ctx, gs))
	require.True(t, k.IsEVMWindowOpen(ctx, addr))
	require.False(t, k.IsEVMWindowOpen(ctx.WithBlockHeight(50), addr))

	exported := pqauth.ExportGenesis(ctx, k)
	require.Equal(t, gs.Keys, exported.Keys)
	require.Equal(t, gs.Windows, exported.Windows)
	require.Equal(t, types.DefaultParams(), exported.Params)
}

func TestEVMWindowGenesisRejectsInvalidState(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(ethPriv.PubKey().Address())

	invalid := types.GenesisState{Windows: []types.EVMWindow{{
		Account:      addr.String(),
		ExpiryHeight: 0,
	}}}
	require.ErrorIs(t, invalid.Validate(), types.ErrInvalidEVMWindow)

	duplicate := types.GenesisState{Windows: []types.EVMWindow{
		{Account: addr.String(), ExpiryHeight: 10},
		{Account: addr.String(), ExpiryHeight: 20},
	}}
	require.ErrorIs(t, duplicate.Validate(), types.ErrInvalidEVMWindow)

	ctx, k := setupKeeper(t)
	missing := types.GenesisState{Windows: []types.EVMWindow{{
		Account:      addr.String(),
		ExpiryHeight: 10,
	}}}
	require.NoError(t, missing.Validate())
	require.ErrorIs(t, k.InitGenesis(ctx, missing), types.ErrAccountNotFound)
}

func TestRegisterMLDSAKeyRejectsBadProofAndRevokesGrants(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	ethPub := ethPriv.PubKey()
	addr := sdk.AccAddress(ethPub.Address())
	acc := authtypes.NewBaseAccount(addr, ethPub, 1, 0)
	other := sdk.AccAddress(bytes20())
	mldsaPriv, pubKey := newMLDSAKey(t)

	ctx, k := setupKeeper(t, acc)
	_, err = k.RegisterMLDSAKey(ctx, &types.MsgRegisterMLDSAKey{
		Signer:       addr.String(),
		PubKey:       pubKey,
		PopSignature: dummyPop(),
	})
	require.ErrorIs(t, err, types.ErrInvalidProof)
	_, found := k.GetMLDSAPubKey(ctx, addr)
	require.False(t, found)

	authorization := authz.NewGenericAuthorization(sdk.MsgTypeURL(&types.MsgOpenEVMWindow{}))
	grant, err := authz.NewGrant(time.Unix(0, 0), authorization, nil)
	require.NoError(t, err)
	grants := &memAuthz{grants: []storedGrant{
		{granter: addr, grantee: other, msgType: authorization.MsgTypeURL(), grant: grant},
		{granter: other, grantee: addr, msgType: authorization.MsgTypeURL(), grant: grant},
	}}
	fees := &memFees{allowances: []storedAllowance{
		{granter: addr, grantee: other},
		{granter: other, grantee: addr},
	}}
	k.SetDelegationKeepers(grants, fees, nil)

	_, err = k.RegisterMLDSAKey(ctx, &types.MsgRegisterMLDSAKey{
		Signer:       addr.String(),
		PubKey:       pubKey,
		PopSignature: accountPop(t, mldsaPriv, addr, ethPub.Bytes(), pubKey),
	})
	require.NoError(t, err)
	require.Len(t, grants.grants, 1)
	require.True(t, grants.grants[0].granter.Equals(other))
	require.GreaterOrEqual(t, grants.granterQueries, 1)
	require.Len(t, fees.allowances, 1)
	require.True(t, fees.allowances[0].granter.Equals(other))
}

func TestRotateMLDSAKeyRequiresBothKeys(t *testing.T) {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	ethPub := ethPriv.PubKey()
	addr := sdk.AccAddress(ethPub.Address())
	acc := authtypes.NewBaseAccount(addr, ethPub, 1, 0)
	oldPriv, oldPub := newMLDSAKey(t)
	newPriv, newPub := newMLDSAKey(t)

	ctx, k := setupKeeper(t, acc)
	_, err = k.RegisterMLDSAKey(ctx, &types.MsgRegisterMLDSAKey{
		Signer:       addr.String(),
		PubKey:       oldPub,
		PopSignature: accountPop(t, oldPriv, addr, ethPub.Bytes(), oldPub),
	})
	require.NoError(t, err)

	payload, err := types.RotateSignBytes(addr, testChainID, ethPub.Bytes(), oldPub, newPub)
	require.NoError(t, err)
	oldSig, err := oldPriv.Sign(rand.Reader, payload, nil)
	require.NoError(t, err)
	newSig, err := newPriv.Sign(rand.Reader, payload, nil)
	require.NoError(t, err)

	_, err = k.RotateMLDSAKey(ctx, &types.MsgRotateMLDSAKey{
		Signer:       addr.String(),
		NewPubKey:    newPub,
		OldSignature: dummyPop(),
		NewSignature: newSig,
	})
	require.ErrorIs(t, err, types.ErrInvalidProof)
	got, found := k.GetMLDSAPubKey(ctx, addr)
	require.True(t, found)
	require.Equal(t, oldPub, got)

	_, err = k.RotateMLDSAKey(ctx, &types.MsgRotateMLDSAKey{
		Signer:       addr.String(),
		NewPubKey:    newPub,
		OldSignature: oldSig,
		NewSignature: newSig,
	})
	require.NoError(t, err)
	got, found = k.GetMLDSAPubKey(ctx, addr)
	require.True(t, found)
	require.Equal(t, newPub, got)
}

func TestQueryUnknownEVMWindow(t *testing.T) {
	ctx, k := setupKeeper(t)
	addr := sdk.AccAddress(bytes20())
	res, err := k.EVMWindow(ctx, &types.QueryEVMWindowRequest{Account: addr.String()})
	require.NoError(t, err)
	require.False(t, res.Open)
	require.Zero(t, res.ExpiryHeight)
	require.False(t, k.IsEVMWindowOpen(ctx, addr))
}
