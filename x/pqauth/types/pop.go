package types

import (
	"bytes"
	"encoding/binary"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"

	errorsmod "cosmossdk.io/errors"
)

const (
	// AccountPoPDomain separates an account proof of possession from every
	// other ML-DSA message. The signed bytes are this domain, the account
	// address, the chain ID, the ethsecp256k1 public key, and the ML-DSA
	// public key being registered.
	AccountPoPDomain = "qomx-mldsa-account-pop-v1"

	// RotateDomain separates a key rotation from registration. Both the
	// current key and the replacement key sign these bytes.
	RotateDomain = "qomx-mldsa-rotate-v1"

	// ValidatorPoPDomain separates a validator consensus-key binding from an
	// account binding. The signed bytes include the consensus address and the
	// new ML-DSA public key.
	ValidatorPoPDomain = "qomx-mldsa-validator-pop-v1"
)

const (
	_ = uint64(MLDSA87SignatureSize - mldsa87.SignatureSize)
	_ = uint64(mldsa87.SignatureSize - MLDSA87SignatureSize)
)

// ValidateMLDSASignature checks that sig is a packed ML-DSA-87 signature.
func ValidateMLDSASignature(sig []byte) error {
	if len(sig) != MLDSA87SignatureSize {
		return errorsmod.Wrapf(ErrInvalidProof, "ML-DSA-87 signature must be %d bytes, got %d", MLDSA87SignatureSize, len(sig))
	}
	return nil
}

// AccountPoPSignBytes is the payload signed by MsgRegisterMLDSAKey.
func AccountPoPSignBytes(addr []byte, chainID string, secpPubKey, mldsaPubKey []byte) ([]byte, error) {
	if err := ValidateMLDSAPubKey(mldsaPubKey); err != nil {
		return nil, err
	}
	return domainSignBytes(AccountPoPDomain, addr, chainID, secpPubKey, mldsaPubKey)
}

// RotateSignBytes is the payload signed by both keys in MsgRotateMLDSAKey.
func RotateSignBytes(addr []byte, chainID string, secpPubKey, oldPub, newPub []byte) ([]byte, error) {
	if err := ValidateMLDSAPubKey(oldPub); err != nil {
		return nil, errorsmod.Wrap(err, "current ML-DSA public key")
	}
	if err := ValidateMLDSAPubKey(newPub); err != nil {
		return nil, errorsmod.Wrap(err, "new ML-DSA public key")
	}
	if bytes.Equal(oldPub, newPub) {
		return nil, errorsmod.Wrap(ErrInvalidProof, "replacement ML-DSA public key matches the current key")
	}
	return domainSignBytes(RotateDomain, addr, chainID, secpPubKey, oldPub, newPub)
}

// ValidatorPoPSignBytes is the payload signed by MsgRegisterValidatorMLDSAKey.
// A replacement also requires the current key's signature over these bytes.
func ValidatorPoPSignBytes(chainID string, consAddr, secpPubKey, mldsaPubKey []byte) ([]byte, error) {
	if len(consAddr) != ConsensusAddressSize {
		return nil, errorsmod.Wrapf(ErrInvalidValidator, "consensus address must be %d bytes, got %d", ConsensusAddressSize, len(consAddr))
	}
	if err := ValidateMLDSAPubKey(mldsaPubKey); err != nil {
		return nil, err
	}
	return domainSignBytes(ValidatorPoPDomain, consAddr, chainID, secpPubKey, mldsaPubKey)
}

// VerifyMLDSA reports whether signature is a valid ML-DSA-87 signature by
// pubKey over message.
func VerifyMLDSA(pubKey, message, signature []byte) error {
	if err := ValidateMLDSASignature(signature); err != nil {
		return err
	}
	var pk mldsa87.PublicKey
	if err := pk.UnmarshalBinary(pubKey); err != nil {
		return errorsmod.Wrapf(ErrInvalidMLDSAPubKey, "%s", err.Error())
	}
	if !mldsa87.Verify(&pk, message, nil, signature) {
		return errorsmod.Wrap(ErrInvalidProof, "ML-DSA-87 signature does not verify")
	}
	return nil
}

func domainSignBytes(domain string, addr []byte, chainID string, fields ...[]byte) ([]byte, error) {
	if domain == "" {
		return nil, errorsmod.Wrap(ErrInvalidProof, "proof domain is empty")
	}
	if len(addr) == 0 {
		return nil, errorsmod.Wrap(ErrInvalidProof, "proof address is empty")
	}
	if chainID == "" {
		return nil, errorsmod.Wrap(ErrInvalidProof, "proof chain ID is empty")
	}
	var buf bytes.Buffer
	buf.WriteString(domain)
	if err := writeProofField(&buf, addr); err != nil {
		return nil, err
	}
	if err := writeProofField(&buf, []byte(chainID)); err != nil {
		return nil, err
	}
	for _, field := range fields {
		if len(field) == 0 {
			return nil, errorsmod.Wrap(ErrInvalidProof, "proof public key is empty")
		}
		if err := writeProofField(&buf, field); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func writeProofField(buf *bytes.Buffer, b []byte) error {
	if len(b) > int(^uint32(0)) {
		return errorsmod.Wrapf(ErrInvalidProof, "proof field is too long: %d bytes", len(b))
	}
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b)))
	buf.Write(n[:])
	buf.Write(b)
	return nil
}
