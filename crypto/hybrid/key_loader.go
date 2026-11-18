package hybrid

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	cometprivval "github.com/cometbft/cometbft/privval"
)

// MLDSAPrivValidatorKeyFile is the ML-DSA-87 key stored beside the Ed25519
// priv_validator_key.json. The Ed25519 file remains the consensus key.
const MLDSAPrivValidatorKeyFile = "mldsa_priv_validator_key.json"

// MLDSAKeyPath returns the ML-DSA key path for a CometBFT priv_validator_key.json path.
func MLDSAKeyPath(privValidatorKeyFile string) string {
	return filepath.Join(filepath.Dir(privValidatorKeyFile), MLDSAPrivValidatorKeyFile)
}

// MLDSAKeyFile represents the structure for storing ML-DSA keys on disk
type MLDSAKeyFile struct {
	PrivKey []byte `json:"priv_key"`
	PubKey  []byte `json:"pub_key"`
}

// LoadFilePV loads the standard Ed25519 validator key.
// Note: This matches the current signature used in the codebase.
func LoadFilePV(keyFilePath, stateFilePath string) *cometprivval.FilePV {
	return cometprivval.LoadFilePV(keyFilePath, stateFilePath)
}

// LoadMLDSAKey loads an ML-DSA key from a JSON file
func LoadMLDSAKey(filePath string) (*mldsa87.PrivateKey, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read ML-DSA key file: %w", err)
	}

	var keyFile MLDSAKeyFile
	if err := json.Unmarshal(data, &keyFile); err != nil {
		return nil, fmt.Errorf("failed to parse ML-DSA key file: %w", err)
	}

	priv := new(mldsa87.PrivateKey)
	if err := priv.UnmarshalBinary(keyFile.PrivKey); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ML-DSA private key: %w", err)
	}

	return priv, nil
}

// SaveMLDSAKey saves an ML-DSA private key to a JSON file
func SaveMLDSAKey(filePath string, priv *mldsa87.PrivateKey) error {
	pubIface := priv.Public()
	pub, ok := pubIface.(*mldsa87.PublicKey)
	if !ok {
		return fmt.Errorf("unexpected public key type from ML-DSA")
	}

	privBytes, err := priv.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshal ML-DSA private key: %w", err)
	}

	pubBytes, err := pub.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshal ML-DSA public key: %w", err)
	}

	keyFile := MLDSAKeyFile{
		PrivKey: privBytes,
		PubKey:  pubBytes,
	}

	data, err := json.MarshalIndent(keyFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal ML-DSA key file: %w", err)
	}

	return os.WriteFile(filePath, data, 0600)
}

// GenerateHybridKeys creates a new ML-DSA key and saves it.
// Note: We removed fmt.Printf here. Logging will be handled at a higher level.
// GenerateHybridKeys creates a new ML-DSA key and saves it.
func GenerateHybridKeys(mldsaKeyPath string) error {
	priv, err := GenerateMLDSAKey() // Use the new clean function
	if err != nil {
		return fmt.Errorf("failed to generate ML-DSA key: %w", err)
	}

	if err := SaveMLDSAKey(mldsaKeyPath, priv); err != nil {
		return fmt.Errorf("failed to save ML-DSA key: %w", err)
	}

	return nil
}

// GenerateMLDSAKey generates a new ML-DSA-87 private key.
func GenerateMLDSAKey() (*mldsa87.PrivateKey, error) {
	_, priv, err := mldsa87.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ML-DSA key: %w", err)
	}
	return priv, nil
}
