package types

import (
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

// AcceptsCosmosExtensionOption reports whether a critical tx extension is one
// the Cosmos ante path understands.
func AcceptsCosmosExtensionOption(anyType *codectypes.Any) bool {
	if anyType == nil {
		return false
	}
	if HasDynamicFeeExtensionOption(anyType) {
		return true
	}
	if _, ok := anyType.GetCachedValue().(*pqauthtypes.ExtensionOptionMLDSASig); ok {
		return true
	}
	return anyType.GetTypeUrl() == pqauthtypes.MLDSAExtensionTypeURL
}
