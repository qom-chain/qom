package abci

import (
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/crypto/hybrid"
)

// ValidatorMLDSAKeys loads the ML-DSA public key stored for a consensus address.
type ValidatorMLDSAKeys interface {
	GetValidatorMLDSAPubKey(ctx sdk.Context, consAddr sdk.ConsAddress) ([]byte, bool)
}

// ExtendVoteHandler signs a reconstructible ML-DSA-87 payload and returns it
// as the vote extension. The Ed25519 consensus key is not used here. CometBFT
// signs the returned bytes with Ed25519 after this handler returns.
//
// A signing failure is returned to the caller. Cosmos SDK v0.54 turns that
// error into an empty extension, and VerifyVoteExtensionHandler rejects it.
func ExtendVoteHandler() sdk.ExtendVoteHandler {
	return func(ctx sdk.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
		if req == nil {
			return nil, fmt.Errorf("extend vote request is nil")
		}
		if ctx.ChainID() == "" {
			return nil, fmt.Errorf("chain ID is empty; cannot sign ML-DSA vote extension")
		}

		hpv := hybrid.GetGlobalHybridValidator()
		if hpv == nil || !hpv.IsHybridEnabled() {
			return nil, fmt.Errorf("hybrid validator is not loaded; cannot sign ML-DSA vote extension")
		}

		extension, err := hpv.SignVoteExtension(ctx.ChainID(), req.Height, req.Round, req.VoteType, req.Hash)
		if err != nil {
			return nil, err
		}
		return &abci.ResponseExtendVote{VoteExtension: extension}, nil
	}
}

// VerifyVoteExtensionHandler checks the ML-DSA-87 vote extension against the
// on-chain key for the validator. Invalid, missing, or mismatched signatures
// are rejected. A Go error is not returned: CometBFT treats an error from
// this call as a failure of the connection, so the status carries the decision.
func VerifyVoteExtensionHandler(keys ValidatorMLDSAKeys) sdk.VerifyVoteExtensionHandler {
	return func(ctx sdk.Context, req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
		reject := func(err error) (*abci.ResponseVerifyVoteExtension, error) {
			ctx.Logger().Error("rejected ML-DSA vote extension", "err", err)
			return &abci.ResponseVerifyVoteExtension{
				Status: abci.ResponseVerifyVoteExtension_REJECT,
			}, nil
		}

		if req == nil {
			return reject(fmt.Errorf("verify vote extension request is nil"))
		}
		if keys == nil {
			return reject(fmt.Errorf("validator ML-DSA key keeper is nil"))
		}
		expected, found := keys.GetValidatorMLDSAPubKey(ctx, sdk.ConsAddress(req.ValidatorAddress))
		if !found {
			expected = nil
		}

		err := hybrid.VerifyVoteExtensionSignature(
			ctx.ChainID(),
			req.Height,
			req.Round,
			req.VoteType,
			req.Hash,
			req.ValidatorAddress,
			req.VoteExtension,
			expected,
		)
		if err != nil {
			return reject(fmt.Errorf("validator %X height %d round %d: %w", req.ValidatorAddress, req.Height, req.Round, err))
		}

		return &abci.ResponseVerifyVoteExtension{
			Status: abci.ResponseVerifyVoteExtension_ACCEPT,
		}, nil
	}
}
