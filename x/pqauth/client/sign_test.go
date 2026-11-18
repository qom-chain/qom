package client_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	cosmosante "github.com/cosmos/evm/ante/cosmos"
	cryptocodec "github.com/cosmos/evm/crypto/codec"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/cosmos/evm/crypto/hd"
	pqauth "github.com/cosmos/evm/x/pqauth"
	pqauthclient "github.com/cosmos/evm/x/pqauth/client"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	"github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"
)

const (
	testKeyName       = "alice"
	testChainID       = "qom_9000-1"
	testAccountNumber = uint64(7)
	testSequence      = uint64(3)
)

func TestMain(m *testing.M) {
	cfg := sdk.GetConfig()
	if cfg.GetBech32AccountAddrPrefix() == "" {
		cfg.SetBech32PrefixForAccount("cosmos", "cosmospub")
		cfg.SetBech32PrefixForValidator("cosmosvaloper", "cosmosvaloperpub")
		cfg.SetBech32PrefixForConsensusNode("cosmosvalcons", "cosmosvalconspub")
	}
	os.Exit(m.Run())
}

func TestSignHybridBothLayers(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(pqauth.AppModuleBasic{})
	cryptocodec.RegisterInterfaces(encCfg.InterfaceRegistry)

	kb := keyring.NewInMemory(encCfg.Codec, hd.EthSecp256k1Option())
	record, _, err := kb.NewMnemonic(
		testKeyName,
		keyring.English,
		hd.BIP44HDPath,
		keyring.DefaultBIP39Passphrase,
		hd.EthSecp256k1,
	)
	require.NoError(t, err)
	pubKey, err := record.GetPubKey()
	require.NoError(t, err)
	addr := sdk.AccAddress(pubKey.Address())

	_, mldsaPriv, err := mldsa87.GenerateKey(rand.Reader)
	require.NoError(t, err)
	mldsaPub, ok := mldsaPriv.Public().(*mldsa87.PublicKey)
	require.True(t, ok)

	builder := encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(&pqauthtypes.MsgOpenEVMWindow{Signer: addr.String()}))
	builder.SetGasLimit(200_000)
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("stake", 1)))

	txf := tx.Factory{}.
		WithTxConfig(encCfg.TxConfig).
		WithKeybase(kb).
		WithChainID(testChainID).
		WithAccountNumber(testAccountNumber).
		WithSequence(testSequence).
		WithSignMode(signingtypes.SignMode_SIGN_MODE_DIRECT)

	require.NoError(t, pqauthclient.SignHybrid(context.Background(), txf, encCfg.TxConfig, testKeyName, builder, mldsaPriv))

	signed := builder.GetTx()
	ext := mldsaExtension(t, signed)
	require.Len(t, ext.Signatures, 1)
	require.Equal(t, addr.String(), ext.Signatures[0].Signer)
	require.Len(t, ext.Signatures[0].Signature, mldsa87.SignatureSize)

	signerData := signerDataFor(t, addr, pubKey)
	mldsaBytes, err := cosmosante.MLDSASignBytes(context.Background(), encCfg.TxConfig.SignModeHandler(), signerData, signed)
	require.NoError(t, err)
	require.True(t, mldsa87.Verify(mldsaPub, mldsaBytes, nil, ext.Signatures[0].Signature))

	tampered := append([]byte(nil), mldsaBytes...)
	tampered[len(tampered)-1] ^= 0xff
	require.False(t, mldsa87.Verify(mldsaPub, tampered, nil, ext.Signatures[0].Signature))

	secpBytes, err := authsigning.GetSignBytesAdapter(
		context.Background(),
		encCfg.TxConfig.SignModeHandler(),
		signingtypes.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{
			Address:       addr.String(),
			ChainID:       testChainID,
			AccountNumber: testAccountNumber,
			Sequence:      testSequence,
			PubKey:        pubKey,
		},
		signed,
	)
	require.NoError(t, err)
	require.NotEqual(t, mldsaBytes, secpBytes, "ethsecp256k1 must cover the filled ML-DSA signature")

	sigTx := signed.(authsigning.SigVerifiableTx)
	sigs, err := sigTx.GetSignaturesV2()
	require.NoError(t, err)
	require.Len(t, sigs, 1)
	single, ok := sigs[0].Data.(*signingtypes.SingleSignatureData)
	require.True(t, ok)
	require.Equal(t, signingtypes.SignMode_SIGN_MODE_DIRECT, single.SignMode)
	require.Equal(t, testSequence, sigs[0].Sequence)
	ethPub, ok := pubKey.(*ethsecp256k1.PubKey)
	require.True(t, ok)
	require.True(t, ethPub.VerifySignature(secpBytes, single.Signature))

	bz, err := encCfg.TxConfig.TxEncoder()(signed)
	require.NoError(t, err)
	decoded, err := encCfg.TxConfig.TxDecoder()(bz)
	require.NoError(t, err)
	decodedExt := mldsaExtension(t, decoded)
	decodedBytes, err := cosmosante.MLDSASignBytes(context.Background(), encCfg.TxConfig.SignModeHandler(), signerData, decoded)
	require.NoError(t, err)
	require.Equal(t, mldsaBytes, decodedBytes)
	require.True(t, mldsa87.Verify(mldsaPub, decodedBytes, nil, decodedExt.Signatures[0].Signature))
}

