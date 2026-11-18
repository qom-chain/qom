package hybrid

import (
	"fmt"

	cmttypes "github.com/cometbft/cometbft/types"

	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

// EnsureVoteExtensionsEnabled sets vote extensions on at VoteExtensionsEnableHeight
// when they are currently disabled. A positive height chosen by the operator is kept.
// The validator public key type is not changed.
func EnsureVoteExtensionsEnabled(params *cmttypes.ConsensusParams) {
	if params == nil {
		return
	}
	if params.ABCI.VoteExtensionsEnableHeight == 0 {
		params.ABCI.VoteExtensionsEnableHeight = VoteExtensionsEnableHeight
	}
}

// EnableVoteExtensionsInGenesis writes vote_extensions_enable_height into an
// application genesis file when it is still disabled.
//
// The field is consensus.params.abci.vote_extensions_enable_height.
func EnableVoteExtensionsInGenesis(genFile string) error {
	appGenesis, err := genutiltypes.AppGenesisFromFile(genFile)
	if err != nil {
		return fmt.Errorf("read genesis for vote extensions: %w", err)
	}
	if appGenesis.Consensus == nil || appGenesis.Consensus.Params == nil {
		return fmt.Errorf("genesis %s has no consensus params", genFile)
	}

	EnsureVoteExtensionsEnabled(appGenesis.Consensus.Params)
	if err := appGenesis.SaveAs(genFile); err != nil {
		return fmt.Errorf("write genesis vote extensions: %w", err)
	}
	return nil
}
