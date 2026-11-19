package keeper_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/cosmos/evm/x/pqauth/keeper"
	"github.com/cosmos/evm/x/pqauth/types"

	"context"
	"cosmossdk.io/core/address"
	"github.com/cosmos/cosmos-sdk/baseapp"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
)

type parityAuthzAK struct {
	accountKeeperStub
	codec address.Codec
}

func (s parityAuthzAK) AddressCodec() address.Codec { return s.codec }
func (s parityAuthzAK) NewAccountWithAddress(_ context.Context, a sdk.AccAddress) sdk.AccountI {
	return authtypes.NewBaseAccountWithAddress(a)
}
func (s parityAuthzAK) SetAccount(context.Context, sdk.AccountI) {}

// registrationGasScenario registers one account (fixed-size keys) that has 25
// authz grants, with 5 grants from another granter, against the real SDK
// authz keeper at height 50, and returns gas consumed by the handler.
func registrationGasScenario(t *testing.T, setup func(k *keeper.Keeper)) uint64 {
	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	ethPub := ethPriv.PubKey()
	addr := sdk.AccAddress(ethPub.Address())
	acc := authtypes.NewBaseAccount(addr, ethPub, 1, 0)
	mldsaPriv, pubKey := newMLDSAKey(t)

	pqKey := storetypes.NewKVStoreKey(types.StoreKey)
	authzKey := storetypes.NewKVStoreKey(authzkeeper.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{types.StoreKey: pqKey, authzkeeper.StoreKey: authzKey},
		map[string]*storetypes.TransientStoreKey{"t": storetypes.NewTransientStoreKey("t")}, nil,
	).WithChainID(testChainID).WithBlockHeight(50).WithBlockTime(time.Unix(1_700_000_000, 0))
	accounts := accountKeeperStub{accounts: map[string]sdk.AccountI{addr.String(): acc}}
	enc := moduletestutil.MakeTestEncodingConfig(authzmodule.AppModuleBasic{})
	authzK := authzkeeper.NewKeeper(runtime.NewKVStoreService(authzKey), enc.Codec, baseapp.NewMsgServiceRouter(),
		parityAuthzAK{accounts, addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())})
	k := keeper.NewKeeper(pqKey, accounts)
	k.SetDelegationKeepers(authzK, nopFees{}, nil)
	setup(&k)
	mt := sdk.MsgTypeURL(&types.MsgOpenEVMWindow{})
	other := sdk.AccAddress(bytes20())
	for i := 0; i < 25; i++ {
		g := make([]byte, 20)
		g[0], g[19] = 0x55, byte(i)
		require.NoError(t, authzK.SaveGrant(ctx, g, addr, authz.NewGenericAuthorization(mt), nil))
		if i < 5 {
			require.NoError(t, authzK.SaveGrant(ctx, g, other, authz.NewGenericAuthorization(mt), nil))
		}
	}
	before := ctx.GasMeter().GasConsumed()
	_, err = k.RegisterMLDSAKey(ctx, &types.MsgRegisterMLDSAKey{
		Signer: addr.String(), PubKey: pubKey, PopSignature: accountPop(t, mldsaPriv, addr, ethPub.Bytes(), pubKey),
	})
	require.NoError(t, err)
	gas := ctx.GasMeter().GasConsumed() - before
	if p := os.Getenv("PARITY_OUT"); p != "" {
		require.NoError(t, os.WriteFile(p, []byte(fmt.Sprint(gas)), 0o644))
	}
	return gas
}
