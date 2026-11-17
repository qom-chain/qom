package hybrid

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
)

const (
	// VoteExtensionDomain separates this payload from every other ML-DSA message
	// the chain signs.
	VoteExtensionDomain = "qomx-mldsa-vote-ext-v1"

	// VoteExtensionsEnableHeight is the first block height at which ML-DSA vote
	// extensions are required.
	//
	// Default genesis sets this on the consensus params:
	//
	//	consensus.params.abci.vote_extensions_enable_height = 1
	//
	// A value of 0 leaves vote extensions disabled. The consensus key remains Ed25519.
	VoteExtensionsEnableHeight int64 = 1
)

// Compile-time check: packed sizes stay on the FIPS 204 ML-DSA-87 values.
const (
	mldsa87PubKeySize    = 2592
	mldsa87SignatureSize = 4627

	_ = uint64(mldsa87PubKeySize - mldsa87.PublicKeySize)
	_ = uint64(mldsa87.PublicKeySize - mldsa87PubKeySize)
	_ = uint64(mldsa87SignatureSize - mldsa87.SignatureSize)
	_ = uint64(mldsa87.SignatureSize - mldsa87SignatureSize)
)

const (
	voteExtensionRoundLen  = 4
	voteExtensionTypeLen   = 1
	voteExtensionPrefixLen = voteExtensionRoundLen + voteExtensionTypeLen
)

// VoteExtensionPrecommit is CometBFT's precommit vote type.
const VoteExtensionPrecommit = int32(cmtproto.PrecommitType)

// VoteExtensionSize is the packed vote extension:
// round (uint32) || vote type (byte) || public key || signature.
const VoteExtensionSize = voteExtensionPrefixLen + mldsa87PubKeySize + mldsa87SignatureSize

// ConsensusAddressID is the registry key for a CometBFT consensus address.
// It matches crypto.Address.String (uppercase hex).
func ConsensusAddressID(addr []byte) string {
	return strings.ToUpper(hex.EncodeToString(addr))
}

// VoteExtensionSignBytes is the ML-DSA message for one precommit.
//
// The payload includes the round and the vote type. ExtendVote and
// VerifyVoteExtension receive both from the in-process CometBFT request, and
// SignVote writes the extension again from the vote itself before Ed25519
// signs it. A socket ABCI client does not receive those fields.
func VoteExtensionSignBytes(chainID string, height int64, round int32, voteType int32, blockHash, validatorAddr []byte) ([]byte, error) {
	if chainID == "" {
		return nil, fmt.Errorf("vote extension chain ID is empty")
	}
	if height <= 0 {
		return nil, fmt.Errorf("vote extension height must be positive, got %d", height)
	}
	if round < 0 {
		return nil, fmt.Errorf("vote extension round must not be negative, got %d", round)
	}
	if voteType != VoteExtensionPrecommit {
		return nil, fmt.Errorf("vote extension vote type must be precommit (%d), got %d", VoteExtensionPrecommit, voteType)
	}
	if len(blockHash) == 0 {
		return nil, fmt.Errorf("vote extension block hash is empty")
	}
	if len(validatorAddr) == 0 {
		return nil, fmt.Errorf("vote extension validator address is empty")
	}

	var buf bytes.Buffer
	buf.WriteString(VoteExtensionDomain)
	if err := writePrefixed(&buf, []byte(chainID)); err != nil {
		return nil, err
	}
	var heightBytes [8]byte
	binary.BigEndian.PutUint64(heightBytes[:], uint64(height))
	buf.Write(heightBytes[:])
	var roundBytes [4]byte
	binary.BigEndian.PutUint32(roundBytes[:], uint32(round))
	buf.Write(roundBytes[:])
	buf.WriteByte(byte(voteType))
	if err := writePrefixed(&buf, blockHash); err != nil {
		return nil, err
	}
	if err := writePrefixed(&buf, validatorAddr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writePrefixed(buf *bytes.Buffer, b []byte) error {
	if len(b) > int(^uint32(0)) {
		return fmt.Errorf("vote extension field is too long: %d bytes", len(b))
	}
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b)))
	buf.Write(n[:])
	buf.Write(b)
	return nil
}

