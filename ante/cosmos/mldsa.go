package cosmos

import (
	"context"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"google.golang.org/protobuf/types/known/anypb"

	anteinterfaces "github.com/cosmos/evm/ante/interfaces"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	"github.com/cosmos/gogoproto/proto"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"
)

// MLDSA87VerifyGas is charged for each registered signer before the ML-DSA-87
// signature is verified.
//
// ethsecp256k1 verification in this chain costs 21,000 gas
// (ante.Secp256k1VerifyCost). ML-DSA-87 verification is several times more
// work: published OpenSSL rates put ML-DSA-87 verify at roughly six times
// ECDSA P-256, and circl's pure-Go implementation is in that same band rather
// than tens of times slower. 250,000 gas is about twelve times the
// ethsecp256k1 cost. That matches the fixed portion of the Dilithium
// precompile and leaves room for the slower tail of rejection-sampled
// verification without charging the previous 1,000,000. At a 10,000,000 gas
// block limit, that is at most 40 ML-DSA verifications per block.
const MLDSA87VerifyGas uint64 = 250_000

// accountKeeper is the account lookup used to rebuild SDK signer data.
type accountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}

// MLDSASigDecorator requires a valid ML-DSA-87 signature, carried in
// ExtensionOptionMLDSASig, for every signer that has a registered key.
// It also requires one for a registered fee granter or authz granter whose
// funds or messages the transaction uses. Unregistered signers are unchanged.
type MLDSASigDecorator struct {
	ak              accountKeeper
	pq              anteinterfaces.PQAuthKeeper
	authz           anteinterfaces.AuthzGrantKeeper
	cdc             codec.BinaryCodec
	signModeHandler *txsigning.HandlerMap
}

// NewMLDSASigDecorator returns a decorator that runs after secp256k1 signature
// verification. authzKeeper may be nil; a MsgExec that spends a registered
// granter then fails closed. cdc extracts signers from messages inside
// MsgExec. The application codec has the chain's address codec.
func NewMLDSASigDecorator(ak accountKeeper, pq anteinterfaces.PQAuthKeeper, signModeHandler *txsigning.HandlerMap, authzKeeper anteinterfaces.AuthzGrantKeeper, cdc codec.BinaryCodec) MLDSASigDecorator {
	return MLDSASigDecorator{
		ak:              ak,
		pq:              pq,
		authz:           authzKeeper,
		cdc:             cdc,
		signModeHandler: signModeHandler,
	}
}

// AnteHandle rejects a transaction when a registered signer, fee granter, or
// authz granter does not present a valid ML-DSA-87 signature over that
// account's SDK sign bytes. GenericAuthorization is rejected for a registered
// granter. Registration cannot share a transaction with MsgGrant or MsgExec.
func (d MLDSASigDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if d.pq == nil {
		return ctx, errorsmod.Wrap(sdkerrors.ErrLogic, "ML-DSA check: pqauth keeper is nil")
	}
	if err := d.rejectAuthzBypass(ctx, tx); err != nil {
		return ctx, err
	}
	if simulate || ctx.IsReCheckTx() || !ctx.IsSigverifyTx() {
		return next(ctx, tx, simulate)
	}

	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "ML-DSA check: transaction does not implement the auth signing interface")
	}
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, errorsmod.Wrap(err, "ML-DSA check: could not read transaction signatures")
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, errorsmod.Wrap(err, "ML-DSA check: could not read transaction signers")
	}
	if len(sigs) != len(signers) {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "ML-DSA check: transaction has %d signers but %d signatures", len(signers), len(sigs))
	}

	outer := make(map[string]struct{}, len(signers))
	anyRegistered := false
	for _, signer := range signers {
		addr := sdk.AccAddress(signer)
		outer[string(addr)] = struct{}{}
		if _, found := d.pq.GetMLDSAPubKey(ctx, addr); found {
			anyRegistered = true
		}
	}
	extras, err := d.extraPrincipals(ctx, tx, outer)
	if err != nil {
		return ctx, err
	}
	if !anyRegistered && len(extras) == 0 {
		return next(ctx, tx, simulate)
	}

	provided, err := mldsaSignatures(tx)
	if err != nil {
		return ctx, err
	}

	for i, sig := range sigs {
		signer := sdk.AccAddress(signers[i])
		if _, found := d.pq.GetMLDSAPubKey(ctx, signer); !found {
			continue
		}
		single, ok := sig.Data.(*signingtypes.SingleSignatureData)
		if !ok || single.SignMode != signingtypes.SignMode_SIGN_MODE_DIRECT {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrInvalidType, "account %s has a registered ML-DSA-87 key and must sign with SIGN_MODE_DIRECT", signer)
		}
		if err := d.verifyRegistered(ctx, tx, signer, sig.Sequence, provided); err != nil {
			return ctx, err
		}
	}
	for _, signer := range extras {
		acc := d.ak.GetAccount(ctx, signer)
		if acc == nil {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrUnknownAddress, "account %s does not exist, so its ML-DSA signature cannot be checked", signer)
		}
		if err := d.verifyRegistered(ctx, tx, signer, acc.GetSequence(), provided); err != nil {
			return ctx, err
		}
	}

	return next(ctx, tx, simulate)
}

