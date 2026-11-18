package hybrid

import (
	"crypto/rand"
	"fmt"

	tmcrypto "github.com/cometbft/cometbft/crypto"
	cometprivval "github.com/cometbft/cometbft/privval"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cometbfttypes "github.com/cometbft/cometbft/types"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
)

// HybridPrivValidator wraps the standard Ed25519 FilePV and adds
// optional ML-DSA (post-quantum) signing support.
type HybridPrivValidator struct {
	StdPV        *cometprivval.FilePV
	MLDSAPriv    *mldsa87.PrivateKey
	MLDSAPub     *mldsa87.PublicKey
	lastMLDSASig []byte // Last ML-DSA signature generated for votes
}

func NewHybridPrivValidator(stdPV *cometprivval.FilePV, mldsaPriv *mldsa87.PrivateKey) *HybridPrivValidator {
	hpv := &HybridPrivValidator{StdPV: stdPV}

	if mldsaPriv != nil {
		hpv.MLDSAPriv = mldsaPriv
		if pub, ok := mldsaPriv.Public().(*mldsa87.PublicKey); ok {
			hpv.MLDSAPub = pub
			SetMLDSAPubKey(pub)

			// Register this node's consensus address. Remote validators are
			// checked from the public key carried in the vote extension.
			if stdPV != nil {
				if addr := []byte(stdPV.GetAddress()); len(addr) > 0 {
					RegisterMLDSAPubKey(ConsensusAddressID(addr), pub)
				}
			}
		}
	}
	return hpv
}

// IsHybridEnabled returns true if an ML-DSA key is loaded.
func (hpv *HybridPrivValidator) IsHybridEnabled() bool {
	return hpv.MLDSAPriv != nil
}

func (hpv *HybridPrivValidator) GetPubKey() (tmcrypto.PubKey, error) {
	return hpv.StdPV.GetPubKey()
}

func (hpv *HybridPrivValidator) AsPrivValidator() cometbfttypes.PrivValidator {
	return hpv
}

// ======================
// ML-DSA Extension Signing
// ======================

func (hpv *HybridPrivValidator) SignMLDSAExtension(chainID string, vote *tmproto.Vote) ([]byte, error) {
	if !hpv.IsHybridEnabled() {
		return nil, nil
	}

	signBytes := cometbfttypes.VoteSignBytes(chainID, vote)
	sig, err := hpv.MLDSAPriv.Sign(rand.Reader, signBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("ml-dsa extension signing failed: %w", err)
	}
	return sig, nil
}

// SignVoteExtension returns the packed ML-DSA-87 vote extension for this
// validator. The Ed25519 consensus key is not used here. CometBFT signs the
// returned bytes with Ed25519 after ExtendVote returns. round and voteType
// are part of the signed payload. voteType must be a precommit.
func (hpv *HybridPrivValidator) SignVoteExtension(chainID string, height int64, round int32, voteType int32, blockHash []byte) ([]byte, error) {
	if hpv == nil || !hpv.IsHybridEnabled() || hpv.MLDSAPriv == nil || hpv.MLDSAPub == nil {
		return nil, fmt.Errorf("ML-DSA vote extension signing is disabled")
	}
	if hpv.StdPV == nil {
		return nil, fmt.Errorf("Ed25519 consensus key is not loaded")
	}
	addr := []byte(hpv.StdPV.GetAddress())
	if len(addr) == 0 {
		return nil, fmt.Errorf("consensus address is empty")
	}

	msg, err := VoteExtensionSignBytes(chainID, height, round, voteType, blockHash, addr)
	if err != nil {
		return nil, err
	}
	sig, err := hpv.MLDSAPriv.Sign(rand.Reader, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("ML-DSA-87 vote extension signing failed: %w", err)
	}
	pubKey, err := hpv.MLDSAPub.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to encode ML-DSA-87 public key: %w", err)
	}
	if round < 0 || voteType < 0 || voteType > 255 {
		return nil, fmt.Errorf("vote extension round %d type %d is out of range", round, voteType)
	}
	return PackVoteExtension(uint32(round), byte(voteType), pubKey, sig)
}