// PackVoteExtension concatenates the round, vote type, ML-DSA-87 public key,
// and signature.
func PackVoteExtension(round uint32, voteType byte, pubKey, signature []byte) ([]byte, error) {
	if len(pubKey) != mldsa87PubKeySize {
		return nil, fmt.Errorf("ML-DSA-87 public key must be %d bytes, got %d", mldsa87PubKeySize, len(pubKey))
	}
	if len(signature) != mldsa87SignatureSize {
		return nil, fmt.Errorf("ML-DSA-87 signature must be %d bytes, got %d", mldsa87SignatureSize, len(signature))
	}
	out := make([]byte, 0, VoteExtensionSize)
	var roundBytes [voteExtensionRoundLen]byte
	binary.BigEndian.PutUint32(roundBytes[:], round)
	out = append(out, roundBytes[:]...)
	out = append(out, voteType)
	out = append(out, pubKey...)
	out = append(out, signature...)
	return out, nil
}

// UnpackVoteExtension splits a packed extension into the round, vote type,
// public key, and signature.
func UnpackVoteExtension(extension []byte) (round uint32, voteType byte, pubKey, signature []byte, err error) {
	if len(extension) == 0 {
		return 0, 0, nil, nil, fmt.Errorf("vote extension is empty")
	}
	if len(extension) != VoteExtensionSize {
		return 0, 0, nil, nil, fmt.Errorf("vote extension length %d, want %d", len(extension), VoteExtensionSize)
	}
	round = binary.BigEndian.Uint32(extension[:voteExtensionRoundLen])
	voteType = extension[voteExtensionRoundLen]
	rest := extension[voteExtensionPrefixLen:]
	pubKey = rest[:mldsa87PubKeySize]
	signature = rest[mldsa87PubKeySize:]
	return round, voteType, pubKey, signature, nil
}

// VoteExtensionCommits reports whether extension's clear prefix matches round
// and voteType. It does not check the signature.
func VoteExtensionCommits(extension []byte, round int32, voteType int32) bool {
	gotRound, gotType, _, _, err := UnpackVoteExtension(extension)
	if err != nil || round < 0 || voteType < 0 || voteType > 255 {
		return false
	}
	return gotRound == uint32(round) && int32(gotType) == voteType
}

// VerifyVoteExtensionSignature checks an ML-DSA-87 vote extension against the
// on-chain public key for the validator.
//
// expectedPubKey is required. A missing key, a different embedded key, a
// round or vote-type mismatch, or an invalid signature is rejected. The
// process-local registry is not consulted.
func VerifyVoteExtensionSignature(chainID string, height int64, round int32, voteType int32, blockHash, validatorAddr, extension, expectedPubKey []byte) error {
	if len(expectedPubKey) == 0 {
		return fmt.Errorf("validator %s has no ML-DSA public key on chain", ConsensusAddressID(validatorAddr))
	}
	if len(expectedPubKey) != mldsa87PubKeySize {
		return fmt.Errorf("on-chain ML-DSA public key must be %d bytes, got %d", mldsa87PubKeySize, len(expectedPubKey))
	}

	gotRound, gotType, pubKeyBytes, signature, err := UnpackVoteExtension(extension)
	if err != nil {
		return err
	}
	if round < 0 || gotRound != uint32(round) {
		return fmt.Errorf("vote extension round %d does not match vote round %d", gotRound, round)
	}
	if int32(gotType) != voteType {
		return fmt.Errorf("vote extension type %d does not match vote type %d", gotType, voteType)
	}
	if !bytes.Equal(pubKeyBytes, expectedPubKey) {
		return fmt.Errorf("validator %s ML-DSA public key does not match the on-chain key", ConsensusAddressID(validatorAddr))
	}

	msg, err := VoteExtensionSignBytes(chainID, height, round, voteType, blockHash, validatorAddr)
	if err != nil {
		return err
	}
	var pubKey mldsa87.PublicKey
	if err := pubKey.UnmarshalBinary(pubKeyBytes); err != nil {
		return fmt.Errorf("vote extension public key is not a valid ML-DSA-87 key: %w", err)
	}
	if !mldsa87.Verify(&pubKey, msg, nil, signature) {
		return fmt.Errorf("ML-DSA-87 vote extension signature is invalid")
	}
	return nil
}
