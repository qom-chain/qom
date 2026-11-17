package hybrid

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cometprivval "github.com/cometbft/cometbft/privval"
	cmttypes "github.com/cometbft/cometbft/types"

	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

func TestVoteExtensionRoundTrip(t *testing.T) {
	ResetMLDSAPubKeys()
	t.Cleanup(ResetMLDSAPubKeys)

	hpv := newTestHybridValidator(t)
	pub, err := hpv.GetPubKey()
	require.NoError(t, err)
	require.Equal(t, ed25519.KeyType, pub.Type())

	const (
		chainID  = "qomx-1"
		height   = int64(7)
		round    = int32(3)
		voteType = VoteExtensionPrecommit
	)
	blockHash := bytes32(0x11)

	ext, err := hpv.SignVoteExtension(chainID, height, round, voteType, blockHash)
	require.NoError(t, err)
	require.Len(t, ext, VoteExtensionSize)

	addr := []byte(hpv.StdPV.GetAddress())
	pubKey, err := hpv.GetMLDSAPubKey().MarshalBinary()
	require.NoError(t, err)
	require.NoError(t, VerifyVoteExtensionSignature(chainID, height, round, voteType, blockHash, addr, ext, pubKey))

	require.Error(t, VerifyVoteExtensionSignature(chainID, height+1, round, voteType, blockHash, addr, ext, pubKey))
	require.Error(t, VerifyVoteExtensionSignature("other-chain", height, round, voteType, blockHash, addr, ext, pubKey))
	require.Error(t, VerifyVoteExtensionSignature(chainID, height, round+1, voteType, blockHash, addr, ext, pubKey))
	require.Error(t, VerifyVoteExtensionSignature(chainID, height, round, voteType, bytes32(0x22), addr, ext, pubKey))
	otherAddr := append([]byte(nil), addr...)
	otherAddr[0] ^= 0xff
	require.Error(t, VerifyVoteExtensionSignature(chainID, height, round, voteType, blockHash, otherAddr, ext, pubKey))

	tampered := append([]byte(nil), ext...)
	tampered[len(tampered)-1] ^= 0xff
	err = VerifyVoteExtensionSignature(chainID, height, round, voteType, blockHash, addr, tampered, pubKey)
	require.Error(t, err)
	require.Contains(t, err.Error(), "signature is invalid")

	require.Error(t, VerifyVoteExtensionSignature(chainID, height, round, voteType, blockHash, addr, nil, pubKey))
	require.Error(t, VerifyVoteExtensionSignature(chainID, height, round, voteType, blockHash, addr, []byte{0x01}, pubKey))
	require.Error(t, VerifyVoteExtensionSignature(chainID, height, round, voteType, blockHash, addr, ext, nil))
}

func TestVoteExtensionRejectsMismatchedRegisteredKey(t *testing.T) {
	ResetMLDSAPubKeys()
	t.Cleanup(ResetMLDSAPubKeys)

	hpv := newTestHybridValidator(t)
	const (
		chainID  = "qomx-1"
		round    = int32(1)
		voteType = VoteExtensionPrecommit
	)
	blockHash := bytes32(0x33)
	ext, err := hpv.SignVoteExtension(chainID, 3, round, voteType, blockHash)
	require.NoError(t, err)

	_, other, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)
	otherPub, ok := other.Public().(*mldsa87.PublicKey)
	require.True(t, ok)
	otherBz, err := otherPub.MarshalBinary()
	require.NoError(t, err)
	addr := []byte(hpv.StdPV.GetAddress())

	err = VerifyVoteExtensionSignature(chainID, 3, round, voteType, blockHash, addr, ext, otherBz)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match the on-chain key")
}

