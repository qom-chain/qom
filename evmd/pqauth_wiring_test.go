package evmd

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
	testconstants "github.com/cosmos/evm/testutil/constants"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// TestPQAuthEVMWiring checks the app wiring the hardening relies on: the
// pqauth msg server (as routed) sees the EVM and ERC-20 keepers, and the
// ERC-20 keeper used for precompiles sees the pqauth keeper.
func TestPQAuthEVMWiring(t *testing.T) {
	// Setup seals process-wide EVM coin config; reset it so later tests in
	// this package can build their own app.
	t.Cleanup(func() { evmtypes.NewEVMConfigurator().ResetTestConfig() })
	t.Cleanup(pqauthtypes.SetHardeningV2ActivationHeightForTesting(0))
	chain := testconstants.ExampleChainID
	app := Setup(t, chain.ChainID, chain.EVMChainID)
	ctx := app.NewContextLegacy(false, cmtproto.Header{ChainID: chain.ChainID, Height: 10})

	ethPriv, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	ethPub := ethPriv.PubKey()
	addr := sdk.AccAddress(ethPub.Address())
	owner := common.BytesToAddress(addr)
	acc := app.AccountKeeper.NewAccountWithAddress(ctx, addr)
	require.NoError(t, acc.SetPubKey(ethPub))
	app.AccountKeeper.SetAccount(ctx, acc)

	pub, priv, err := mldsa87.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubBz, err := pub.MarshalBinary()
	require.NoError(t, err)
	payload, err := pqauthtypes.AccountPoPSignBytes(addr, ctx.ChainID(), ethPub.Bytes(), pubBz)
	require.NoError(t, err)
	pop, err := priv.Sign(rand.Reader, payload, nil)
	require.NoError(t, err)
	msg := &pqauthtypes.MsgRegisterMLDSAKey{Signer: addr.String(), PubKey: pubBz, PopSignature: pop}
	handler := app.MsgServiceRouter().Handler(msg)
	require.NotNil(t, handler)

	// 1. Account with code: the routed msg server refuses it, so the module's
	//    keeper copy has the EVM keeper.
	app.EVMKeeper.SetCodeHash(ctx, owner.Bytes(), common.HexToHash("0x01").Bytes())
	_, err = handler(ctx, msg)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
	require.Contains(t, err.Error(), "has EVM code")
	app.EVMKeeper.DeleteCodeHash(ctx, owner)

	// 2. ERC-20 allowance owned by the account is deleted on registration.
	token := common.HexToAddress("0x00000000000000000000000000000000000c0de1")
	pair := erc20types.NewTokenPair(token, "atest", erc20types.OWNER_MODULE)
	app.Erc20Keeper.SetTokenPair(ctx, pair)
	app.Erc20Keeper.SetERC20Map(ctx, token, pair.GetID())
	app.Erc20Keeper.SetDenomMap(ctx, pair.Denom, pair.GetID())
	spender := common.HexToAddress("0x00000000000000000000000000000000000000a5")
	require.NoError(t, app.Erc20Keeper.SetAllowance(ctx, token, owner, spender, big.NewInt(77)))

	_, err = handler(ctx, msg)
	require.NoError(t, err)
	allowance, err := app.Erc20Keeper.GetAllowance(ctx, token, owner, spender)
	require.NoError(t, err)
	require.Zero(t, allowance.Sign())

	// 3. The ERC-20 keeper (the one the EVM keeper uses for precompiles)
	//    gates the registered owner while its window is closed.
	require.ErrorIs(t, app.Erc20Keeper.CheckOwnerEVMWindow(ctx, owner), sdkerrors.ErrUnauthorized)
	require.NoError(t, app.PQAuthKeeper.SetEVMWindowExpiry(ctx, addr, ctx.BlockHeight()+5))
	require.NoError(t, app.Erc20Keeper.CheckOwnerEVMWindow(ctx, owner))
}