// GetLastMLDSASignature returns the last ML-DSA signature generated.
func (hpv *HybridPrivValidator) GetLastMLDSASignature() []byte {
	return hpv.lastMLDSASig
}

// GetMLDSAPubKey returns the ML-DSA-87 public key loaded for this validator.
// It is nil when hybrid signing is disabled.
func (hpv *HybridPrivValidator) GetMLDSAPubKey() *mldsa87.PublicKey {
	return hpv.MLDSAPub
}

// ======================
// Normal Signing (Ed25519 ONLY)
// ======================

func (hpv *HybridPrivValidator) SignProposal(chainID string, proposal *tmproto.Proposal) error {
	return hpv.StdPV.SignProposal(chainID, proposal)
}

func (hpv *HybridPrivValidator) SignVote(chainID string, vote *tmproto.Vote) error {
	// On the round-aware CometBFT tree, ExtendVote already signs with req.Round
	// and req.VoteType, so VoteExtensionCommits matches and this rewrite is a
	// no-op. It is kept for the case where the ABCI request omitted those
	// fields (stock or socket/gRPC CometBFT). Rewriting here without a matching
	// VerifyVoteExtension round is what halted a multi-validator set at r>0
	// on stock CometBFT; the patched requests and handlers must stay in sync.
	if hpv.IsHybridEnabled() && vote != nil && vote.Type == tmproto.PrecommitType && len(vote.BlockID.Hash) > 0 {
		if !VoteExtensionCommits(vote.Extension, vote.Round, int32(vote.Type)) {
			ext, err := hpv.SignVoteExtension(chainID, vote.Height, vote.Round, int32(vote.Type), vote.BlockID.Hash)
			if err != nil {
				return err
			}
			vote.Extension = ext
		}
	}
	return hpv.StdPV.SignVote(chainID, vote)
}

// ======================
// Hybrid Signing (for future use)
// ======================

func (hpv *HybridPrivValidator) SignProposalHybrid(chainID string, proposal *tmproto.Proposal) (ed25519Sig, mldsaSig []byte, err error) {
	signBytes := cometbfttypes.ProposalSignBytes(chainID, proposal)

	ed25519Sig, err = hpv.StdPV.Key.PrivKey.Sign(signBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("ed25519 proposal signing failed: %w", err)
	}

	if hpv.IsHybridEnabled() {
		mldsaSig, err = hpv.MLDSAPriv.Sign(rand.Reader, signBytes, nil)
		if err != nil {
			return ed25519Sig, nil, fmt.Errorf("ml-dsa proposal signing failed: %w", err)
		}
	}
	return ed25519Sig, mldsaSig, nil
}

func (hpv *HybridPrivValidator) SignVoteHybrid(chainID string, vote *tmproto.Vote) (ed25519Sig, mldsaSig []byte, err error) {
	signBytes := cometbfttypes.VoteSignBytes(chainID, vote)

	ed25519Sig, err = hpv.StdPV.Key.PrivKey.Sign(signBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("ed25519 vote signing failed: %w", err)
	}

	if hpv.IsHybridEnabled() {
		mldsaSig, err = hpv.MLDSAPriv.Sign(rand.Reader, signBytes, nil)
		if err != nil {
			return ed25519Sig, nil, fmt.Errorf("ml-dsa vote signing failed: %w", err)
		}

		hpv.lastMLDSASig = mldsaSig
		SetLastMLDSASignature(mldsaSig)
	} else {
		hpv.lastMLDSASig = nil
	}

	return ed25519Sig, mldsaSig, nil
}

// ======================
// Package-level helpers
// ======================

var lastMLDSASignature []byte
var lastMLDSAPubKey *mldsa87.PublicKey

func SetLastMLDSASignature(sig []byte) {
	lastMLDSASignature = sig
}

func GetLastMLDSASignature() []byte {
	return lastMLDSASignature
}

func SetMLDSAPubKey(pub *mldsa87.PublicKey) {
	lastMLDSAPubKey = pub
}

// ======================
// Global access
// ======================

var globalHybridValidator *HybridPrivValidator

func SetGlobalHybridValidator(hpv *HybridPrivValidator) {
	globalHybridValidator = hpv
}

func GetGlobalHybridValidator() *HybridPrivValidator {
	return globalHybridValidator
}
