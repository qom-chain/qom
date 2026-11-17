package evmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/math"

	testconstants "github.com/cosmos/evm/testutil/constants"
	"github.com/cosmos/evm/testutil/integration/evm/network"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// TestEmptyVoteExtensionIsRejected fails while the SDK no-op verifier is still
// installed. That handler accepts an empty extension. The hybrid verifier
// rejects it. Vote extensions must be enabled or BaseApp returns before the
// handler, and the two results cannot be told apart.
func TestEmptyVoteExtensionIsRejected(t *testing.T) {
	chain := testconstants.ExampleChainID
	app, genesisState := setup(true, 5, chain.ChainID, chain.EVMChainID)

	// Staking InitGenesis rejects an empty validator set. Bond one validator
	// the same way as SetupWithGenesisValSet, then restore the EVM denom
	// metadata that helper replaces.
	privVal := mock.NewPV()
	pubKey, err := privVal.GetPubKey()
	require.NoError(t, err)
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(pubKey, 1)})
	senderPrivKey := secp256k1.GenPrivKey()
	acc := authtypes.NewBaseAccount(senderPrivKey.PubKey().Address().Bytes(), senderPrivKey.PubKey(), 0, 0)
	balance := banktypes.Balance{
		Address: acc.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(evmtypes.DefaultEVMExtendedDenom, math.NewInt(100000000000000))),
	}
	genesisState, err = simtestutil.GenesisStateWithValSet(
		app.AppCodec(), genesisState, valSet, []authtypes.GenesisAccount{acc}, balance,
	)
	require.NoError(t, err)

	var bankGenesis banktypes.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesisState[banktypes.ModuleName], &bankGenesis)
	bankGenesis.DenomMetadata = network.GenerateBankGenesisMetadata(chain.EVMChainID)
	genesisState[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(&bankGenesis)
	var evmGenesis evmtypes.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesisState[evmtypes.ModuleName], &evmGenesis)
	evmGenesis.Params.EvmDenom = testconstants.ChainsCoinInfo[chain.EVMChainID].Denom
	genesisState[evmtypes.ModuleName] = app.AppCodec().MustMarshalJSON(&evmGenesis)

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)

	src := simtestutil.DefaultConsensusParams
	params := &cmtproto.ConsensusParams{
		Block:     src.Block,
		Evidence:  src.Evidence,
		Validator: src.Validator,
		Authority: src.Authority,
		Abci:      &cmtproto.ABCIParams{VoteExtensionsEnableHeight: 1},
	}

	_, err = app.InitChain(&abci.RequestInitChain{
		ConsensusParams: params,
		AppStateBytes:   stateBytes,
		ChainId:         chain.ChainID,
	})
	require.NoError(t, err)

	resp, err := app.VerifyVoteExtension(&abci.RequestVerifyVoteExtension{
		Height:           1,
		Hash:             []byte("0123456789abcdef0123456789abcdef"),
		ValidatorAddress: []byte("0123456789abcdefghij"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, abci.ResponseVerifyVoteExtension_REJECT, resp.Status)
}
