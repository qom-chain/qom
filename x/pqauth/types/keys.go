package types

const (
	// ModuleName is the name of the pqauth module.
	ModuleName = "pqauth"

	// StoreKey is the KV store key for account → ML-DSA public key bindings.
	StoreKey = ModuleName

	// RouterKey is the message route for the module.
	RouterKey = ModuleName
)

// MLDSA87PubKeySize is the FIPS 204 packed ML-DSA-87 public key size.
const MLDSA87PubKeySize = 2592

// MLDSA87SignatureSize is the FIPS 204 packed ML-DSA-87 signature size.
const MLDSA87SignatureSize = 4627

// ConsensusAddressSize is the CometBFT Ed25519 consensus address length.
const ConsensusAddressSize = 20

const (
	// KeyPrefixMLDSAPubKey prefixes account → ML-DSA-87 public key entries.
	KeyPrefixMLDSAPubKey byte = 0x01

	// KeyPrefixEVMWindow prefixes account → EVM window expiry entries.
	KeyPrefixEVMWindow byte = 0x02

	// KeyPrefixParams is the single key under which module parameters are stored.
	KeyPrefixParams byte = 0x03

	// KeyPrefixValidatorMLDSA prefixes consensus address → validator ML-DSA key.
	KeyPrefixValidatorMLDSA byte = 0x04
)

// ParamsKey is the KV key for the marshalled module parameters.
func ParamsKey() []byte {
	return []byte{KeyPrefixParams}
}

// MLDSAPubKeyStoreKey returns the store key for an account's ML-DSA public key.
func MLDSAPubKeyStoreKey(addr []byte) []byte {
	return prefixedStoreKey(KeyPrefixMLDSAPubKey, addr)
}

// EVMWindowStoreKey returns the store key for an account's EVM window expiry.
func EVMWindowStoreKey(addr []byte) []byte {
	return prefixedStoreKey(KeyPrefixEVMWindow, addr)
}

// ValidatorMLDSAStoreKey returns the store key for a validator consensus address.
func ValidatorMLDSAStoreKey(consAddr []byte) []byte {
	return prefixedStoreKey(KeyPrefixValidatorMLDSA, consAddr)
}

func prefixedStoreKey(prefix byte, addr []byte) []byte {
	key := make([]byte, 1+len(addr))
	key[0] = prefix
	copy(key[1:], addr)
	return key
}
