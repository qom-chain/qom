package types

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg              = &MsgRegisterMLDSAKey{}
	_ sdk.Msg              = &MsgRotateMLDSAKey{}
	_ sdk.Msg              = &MsgRegisterValidatorMLDSAKey{}
	_ sdk.Msg              = &MsgOpenEVMWindow{}
	_ sdk.HasValidateBasic = &MsgRegisterMLDSAKey{}
	_ sdk.HasValidateBasic = &MsgRotateMLDSAKey{}
	_ sdk.HasValidateBasic = &MsgRegisterValidatorMLDSAKey{}
	_ sdk.HasValidateBasic = &MsgOpenEVMWindow{}
)

// ValidateBasic checks the signer, the ML-DSA-87 public key, and the proof.
func (m *MsgRegisterMLDSAKey) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errorsmod.Wrap(err, "invalid signer address")
	}
	if err := ValidateMLDSAPubKey(m.PubKey); err != nil {
		return err
	}
	return ValidateMLDSASignature(m.PopSignature)
}

// GetSignBytes implements the LegacyMsg interface.
func (m MsgRegisterMLDSAKey) GetSignBytes() []byte {
	return AminoCdc.MustMarshalJSON(&m)
}

// ValidateBasic checks the signer, the replacement key, and both signatures.
func (m *MsgRotateMLDSAKey) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errorsmod.Wrap(err, "invalid signer address")
	}
	if err := ValidateMLDSAPubKey(m.NewPubKey); err != nil {
		return err
	}
	if err := ValidateMLDSASignature(m.OldSignature); err != nil {
		return errorsmod.Wrap(err, "old signature")
	}
	return errorsmod.Wrap(ValidateMLDSASignature(m.NewSignature), "new signature")
}

// GetSignBytes implements the LegacyMsg interface.
func (m MsgRotateMLDSAKey) GetSignBytes() []byte {
	return AminoCdc.MustMarshalJSON(&m)
}

// ValidateBasic checks the operator, consensus address, key, and proof.
// An empty old signature is accepted here. The handler requires it when a
// key is already stored.
func (m *MsgRegisterValidatorMLDSAKey) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errorsmod.Wrap(err, "invalid signer address")
	}
	if len(m.ConsensusAddress) != ConsensusAddressSize {
		return errorsmod.Wrapf(ErrInvalidValidator, "consensus address must be %d bytes, got %d", ConsensusAddressSize, len(m.ConsensusAddress))
	}
	if err := ValidateMLDSAPubKey(m.PubKey); err != nil {
		return err
	}
	if err := ValidateMLDSASignature(m.PopSignature); err != nil {
		return err
	}
	if len(m.OldSignature) == 0 {
		return nil
	}
	return errorsmod.Wrap(ValidateMLDSASignature(m.OldSignature), "old signature")
}

// GetSignBytes implements the LegacyMsg interface.
func (m MsgRegisterValidatorMLDSAKey) GetSignBytes() []byte {
	return AminoCdc.MustMarshalJSON(&m)
}

// ValidateBasic checks the signer address.
func (m *MsgOpenEVMWindow) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errorsmod.Wrap(err, "invalid signer address")
	}
	return nil
}

// GetSignBytes implements the LegacyMsg interface.
func (m MsgOpenEVMWindow) GetSignBytes() []byte {
	return AminoCdc.MustMarshalJSON(&m)
}
