package types

const (
	EventTypeRegisterMLDSAKey          = "register_mldsa_key"
	EventTypeRotateMLDSAKey            = "rotate_mldsa_key"
	EventTypeRegisterValidatorMLDSAKey = "register_validator_mldsa_key"
	EventTypeOpenEVMWindow             = "open_evm_window"

	// EventTypeJailValidatorMissingMLDSAKey is emitted when the validator key
	// guard jails a validator that has no ML-DSA key.
	EventTypeJailValidatorMissingMLDSAKey = "jail_validator_missing_mldsa_key"

	AttributeKeyValidator = "validator"

	AttributeKeyConsensusAddress = "consensus_address"

	AttributeKeyAccount = "account"
	AttributeKeyPubKey  = "mldsa_pub_key"
	AttributeKeyExpiry  = "expiry_height"
)
