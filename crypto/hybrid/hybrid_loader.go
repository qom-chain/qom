package hybrid

import (
	"fmt"
	"os"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	cometprivval "github.com/cometbft/cometbft/privval"
)

// LoadHybridValidator loads both the Ed25519 validator key and the ML-DSA key.
// It generates a new ML-DSA key if one doesn't exist.
//
// Returns an error if it fails to load or generate the ML-DSA key.
// The Ed25519 key file must already exist.
func LoadHybridValidator(keyFilePath, stateFilePath, mldsaKeyPath string) (*HybridPrivValidator, error) {
	return AttachMLDSAKey(LoadFilePV(keyFilePath, stateFilePath), mldsaKeyPath)
}

// AttachMLDSAKey loads or generates the ML-DSA-87 key and wraps the Ed25519 FilePV.
// SignVote and SignProposal still use the Ed25519 key.
func AttachMLDSAKey(stdPV *cometprivval.FilePV, mldsaKeyPath string) (*HybridPrivValidator, error) {
	if stdPV == nil {
		return nil, fmt.Errorf("Ed25519 validator key is nil")
	}

	mldsaPriv, err := loadOrGenerateMLDSAKey(mldsaKeyPath)
	if err != nil {
		return nil, err
	}
	return NewHybridPrivValidator(stdPV, mldsaPriv), nil
}

func loadOrGenerateMLDSAKey(mldsaKeyPath string) (*mldsa87.PrivateKey, error) {
	_, statErr := os.Stat(mldsaKeyPath)
	if statErr == nil {
		priv, err := LoadMLDSAKey(mldsaKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load existing ML-DSA key: %w", err)
		}
		return priv, nil
	}
	if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("failed to stat ML-DSA key %s: %w", mldsaKeyPath, statErr)
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
