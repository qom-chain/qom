package cosmos_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/stretchr/testify/require"

	cosmosante "github.com/cosmos/evm/ante/cosmos"
	anteinterfaces "github.com/cosmos/evm/ante/interfaces"
	cryptocodec "github.com/cosmos/evm/crypto/codec"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	pqauth "github.com/cosmos/evm/x/pqauth"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	"github.com/cosmos/gogoproto/proto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

const (
	testAccountNumber uint64 = 5
	testSequence      uint64 = 4
	testChainID              = "qom_9000-1"
)

type pqKeeper struct {
	keys map[string][]byte
}

func (k pqKeeper) GetMLDSAPubKey(_ sdk.Context, addr sdk.AccAddress) ([]byte, bool) {
	bz, ok := k.keys[addr.String()]
	return bz, ok
}

func (k pqKeeper) IsEVMWindowOpen(sdk.Context, sdk.AccAddress) bool { return false }

func (k pqKeeper) RestrictCosmosPrecompiles(sdk.Context) bool { return false }

type accountKeeper struct {
	accs map[string]sdk.AccountI
}

func (k accountKeeper) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return k.accs[addr.String()]
}

func (k accountKeeper) GetParams(_ context.Context) authtypes.Params {
	return authtypes.DefaultParams()
}

type harness struct {
	ctx      sdk.Context
	encCfg   moduletestutil.TestEncodingConfig
	addr     sdk.AccAddress
	acc      sdk.AccountI
	pub      types.PubKey
	mldsaKey *mldsa87.PrivateKey
	pq       pqKeeper
	ak       accountKeeper
}

func newHarness(t *testing.T, registered bool) harness {
	t.Helper()

	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	pub := ethPriv.PubKey()
	addr := sdk.AccAddress(pub.Address())
	acc := authtypes.NewBaseAccount(addr, pub, testAccountNumber, testSequence)

	mldsaPub, mldsaPriv, err := mldsa87.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubBz, err := mldsaPub.MarshalBinary()
	require.NoError(t, err)
	require.Len(t, pubBz, pqauthtypes.MLDSA87PubKeySize)

	encCfg := moduletestutil.MakeTestEncodingConfig(pqauth.AppModuleBasic{})
	cryptocodec.RegisterInterfaces(encCfg.InterfaceRegistry)

	keys := map[string][]byte{}
	if registered {
		keys[addr.String()] = pubBz
	}

	ctx := testutil.DefaultContext(
		storetypes.NewKVStoreKey("pqauth-test"),
		storetypes.NewTransientStoreKey("transient-pqauth-test"),
	).WithBlockHeight(8).WithChainID(testChainID)

	return harness{
		ctx:      ctx,
		encCfg:   encCfg,
		addr:     addr,
		acc:      acc,
		pub:      pub,
		mldsaKey: mldsaPriv,
		pq:       pqKeeper{keys: keys},
		ak:       accountKeeper{accs: map[string]sdk.AccountI{addr.String(): acc}},
	}
}

func (h harness) decorator() cosmosante.MLDSASigDecorator {
	return h.decoratorWith(nil)
}

func (h harness) decoratorWith(authzKeeper anteinterfaces.AuthzGrantKeeper) cosmosante.MLDSASigDecorator {
	return cosmosante.NewMLDSASigDecorator(h.ak, h.pq, h.encCfg.TxConfig.SignModeHandler(), authzKeeper, h.encCfg.Codec)
}

type fakeAuthz struct {
	authorization authz.Authorization
}

func (f fakeAuthz) GetAuthorization(context.Context, sdk.AccAddress, sdk.AccAddress, string) (authz.Authorization, *time.Time) {
	return f.authorization, nil
}

func (h harness) emptyExt() *pqauthtypes.ExtensionOptionMLDSASig {
	return &pqauthtypes.ExtensionOptionMLDSASig{
		Signatures: []pqauthtypes.MLDSASig{{
			Signer: h.addr.String(),
		}},
	}
}

func (h harness) withSig(sig []byte) *pqauthtypes.ExtensionOptionMLDSASig {
	ext := h.emptyExt()
	ext.Signatures[0].Signature = sig
	return ext
}

func setExtension(t *testing.T, builder authtx.ExtensionOptionsTxBuilder, ext *pqauthtypes.ExtensionOptionMLDSASig) {
	t.Helper()
	packed, err := codectypes.NewAnyWithValue(ext)
	require.NoError(t, err)
	builder.SetExtensionOptions(packed)
}

