package cosmos

import (
	"fmt"

	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	protov2 "google.golang.org/protobuf/proto"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// maxAuthzDepth matches AuthzLimiterDecorator. A deeper MsgExec is rejected.
const maxAuthzDepth = 7

// rejectAuthzBypass closes the grants that would otherwise spend a registered
// account without its ML-DSA key.
//
// Ante runs before messages, so deleting grants inside MsgRegisterMLDSAKey
// cannot stop a MsgGrant or MsgExec in the same transaction. Those messages
// are rejected together. GenericAuthorization stays rejected after
// registration as well: one type URL is broad enough to recreate the bypass.
func (d MLDSASigDecorator) rejectAuthzBypass(ctx sdk.Context, tx sdk.Tx) error {
	msgs := tx.GetMsgs()
	registering := make(map[string]struct{})
	privileged := false
	usesAuthz := false
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *pqauthtypes.MsgRegisterMLDSAKey:
			privileged = true
			addr, err := sdk.AccAddressFromBech32(m.Signer)
			if err != nil {
				return errorsmod.Wrap(err, "ML-DSA registration signer")
			}
			registering[string(addr)] = struct{}{}
		case *pqauthtypes.MsgRotateMLDSAKey, *pqauthtypes.MsgRegisterValidatorMLDSAKey:
			privileged = true
		case *authz.MsgGrant, *authz.MsgExec:
			usesAuthz = true
		}
	}
	if privileged && usesAuthz {
		return errorsmod.Wrap(sdkerrors.ErrUnauthorized, "ML-DSA registration, rotation, and validator-key messages must be their own transaction; they cannot be combined with MsgGrant or MsgExec")
	}
	return d.rejectGenericAuth(ctx, msgs, registering, 1)
}

func (d MLDSASigDecorator) rejectGenericAuth(ctx sdk.Context, msgs []sdk.Msg, registering map[string]struct{}, depth int) error {
	if depth >= maxAuthzDepth {
		return errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "found more nested msgs than permitted; got: %d, expected: <%d", depth, maxAuthzDepth)
	}
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *authz.MsgGrant:
			granter, err := sdk.AccAddressFromBech32(m.Granter)
			if err != nil {
				return errorsmod.Wrap(err, "authz granter")
			}
			if !d.effectivelyRegistered(ctx, granter, registering) {
				continue
			}
			authorization, err := m.GetAuthorization()
			if err != nil {
				return err
			}
			if _, ok := authorization.(*authz.GenericAuthorization); ok {
				return errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "account %s has a registered ML-DSA-87 key and cannot grant GenericAuthorization", granter)
			}
		case *authz.MsgExec:
			grantee, err := sdk.AccAddressFromBech32(m.Grantee)
			if err != nil {
				return errorsmod.Wrap(err, "authz grantee")
			}
			inners, err := m.GetMessages()
			if err != nil {
				return err
			}
			if err := d.rejectGenericAuth(ctx, inners, registering, depth+1); err != nil {
				return err
			}
			for _, inner := range inners {
				signers, err := d.messageSigners(inner)
				if err != nil {
					return err
				}
				for _, granter := range signers {
					if granter.Equals(grantee) || !d.effectivelyRegistered(ctx, granter, registering) {
						continue
					}
					if d.authz == nil {
						return errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "account %s has a registered ML-DSA-87 key; MsgExec was rejected because the authz keeper is not available to check for GenericAuthorization", granter)
					}
					authorization, _ := d.authz.GetAuthorization(ctx, grantee, granter, sdk.MsgTypeURL(inner))
					if _, ok := authorization.(*authz.GenericAuthorization); ok {
						return errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "account %s has a registered ML-DSA-87 key and cannot be spent through GenericAuthorization", granter)
					}
				}
			}
		}
	}
	return nil
}

// messageSigners returns the accounts that authorize msg. Authz uses the
// application codec's proto signer annotations. Legacy GetSigners remains for
// messages that still implement it when no codec is available.
func (d MLDSASigDecorator) messageSigners(msg sdk.Msg) ([]sdk.AccAddress, error) {
	type v1SignerCodec interface {
		GetMsgV1Signers(gogoproto.Message) ([][]byte, protov2.Message, error)
	}
	if src, ok := d.cdc.(v1SignerCodec); ok && src != nil {
		raw, _, err := src.GetMsgV1Signers(msg)
		if err != nil {
			return nil, errorsmod.Wrapf(err, "signers of %s", sdk.MsgTypeURL(msg))
		}
		if len(raw) == 0 {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "message %s has no signer", sdk.MsgTypeURL(msg))
		}
		out := make([]sdk.AccAddress, len(raw))
		for i, bz := range raw {
			out[i] = sdk.AccAddress(bz)
		}
		return out, nil
	}
	legacy, ok := msg.(sdk.LegacyMsg)
	if !ok {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "cannot identify the granter of %s inside MsgExec", sdk.MsgTypeURL(msg))
	}
	signers := legacy.GetSigners()
	if len(signers) == 0 {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "message %s inside MsgExec has no signer", sdk.MsgTypeURL(msg))
	}
	return signers, nil
}

func (d MLDSASigDecorator) effectivelyRegistered(ctx sdk.Context, addr sdk.AccAddress, registering map[string]struct{}) bool {
	if _, ok := registering[string(addr)]; ok {
		return true
	}
	_, found := d.pq.GetMLDSAPubKey(ctx, addr)
	return found
}

// extraPrincipals returns registered accounts whose funds or messages this
// transaction uses even though they did not sign it with secp256k1. Each one
// must still present an ML-DSA signature. Their account sequence is not
// incremented; replay is bound to the outer transaction.
func (d MLDSASigDecorator) extraPrincipals(ctx sdk.Context, tx sdk.Tx, outer map[string]struct{}) ([]sdk.AccAddress, error) {
	seen := make(map[string]struct{})
	var extras []sdk.AccAddress
	add := func(addr sdk.AccAddress) {
		if len(addr) == 0 {
			return
		}
		id := string(addr)
		if _, ok := outer[id]; ok {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		if _, found := d.pq.GetMLDSAPubKey(ctx, addr); !found {
			return
		}
		seen[id] = struct{}{}
		extras = append(extras, append(sdk.AccAddress(nil), addr...))
	}
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		add(sdk.AccAddress(feeTx.FeeGranter()))
	}
	if err := d.collectExecGranters(tx.GetMsgs(), add, 1); err != nil {
		return nil, err
	}
	return extras, nil
}

func (d MLDSASigDecorator) collectExecGranters(msgs []sdk.Msg, add func(sdk.AccAddress), depth int) error {
	if depth >= maxAuthzDepth {
		return fmt.Errorf("found more nested msgs than permitted; got: %d, expected: <%d", depth, maxAuthzDepth)
	}
	for _, msg := range msgs {
		exec, ok := msg.(*authz.MsgExec)
		if !ok {
			continue
		}
		inners, err := exec.GetMessages()
		if err != nil {
			return err
		}
		for _, inner := range inners {
			if nested, ok := inner.(*authz.MsgExec); ok {
				if err := d.collectExecGranters([]sdk.Msg{nested}, add, depth+1); err != nil {
					return err
				}
				continue
			}
			signers, err := d.messageSigners(inner)
			if err != nil {
				return err
			}
			for _, signer := range signers {
				add(signer)
			}
		}
	}
	return nil
}
