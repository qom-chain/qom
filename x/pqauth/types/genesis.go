package types

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesisState returns pqauth genesis with the default parameters.
// The default EVM authorisation window is 100 blocks.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{Params: DefaultParams()}
}

// Validate checks genesis bindings for unique accounts, valid ML-DSA-87 keys,
// valid EVM window expiries, and module parameters.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if err := validateKeys(gs.Keys); err != nil {
		return err
	}
	if err := validateWindows(gs.Windows); err != nil {
		return err
	}
	return validateValidatorKeys(gs.ValidatorKeys)
}

func validateKeys(keys []MLDSAKey) error {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		addr, err := sdk.AccAddressFromBech32(key.Account)
		if err != nil {
			return errorsmod.Wrapf(err, "invalid account %s", key.Account)
		}
		if err := ValidateMLDSAPubKey(key.PubKey); err != nil {
			return errorsmod.Wrapf(err, "account %s", key.Account)
		}
		id := string(addr)
		if _, ok := seen[id]; ok {
			return errorsmod.Wrapf(ErrMLDSAKeyExists, "duplicate account %s", key.Account)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateWindows(windows []EVMWindow) error {
	seen := make(map[string]struct{}, len(windows))
	for _, window := range windows {
		addr, err := sdk.AccAddressFromBech32(window.Account)
		if err != nil {
			return errorsmod.Wrapf(err, "invalid account %s", window.Account)
		}
		if err := ValidateEVMWindowExpiry(window.ExpiryHeight); err != nil {
			return errorsmod.Wrapf(err, "account %s", window.Account)
		}
		id := string(addr)
		if _, ok := seen[id]; ok {
			return errorsmod.Wrapf(ErrInvalidEVMWindow, "duplicate account %s", window.Account)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateValidatorKeys(keys []ValidatorMLDSAKey) error {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if len(key.ConsensusAddress) != ConsensusAddressSize {
			return errorsmod.Wrapf(ErrInvalidValidator, "consensus address must be %d bytes, got %d", ConsensusAddressSize, len(key.ConsensusAddress))
		}
		if err := ValidateMLDSAPubKey(key.PubKey); err != nil {
			return errorsmod.Wrap(err, "validator ML-DSA public key")
		}
		id := string(key.ConsensusAddress)
		if _, ok := seen[id]; ok {
			return errorsmod.Wrap(ErrMLDSAKeyExists, "duplicate validator consensus address")
		}
		seen[id] = struct{}{}
	}
	return nil
}
