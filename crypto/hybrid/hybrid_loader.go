package hybrid

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	cometprivval "github.com/cometbft/cometbft/privval"
)

// LoadHybridValidator loads both the Ed25519 validator key and the ML-DSA key.
// boundPubKey is the on-chain or genesis ML-DSA public key for this validator,
// or nil if no binding exists (a fresh node may generate a key).
//
// The Ed25519 key file must already exist.
func LoadHybridValidator(keyFilePath, stateFilePath, mldsaKeyPath string, boundPubKey []byte) (*HybridPrivValidator, error) {
	return AttachMLDSAKey(LoadFilePV(keyFilePath, stateFilePath), mldsaKeyPath, boundPubKey)
}

// AttachMLDSAKey loads the ML-DSA-87 key and wraps the Ed25519 FilePV.
// SignVote and SignProposal still use the Ed25519 key.
//
// boundPubKey is the ML-DSA public key already bound to this validator's
// consensus address in app state or genesis. When it is non-empty, a missing
// or mismatched key file is a hard error and a new key is never generated.
// Auto-generation is only for a fresh node with no binding.
func AttachMLDSAKey(stdPV *cometprivval.FilePV, mldsaKeyPath string, boundPubKey []byte) (*HybridPrivValidator, error) {
	if stdPV == nil {
		return nil, fmt.Errorf("Ed25519 validator key is nil")
	}

	mldsaPriv, err := loadOrGenerateMLDSAKey(mldsaKeyPath, []byte(stdPV.GetAddress()), boundPubKey)
	if err != nil {
		return nil, err
	}
	return NewHybridPrivValidator(stdPV, mldsaPriv), nil
}

func loadOrGenerateMLDSAKey(mldsaKeyPath string, consAddr, boundPubKey []byte) (*mldsa87.PrivateKey, error) {
	_, statErr := os.Stat(mldsaKeyPath)
	if statErr == nil {
		priv, err := LoadMLDSAKey(mldsaKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load existing ML-DSA key: %w", err)
		}
		if err := checkBoundMLDSAKey(priv, consAddr, boundPubKey, mldsaKeyPath); err != nil {
			return nil, err
		}
		return priv, nil
	}
	if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("failed to stat ML-DSA key %s: %w", mldsaKeyPath, statErr)
	}

	if len(boundPubKey) > 0 {
		return nil, fmt.Errorf("ML-DSA key file %s is missing, but consensus address %X has an on-chain or genesis ML-DSA binding; refusing to auto-generate a new key", mldsaKeyPath, consAddr)
	}

	if err := GenerateHybridKeys(mldsaKeyPath); err != nil {
		return nil, fmt.Errorf("failed to generate ML-DSA key: %w", err)
	}
	priv, err := LoadMLDSAKey(mldsaKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load newly generated ML-DSA key: %w", err)
	}
	return priv, nil
}

func checkBoundMLDSAKey(priv *mldsa87.PrivateKey, consAddr, boundPubKey []byte, mldsaKeyPath string) error {
	if len(boundPubKey) == 0 {
		return nil
	}
	pub, ok := priv.Public().(*mldsa87.PublicKey)
	if !ok || pub == nil {
		return fmt.Errorf("ML-DSA key file %s has no public key", mldsaKeyPath)
	}
	got, err := pub.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to encode ML-DSA public key from %s: %w", mldsaKeyPath, err)
	}
	if !bytes.Equal(got, boundPubKey) {
		return fmt.Errorf("ML-DSA key file %s does not match the on-chain or genesis binding for consensus address %X", mldsaKeyPath, consAddr)
	}
	return nil
}

// ResolveValidatorMLDSABinding returns the ML-DSA public key bound to consAddr.
// A store lookup (committed app state) wins when present, so a later key
// rotation is honoured. Otherwise the genesis pqauth validator_keys entry is
// used. found is false when neither source has a binding.
func ResolveValidatorMLDSABinding(consAddr []byte, genesisFile string, storeLookup func([]byte) ([]byte, bool)) ([]byte, bool, error) {
	if storeLookup != nil {
		if pub, ok := storeLookup(consAddr); ok && len(pub) > 0 {
			return pub, true, nil
		}
	}
	if genesisFile == "" {
		return nil, false, nil
	}
	return LookupGenesisValidatorMLDSAPubKey(genesisFile, consAddr)
}

// LookupGenesisValidatorMLDSAPubKey returns the ML-DSA-87 public key bound to
// consAddr in genesis app_state.pqauth.validator_keys, if any.
func LookupGenesisValidatorMLDSAPubKey(genesisFile string, consAddr []byte) ([]byte, bool, error) {
	raw, err := os.ReadFile(genesisFile)
	if err != nil {
		return nil, false, fmt.Errorf("read genesis for ML-DSA binding: %w", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, false, fmt.Errorf("decode genesis for ML-DSA binding: %w", err)
	}
	appRaw, ok := root["app_state"]
	if !ok || len(appRaw) == 0 {
		return nil, false, nil
	}
	var app map[string]json.RawMessage
	if err := json.Unmarshal(appRaw, &app); err != nil {
		return nil, false, fmt.Errorf("decode genesis app_state: %w", err)
	}
	pqRaw, ok := app["pqauth"]
	if !ok || len(pqRaw) == 0 || string(pqRaw) == "null" {
		return nil, false, nil
	}
	var pq struct {
		ValidatorKeys []struct {
			ConsensusAddress json.RawMessage `json:"consensus_address"`
			PubKey           json.RawMessage `json:"pub_key"`
		} `json:"validator_keys"`
	}
	if err := json.Unmarshal(pqRaw, &pq); err != nil {
		return nil, false, fmt.Errorf("decode genesis pqauth: %w", err)
	}
	for _, entry := range pq.ValidatorKeys {
		addr, err := decodeGenesisBytes(entry.ConsensusAddress)
		if err != nil {
			return nil, false, fmt.Errorf("decode genesis validator consensus address: %w", err)
		}
		if !bytes.Equal(addr, consAddr) {
			continue
		}
		pub, err := decodeGenesisBytes(entry.PubKey)
		if err != nil {
			return nil, false, fmt.Errorf("decode genesis validator ML-DSA public key: %w", err)
		}
		if len(pub) == 0 {
			return nil, false, nil
		}
		return pub, true, nil
	}
	return nil, false, nil
}

func decodeGenesisBytes(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if asString == "" {
			return nil, nil
		}
		if b, err := base64.StdEncoding.DecodeString(asString); err == nil {
			return b, nil
		}
		if b, err := hex.DecodeString(asString); err == nil {
			return b, nil
		}
		return nil, fmt.Errorf("not standard-base64 or hex: %q", asString)
	}
	var asBytes []byte
	if err := json.Unmarshal(raw, &asBytes); err != nil {
		return nil, err
	}
	return asBytes, nil
}