func (h harness) buildTx(t *testing.T, ext *pqauthtypes.ExtensionOptionMLDSASig, mode signing.SignMode) authtx.ExtensionOptionsTxBuilder {
	t.Helper()
	builder := h.encCfg.TxConfig.NewTxBuilder().(authtx.ExtensionOptionsTxBuilder)
	require.NoError(t, builder.SetMsgs(&pqauthtypes.MsgOpenEVMWindow{Signer: h.addr.String()}))
	builder.SetGasLimit(200_000)
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("stake", 1)))
	if ext != nil {
		setExtension(t, builder, ext)
	}
	require.NoError(t, builder.SetSignatures(signing.SignatureV2{
		PubKey: h.pub,
		Data: &signing.SingleSignatureData{
			SignMode:  mode,
			Signature: []byte{0x01, 0x02, 0x03},
		},
		Sequence: testSequence,
	}))
	return builder
}

func (h harness) signBytes(t *testing.T, tx sdk.Tx) []byte {
	t.Helper()
	signerData, err := cosmosante.MLDSASignerData(h.ctx, h.acc, testSequence)
	require.NoError(t, err)
	bz, err := cosmosante.MLDSASignBytes(h.ctx, h.encCfg.TxConfig.SignModeHandler(), signerData, tx)
	require.NoError(t, err)
	require.NotEmpty(t, bz)
	return bz
}

func (h harness) sign(t *testing.T, tx sdk.Tx) []byte {
	t.Helper()
	sig, err := h.mldsaKey.Sign(rand.Reader, h.signBytes(t, tx), nil)
	require.NoError(t, err)
	require.Len(t, sig, mldsa87.SignatureSize)
	return sig
}

func runDecorator(dec cosmosante.MLDSASigDecorator, ctx sdk.Context, tx sdk.Tx, simulate bool) (bool, error) {
	called := false
	_, err := dec.AnteHandle(ctx, tx, simulate, func(newCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return newCtx, nil
	})
	return called, err
}

func TestMLDSAExtensionTypeURL(t *testing.T) {
	packed, err := codectypes.NewAnyWithValue(&pqauthtypes.ExtensionOptionMLDSASig{})
	require.NoError(t, err)
	require.Equal(t, pqauthtypes.MLDSAExtensionTypeURL, packed.GetTypeUrl())
	require.Equal(t, "/"+proto.MessageName(&pqauthtypes.ExtensionOptionMLDSASig{}), packed.GetTypeUrl())
}

func TestMLDSASignBytesIgnoreSignatureBytes(t *testing.T) {
	h := newHarness(t, true)
	builder := h.buildTx(t, h.emptyExt(), signing.SignMode_SIGN_MODE_DIRECT)
	before := h.signBytes(t, builder.GetTx())

	setExtension(t, builder, h.withSig(h.sign(t, builder.GetTx())))
	require.Equal(t, before, h.signBytes(t, builder.GetTx()))

	bz, err := h.encCfg.TxConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	decoded, err := h.encCfg.TxConfig.TxDecoder()(bz)
	require.NoError(t, err)
	require.Equal(t, before, h.signBytes(t, decoded))
}

func TestUnregisteredAccountSkipsMLDSA(t *testing.T) {
	h := newHarness(t, false)
	dec := h.decorator()

	called, err := runDecorator(dec, h.ctx, h.buildTx(t, nil, signing.SignMode_SIGN_MODE_DIRECT).GetTx(), false)
	require.NoError(t, err)
	require.True(t, called)

	called, err = runDecorator(dec, h.ctx, h.buildTx(t, h.withSig([]byte("ignored")), signing.SignMode_SIGN_MODE_DIRECT).GetTx(), false)
	require.NoError(t, err)
	require.True(t, called)
}

func TestRegisteredAccountRequiresMLDSASignature(t *testing.T) {
	h := newHarness(t, true)
	dec := h.decorator()

	called, err := runDecorator(dec, h.ctx, h.buildTx(t, nil, signing.SignMode_SIGN_MODE_DIRECT).GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)

	called, err = runDecorator(dec, h.ctx, h.buildTx(t, h.emptyExt(), signing.SignMode_SIGN_MODE_DIRECT).GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)
}

