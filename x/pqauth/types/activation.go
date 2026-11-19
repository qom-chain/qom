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
// The value is fixed at build time and every node on the network must use the
// same value. Set it with
//
//	go build -ldflags "-X github.com/cosmos/evm/x/pqauth/types.hardeningV2ActivationHeightValue=34000" ./cmd/evmd
//
// or by editing the default below. "disabled" (the default) never activates,
// so a binary built without the flag behaves exactly like 32ee354.
var hardeningV2ActivationHeightValue = "disabled"

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
