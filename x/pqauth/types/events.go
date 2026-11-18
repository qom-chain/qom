package types

const (
	EventTypeRegisterMLDSAKey          = "register_mldsa_key"
	EventTypeRotateMLDSAKey            = "rotate_mldsa_key"
	EventTypeRegisterValidatorMLDSAKey = "register_validator_mldsa_key"
	EventTypeOpenEVMWindow             = "open_evm_window"

	AttributeKeyConsensusAddress = "consensus_address"

	AttributeKeyAccount = "account"
	AttributeKeyPubKey  = "mldsa_pub_key"
	AttributeKeyExpiry  = "expiry_height"
)