func TestVoteExtensionSignBytesChangesWithEachField(t *testing.T) {
	base, err := VoteExtensionSignBytes("qomx-1", 4, 1, VoteExtensionPrecommit, bytes32(0x01), []byte{0x0a, 0x0b})
	require.NoError(t, err)

	changedChain, err := VoteExtensionSignBytes("qomx-2", 4, 1, VoteExtensionPrecommit, bytes32(0x01), []byte{0x0a, 0x0b})
	require.NoError(t, err)
	changedHeight, err := VoteExtensionSignBytes("qomx-1", 5, 1, VoteExtensionPrecommit, bytes32(0x01), []byte{0x0a, 0x0b})
	require.NoError(t, err)
	changedRound, err := VoteExtensionSignBytes("qomx-1", 4, 2, VoteExtensionPrecommit, bytes32(0x01), []byte{0x0a, 0x0b})
	require.NoError(t, err)
	changedHash, err := VoteExtensionSignBytes("qomx-1", 4, 1, VoteExtensionPrecommit, bytes32(0x02), []byte{0x0a, 0x0b})
	require.NoError(t, err)
	changedAddr, err := VoteExtensionSignBytes("qomx-1", 4, 1, VoteExtensionPrecommit, bytes32(0x01), []byte{0x0a, 0x0c})
	require.NoError(t, err)

	require.NotEqual(t, base, changedChain)
	require.NotEqual(t, base, changedHeight)
	require.NotEqual(t, base, changedRound)
	require.NotEqual(t, base, changedHash)
	require.NotEqual(t, base, changedAddr)

	_, err = VoteExtensionSignBytes("", 4, 1, VoteExtensionPrecommit, bytes32(0x01), []byte{0x01})
	require.Error(t, err)
	_, err = VoteExtensionSignBytes("qomx-1", 0, 1, VoteExtensionPrecommit, bytes32(0x01), []byte{0x01})
	require.Error(t, err)
	_, err = VoteExtensionSignBytes("qomx-1", 4, -1, VoteExtensionPrecommit, bytes32(0x01), []byte{0x01})
	require.Error(t, err)
	_, err = VoteExtensionSignBytes("qomx-1", 4, 1, 0, bytes32(0x01), []byte{0x01})
	require.Error(t, err)
	_, err = VoteExtensionSignBytes("qomx-1", 4, 1, VoteExtensionPrecommit, nil, []byte{0x01})
	require.Error(t, err)
	_, err = VoteExtensionSignBytes("qomx-1", 4, 1, VoteExtensionPrecommit, bytes32(0x01), nil)
	require.Error(t, err)
}

func TestEnsureVoteExtensionsEnabled(t *testing.T) {
	params := cmttypes.DefaultConsensusParams()
	require.Zero(t, params.ABCI.VoteExtensionsEnableHeight)
	pubKeyTypes := append([]string(nil), params.Validator.PubKeyTypes...)

	EnsureVoteExtensionsEnabled(params)
	require.Equal(t, VoteExtensionsEnableHeight, params.ABCI.VoteExtensionsEnableHeight)
	require.Equal(t, pubKeyTypes, params.Validator.PubKeyTypes)

	params.ABCI.VoteExtensionsEnableHeight = 9
	EnsureVoteExtensionsEnabled(params)
	require.Equal(t, int64(9), params.ABCI.VoteExtensionsEnableHeight)

	EnsureVoteExtensionsEnabled(nil)
}

func TestEnableVoteExtensionsInGenesis(t *testing.T) {
	genFile := filepath.Join(t.TempDir(), "genesis.json")
	appGenesis := &genutiltypes.AppGenesis{
		ChainID:       "qomx-1",
		InitialHeight: 1,
		AppState:      []byte(`{}`),
		Consensus: &genutiltypes.ConsensusGenesis{
			Params: cmttypes.DefaultConsensusParams(),
		},
	}
	require.NoError(t, appGenesis.SaveAs(genFile))

	require.NoError(t, EnableVoteExtensionsInGenesis(genFile))

	loaded, err := genutiltypes.AppGenesisFromFile(genFile)
	require.NoError(t, err)
	require.Equal(t, VoteExtensionsEnableHeight, loaded.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
}

func newTestHybridValidator(t *testing.T) *HybridPrivValidator {
	t.Helper()
	_, priv, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)

	dir := t.TempDir()
	stdPV := cometprivval.GenFilePV(
		filepath.Join(dir, "priv_validator_key.json"),
		filepath.Join(dir, "priv_validator_state.json"),
	)
	hpv := NewHybridPrivValidator(stdPV, priv)
	require.True(t, hpv.IsHybridEnabled())
	return hpv
}

func bytes32(fill byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = fill
	}
	return out
}
