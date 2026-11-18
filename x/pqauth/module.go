package pqauth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/evm/x/pqauth/client/cli"
	"github.com/cosmos/evm/x/pqauth/keeper"
	"github.com/cosmos/evm/x/pqauth/types"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
)

const consensusVersion = 1

var (
	_ module.AppModule           = AppModule{} //nolint:staticcheck // keep for legacy purposes
	_ module.AppModuleBasic      = AppModuleBasic{}
	_ module.AppModuleSimulation = AppModule{}
	_ module.HasABCIGenesis      = AppModule{}
	_ appmodule.AppModule        = AppModule{}
)

// AppModuleBasic implements the stateless module methods.
type AppModuleBasic struct{}

// Name returns the module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the module's amino types.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// ConsensusVersion returns the module consensus version.
func (AppModuleBasic) ConsensusVersion() uint64 { return consensusVersion }

// DefaultGenesis returns the default genesis state.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ValidateGenesis validates genesis state.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return gs.Validate()
}

// RegisterRESTRoutes is a no-op. The module exposes gRPC.
func (AppModuleBasic) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {}

// RegisterGRPCGatewayRoutes registers the gRPC gateway routes.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(c client.Context, serveMux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), serveMux, types.NewQueryClient(c)); err != nil {
		panic(err)
	}
}

// RegisterInterfaces registers the module interfaces.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// GetQueryCmd returns the module query command.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return cli.GetQueryCmd() }

// GetTxCmd returns the module transaction command.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return cli.NewTxCmd() }

// AppModule implements the stateful module methods.
type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

// NewAppModule creates the pqauth app module.
func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{
		AppModuleBasic: AppModuleBasic{},
		keeper:         k,
	}
}

// Name returns the module name.
func (AppModule) Name() string { return types.ModuleName }

// RegisterServices registers the module's gRPC services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), am.keeper)
	types.RegisterQueryServer(cfg.QueryServer(), am.keeper)
}

// InitGenesis initializes the module genesis state.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) []abci.ValidatorUpdate {
	var gs types.GenesisState
	cdc.MustUnmarshalJSON(data, &gs)
	InitGenesis(ctx, am.keeper, gs)
	return []abci.ValidatorUpdate{}
}

// ExportGenesis exports the module genesis state.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(ExportGenesis(ctx, am.keeper))
}

// RegisterStoreDecoder registers no simulation store decoder.
func (AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// GenerateGenesisState creates an empty simulation genesis state.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// WeightedOperations returns no simulation operations.
func (AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType implements depinject.OnePerModuleType.
func (AppModule) IsOnePerModuleType() {}
