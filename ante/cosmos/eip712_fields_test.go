package cosmos_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	cosmosante "github.com/cosmos/evm/ante/cosmos"
	cryptocodec "github.com/cosmos/evm/crypto/codec"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/cosmos/evm/ethereum/eip712"
	pqauth "github.com/cosmos/evm/x/pqauth"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"
)

const eip712TestHeight = 8

type eip712Harness struct {
	ctx    sdk.Context
	encCfg moduletestutil.TestEncodingConfig
	priv   *ethsecp256k1.PrivKey
	addr   sdk.AccAddress
	acc    sdk.AccountI
	ak     accountKeeper
}

func newEIP712Harness(t *testing.T) eip712Harness {
	t.Helper()
	priv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(priv.PubKey().Address())
	acc := authtypes.NewBaseAccount(addr, priv.PubKey(), testAccountNumber, testSequence)

	encCfg := moduletestutil.MakeTestEncodingConfig(pqauth.AppModuleBasic{})
	cryptocodec.RegisterInterfaces(encCfg.InterfaceRegistry)
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	eip712.SetEncodingConfig(encCfg.Amino, encCfg.InterfaceRegistry, 9000)

	ctx := testutil.DefaultContext(
		storetypes.NewKVStoreKey("eip712-fields-test"),
		storetypes.NewTransientStoreKey("transient-eip712-fields-test"),
	).WithBlockHeight(eip712TestHeight).WithChainID(testChainID)

	return eip712Harness{
		ctx:    ctx,
		encCfg: encCfg,
		priv:   priv,
		addr:   addr,
		acc:    acc,
		ak:     accountKeeper{accs: map[string]sdk.AccountI{addr.String(): acc}},
	}
}

type txOpts struct {
	granter, payer bool
	timeout        bool
	unordered      bool
	eip712, badSig bool
}

// buildSignedTx builds a bank send signed in SIGN_MODE_DIRECT, either over
// the plain sign doc (standard ECDSA) or over its EIP-712 representation.
func (h eip712Harness) buildSignedTx(t *testing.T, o txOpts) sdk.Tx {
	t.Helper()
	builder := h.encCfg.TxConfig.NewTxBuilder().(authtx.ExtensionOptionsTxBuilder)
	require.NoError(t, builder.SetMsgs(banktypes.NewMsgSend(h.addr, h.addr, sdk.NewCoins(sdk.NewCoin("aqomx", math.NewInt(1))))))
	builder.SetGasLimit(200_000)
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("aqomx", 1000)))
	if o.granter {
		builder.SetFeeGranter(sdk.AccAddress([]byte("granter_address_____")))
	}
	if o.payer {
		builder.SetFeePayer(h.addr)
	}
	if o.timeout || o.unordered {
		builder.SetTimeoutTimestamp(time.Unix(1_900_000_000, 0).UTC())
	}
	if o.unordered {
		builder.SetUnordered(true)
	}
	seq := testSequence
	if o.unordered {
		seq = 0
	}
	placeholder := signing.SignatureV2{
		PubKey:   h.priv.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT},
		Sequence: seq,
	}
	require.NoError(t, builder.SetSignatures(placeholder))

	anyPk, err := codectypes.NewAnyWithValue(h.priv.PubKey())
	require.NoError(t, err)
	signerData := txsigning.SignerData{
		Address:       h.addr.String(),
		ChainID:       testChainID,
		AccountNumber: testAccountNumber,
		Sequence:      seq,
		PubKey:        &anypb.Any{TypeUrl: anyPk.TypeUrl, Value: anyPk.Value},
	}
	txData := builder.GetTx().(authsigning.V2AdaptableTx).GetSigningTxData()
	signBytes, err := h.encCfg.TxConfig.SignModeHandler().GetSignBytes(h.ctx, signingv1beta1.SignMode_SIGN_MODE_DIRECT, signerData, txData)
	require.NoError(t, err)

	msg := signBytes
	if o.eip712 {
		msg, err = eip712.GetEIP712BytesForMsg(signBytes)
		require.NoError(t, err)
	}
	sig, err := h.priv.Sign(msg)
	require.NoError(t, err)
	if o.badSig {
		sig[5] ^= 0xff
	}
	// The v1.0.1 verification accepts every well-formed case built here.
	require.Equal(t, !o.badSig, h.priv.PubKey().VerifySignature(signBytes, sig))

	placeholder.Data = &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: sig}
	require.NoError(t, builder.SetSignatures(placeholder))
	return builder.GetTx()
}

