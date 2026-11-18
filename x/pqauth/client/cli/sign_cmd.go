package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cosmos/evm/crypto/hybrid"
	pqauthclient "github.com/cosmos/evm/x/pqauth/client"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	authclient "github.com/cosmos/cosmos-sdk/x/auth/client"
)

// NewSignHybridCmd signs one local ethsecp256k1 account and attaches that
// account's ML-DSA-87 signature in the critical extension.
func NewSignHybridCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sign [file]",
		Short: "Sign a Cosmos transaction with ethsecp256k1 and an ML-DSA-87 extension",
		Long: `Read an unsigned transaction from [file], sign it for --from, and print the signed JSON.

The ML-DSA-87 private key is read from --key-file (the file written by gen-mldsa-key).
Its signature is stored in the critical extension and covers the SIGN_MODE_DIRECT
bytes with that signature field cleared. The keyring then signs the transaction
again with the account's ethsecp256k1 key, over the transaction that already
contains the ML-DSA signature. Any previous signature on the transaction is replaced.
Pass "-" to read the transaction from stdin.

Offline mode does not query the node. Pass --account-number and --sequence,
including when either value is zero. Online mode queries the node and ignores
those two flags.

Example:
  evmd tx bank send alice cosmos1... 1stake --generate-only > unsigned.json
  evmd tx pqauth sign unsigned.json --from alice --key-file alice-mldsa.json \
    --chain-id qom_9000-1 --offline --account-number 0 --sequence 0
`,
		Args:   cobra.ExactArgs(1),
		PreRun: preSignHybrid,
		RunE:   runSignHybrid,
	}

	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().String(flagMLDSAKeyFile, "", "ML-DSA-87 private key file from gen-mldsa-key")
	if err := cmd.MarkFlagRequired(flagMLDSAKeyFile); err != nil {
		panic(err)
	}
	return cmd
}

func preSignHybrid(cmd *cobra.Command, _ []string) {
	if offline, _ := cmd.Flags().GetBool(flags.FlagOffline); offline {
		_ = cmd.MarkFlagRequired(flags.FlagAccountNumber)
		_ = cmd.MarkFlagRequired(flags.FlagSequence)
	}
}

func runSignHybrid(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}
	if clientCtx.FromName == "" {
		return fmt.Errorf("pass --%s as the name of a local ethsecp256k1 key", flags.FlagFrom)
	}
	if clientCtx.Keyring == nil {
		return fmt.Errorf("keyring is not available")
	}

	record, err := clientCtx.Keyring.Key(clientCtx.FromName)
	if err != nil {
		return fmt.Errorf("load key %s: %w", clientCtx.FromName, err)
	}
	if record.GetType() != keyring.TypeLocal {
		return fmt.Errorf("hybrid signing needs a local ethsecp256k1 key, got %s", record.GetType())
	}

	unsigned, err := authclient.ReadTxFromFile(clientCtx, args[0])
	if err != nil {
		return fmt.Errorf("read transaction %s: %w", args[0], err)
	}
	txBuilder, err := clientCtx.TxConfig.WrapTxBuilder(unsigned)
	if err != nil {
		return fmt.Errorf("wrap transaction: %w", err)
	}

	txf, err := tx.NewFactoryCLI(clientCtx, cmd.Flags())
	if err != nil {
		return err
	}
	if !clientCtx.Offline {
		if clientCtx.AccountRetriever == nil {
			return fmt.Errorf("account retriever is not available")
		}
		accNum, accSeq, err := clientCtx.AccountRetriever.GetAccountNumberSequence(clientCtx, clientCtx.GetFromAddress())
		if err != nil {
			return fmt.Errorf("query account %s: %w", clientCtx.GetFromAddress(), err)
		}
		txf = txf.WithAccountNumber(accNum).WithSequence(accSeq)
	}

	keyFile, err := cmd.Flags().GetString(flagMLDSAKeyFile)
	if err != nil {
		return err
	}
	priv, err := hybrid.LoadMLDSAKey(keyFile)
	if err != nil {
		return err
	}

	if err := pqauthclient.SignHybrid(
		cmd.Context(),
		txf,
		clientCtx.TxConfig,
		clientCtx.FromName,
		txBuilder,
		priv,
	); err != nil {
		return err
	}

	bz, err := clientCtx.TxConfig.TxJSONEncoder()(txBuilder.GetTx())
	if err != nil {
		return fmt.Errorf("encode signed transaction: %w", err)
	}
	bz = append(bz, '\n')
	return clientCtx.PrintBytes(bz)
}
