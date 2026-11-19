package types

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// hardeningV2ActivationHeightValue is the first block height at which the
// pqauth hardening v2 rules apply:
//
//   - A1: a registered EIP-7702 authority may only clear delegation (0x0);
//   - A2: MsgRegisterMLDSAKey refuses an account that has EVM code;
//   - A3: a direct call to a registered, delegated account needs its window;
//   - B:  ERC-20 precompile approve/transferFrom need the owner's window, and
//     registration deletes ERC-20 precompile allowances the account owns;
//   - C:  registration reads authz grants by granter prefix (gas change).
//
// Below the height every path is the 32ee354 code path, including gas.
//
// Mainnet (qom_33303-1) activates at MainnetHardeningV2ActivationHeight
// (33000, approved Nov 19 2026), which is the hardcoded default: a plain
// `go build ./cmd/evmd` is the correct mainnet binary and needs no flag.
//
// The value can still be overridden at build time for devnets and tests,
//
//	go build -ldflags "-X github.com/cosmos/evm/x/pqauth/types.hardeningV2ActivationHeightValue=20" ./cmd/evmd
//
// ("disabled" never activates). To rule out a misbuilt mainnet binary, evmd
// refuses to start on chain-id qom_33303-1 unless the effective height equals
// MainnetHardeningV2ActivationHeight (see CheckHardeningV2ActivationForChain).
// Every node on a network must use the same value.
var hardeningV2ActivationHeightValue = "33000"

// MainnetChainID is the QOM mainnet cosmos chain-id.
const MainnetChainID = "qom_33303-1"

// MainnetHardeningV2ActivationHeight is the approved mainnet activation height.
const MainnetHardeningV2ActivationHeight int64 = 33000

// HardeningV2Disabled is the activation height used when the rules are off.
const HardeningV2Disabled int64 = math.MaxInt64

var hardeningV2ActivationHeight = mustParseActivationHeight(hardeningV2ActivationHeightValue)

func mustParseActivationHeight(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "disabled") {
		return HardeningV2Disabled
	}
	h, err := strconv.ParseInt(v, 10, 64)
	if err != nil || h < 0 {
		panic(fmt.Sprintf("pqauth: invalid hardening v2 activation height %q (build flag -X ...hardeningV2ActivationHeightValue): want a non-negative integer or \"disabled\"", v))
	}
	return h
}

// CheckHardeningV2ActivationForChain returns an error when chainID is mainnet
// and the effective activation height is not MainnetHardeningV2ActivationHeight.
func CheckHardeningV2ActivationForChain(chainID string) error {
	if chainID == MainnetChainID && hardeningV2ActivationHeight != MainnetHardeningV2ActivationHeight {
		return fmt.Errorf("pqauth: this binary has hardening v2 activation height %d but %s requires %d; rebuild without the hardeningV2ActivationHeightValue override",
			hardeningV2ActivationHeight, MainnetChainID, MainnetHardeningV2ActivationHeight)
	}
	return nil
}

// HardeningV2ActivationHeight returns the configured activation height, or
// HardeningV2Disabled.
func HardeningV2ActivationHeight() int64 { return hardeningV2ActivationHeight }

// HardeningV2Active reports whether the hardening v2 rules apply at height.
func HardeningV2Active(height int64) bool { return height >= hardeningV2ActivationHeight }

// SetHardeningV2ActivationHeightForTesting overrides the activation height and
// returns a function that restores the previous value. Tests only.
func SetHardeningV2ActivationHeightForTesting(height int64) (restore func()) {
	prev := hardeningV2ActivationHeight
	hardeningV2ActivationHeight = height
	return func() { hardeningV2ActivationHeight = prev }
}

// validatorKeyGuardActivationHeightValue is the first block height at which
// the validator ML-DSA key guard applies: at the end of every block, before
// the staking module computes the validator set, a validator that would be
// bonded but has no ML-DSA-87 key bound to its consensus address is jailed,
// so it never gets voting power (see Keeper.EnforceValidatorMLDSAKeys).
//
// Without the guard, an unkeyed validator with more than 1/3 of the voting
// power halts the chain: every node rejects its vote extensions, and the key
// registration can no longer be included in a block.
//
// Below the height the end blocker does nothing, so state, gas, and events
// are those of the hardening v2 build.
//
// Override at build time for devnets and tests,
//
//	go build -ldflags "-X github.com/cosmos/evm/x/pqauth/types.validatorKeyGuardActivationHeightValue=20" ./cmd/evmd
//
// ("disabled" never activates). evmd refuses to start on chain-id
// qom_33303-1 unless the effective height equals
// MainnetValidatorKeyGuardActivationHeight. Every node on a network must use
// the same value.
//
// Mainnet (qom_33303-1) activates at MainnetValidatorKeyGuardActivationHeight
// (34000, approved Nov 19 2026), which is the hardcoded default: a plain
// `go build ./cmd/evmd` is the correct mainnet binary and needs no flag.
var validatorKeyGuardActivationHeightValue = "34000"

// MainnetValidatorKeyGuardActivationHeight is the mainnet activation height of
// the validator ML-DSA key guard.
const MainnetValidatorKeyGuardActivationHeight int64 = 34000

var validatorKeyGuardActivationHeight = mustParseActivationHeight(validatorKeyGuardActivationHeightValue)

// CheckValidatorKeyGuardActivationForChain returns an error when chainID is
// mainnet and the effective validator key guard activation height is not
// MainnetValidatorKeyGuardActivationHeight.
func CheckValidatorKeyGuardActivationForChain(chainID string) error {
	if chainID == MainnetChainID && validatorKeyGuardActivationHeight != MainnetValidatorKeyGuardActivationHeight {
		return fmt.Errorf("pqauth: this binary has validator key guard activation height %d but %s requires %d; rebuild without the validatorKeyGuardActivationHeightValue override",
			validatorKeyGuardActivationHeight, MainnetChainID, MainnetValidatorKeyGuardActivationHeight)
	}
	return nil
}

// ValidatorKeyGuardActivationHeight returns the configured activation height,
// or HardeningV2Disabled when the guard never activates.
func ValidatorKeyGuardActivationHeight() int64 { return validatorKeyGuardActivationHeight }

// ValidatorKeyGuardActive reports whether the validator key guard applies at height.
func ValidatorKeyGuardActive(height int64) bool { return height >= validatorKeyGuardActivationHeight }

// SetValidatorKeyGuardActivationHeightForTesting overrides the activation
// height and returns a function that restores the previous value. Tests only.
func SetValidatorKeyGuardActivationHeightForTesting(height int64) (restore func()) {
	prev := validatorKeyGuardActivationHeight
	validatorKeyGuardActivationHeight = height
	return func() { validatorKeyGuardActivationHeight = prev }
}
