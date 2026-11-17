package client

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"google.golang.org/protobuf/types/known/anypb"

	cosmosante "github.com/cosmos/evm/ante/cosmos"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"
)

// ExtraMLDSASigner is a registered account that did not sign the transaction
// with secp256k1, but whose funds or messages the transaction uses. Its
// ML-DSA signature is checked at AccountNumber and Sequence. The account
// sequence is not incremented.
type ExtraMLDSASigner struct {
	Address       sdk.AccAddress
	Priv          *mldsa87.PrivateKey
	PubKey        cryptotypes.PubKey
	AccountNumber uint64
	Sequence      uint64
}

// SignHybrid signs txBuilder for one ethsecp256k1 account and attaches that
// account's ML-DSA-87 signature in a critical extension.
//
// The ML-DSA signature covers the SIGN_MODE_DIRECT bytes with the extension's
// signature fields cleared. The ethsecp256k1 signature is then made over the
// same transaction with the ML-DSA signature filled in. Registered accounts
// must use SIGN_MODE_DIRECT, so any other mode is rejected.
func SignHybrid(
	ctx context.Context,
	txf tx.Factory,
	txConfig client.TxConfig,
	name string,
	txBuilder client.TxBuilder,
	mldsaPriv *mldsa87.PrivateKey,
) error {
	return signHybrid(ctx, txf, txConfig, name, txBuilder, mldsaPriv, nil)
}

// SignHybridWithExtras signs the --from account and any extra registered
// granters over one shared ML-DSA extension, then secp256k1-signs the from
// account. List every extra granter before signing. Adding a signer changes
// the payload, so a signature made by SignHybrid cannot be reused here.
func SignHybridWithExtras(
	ctx context.Context,
	txf tx.Factory,
	txConfig client.TxConfig,
	name string,
	txBuilder client.TxBuilder,
	mldsaPriv *mldsa87.PrivateKey,
	extras []ExtraMLDSASigner,
) error {
	return signHybrid(ctx, txf, txConfig, name, txBuilder, mldsaPriv, extras)
}

