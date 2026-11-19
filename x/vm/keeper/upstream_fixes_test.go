package keeper_test

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/mock"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (suite *KeeperTestSuite) setupContractForDelete() (common.Address, sdk.AccAddress) {
	addr := common.HexToAddress("0x4bf7a202cD56FC6c4D84a862a43DDFa9F7AaEF3e")
	code := []byte("contract code")
	codeHash := crypto.Keccak256(code)
	suite.vmKeeper.SetCodeHash(suite.ctx, addr.Bytes(), codeHash)
	suite.vmKeeper.SetCode(suite.ctx, codeHash, code)

	cosmosAddr := sdk.AccAddress(addr.Bytes())
	suite.accKeeper.On("GetAccount", mock.Anything, cosmosAddr).
		Return(authtypes.NewBaseAccountWithAddress(cosmosAddr))
	return addr, cosmosAddr
}

// From the activation height a contract with staking positions cannot be
// self-destructed, whatever combination of positions it holds.
func (suite *KeeperTestSuite) TestDeleteAccountWithStakingPositions() {
	defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(suite.ctx.BlockHeight())()
	addr, cosmosAddr := suite.setupContractForDelete()

	delegation := stakingtypes.Delegation{
		DelegatorAddress: cosmosAddr.String(),
		ValidatorAddress: sdk.ValAddress("validator").String(),
	}
	redelegation := stakingtypes.Redelegation{
		DelegatorAddress:    cosmosAddr.String(),
		ValidatorSrcAddress: sdk.ValAddress("validatorA").String(),
		ValidatorDstAddress: sdk.ValAddress("validatorB").String(),
	}
	unbondingDelegation := stakingtypes.UnbondingDelegation{
		DelegatorAddress: cosmosAddr.String(),
		ValidatorAddress: sdk.ValAddress("validator").String(),
	}

	testCases := []struct {
		name                 string
		delegations          []stakingtypes.Delegation
		redelegations        []stakingtypes.Redelegation
		unbondingDelegations []stakingtypes.UnbondingDelegation
	}{
		{"delegations only", []stakingtypes.Delegation{delegation}, nil, nil},
		{"redelegations only", nil, []stakingtypes.Redelegation{redelegation}, nil},
		{"unbonding delegations only", nil, nil, []stakingtypes.UnbondingDelegation{unbondingDelegation}},
		{"delegations and redelegations", []stakingtypes.Delegation{delegation}, []stakingtypes.Redelegation{redelegation}, nil},
		{"delegations and unbonding delegations", []stakingtypes.Delegation{delegation}, nil, []stakingtypes.UnbondingDelegation{unbondingDelegation}},
		{"redelegations and unbonding delegations", nil, []stakingtypes.Redelegation{redelegation}, []stakingtypes.UnbondingDelegation{unbondingDelegation}},
		{"all three", []stakingtypes.Delegation{delegation}, []stakingtypes.Redelegation{redelegation}, []stakingtypes.UnbondingDelegation{unbondingDelegation}},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.stakingKeeper.ExpectedCalls = suite.stakingKeeper.ExpectedCalls[:0]
			suite.stakingKeeper.On("GetAllDelegatorDelegations", mock.Anything, cosmosAddr).
				Return(tc.delegations, nil).Maybe()
			suite.stakingKeeper.On("GetRedelegations", mock.Anything, cosmosAddr, mock.Anything).
				Return(tc.redelegations, nil).Maybe()
			suite.stakingKeeper.On("GetAllUnbondingDelegations", mock.Anything, cosmosAddr).
				Return(tc.unbondingDelegations, nil).Maybe()

			err := suite.vmKeeper.DeleteAccount(suite.ctx, addr)
			suite.Require().Error(err)
			suite.Require().ErrorContains(err, "staking positions")
			suite.Require().NotNil(suite.accKeeper.GetAccount(suite.ctx, cosmosAddr))
		})
	}
}

// Below the activation height the v1.0.1 behaviour is kept: staking positions
// are not consulted and DeleteAccount goes on to reset the account. (The rest
// of the deletion needs the EVM coin config, which this unit suite does not
// set up, so a panic further down is tolerated here.)
func (suite *KeeperTestSuite) TestDeleteAccountWithStakingPositionsPreActivation() {
	defer pqauthtypes.SetUpstreamFixesActivationHeightForTesting(suite.ctx.BlockHeight() + 1)()
	addr, _ := suite.setupContractForDelete()

	suite.accKeeper.On("SetAccount", mock.Anything, mock.Anything).Return().Once()

	func() {
		defer func() { _ = recover() }()
		_ = suite.vmKeeper.DeleteAccount(suite.ctx, addr)
	}()
	suite.accKeeper.AssertCalled(suite.T(), "SetAccount", mock.Anything, mock.Anything)
	suite.stakingKeeper.AssertNotCalled(suite.T(), "GetAllDelegatorDelegations", mock.Anything, mock.Anything)
	suite.stakingKeeper.AssertNotCalled(suite.T(), "GetRedelegations", mock.Anything, mock.Anything, mock.Anything)
	suite.stakingKeeper.AssertNotCalled(suite.T(), "GetAllUnbondingDelegations", mock.Anything, mock.Anything)
}
