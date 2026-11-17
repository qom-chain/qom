package types

import (
	errorsmod "cosmossdk.io/errors"
)

var (
	ErrAccountNotFound    = errorsmod.Register(ModuleName, 2, "account does not exist")
	ErrNotEthSecp256k1    = errorsmod.Register(ModuleName, 3, "account public key is not ethsecp256k1")
	ErrMLDSAKeyExists     = errorsmod.Register(ModuleName, 4, "ml-dsa public key already registered")
	ErrInvalidMLDSAPubKey = errorsmod.Register(ModuleName, 5, "invalid ml-dsa-87 public key")
	ErrInvalidEVMWindow   = errorsmod.Register(ModuleName, 6, "invalid evm authorisation window")
	ErrInvalidProof       = errorsmod.Register(ModuleName, 7, "invalid ml-dsa proof of possession")
	ErrMLDSAKeyNotFound   = errorsmod.Register(ModuleName, 8, "ml-dsa public key is not registered")
	ErrInvalidValidator   = errorsmod.Register(ModuleName, 9, "invalid validator ml-dsa registration")
)
