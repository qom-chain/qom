package cli

import (
	"bytes"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/crypto/hybrid"
	"github.com/cosmos/evm/x/pqauth/types"
)

func TestTxCommands(t *testing.T) {
	names := map[string]bool{}
	for _, cmd := range NewTxCmd().Commands() {
		names[cmd.Name()] = true
	}
	for _, name := range []string{
		"gen-mldsa-key",
		"register-mldsa-key",
		"rotate-mldsa-key",
		"register-validator-mldsa-key",
		"open-evm-window",
		"sign",
	} {
		require.Truef(t, names[name], "missing command %s", name)
	}
}

func TestGenMLDSAKeyCmd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice-mldsa.json")

	buf := &bytes.Buffer{}
	cmd := NewGenMLDSAKeyCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{path})
	require.NoError(t, cmd.Execute())

	pubHex := publicKeyHex(t, buf.String())
	pub, err := hex.DecodeString(pubHex)
	require.NoError(t, err)
	require.NoError(t, types.ValidateMLDSAPubKey(pub))

	loaded, err := hybrid.LoadMLDSAKey(path)
	require.NoError(t, err)
	loadedPub, err := publicKeyBytes(loaded)
	require.NoError(t, err)
	require.Equal(t, pub, loadedPub)

	again := NewGenMLDSAKeyCmd()
	again.SetOut(&bytes.Buffer{})
	again.SetErr(&bytes.Buffer{})
	again.SetArgs([]string{path})
	err = again.Execute()
	require.ErrorContains(t, err, "already exists")

	replaced := NewGenMLDSAKeyCmd()
	replaced.SetOut(&bytes.Buffer{})
	replaced.SetErr(&bytes.Buffer{})
	replaced.SetArgs([]string{"--overwrite", path})
	require.NoError(t, replaced.Execute())
}

func TestSignHybridRequiresKeyFile(t *testing.T) {
	cmd := NewSignHybridCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"unsigned.json", "--from", "alice", "--offline", "--chain-id", "qom_9000-1"})
	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "key-file")
}

func publicKeyHex(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		const prefix = "public key: "
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("public key line not found in %q", out)
	return ""
}
