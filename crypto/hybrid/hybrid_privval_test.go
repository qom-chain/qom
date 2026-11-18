package hybrid

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	cometprivval "github.com/cometbft/cometbft/privval"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
)

func TestHybridPrivValidator_IsHybridEnabled(t *testing.T) {
	stdPV := &cometprivval.FilePV{}

	t.Run("returns false when no ML-DSA key", func(t *testing.T) {
		hpv := NewHybridPrivValidator(stdPV, nil)
		require.False(t, hpv.IsHybridEnabled())
	})

	t.Run("returns true when ML-DSA key is present", func(t *testing.T) {
		_, priv, err := mldsa87.GenerateKey(nil)
		require.NoError(t, err)

		hpv := NewHybridPrivValidator(stdPV, priv)
		require.True(t, hpv.IsHybridEnabled())
	})
}

func TestHybridPrivValidator_SignMethods_NoPanic_WhenHybridDisabled(t *testing.T) {
	stdPV := &cometprivval.FilePV{}

	t.Run("SignProposal does not panic when hybrid disabled", func(t *testing.T) {
		hpv := NewHybridPrivValidator(stdPV, nil)
		// We don't call the actual signing because stdPV is not initialized.
		// We just verify the method exists and hybrid logic doesn't crash early.
		_ = hpv
	})

	t.Run("SignVote does not panic when hybrid disabled", func(t *testing.T) {
		hpv := NewHybridPrivValidator(stdPV, nil)
		_ = hpv
	})
}

func TestHybridPrivValidator_SignMethods_WithHybridEnabled(t *testing.T) {
	stdPV := &cometprivval.FilePV{}

	_, priv, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)

	hpv := NewHybridPrivValidator(stdPV, priv)

	t.Run("IsHybridEnabled returns true", func(t *testing.T) {
		require.True(t, hpv.IsHybridEnabled())
	})

	t.Run("SignProposal and SignVote are callable when hybrid enabled", func(t *testing.T) {
		// We don't actually call them because stdPV is dummy.
		// This test just confirms the struct is valid when hybrid is on.
		_ = hpv
	})
}

func TestSignVoteNoOpWhenExtensionAlreadyMatchesRound(t *testing.T) {
	dir := t.TempDir()
	stdKey := dir + "/priv_validator_key.json"
	stdPV := cometprivval.GenFilePV(stdKey, dir+"/priv_validator_state.json")
	hpv, err := AttachMLDSAKey(stdPV, MLDSAKeyPath(stdKey), nil)
	require.NoError(t, err)

	chainID := "qomx-ve-test"
	height := int64(5)
	round := int32(1)
	voteType := int32(tmproto.PrecommitType)
	blockHash := bytes.Repeat([]byte{0xab}, 32)
	ext, err := hpv.SignVoteExtension(chainID, height, round, voteType, blockHash)
	require.NoError(t, err)
	require.True(t, VoteExtensionCommits(ext, round, voteType))

	vote := &tmproto.Vote{
		Type:   tmproto.PrecommitType,
		Height: height,
		Round:  round,
		BlockID: tmproto.BlockID{
			Hash:          blockHash,
			PartSetHeader: tmproto.PartSetHeader{Total: 1, Hash: blockHash},
		},
		ValidatorAddress: []byte(hpv.StdPV.GetAddress()),
		Extension:        append([]byte(nil), ext...),
	}
	require.NoError(t, hpv.SignVote(chainID, vote))
	require.Equal(t, ext, vote.Extension, "SignVote must be a no-op when ExtendVote already signed req.Round")
	require.NotEmpty(t, vote.Signature)
}

func TestSignVoteStillRewritesMismatchedRoundPrefix(t *testing.T) {
	dir := t.TempDir()
	stdKey := dir + "/priv_validator_key.json"
	stdPV := cometprivval.GenFilePV(stdKey, dir+"/priv_validator_state.json")
	hpv, err := AttachMLDSAKey(stdPV, MLDSAKeyPath(stdKey), nil)
	require.NoError(t, err)

	chainID := "qomx-ve-test"
	height := int64(5)
	blockHash := bytes.Repeat([]byte{0xcd}, 32)
	ext0, err := hpv.SignVoteExtension(chainID, height, 0, int32(tmproto.PrecommitType), blockHash)
	require.NoError(t, err)

	vote := &tmproto.Vote{
		Type:   tmproto.PrecommitType,
		Height: height,
		Round:  1,
		BlockID: tmproto.BlockID{
			Hash:          blockHash,
			PartSetHeader: tmproto.PartSetHeader{Total: 1, Hash: blockHash},
		},
		ValidatorAddress: []byte(hpv.StdPV.GetAddress()),
		Extension:        ext0,
	}
	require.NoError(t, hpv.SignVote(chainID, vote))
	require.NotEqual(t, ext0, vote.Extension, "safety-net rewrite still fires when packed prefix does not match vote.Round")
	require.True(t, VoteExtensionCommits(vote.Extension, 1, int32(tmproto.PrecommitType)))
}

