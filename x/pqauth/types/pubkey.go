package types

import (
	"github.com/cloudflare/circl/sign/mldsa/mldsa87"

	errorsmod "cosmossdk.io/errors"
)

// Compile-time check that the stored key size matches circl's ML-DSA-87.
const (
	_ = uint64(MLDSA87PubKeySize - mldsa87.PublicKeySize)
	_ = uint64(mldsa87.PublicKeySize - MLDSA87PubKeySize)
)

// ValidateMLDSAPubKey checks that bz is a packed ML-DSA-87 public key.
func ValidateMLDSAPubKey(bz []byte) error {
	if len(bz) != MLDSA87PubKeySize {
		return errorsmod.Wrapf(
			ErrInvalidMLDSAPubKey,
			"expected %d bytes, got %d",
			MLDSA87PubKeySize,
			len(bz),
		)
	}

	var pk mldsa87.PublicKey
	if err := pk.UnmarshalBinary(bz); err != nil {
		return errorsmod.Wrapf(ErrInvalidMLDSAPubKey, "%s", err.Error())
	}
	return nil
}