func TestRegisteredAccountRejectsInvalidMLDSASignature(t *testing.T) {
	h := newHarness(t, true)
	dec := h.decorator()

	_, err := runDecorator(dec, h.ctx, h.buildTx(t, h.withSig([]byte{1, 2, 3}), signing.SignMode_SIGN_MODE_DIRECT).GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)

	wrong, err := h.mldsaKey.Sign(rand.Reader, []byte("not the sdk sign bytes"), nil)
	require.NoError(t, err)
	_, err = runDecorator(dec, h.ctx, h.buildTx(t, h.withSig(wrong), signing.SignMode_SIGN_MODE_DIRECT).GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)

	builder := h.buildTx(t, h.emptyExt(), signing.SignMode_SIGN_MODE_DIRECT)
	genesisData, err := cosmosante.MLDSASignerData(h.ctx.WithBlockHeight(0), h.acc, testSequence)
	require.NoError(t, err)
	genesisBytes, err := cosmosante.MLDSASignBytes(h.ctx, h.encCfg.TxConfig.SignModeHandler(), genesisData, builder.GetTx())
	require.NoError(t, err)
	genesisSig, err := h.mldsaKey.Sign(rand.Reader, genesisBytes, nil)
	require.NoError(t, err)
	setExtension(t, builder, h.withSig(genesisSig))
	_, err = runDecorator(dec, h.ctx, builder.GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}

func TestRegisteredAccountAcceptsMLDSASignature(t *testing.T) {
	h := newHarness(t, true)
	builder := h.buildTx(t, h.emptyExt(), signing.SignMode_SIGN_MODE_DIRECT)
	setExtension(t, builder, h.withSig(h.sign(t, builder.GetTx())))

	meter := storetypes.NewGasMeter(10_000_000)
	called, err := runDecorator(h.decorator(), h.ctx.WithGasMeter(meter), builder.GetTx(), false)
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, cosmosante.MLDSA87VerifyGas, meter.GasConsumed())

	builder.SetMemo("tampered")
	called, err = runDecorator(h.decorator(), h.ctx, builder.GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)
}

func TestMLDSASigDecoratorSkipsWhenSignatureVerificationIsSkipped(t *testing.T) {
	h := newHarness(t, true)
	dec := h.decorator()
	tx := h.buildTx(t, nil, signing.SignMode_SIGN_MODE_DIRECT).GetTx()

	called, err := runDecorator(dec, h.ctx, tx, true)
	require.NoError(t, err)
	require.True(t, called)

	called, err = runDecorator(dec, h.ctx.WithIsReCheckTx(true), tx, false)
	require.NoError(t, err)
	require.True(t, called)

	called, err = runDecorator(dec, h.ctx.WithIsSigverifyTx(false), tx, false)
	require.NoError(t, err)
	require.True(t, called)
}

func TestRegisteredAccountRejectsNonDirectSignMode(t *testing.T) {
	h := newHarness(t, true)
	tx := h.buildTx(t, h.emptyExt(), signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON).GetTx()
	called, err := runDecorator(h.decorator(), h.ctx, tx, false)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidType)
	require.False(t, called)
}

func TestDuplicateMLDSASignatureIsRejected(t *testing.T) {
	h := newHarness(t, true)
	ext := &pqauthtypes.ExtensionOptionMLDSASig{Signatures: []pqauthtypes.MLDSASig{
		{Signer: h.addr.String(), Signature: []byte{1}},
		{Signer: h.addr.String(), Signature: []byte{2}},
	}}
	_, err := runDecorator(h.decorator(), h.ctx, h.buildTx(t, ext, signing.SignMode_SIGN_MODE_DIRECT).GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)

	builder := h.buildTx(t, nil, signing.SignMode_SIGN_MODE_DIRECT)
	packed, err := codectypes.NewAnyWithValue(h.emptyExt())
	require.NoError(t, err)
	builder.SetExtensionOptions(packed, packed)
	_, err = runDecorator(h.decorator(), h.ctx, builder.GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
}

func TestPQAuthMessagesAreAuthzDisabled(t *testing.T) {
	disabled := cosmosante.PQAuthzDisabledMsgURLs()
	for _, msg := range []sdk.Msg{
		&pqauthtypes.MsgRegisterMLDSAKey{},
		&pqauthtypes.MsgRotateMLDSAKey{},
		&pqauthtypes.MsgRegisterValidatorMLDSAKey{},
		&pqauthtypes.MsgOpenEVMWindow{},
	} {
		require.Contains(t, disabled, sdk.MsgTypeURL(msg))
	}
}

func TestRegisteredAccountCannotGrantGenericAuthorization(t *testing.T) {
	h := newHarness(t, true)
	grantee := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	msg, err := authz.NewMsgGrant(h.addr, grantee, authz.NewGenericAuthorization(sdk.MsgTypeURL(&authtypes.MsgUpdateParams{})), nil)
	require.NoError(t, err)

	builder := h.encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	called, err := runDecorator(h.decorator(), h.ctx, builder.GetTx(), true)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)
	require.Contains(t, err.Error(), "GenericAuthorization")

	unregistered := newHarness(t, false)
	msg, err = authz.NewMsgGrant(unregistered.addr, grantee, authz.NewGenericAuthorization(sdk.MsgTypeURL(&authtypes.MsgUpdateParams{})), nil)
	require.NoError(t, err)
	builder = unregistered.encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	require.NoError(t, builder.SetSignatures(signing.SignatureV2{
		PubKey: unregistered.pub,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: []byte{0x01},
		},
		Sequence: testSequence,
	}))
	called, err = runDecorator(unregistered.decorator(), unregistered.ctx, builder.GetTx(), false)
	require.NoError(t, err)
	require.True(t, called)
}

