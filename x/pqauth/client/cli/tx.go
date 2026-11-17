package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cosmos/evm/x/pqauth/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
)

// NewTxCmd returns the pqauth transaction commands.
func NewTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "pqauth transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		NewGenMLDSAKeyCmd(),
		NewRegisterMLDSAKeyCmd(),
		NewRotateMLDSAKeyCmd(),
		NewRegisterValidatorMLDSAKeyCmd(),
		NewOpenEVMWindowCmd(),
		NewSignHybridCmd(),
	)
	return cmd
}

const (
	flagMLDSAKeyFile = "key-file"
	flagPopSignature = "pop-signature"
	flagOldKeyFile   = "old-key-file"
	flagNewKeyFile   = "new-key-file"
)

// NewRegisterMLDSAKeyCmd binds an ML-DSA-87 public key to the --from account.
// --key-file signs the proof of possession. A public-key hex argument requires
// --pop-signature over the account address, chain ID, secp256k1 key, and ML-DSA key.
func NewRegisterMLDSAKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-mldsa-key [pub-key-hex]",
		Short: "Register an ML-DSA-87 public key for the signing account",
		Long: `Register an ML-DSA-87 public key for the --from account.

The account must already exist and use ethsecp256k1. The transaction includes
an ML-DSA proof of possession over the account address, chain ID, compressed
secp256k1 public key, and the ML-DSA public key.

Pass --key-file to sign that proof from a gen-mldsa-key file. The private key
stays on this machine. Alternatively pass the public key hex and
--pop-signature.

Registration deletes authz grants and fee allowances where this account is
the granter. The account is not registered yet, so this transaction is signed
with ethsecp256k1 only.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			keyFile, err := cmd.Flags().GetString(flagMLDSAKeyFile)
			if err != nil {
				return err
			}
			popHex, err := cmd.Flags().GetString(flagPopSignature)
			if err != nil {
				return err
			}
			if keyFile != "" && len(args) > 0 {
				return fmt.Errorf("pass either a public key hex argument or --%s, not both", flagMLDSAKeyFile)
			}
			if keyFile != "" && popHex != "" {
				return fmt.Errorf("pass either --%s or --%s, not both", flagMLDSAKeyFile, flagPopSignature)
			}

			var pubKey, pop []byte
			switch {
			case keyFile != "":
				pubKey, pop, err = signAccountPopFromFile(clientCtx, keyFile)
				if err != nil {
					return err
				}
			case len(args) == 1:
				pubKey, err = decodeHexBytes(args[0])
				if err != nil {
					return fmt.Errorf("public key hex: %w", err)
				}
				if popHex == "" {
					return fmt.Errorf("a public key hex argument requires --%s, or pass --%s so the proof can be signed", flagPopSignature, flagMLDSAKeyFile)
				}
				pop, err = decodeHexBytes(popHex)
				if err != nil {
					return fmt.Errorf("proof signature hex: %w", err)
				}
			default:
				return fmt.Errorf("pass a public key hex argument or --%s", flagMLDSAKeyFile)
			}
			if err := types.ValidateMLDSAPubKey(pubKey); err != nil {
				return err
			}
			if err := types.ValidateMLDSASignature(pop); err != nil {
				return err
			}

			msg := &types.MsgRegisterMLDSAKey{
				Signer:       clientCtx.GetFromAddress().String(),
				PubKey:       pubKey,
				PopSignature: pop,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().String(flagMLDSAKeyFile, "", "ML-DSA-87 key file from gen-mldsa-key; the private key signs the proof and is not sent")
	cmd.Flags().String(flagPopSignature, "", "hex ML-DSA proof of possession, required when the public key is passed as hex")
	return cmd
}

// NewOpenEVMWindowCmd opens or refreshes the --from account's EVM window.
func NewOpenEVMWindowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open-evm-window",
		Short: "Open or refresh the EVM authorisation window for the signing account",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgOpenEVMWindow{
				Signer: clientCtx.GetFromAddress().String(),
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