func (d MLDSASigDecorator) verifyRegistered(ctx sdk.Context, tx sdk.Tx, signer sdk.AccAddress, sequence uint64, provided map[string][]byte) error {
	pubKeyBz, found := d.pq.GetMLDSAPubKey(ctx, signer)
	if !found {
		return nil
	}
	sigBz, ok := provided[string(signer)]
	if !ok {
		return errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "account %s has a registered ML-DSA-87 key but the transaction has no ML-DSA signature for it; sign with `tx pqauth sign`", signer)
	}
	acc := d.ak.GetAccount(ctx, signer)
	if acc == nil {
		return errorsmod.Wrapf(sdkerrors.ErrUnknownAddress, "account %s does not exist, so its ML-DSA signature cannot be checked", signer)
	}
	signerData, err := MLDSASignerData(ctx, acc, sequence)
	if err != nil {
		return err
	}
	signBytes, err := MLDSASignBytes(ctx, d.signModeHandler, signerData, tx)
	if err != nil {
		return err
	}
	ctx.GasMeter().ConsumeGas(MLDSA87VerifyGas, "ante verify: mldsa87")
	if err := verifyMLDSA(pubKeyBz, signBytes, sigBz); err != nil {
		return errorsmod.Wrapf(err, "account %s", signer)
	}
	return nil
}

// MLDSASignerData returns the signer data used for an ML-DSA signature.
// At genesis height the account number is zero, matching SDK signature verification.
func MLDSASignerData(ctx sdk.Context, acc sdk.AccountI, sequence uint64) (txsigning.SignerData, error) {
	pubKey := acc.GetPubKey()
	if pubKey == nil {
		return txsigning.SignerData{}, errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "account public key is not set, so the ML-DSA payload cannot be built")
	}
	anyPk, err := codectypes.NewAnyWithValue(pubKey)
	if err != nil {
		return txsigning.SignerData{}, err
	}

	var accNum uint64
	if ctx.BlockHeight() > 0 {
		accNum = acc.GetAccountNumber()
	}
	return txsigning.SignerData{
		Address:       acc.GetAddress().String(),
		ChainID:       ctx.ChainID(),
		AccountNumber: accNum,
		Sequence:      sequence,
		PubKey: &anypb.Any{
			TypeUrl: anyPk.TypeUrl,
			Value:   anyPk.Value,
		},
	}, nil
}

// MLDSASignBytes returns the SIGN_MODE_DIRECT sign bytes for signerData.
// Signature bytes inside ExtensionOptionMLDSASig are cleared first, so the
// signature can be carried in the transaction it covers. Callers must sign the
// bytes returned here and then set the signature on the extension.
func MLDSASignBytes(ctx context.Context, handler *txsigning.HandlerMap, signerData txsigning.SignerData, tx sdk.Tx) ([]byte, error) {
	if handler == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "ML-DSA sign bytes: sign mode handler is nil")
	}
	adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
	if !ok {
		return nil, errorsmod.Wrapf(sdkerrors.ErrTxDecode, "expected V2AdaptableTx, got %T", tx)
	}
	txData := adaptableTx.GetSigningTxData()
	cleared, err := bodyBytesWithClearedMLDSASigs(txData.BodyBytes)
	if err != nil {
		return nil, err
	}
	txData.BodyBytes = cleared
	return handler.GetSignBytes(ctx, signingv1beta1.SignMode_SIGN_MODE_DIRECT, signerData, txData)
}

