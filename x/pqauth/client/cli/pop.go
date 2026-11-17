package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/cosmos/evm/crypto/hybrid"
	"github.com/cosmos/evm/x/pqauth/types"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func decodeHexBytes(raw string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(raw), "0x"))
}

func secpPubKeyBytes(clientCtx client.Context) ([]byte, error) {
	if clientCtx.Keyring == nil {
		return nil, fmt.Errorf("keyring is not set")
	}
	if clientCtx.FromName == "" {
		return nil, fmt.Errorf("from key name is empty")
	}
	record, err := clientCtx.Keyring.Key(clientCtx.FromName)
	if err != nil {
		return nil, fmt.Errorf("load key %s: %w", clientCtx.FromName, err)
	}
	pubKey, err := record.GetPubKey()
	if err != nil {
		return nil, err
	}
	if pubKey.Type() != ethsecp256k1.KeyType {
		return nil, fmt.Errorf("key %s has type %q; pqauth proofs require %s", clientCtx.FromName, pubKey.Type(), ethsecp256k1.KeyType)
	}
	return pubKey.Bytes(), nil
}

func signMLDSA(priv *mldsa87.PrivateKey, payload []byte) ([]byte, error) {
	sig, err := priv.Sign(rand.Reader, payload, nil)
	if err != nil {
		return nil, fmt.Errorf("ML-DSA-87 sign failed: %w", err)
	}
	if err := types.ValidateMLDSASignature(sig); err != nil {
		return nil, err
	}
	return sig, nil
}

func proofContext(clientCtx client.Context) (sdk.AccAddress, []byte, error) {
	if clientCtx.ChainID == "" {
		return nil, nil, fmt.Errorf("chain ID is empty; pass --chain-id")
	}
	addr := clientCtx.GetFromAddress()
	if addr.Empty() {
		return nil, nil, fmt.Errorf("from address is empty")
	}
	secpPub, err := secpPubKeyBytes(clientCtx)
	if err != nil {
		return nil, nil, err
	}
	return addr, secpPub, nil
}

// signAccountPopFromFile loads an ML-DSA key file and signs the account
// registration proof. The private key is not returned.
func signAccountPopFromFile(clientCtx client.Context, path string) (pubKey, signature []byte, err error) {
	priv, err := hybrid.LoadMLDSAKey(path)
	if err != nil {
		return nil, nil, err
	}
	pubKey, err = publicKeyBytes(priv)
	if err != nil {
		return nil, nil, err
	}
	addr, secpPub, err := proofContext(clientCtx)
	if err != nil {
		return nil, nil, err
	}
	payload, err := types.AccountPoPSignBytes(addr, clientCtx.ChainID, secpPub, pubKey)
	if err != nil {
		return nil, nil, err
	}
	signature, err = signMLDSA(priv, payload)
	if err != nil {
		return nil, nil, err
	}
	return pubKey, signature, nil
}

func signRotateFromFiles(clientCtx client.Context, oldPath, newPath string) (newPub, oldSig, newSig []byte, err error) {
	oldPriv, err := hybrid.LoadMLDSAKey(oldPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("old ML-DSA key: %w", err)
	}
	newPriv, err := hybrid.LoadMLDSAKey(newPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("new ML-DSA key: %w", err)
	}
	oldPub, err := publicKeyBytes(oldPriv)
	if err != nil {
		return nil, nil, nil, err
	}
	newPub, err = publicKeyBytes(newPriv)
	if err != nil {
		return nil, nil, nil, err
	}
	addr, secpPub, err := proofContext(clientCtx)
	if err != nil {
		return nil, nil, nil, err
	}
	payload, err := types.RotateSignBytes(addr, clientCtx.ChainID, secpPub, oldPub, newPub)
	if err != nil {
		return nil, nil, nil, err
	}
	oldSig, err = signMLDSA(oldPriv, payload)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("old key: %w", err)
	}
	newSig, err = signMLDSA(newPriv, payload)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("new key: %w", err)
	}
	return newPub, oldSig, newSig, nil
}

func signValidatorPopFromFile(clientCtx client.Context, consAddr []byte, keyFile, oldKeyFile string) (pubKey, pop, oldSig []byte, err error) {
	priv, err := hybrid.LoadMLDSAKey(keyFile)
	if err != nil {
		return nil, nil, nil, err
	}
	pubKey, err = publicKeyBytes(priv)
	if err != nil {
		return nil, nil, nil, err
	}
	_, secpPub, err := proofContext(clientCtx)
	if err != nil {
		return nil, nil, nil, err
	}
	payload, err := types.ValidatorPoPSignBytes(clientCtx.ChainID, consAddr, secpPub, pubKey)
	if err != nil {
		return nil, nil, nil, err
	}
	pop, err = signMLDSA(priv, payload)
	if err != nil {
		return nil, nil, nil, err
	}
	if oldKeyFile == "" {
		return pubKey, pop, nil, nil
	}
	oldPriv, err := hybrid.LoadMLDSAKey(oldKeyFile)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("current validator ML-DSA key: %w", err)
	}
	oldSig, err = signMLDSA(oldPriv, payload)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("current validator key: %w", err)
	}
	return pubKey, pop, oldSig, nil
}
