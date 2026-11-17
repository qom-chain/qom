package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cosmos/evm/x/pqauth/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
)

// NewRotateMLDSAKeyCmd replaces the account ML-DSA key. Both the current key
// and the new key sign the rotation payload. The transaction itself must also
// carry the current key's ML-DSA signature, so broadcast it with
// `tx pqauth sign` after --generate-only when the account is already registered.
func NewRotateMLDSAKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate-mldsa-key",
		Short: "Replace the ML-DSA-87 public key of the signing account",
		Long: `Replace the ML-DSA-87 public key of the --from account.

--old-key-file is the key currently registered. --new-key-file is the
replacement. Both sign a payload that includes the account address, chain ID,
secp256k1 public key, and both ML-DSA public keys.

The account is already registered, so the transaction also needs the current
key's transaction signature. Generate the transaction with --generate-only and
sign it with "tx pqauth sign" using the current key file.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			oldFile, err := cmd.Flags().GetString(flagOldKeyFile)
			if err != nil {
				return err
			}
			newFile, err := cmd.Flags().GetString(flagNewKeyFile)
			if err != nil {
				return err
			}
			if oldFile == "" || newFile == "" {
				return fmt.Errorf("--%s and --%s are required", flagOldKeyFile, flagNewKeyFile)
			}
			newPub, oldSig, newSig, err := signRotateFromFiles(clientCtx, oldFile, newFile)
			if err != nil {
				return err
			}
			msg := &types.MsgRotateMLDSAKey{
				Signer:       clientCtx.GetFromAddress().String(),
				NewPubKey:    newPub,
				OldSignature: oldSig,
				NewSignature: newSig,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().String(flagOldKeyFile, "", "current ML-DSA-87 key file")
	cmd.Flags().String(flagNewKeyFile, "", "replacement ML-DSA-87 key file")
	return cmd
}

// NewRegisterValidatorMLDSAKeyCmd binds an ML-DSA key to the consensus address
// of a validator operated by --from. A replacement also requires the current
// key file.
func NewRegisterValidatorMLDSAKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-validator-mldsa-key [consensus-address-hex]",
		Short: "Register the ML-DSA-87 public key for a validator consensus address",
		Long: `Bind an ML-DSA-87 public key to a validator consensus address.

The consensus address is 20 raw bytes, hex encoded. --from must be that
validator's operator. --key-file signs the proof of possession.

When the consensus address already has a key, pass --old-key-file so the
current key signs the replacement. The account transaction signature is still
required: use --generate-only and "tx pqauth sign" when the operator account
is registered.

Vote extensions are checked against this on-chain key.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			consAddr, err := decodeHexBytes(args[0])
			if err != nil {
				return fmt.Errorf("consensus address hex: %w", err)
			}
			if len(consAddr) != types.ConsensusAddressSize {
				return fmt.Errorf("consensus address must be %d bytes, got %d", types.ConsensusAddressSize, len(consAddr))
			}
			keyFile, err := cmd.Flags().GetString(flagMLDSAKeyFile)
			if err != nil {
				return err
			}
			if keyFile == "" {
				return fmt.Errorf("--%s is required", flagMLDSAKeyFile)
			}
			oldFile, err := cmd.Flags().GetString(flagOldKeyFile)
			if err != nil {
				return err
			}
			pubKey, pop, oldSig, err := signValidatorPopFromFile(clientCtx, consAddr, keyFile, oldFile)
			if err != nil {
				return err
			}
			msg := &types.MsgRegisterValidatorMLDSAKey{
				Signer:           clientCtx.GetFromAddress().String(),
				ConsensusAddress: consAddr,
				PubKey:           pubKey,
				PopSignature:     pop,
				OldSignature:     oldSig,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().String(flagMLDSAKeyFile, "", "ML-DSA-87 key file to register")
	cmd.Flags().String(flagOldKeyFile, "", "current validator ML-DSA-87 key file, required when replacing a key")
	return cmd
}