func verifyMLDSA(pubKeyBz, message, signature []byte) error {
	if len(signature) != mldsa87.SignatureSize {
		return errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "ML-DSA-87 signature must be %d bytes, got %d", mldsa87.SignatureSize, len(signature))
	}
	var pubKey mldsa87.PublicKey
	if err := pubKey.UnmarshalBinary(pubKeyBz); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey, "stored ML-DSA-87 public key is not valid: %s", err.Error())
	}
	if !mldsa87.Verify(&pubKey, message, nil, signature) {
		return errorsmod.Wrap(sdkerrors.ErrUnauthorized, "ML-DSA-87 signature does not verify over the SIGN_MODE_DIRECT sign bytes")
	}
	return nil
}

// mldsaSignatures returns signer address bytes → signature from the critical
// extension. A transaction may contain one ExtensionOptionMLDSASig.
func mldsaSignatures(tx sdk.Tx) (map[string][]byte, error) {
	hasExt, ok := tx.(authante.HasExtensionOptionsTx)
	if !ok {
		return map[string][]byte{}, nil
	}

	var ext *pqauthtypes.ExtensionOptionMLDSASig
	for _, opt := range hasExt.GetExtensionOptions() {
		parsed, ok, err := unpackMLDSAExtension(opt)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if ext != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "transaction contains more than one ML-DSA signature extension")
		}
		ext = parsed
	}
	if ext == nil {
		return map[string][]byte{}, nil
	}

	sigs := make(map[string][]byte, len(ext.Signatures))
	for _, sig := range ext.Signatures {
		addr, err := sdk.AccAddressFromBech32(sig.Signer)
		if err != nil {
			return nil, errorsmod.Wrapf(err, "ML-DSA signature signer %q is not a bech32 account address", sig.Signer)
		}
		if _, exists := sigs[string(addr)]; exists {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "transaction contains two ML-DSA signatures for %s", sig.Signer)
		}
		copied := make([]byte, len(sig.Signature))
		copy(copied, sig.Signature)
		sigs[string(addr)] = copied
	}
	return sigs, nil
}

func unpackMLDSAExtension(opt *codectypes.Any) (*pqauthtypes.ExtensionOptionMLDSASig, bool, error) {
	if opt == nil {
		return nil, false, nil
	}
	if cached, ok := opt.GetCachedValue().(*pqauthtypes.ExtensionOptionMLDSASig); ok {
		return cached, true, nil
	}
	if opt.GetTypeUrl() != pqauthtypes.MLDSAExtensionTypeURL {
		return nil, false, nil
	}
	var ext pqauthtypes.ExtensionOptionMLDSASig
	if err := proto.Unmarshal(opt.GetValue(), &ext); err != nil {
		return nil, false, errorsmod.Wrapf(err, "unmarshal ML-DSA extension")
	}
	return &ext, true, nil
}

func bodyBytesWithClearedMLDSASigs(bodyBytes []byte) ([]byte, error) {
	var body txtypes.TxBody
	if err := proto.Unmarshal(bodyBytes, &body); err != nil {
		return nil, errorsmod.Wrap(err, "unmarshal tx body")
	}
	if err := clearMLDSASigs(body.ExtensionOptions); err != nil {
		return nil, err
	}
	if err := clearMLDSASigs(body.NonCriticalExtensionOptions); err != nil {
		return nil, err
	}
	bz, err := proto.Marshal(&body)
	if err != nil {
		return nil, errorsmod.Wrap(err, "marshal tx body")
	}
	return bz, nil
}

func clearMLDSASigs(opts []*codectypes.Any) error {
	for _, opt := range opts {
		if opt == nil || opt.TypeUrl != pqauthtypes.MLDSAExtensionTypeURL {
			continue
		}
		var ext pqauthtypes.ExtensionOptionMLDSASig
		if err := proto.Unmarshal(opt.Value, &ext); err != nil {
			return errorsmod.Wrap(err, "unmarshal ML-DSA extension")
		}
		for i := range ext.Signatures {
			ext.Signatures[i].Signature = nil
		}
		bz, err := proto.Marshal(&ext)
		if err != nil {
			return errorsmod.Wrap(err, "marshal ML-DSA extension")
		}
		opt.Value = bz
	}
	return nil
}
