package server

import (
	"fmt"
	"reflect"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtstate "github.com/cometbft/cometbft/state"
)

// assertVoteExtensionRoundPatch refuses to start evmd unless this binary was
// built against the QOMX CometBFT vote-extension patch. The compile-time
// reference to QOMXABCIVoteExtensionRoundPatched fails to compile on stock
// v0.39.3. The reflection checks catch a types-only copy that is missing the
// Round field. The const is written by scripts/patch-cometbft-voteext.sh
// together with the execution.go injection, so a complete patch is required
// to both compile and start.
func assertVoteExtensionRoundPatch() error {
	if !cmtstate.QOMXABCIVoteExtensionRoundPatched {
		return fmt.Errorf("evmd was built without the CometBFT vote-extension round patch; refuse to start (ABCI ExtendVote/VerifyVoteExtension would not carry Round)")
	}
	if _, ok := reflect.TypeOf(abci.RequestExtendVote{}).FieldByName("Round"); !ok {
		return fmt.Errorf("RequestExtendVote has no Round field; evmd requires scripts/patch-cometbft-voteext.sh")
	}
	if _, ok := reflect.TypeOf(abci.RequestVerifyVoteExtension{}).FieldByName("Round"); !ok {
		return fmt.Errorf("RequestVerifyVoteExtension has no Round field; evmd requires scripts/patch-cometbft-voteext.sh")
	}
	if _, ok := reflect.TypeOf(abci.RequestExtendVote{}).FieldByName("VoteType"); !ok {
		return fmt.Errorf("RequestExtendVote has no VoteType field; evmd requires scripts/patch-cometbft-voteext.sh")
	}
	return nil
}