func TestSignHybridRejects(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(pqauth.AppModuleBasic{})
	cryptocodec.RegisterInterfaces(encCfg.InterfaceRegistry)

	kb := keyring.NewInMemory(encCfg.Codec, hd.EthSecp256k1Option())
	_, _, err := kb.NewMnemonic(testKeyName, keyring.English, hd.BIP44HDPath, keyring.DefaultBIP39Passphrase, hd.EthSecp256k1)
	require.NoError(t, err)
	_, mldsaPriv, err := mldsa87.GenerateKey(rand.Reader)
	require.NoError(t, err)

	builder := encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(&pqauthtypes.MsgOpenEVMWindow{
		Signer: sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String(),
	}))

	base := tx.Factory{}.
		WithTxConfig(encCfg.TxConfig).
		WithKeybase(kb).
		WithChainID(testChainID).
		WithSignMode(signingtypes.SignMode_SIGN_MODE_DIRECT)

	err = pqauthclient.SignHybrid(context.Background(), base, encCfg.TxConfig, testKeyName, builder, nil)
	require.ErrorContains(t, err, "ML-DSA private key is nil")

	amino := base.WithSignMode(signingtypes.SignMode_SIGN_MODE_LEGACY_AMINO_JSON)
	err = pqauthclient.SignHybrid(context.Background(), amino, encCfg.TxConfig, testKeyName, builder, mldsaPriv)
	require.ErrorContains(t, err, "hybrid signatures require SIGN_MODE_DIRECT")

	err = pqauthclient.SignHybrid(context.Background(), base, encCfg.TxConfig, testKeyName, builder, mldsaPriv)
	require.ErrorContains(t, err, "is not a signer of this transaction")

	err = pqauthclient.SignHybrid(context.Background(), base.WithChainID(""), encCfg.TxConfig, testKeyName, builder, mldsaPriv)
	require.ErrorContains(t, err, "chain ID is empty")
}

func signerDataFor(t *testing.T, addr sdk.AccAddress, pubKey cryptotypes.PubKey) txsigning.SignerData {
	t.Helper()
	packed, err := codectypes.NewAnyWithValue(pubKey)
	require.NoError(t, err)
	return txsigning.SignerData{
		Address:       addr.String(),
		ChainID:       testChainID,
		AccountNumber: testAccountNumber,
		Sequence:      testSequence,
		PubKey: &anypb.Any{
			TypeUrl: packed.TypeUrl,
			Value:   packed.Value,
		},
	}
}

func mldsaExtension(t *testing.T, tx sdk.Tx) *pqauthtypes.ExtensionOptionMLDSASig {
	t.Helper()
	has, ok := tx.(authante.HasExtensionOptionsTx)
	require.True(t, ok)
	var found *pqauthtypes.ExtensionOptionMLDSASig
	for _, opt := range has.GetExtensionOptions() {
		if opt == nil || opt.TypeUrl != pqauthtypes.MLDSAExtensionTypeURL {
			continue
		}
		require.Nil(t, found, "duplicate ML-DSA extension")
		if cached, ok := opt.GetCachedValue().(*pqauthtypes.ExtensionOptionMLDSASig); ok && cached != nil {
			found = cached
			continue
		}
		parsed := &pqauthtypes.ExtensionOptionMLDSASig{}
		require.NoError(t, parsed.Unmarshal(opt.Value))
		found = parsed
	}
	require.NotNil(t, found)
	return found
}
