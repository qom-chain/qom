package hybrid

import (
	"sync"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
)

// MLDSAPubKeyRegistry is a thread-safe registry for validator ML-DSA public keys
type MLDSAPubKeyRegistry struct {
	mu   sync.RWMutex
	keys map[string]*mldsa87.PublicKey // validator consensus address (hex) -> pubkey
}

var (
	globalRegistry = &MLDSAPubKeyRegistry{
		keys: make(map[string]*mldsa87.PublicKey),
	}
)

// RegisterMLDSAPubKey registers a validator's ML-DSA public key
func RegisterMLDSAPubKey(validatorAddr string, pubKey *mldsa87.PublicKey) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.keys[validatorAddr] = pubKey
}

// GetMLDSAPubKey returns the ML-DSA public key for a validator
func GetMLDSAPubKey(validatorAddr string) *mldsa87.PublicKey {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	return globalRegistry.keys[validatorAddr]
}

// GetGlobalRegistry returns the registry (for debugging)
func GetGlobalRegistry() *MLDSAPubKeyRegistry {
	return globalRegistry
}

// ResetMLDSAPubKeys clears the in-memory registry. Tests use it.
func ResetMLDSAPubKeys() {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.keys = make(map[string]*mldsa87.PublicKey)
}
