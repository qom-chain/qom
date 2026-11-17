package cli

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/spf13/cobra"

	"github.com/cosmos/evm/crypto/hybrid"
)

const flagOverwriteKey = "overwrite"

// NewGenMLDSAKeyCmd writes a new ML-DSA-87 key file and prints the public key.
// The file contains the private key. Register only the printed public key.
func NewGenMLDSAKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen-mldsa-key [file]",
		Short: "Generate an ML-DSA-87 key and write it to a file",
		Long: `Generate an ML-DSA-87 private key and write it as JSON.

The file contains the private key and is written with mode 0600.
The command prints the 2592-byte public key as hex. Pass the file to
"register-mldsa-key --key-file" so the command can sign the proof of
possession. Register only the public key that this command prints.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			overwrite, err := cmd.Flags().GetBool(flagOverwriteKey)
			if err != nil {
				return err
			}
			path := args[0]
			if !overwrite {
				if _, statErr := os.Stat(path); statErr == nil {
					return fmt.Errorf("ML-DSA key file %s already exists (pass --%s to replace it)", path, flagOverwriteKey)
				} else if !os.IsNotExist(statErr) {
					return fmt.Errorf("stat ML-DSA key file: %w", statErr)
				}
			}

			priv, err := hybrid.GenerateMLDSAKey()
			if err != nil {
				return err
			}
			if err := hybrid.SaveMLDSAKey(path, priv); err != nil {
				return err
			}
			pubKey, err := publicKeyBytes(priv)
			if err != nil {
				return err
			}
			cmd.Printf("wrote ML-DSA-87 key to %s\n", path)
			cmd.Printf("public key: %s\n", hex.EncodeToString(pubKey))
			return nil
		},
	}
	cmd.Flags().Bool(flagOverwriteKey, false, "replace an existing key file")
	return cmd
}

func publicKeyFromFile(path string) ([]byte, error) {
	priv, err := hybrid.LoadMLDSAKey(path)
	if err != nil {
		return nil, err
	}
	return publicKeyBytes(priv)
}

func publicKeyBytes(priv *mldsa87.PrivateKey) ([]byte, error) {
	pub, ok := priv.Public().(*mldsa87.PublicKey)
	if !ok || pub == nil {
		return nil, fmt.Errorf("ML-DSA private key has no ML-DSA-87 public key")
	}
	bz, err := pub.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("encode ML-DSA-87 public key: %w", err)
	}
	return bz, nil
}
