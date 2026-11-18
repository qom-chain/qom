package dilithium

import (
	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	evmtypes "github.com/cosmos/evm/x/vm/types"
)

var _ vm.PrecompiledContract = &Precompile{}

// ML-DSA-87 packed sizes (FIPS 204). These match circl's mldsa87 constants.
const (
	mlDsa87PubKeySize    = 2592
	mlDsa87SignatureSize = 4627
)

// Compile-time check: circl's ML-DSA-87 sizes stay on the FIPS 204 values.
const (
	_ = uint64(mlDsa87PubKeySize - mldsa87.PublicKeySize)
	_ = uint64(mldsa87.PublicKeySize - mlDsa87PubKeySize)
	_ = uint64(mlDsa87SignatureSize - mldsa87.SignatureSize)
	_ = uint64(mldsa87.SignatureSize - mlDsa87SignatureSize)
)

// Precompile implements ML-DSA-87 (FIPS 204) post-quantum signature verification.
type Precompile struct{}

// Name returns the name of the precompile.
func (Precompile) Name() string {
	return "dilithium"
}

// Address returns the address of the precompile.
func (Precompile) Address() common.Address {
	return common.HexToAddress(evmtypes.DilithiumPrecompileAddress)
}

// RequiredGas returns the gas cost for ML-DSA verification.
func (Precompile) RequiredGas(input []byte) uint64 {
	return 250_000 + uint64(len(input))*8
}

// Run executes the ML-DSA-87 precompile.
//
// Input format (raw concatenation):
// message || pubKey (2592 bytes) || signature (4627 bytes)
func (p Precompile) Run(_ *vm.EVM, contract *vm.Contract, _ bool) ([]byte, error) {
	input := contract.Input

	if len(input) < mlDsa87PubKeySize+mlDsa87SignatureSize {
		return nil, vm.ErrExecutionReverted
	}

	sigStart := len(input) - mlDsa87SignatureSize
	pubKeyStart := sigStart - mlDsa87PubKeySize

	message := input[:pubKeyStart]
	pubKey := input[pubKeyStart:sigStart]
	signature := input[sigStart:]

	var pk mldsa87.PublicKey
	if err := pk.UnmarshalBinary(pubKey); err != nil {
		return nil, vm.ErrExecutionReverted
	}

	if mldsa87.Verify(&pk, message, nil, signature) {
		return common.LeftPadBytes([]byte{0x01}, 32), nil
	}
	return common.LeftPadBytes([]byte{0x00}, 32), nil
}