func TestRegisterCannotShareATransactionWithAuthz(t *testing.T) {
	h := newHarness(t, false)
	grantee := sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20))
	builder := h.encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(
		&pqauthtypes.MsgRegisterMLDSAKey{Signer: h.addr.String()},
		&authz.MsgGrant{Granter: h.addr.String(), Grantee: grantee.String()},
	))
	called, err := runDecorator(h.decorator(), h.ctx, builder.GetTx(), true)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)
	require.Contains(t, err.Error(), "cannot be combined")
}

func TestRegisteredGranterGenericExecIsRejected(t *testing.T) {
	h := newHarness(t, true)
	granteeKey, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	grantee := sdk.AccAddress(granteeKey.PubKey().Address())
	exec := authz.NewMsgExec(grantee, []sdk.Msg{&pqauthtypes.MsgOpenEVMWindow{Signer: h.addr.String()}})

	builder := h.encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(&exec))
	dec := h.decoratorWith(fakeAuthz{authorization: authz.NewGenericAuthorization(sdk.MsgTypeURL(&pqauthtypes.MsgOpenEVMWindow{}))})
	called, err := runDecorator(dec, h.ctx, builder.GetTx(), true)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)
	require.Contains(t, err.Error(), "GenericAuthorization")
}

func TestRegisteredFeeGranterRequiresMLDSA(t *testing.T) {
	h := newHarness(t, true)
	granterKey, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	granterPub := granterKey.PubKey()
	granter := sdk.AccAddress(granterPub.Address())
	granterAcc := authtypes.NewBaseAccount(granter, granterPub, 9, 3)
	mldsaPub, mldsaPriv, err := mldsa87.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubBz, err := mldsaPub.MarshalBinary()
	require.NoError(t, err)
	h.pq.keys[granter.String()] = pubBz
	h.ak.accs[granter.String()] = granterAcc

	builder := h.buildTx(t, &pqauthtypes.ExtensionOptionMLDSASig{Signatures: []pqauthtypes.MLDSASig{
		{Signer: h.addr.String()},
		{Signer: granter.String()},
	}}, signing.SignMode_SIGN_MODE_DIRECT)
	builder.SetFeeGranter(granter)

	called, err := runDecorator(h.decorator(), h.ctx, builder.GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, called)

	granterData, err := cosmosante.MLDSASignerData(h.ctx, granterAcc, granterAcc.GetSequence())
	require.NoError(t, err)
	granterBytes, err := cosmosante.MLDSASignBytes(h.ctx, h.encCfg.TxConfig.SignModeHandler(), granterData, builder.GetTx())
	require.NoError(t, err)
	granterSig, err := mldsaPriv.Sign(rand.Reader, granterBytes, nil)
	require.NoError(t, err)
	ownerSig := h.sign(t, builder.GetTx())
	setExtension(t, builder, &pqauthtypes.ExtensionOptionMLDSASig{Signatures: []pqauthtypes.MLDSASig{
		{Signer: h.addr.String(), Signature: ownerSig},
		{Signer: granter.String(), Signature: granterSig},
	}})

	meter := storetypes.NewGasMeter(10_000_000)
	called, err = runDecorator(h.decorator(), h.ctx.WithGasMeter(meter), builder.GetTx(), false)
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, 2*cosmosante.MLDSA87VerifyGas, meter.GasConsumed())
}
