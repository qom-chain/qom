package hybrid

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	cometprivval "github.com/cometbft/cometbft/privval"
)

func TestAttachMLDSAKeyFreshNodeAutoGenerates(t *testing.T) {
	dir := t.TempDir()
	stdPV := cometprivval.GenFilePV(filepath.Join(dir, "priv_validator_key.json"), filepath.Join(dir, "priv_validator_state.json"))
	path := MLDSAKeyPath(filepath.Join(dir, "priv_validator_key.json"))

	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err))

	hpv, err := AttachMLDSAKey(stdPV, path, nil)
	require.NoError(t, err)
	require.True(t, hpv.IsHybridEnabled())
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Second start with no binding reuses the generated file.
	hpv2, err := AttachMLDSAKey(stdPV, path, nil)
	require.NoError(t, err)
	require.Equal(t, mustPub(t, hpv), mustPub(t, hpv2))
}

func TestAttachMLDSAKeyMissingFileFailsWhenBound(t *testing.T) {
	dir := t.TempDir()
	stdPV := cometprivval.GenFilePV(filepath.Join(dir, "priv_validator_key.json"), filepath.Join(dir, "priv_validator_state.json"))
	path := MLDSAKeyPath(filepath.Join(dir, "priv_validator_key.json"))
	bound := newPubBytes(t)

	hpv, err := AttachMLDSAKey(stdPV, path, bound)
	require.Error(t, err)
	require.Nil(t, hpv)
	require.Contains(t, err.Error(), "refusing to auto-generate")
	_, statErr := os.Stat(path)
	require.True(t, os.IsNotExist(statErr), "must not create a key file when a binding exists")
}

func TestAttachMLDSAKeyMismatchedFileFailsWhenBound(t *testing.T) {
	dir := t.TempDir()
	stdPV := cometprivval.GenFilePV(filepath.Join(dir, "priv_validator_key.json"), filepath.Join(dir, "priv_validator_state.json"))
	path := MLDSAKeyPath(filepath.Join(dir, "priv_validator_key.json"))

	hpv, err := AttachMLDSAKey(stdPV, path, nil)
	require.NoError(t, err)
	filePub := mustPub(t, hpv)

	other := newPubBytes(t)
	require.NotEqual(t, filePub, other)

	hpv2, err := AttachMLDSAKey(stdPV, path, other)
	require.Error(t, err)
	require.Nil(t, hpv2)
	require.Contains(t, err.Error(), "does not match the on-chain or genesis binding")

	// Matching binding succeeds and does not rewrite the file.
	hpv3, err := AttachMLDSAKey(stdPV, path, filePub)
	require.NoError(t, err)
	require.Equal(t, filePub, mustPub(t, hpv3))
}

func TestLookupGenesisAndResolveBinding(t *testing.T) {
	dir := t.TempDir()
	cons := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01, 0x02, 0x03, 0x04, 0x05}
	pub := newPubBytes(t)
	genesis := filepath.Join(dir, "genesis.json")
	doc := map[string]any{
		"app_state": map[string]any{
			"pqauth": map[string]any{
				"validator_keys": []map[string]any{{
					"consensus_address": base64.StdEncoding.EncodeToString(cons),
					"pub_key":           base64.StdEncoding.EncodeToString(pub),
				}},
			},
		},
	}
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(genesis, raw, 0o600))

	got, found, err := LookupGenesisValidatorMLDSAPubKey(genesis, cons)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, pub, got)

	_, found, err = LookupGenesisValidatorMLDSAPubKey(genesis, []byte("not-this-validator-addr"))
	require.NoError(t, err)
	require.False(t, found)

	// Store lookup wins over genesis (rotated key).
	rotated := newPubBytes(t)
	got, found, err = ResolveValidatorMLDSABinding(cons, genesis, func(addr []byte) ([]byte, bool) {
		if string(addr) == string(cons) {
			return rotated, true
		}
		return nil, false
	})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, rotated, got)

	// Empty store falls back to genesis.
	got, found, err = ResolveValidatorMLDSABinding(cons, genesis, func([]byte) ([]byte, bool) { return nil, false })
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, pub, got)
}

func mustPub(t *testing.T, hpv *HybridPrivValidator) []byte {
	t.Helper()
	bz, err := hpv.GetMLDSAPubKey().MarshalBinary()
	require.NoError(t, err)
	return bz
}

func newPubBytes(t *testing.T) []byte {
	t.Helper()
	pub, _, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)
	bz, err := pub.MarshalBinary()
	require.NoError(t, err)
	return bz
}
