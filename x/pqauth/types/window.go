package types

import (
	"encoding/binary"
	"math"

	errorsmod "cosmossdk.io/errors"
)

// EVMWindowDuration is the default window length, in blocks.
// New code should read the module parameter. This name remains so callers
// that want the default do not need the keeper.
const EVMWindowDuration = DefaultEVMWindowDuration

// EVMWindowExpiryHeight returns the exclusive expiry for a window opened at
// height. duration is the module parameter, in blocks.
func EVMWindowExpiryHeight(height, duration int64) (int64, error) {
	if duration <= 0 {
		return 0, errorsmod.Wrapf(ErrInvalidEVMWindow, "evm_window_duration must be positive, got %d", duration)
	}
	if height < 0 {
		return 0, errorsmod.Wrapf(ErrInvalidEVMWindow, "negative block height %d", height)
	}
	if height > math.MaxInt64-duration {
		return 0, errorsmod.Wrapf(ErrInvalidEVMWindow, "block height %d plus window duration %d overflows", height, duration)
	}
	return height + duration, nil
}

// EVMWindowOpen reports whether a stored expiry is open at height.
// expiry is the first height at which the window is closed.
func EVMWindowOpen(height, expiry int64) bool {
	return expiry > height
}

// ValidateEVMWindowExpiry checks that expiry is a positive exclusive block height.
func ValidateEVMWindowExpiry(expiry int64) error {
	if expiry <= 0 {
		return errorsmod.Wrapf(ErrInvalidEVMWindow, "expiry height must be positive, got %d", expiry)
	}
	return nil
}

// EncodeEVMWindowExpiry encodes an expiry height for the KV store.
func EncodeEVMWindowExpiry(expiry int64) ([]byte, error) {
	if err := ValidateEVMWindowExpiry(expiry); err != nil {
		return nil, err
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(expiry))
	return buf, nil
}

// DecodeEVMWindowExpiry decodes an expiry height stored by EncodeEVMWindowExpiry.
func DecodeEVMWindowExpiry(bz []byte) (int64, error) {
	if len(bz) != 8 {
		return 0, errorsmod.Wrapf(ErrInvalidEVMWindow, "expiry encoding is %d bytes", len(bz))
	}
	expiry := int64(binary.BigEndian.Uint64(bz))
	if err := ValidateEVMWindowExpiry(expiry); err != nil {
		return 0, err
	}
	return expiry, nil
}