func (h eip712Harness) run(tx sdk.Tx, ctx sdk.Context, simulate bool) (bool, error) {
	dec := cosmosante.NewEIP712SignDocFieldsDecorator(h.ak, h.encCfg.TxConfig.SignModeHandler())
	called := false
	_, err := dec.AnteHandle(ctx, tx, simulate, func(newCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return newCtx, nil
	})
	return called, err
}

func TestEIP712SignDocFieldsDecorator(t *testing.T) {
	h := newEIP712Harness(t)

	cases := []struct {
		name       string
		opts       txOpts
		rejectPost bool
	}{
		{"eip712 with fee granter", txOpts{granter: true, eip712: true}, true},
		{"eip712 with fee payer", txOpts{payer: true, eip712: true}, true},
		{"eip712 with timeout timestamp", txOpts{timeout: true, eip712: true}, true},
		{"eip712 unordered", txOpts{unordered: true, eip712: true}, true},
		{"eip712 with granter, payer, timeout, unordered", txOpts{granter: true, payer: true, unordered: true, eip712: true}, true},
		{"eip712 plain", txOpts{eip712: true}, false},
		{"direct with fee granter", txOpts{granter: true}, false},
		{"direct unordered", txOpts{unordered: true}, false},
		{"direct plain", txOpts{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := h.buildSignedTx(t, tc.opts)

			restore := pqauthtypes.SetUpstreamFixesActivationHeightForTesting(eip712TestHeight)
			called, err := h.run(tx, h.ctx, false)
			if tc.rejectPost {
				require.Error(t, err)
				require.ErrorContains(t, err, "EIP-712 signatures do not support")
				require.False(t, called)
			} else {
				require.NoError(t, err)
				require.True(t, called)
			}
			// simulation and recheck are not affected
			called, err = h.run(tx, h.ctx, true)
			require.NoError(t, err)
			require.True(t, called)
			called, err = h.run(tx, h.ctx.WithIsReCheckTx(true), false)
			require.NoError(t, err)
			require.True(t, called)
			restore()

			// below the activation height the decorator is a no-op
			restore = pqauthtypes.SetUpstreamFixesActivationHeightForTesting(eip712TestHeight + 1)
			called, err = h.run(tx, h.ctx, false)
			require.NoError(t, err)
			require.True(t, called)
			restore()
		})
	}
}

func TestProtobufSignDocHasUncoveredFields(t *testing.T) {
	h := newEIP712Harness(t)
	signBytesOf := func(o txOpts) []byte {
		tx := h.buildSignedTx(t, o)
		anyPk, err := codectypes.NewAnyWithValue(h.priv.PubKey())
		require.NoError(t, err)
		seq := testSequence
		if o.unordered {
			seq = 0
		}
		bz, err := h.encCfg.TxConfig.SignModeHandler().GetSignBytes(h.ctx, signingv1beta1.SignMode_SIGN_MODE_DIRECT, txsigning.SignerData{
			Address: h.addr.String(), ChainID: testChainID, AccountNumber: testAccountNumber, Sequence: seq,
			PubKey: &anypb.Any{TypeUrl: anyPk.TypeUrl, Value: anyPk.Value},
		}, tx.(authsigning.V2AdaptableTx).GetSigningTxData())
		require.NoError(t, err)
		return bz
	}
	require.False(t, eip712.ProtobufSignDocHasUncoveredFields(signBytesOf(txOpts{})))
	require.True(t, eip712.ProtobufSignDocHasUncoveredFields(signBytesOf(txOpts{granter: true})))
	require.True(t, eip712.ProtobufSignDocHasUncoveredFields(signBytesOf(txOpts{payer: true})))
	require.True(t, eip712.ProtobufSignDocHasUncoveredFields(signBytesOf(txOpts{timeout: true})))
	require.True(t, eip712.ProtobufSignDocHasUncoveredFields(signBytesOf(txOpts{unordered: true})))
	require.False(t, eip712.ProtobufSignDocHasUncoveredFields([]byte("not a sign doc")))
	require.False(t, eip712.ProtobufSignDocHasUncoveredFields(nil))
}
