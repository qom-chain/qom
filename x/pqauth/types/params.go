package types

import (
	errorsmod "cosmossdk.io/errors"
)

const (
	// DefaultEVMWindowDuration is the EVM authorisation window length used when
	// genesis omits the parameter or stores zero.
	DefaultEVMWindowDuration int64 = 100

	// MaxEVMWindowDuration is the largest window the module will accept.
	// It stays far below the point where height + duration can overflow.
	MaxEVMWindowDuration int64 = 10_000_000
)

// DefaultParams returns the default pqauth module parameters.
// Cosmos precompile calls stay allowed during an open EVM window.
func DefaultParams() Params {
	return Params{
		EvmWindowDuration:         DefaultEVMWindowDuration,
		RestrictCosmosPrecompiles: false,
	}
}

// Validate checks the module parameters. A zero duration is accepted and means
// DefaultEVMWindowDuration when the parameter is stored.
func (p Params) Validate() error {
	return validateEVMWindowDuration(p.EvmWindowDuration)
}

// WindowDuration returns the duration used to open a window.
// Zero is the default of 100 blocks.
func (p Params) WindowDuration() int64 {
	if p.EvmWindowDuration == 0 {
		return DefaultEVMWindowDuration
	}
	return p.EvmWindowDuration
}

func validateEVMWindowDuration(duration int64) error {
	if duration == 0 {
		return nil
	}
	if duration < 0 {
		return errorsmod.Wrapf(ErrInvalidEVMWindow, "evm_window_duration must be positive, got %d", duration)
	}
	if duration > MaxEVMWindowDuration {
		return errorsmod.Wrapf(
			ErrInvalidEVMWindow,
			"evm_window_duration %d exceeds maximum %d",
			duration,
			MaxEVMWindowDuration,
		)
	}
	return nil
}
