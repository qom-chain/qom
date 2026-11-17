package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

var (
	amino = codec.NewLegacyAmino()

	// ModuleCdc is the protobuf codec for the pqauth module.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	// AminoCdc is the amino codec used for legacy sign bytes.
	AminoCdc = codec.NewAminoCodec(amino) //nolint:staticcheck
)

const (
	registerMLDSAKeyAminoName          = "cosmos/evm/x/pqauth/MsgRegisterMLDSAKey"
	rotateMLDSAKeyAminoName            = "cosmos/evm/x/pqauth/MsgRotateMLDSAKey"
	registerValidatorMLDSAKeyAminoName = "cosmos/evm/x/pqauth/MsgRegisterValidatorMLDSAKey"
	openEVMWindowAminoName             = "cosmos/evm/x/pqauth/MsgOpenEVMWindow"
)

func init() {
	RegisterLegacyAminoCodec(amino)
	amino.Seal()
}

// RegisterInterfaces registers the pqauth module interfaces on the encoding config.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgRegisterMLDSAKey{},
		&MsgRotateMLDSAKey{},
		&MsgRegisterValidatorMLDSAKey{},
		&MsgOpenEVMWindow{},
	)
	registry.RegisterImplementations(
		(*tx.TxExtensionOptionI)(nil),
		&ExtensionOptionMLDSASig{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

// RegisterLegacyAminoCodec registers the module's concrete types on the amino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterMLDSAKey{}, registerMLDSAKeyAminoName, nil)
	cdc.RegisterConcrete(&MsgRotateMLDSAKey{}, rotateMLDSAKeyAminoName, nil)
	cdc.RegisterConcrete(&MsgRegisterValidatorMLDSAKey{}, registerValidatorMLDSAKeyAminoName, nil)
	cdc.RegisterConcrete(&MsgOpenEVMWindow{}, openEVMWindowAminoName, nil)
}