func signHybrid(
	ctx context.Context,
	txf tx.Factory,
	txConfig client.TxConfig,
	name string,
	txBuilder client.TxBuilder,
	mldsaPriv *mldsa87.PrivateKey,
	extras []ExtraMLDSASigner,
) error {
	if txConfig == nil || txConfig.SignModeHandler() == nil {
		return fmt.Errorf("tx config must be set before signing")
	}
	if txf.Keybase() == nil {
		return fmt.Errorf("keyring must be set before signing")
	}
	if mldsaPriv == nil {
		return fmt.Errorf("ML-DSA private key is nil")
	}
	if txf.ChainID() == "" {
		return fmt.Errorf("chain ID is empty")
	}

	signMode := txf.SignMode()
	if signMode == signingtypes.SignMode_SIGN_MODE_UNSPECIFIED {
		signMode = signingtypes.SignMode_SIGN_MODE_DIRECT
	}
	if signMode != signingtypes.SignMode_SIGN_MODE_DIRECT {
		return fmt.Errorf("hybrid signatures require SIGN_MODE_DIRECT, got %s", signMode)
	}

	record, err := txf.Keybase().Key(name)
	if err != nil {
		return fmt.Errorf("load key %s: %w", name, err)
	}
	pubKey, err := record.GetPubKey()
	if err != nil {
		return err
	}
	addr := sdk.AccAddress(pubKey.Address())

	signers, err := txBuilder.GetTx().GetSigners()
	if err != nil {
		return err
	}
	if !isTxSigner(addr, signers) {
		return fmt.Errorf("%s is not a signer of this transaction", addr)
	}

	extBuilder, ok := txBuilder.(client.ExtendedTxBuilder)
	if !ok {
		return fmt.Errorf("transaction builder cannot carry an extension option")
	}

	type mldsaParty struct {
		addr   sdk.AccAddress
		priv   *mldsa87.PrivateKey
		number uint64
		seq    uint64
		pubKey cryptotypes.PubKey
	}
	parties := []mldsaParty{{
		addr:   addr,
		priv:   mldsaPriv,
		number: txf.AccountNumber(),
		seq:    txf.Sequence(),
		pubKey: pubKey,
	}}
	seen := map[string]struct{}{string(addr): {}}
	emptySigs := []pqauthtypes.MLDSASig{{Signer: addr.String()}}
	for _, extra := range extras {
		if extra.Priv == nil {
			return fmt.Errorf("extra ML-DSA private key is nil")
		}
		if extra.PubKey == nil {
			return fmt.Errorf("extra ML-DSA signer public key is nil")
		}
		if extra.Address.Empty() {
			return fmt.Errorf("extra ML-DSA signer address is empty")
		}
		if !extra.Address.Equals(sdk.AccAddress(extra.PubKey.Address())) {
			return fmt.Errorf("extra ML-DSA signer %s does not match its public key", extra.Address)
		}
		if _, ok := seen[string(extra.Address)]; ok {
			continue
		}
		seen[string(extra.Address)] = struct{}{}
		emptySigs = append(emptySigs, pqauthtypes.MLDSASig{Signer: extra.Address.String()})
		parties = append(parties, mldsaParty{
			addr:   extra.Address,
			priv:   extra.Priv,
			number: extra.AccountNumber,
			seq:    extra.Sequence,
			pubKey: extra.PubKey,
		})
	}

	emptyExt, err := withMLDSAExtension(txBuilder.GetTx(), &pqauthtypes.ExtensionOptionMLDSASig{
		Signatures: emptySigs,
	})
	if err != nil {
		return err
	}
	extBuilder.SetExtensionOptions(emptyExt...)

	emptySig := signingtypes.SignatureV2{
		PubKey: pubKey,
		Data: &signingtypes.SingleSignatureData{
			SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: txf.Sequence(),
	}
	if err := txBuilder.SetSignatures(emptySig); err != nil {
		return fmt.Errorf("set signer info: %w", err)
	}

	filled := make([]pqauthtypes.MLDSASig, len(parties))
	for i, party := range parties {
		anyPk, err := codectypes.NewAnyWithValue(party.pubKey)
		if err != nil {
			return err
		}
		signerData := txsigning.SignerData{
			Address:       party.addr.String(),
			ChainID:       txf.ChainID(),
			AccountNumber: party.number,
			Sequence:      party.seq,
			PubKey: &anypb.Any{
				TypeUrl: anyPk.TypeUrl,
				Value:   anyPk.Value,
			},
		}
		mldsaBytes, err := cosmosante.MLDSASignBytes(ctx, txConfig.SignModeHandler(), signerData, txBuilder.GetTx())
		if err != nil {
			return fmt.Errorf("build ML-DSA sign bytes: %w", err)
		}
		mldsaSig, err := party.priv.Sign(rand.Reader, mldsaBytes, nil)
		if err != nil {
			return fmt.Errorf("ML-DSA-87 sign failed: %w", err)
		}
		if len(mldsaSig) != mldsa87.SignatureSize {
			return fmt.Errorf("ML-DSA-87 signature length %d, want %d", len(mldsaSig), mldsa87.SignatureSize)
		}
		filled[i] = pqauthtypes.MLDSASig{Signer: party.addr.String(), Signature: mldsaSig}
	}

	signedExt, err := withMLDSAExtension(txBuilder.GetTx(), &pqauthtypes.ExtensionOptionMLDSASig{
		Signatures: filled,
	})
	if err != nil {
		return err
	}
	extBuilder.SetExtensionOptions(signedExt...)

	secpBytes, err := authsigning.GetSignBytesAdapter(
		ctx,
		txConfig.SignModeHandler(),
		signingtypes.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{
			ChainID:       txf.ChainID(),
			AccountNumber: txf.AccountNumber(),
			Sequence:      txf.Sequence(),
			PubKey:        pubKey,
			Address:       addr.String(),
		},
		txBuilder.GetTx(),
	)
	if err != nil {
		return fmt.Errorf("build ethsecp256k1 sign bytes: %w", err)
	}
	secpSig, _, err := txf.Keybase().Sign(name, secpBytes, signingtypes.SignMode_SIGN_MODE_DIRECT)
	if err != nil {
		return fmt.Errorf("ethsecp256k1 sign failed: %w", err)
	}

	return txBuilder.SetSignatures(signingtypes.SignatureV2{
		PubKey: pubKey,
		Data: &signingtypes.SingleSignatureData{
			SignMode:  signingtypes.SignMode_SIGN_MODE_DIRECT,
			Signature: secpSig,
		},
		Sequence: txf.Sequence(),
	})
}

// withMLDSAExtension keeps every existing extension except a previous ML-DSA
// extension, which is replaced.
func withMLDSAExtension(tx sdk.Tx, ext *pqauthtypes.ExtensionOptionMLDSASig) ([]*codectypes.Any, error) {
	packed, err := codectypes.NewAnyWithValue(ext)
	if err != nil {
		return nil, fmt.Errorf("pack ML-DSA extension: %w", err)
	}

	var opts []*codectypes.Any
	if has, ok := tx.(authante.HasExtensionOptionsTx); ok {
		for _, opt := range has.GetExtensionOptions() {
			if opt == nil || opt.TypeUrl == pqauthtypes.MLDSAExtensionTypeURL {
				continue
			}
			opts = append(opts, opt)
		}
	}
	return append(opts, packed), nil
}

func isTxSigner(addr sdk.AccAddress, signers [][]byte) bool {
	for _, signer := range signers {
		if addr.Equals(sdk.AccAddress(signer)) {
			return true
		}
	}
	return false
}
