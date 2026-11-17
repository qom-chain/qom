package abci

import (
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cometprivval "github.com/cometbft/cometbft/privval"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/crypto/hybrid"
)

func TestVoteExtensionHandlersAcceptAndReject(t *testing.T) {
	hybrid.ResetMLDSAPubKeys()
	t.Cleanup(func() {
		hybrid.SetGlobalHybridValidator(nil)
		hybrid.ResetMLDSAPubKeys()
	})

	hpv := newHandlerValidator(t)
	hybrid.SetGlobalHybridValidator(hpv)

	ctx := sdk.NewContext(nil, cmtproto.Header{ChainID: "qomx-1"}, false, log.NewNopLogger())
	blockHash := make([]byte, 32)
	for i := range blockHash {
		blockHash[i] = 0xab
	}

	const (
		height   = int64(4)
		round    = int32(2)
		voteType = hybrid.VoteExtensionPrecommit
	)
	extend := ExtendVoteHandler()
	resp, err := extend(ctx, &abci.RequestExtendVote{
		Hash:     blockHash,
		Height:   height,
		Round:    round,
		VoteType: voteType,
	})
	require.NoError(t, err)
	require.Len(t, resp.VoteExtension, hybrid.VoteExtensionSize)

	pubKey, err := hpv.GetMLDSAPubKey().MarshalBinary()
	require.NoError(t, err)
	consAddr := sdk.ConsAddress(hpv.StdPV.GetAddress())
	verify := VerifyVoteExtensionHandler(stubValidatorKeys{addr: consAddr, pubKey: pubKey})
	accepted, err := verify(ctx, &abci.RequestVerifyVoteExtension{
		Hash:             blockHash,
		Height:           height,
		Round:            round,
		VoteType:         voteType,
		ValidatorAddress: consAddr,
		VoteExtension:    resp.VoteExtension,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseVerifyVoteExtension_ACCEPT, accepted.Status)

	rejected, err := verify(ctx, &abci.RequestVerifyVoteExtension{
		Hash:             blockHash,
		Height:           height,
		Round:            round,
		VoteType:         voteType,
		ValidatorAddress: consAddr,
		VoteExtension:    nil,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseVerifyVoteExtension_REJECT, rejected.Status)

	missing, err := VerifyVoteExtensionHandler(stubValidatorKeys{})(ctx, &abci.RequestVerifyVoteExtension{
		Hash:             blockHash,
		Height:           height,
		Round:            round,
		VoteType:         voteType,
		ValidatorAddress: consAddr,
		VoteExtension:    resp.VoteExtension,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseVerifyVoteExtension_REJECT, missing.Status)

	otherPub, _, err := newOtherMLDSAPubKey(t)
	require.NoError(t, err)
	mismatch, err := VerifyVoteExtensionHandler(stubValidatorKeys{addr: consAddr, pubKey: otherPub})(ctx, &abci.RequestVerifyVoteExtension{
		Hash:             blockHash,
		Height:           height,
		Round:            round,
		VoteType:         voteType,
		ValidatorAddress: consAddr,
		VoteExtension:    resp.VoteExtension,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseVerifyVoteExtension_REJECT, mismatch.Status)

	badHash := append([]byte(nil), blockHash...)
	badHash[0] ^= 0xff
	rejected, err = verify(ctx, &abci.RequestVerifyVoteExtension{
		Hash:             badHash,
		Height:           height,
		Round:            round,
		VoteType:         voteType,
		ValidatorAddress: consAddr,
		VoteExtension:    resp.VoteExtension,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseVerifyVoteExtension_REJECT, rejected.Status)
}

func TestExtendVoteHandlerRequiresHybridValidator(t *testing.T) {
	hybrid.SetGlobalHybridValidator(nil)
	t.Cleanup(func() { hybrid.SetGlobalHybridValidator(nil) })

	ctx := sdk.NewContext(nil, cmtproto.Header{ChainID: "qomx-1"}, false, log.NewNopLogger())
	_, err := ExtendVoteHandler()(ctx, &abci.RequestExtendVote{
		Hash:   []byte{0x01},
		Height: 1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "hybrid validator is not loaded")
}

type stubValidatorKeys struct {
	addr   sdk.ConsAddress
	pubKey []byte
}

func (s stubValidatorKeys) GetValidatorMLDSAPubKey(_ sdk.Context, consAddr sdk.ConsAddress) ([]byte, bool) {
	if len(s.pubKey) == 0 || !s.addr.Equals(consAddr) {
		return nil, false
	}
	return s.pubKey, true
}

func newOtherMLDSAPubKey(t *testing.T) ([]byte, *mldsa87.PrivateKey, error) {
	t.Helper()
	pub, priv, err := mldsa87.GenerateKey(nil)
	if err != nil {
		return nil, nil, err
	}
	bz, err := pub.MarshalBinary()
	return bz, priv, err
}

func newHandlerValidator(t *testing.T) *hybrid.HybridPrivValidator {
	t.Helper()
	dir := t.TempDir()
	stdKey := dir + "/priv_validator_key.json"
	stdPV := cometprivval.GenFilePV(stdKey, dir+"/priv_validator_state.json")
	hpv, err := hybrid.AttachMLDSAKey(stdPV, hybrid.MLDSAKeyPath(stdKey))
	require.NoError(t, err)
	return hpv
}
