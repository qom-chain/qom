// Command qomx9000net prepares and checks a qomx_9000-3 genesis.
//
//	go run ./scripts/qomx9000net.go bind --genesis FILE --chain-id ID --val home=DIR,key=NAME,mldsa=FILE
//	go run ./scripts/qomx9000net.go check-block --genesis FILE --chain-id ID --block FILE
//	go run ./scripts/qomx9000net.go watch-votes --genesis FILE --chain-id ID --node tcp://127.0.0.1:36657
//
// bind verifies a validator proof of possession and stores the ML-DSA public
// key under that consensus address. check-block checks committed vote
// extensions, then rejects an empty extension and a different public key.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/evm/crypto/hybrid"
	evmkeyring "github.com/cosmos/evm/crypto/keyring"
	evmencoding "github.com/cosmos/evm/encoding"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "bind":
		os.Exit(runBind(os.Args[2:]))
	case "check-block":
		os.Exit(runCheckBlock(os.Args[2:]))
	case "watch-votes":
		os.Exit(runWatchVotes(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: qomx9000net bind|check-block [flags]\n")
}

type valSpec struct {
	home  string
	key   string
	mldsa string
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ";") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func runBind(args []string) int {
	fs := flag.NewFlagSet("bind", flag.ExitOnError)
	genesis := fs.String("genesis", "", "genesis.json to update")
	chainID := fs.String("chain-id", "", "chain ID covered by the proof")
	var specs multiFlag
	fs.Var(&specs, "val", "home=DIR,key=NAME,mldsa=FILE (repeat for each validator)")
	_ = fs.Parse(args)
	if *genesis == "" || *chainID == "" || len(specs) == 0 {
		fmt.Fprintln(os.Stderr, "bind requires --genesis, --chain-id, and at least one --val")
		return 2
	}

	enc := evmencoding.MakeConfig(9000)
	cdc := enc.Codec
	root, err := readJSON(*genesis)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	app, ok := root["app_state"].(map[string]any)
	if !ok {
		fmt.Fprintln(os.Stderr, "genesis has no app_state object")
		return 1
	}
	rawPQ, err := json.Marshal(app["pqauth"])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var gs pqauthtypes.GenesisState
	if err := cdc.UnmarshalJSON(rawPQ, &gs); err != nil {
		fmt.Fprintf(os.Stderr, "decode pqauth genesis: %v\n", err)
		return 1
	}

	for _, specText := range specs {
		spec, err := parseVal(specText)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		entry, err := bindOne(*chainID, spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", spec.home, err)
			return 1
		}
		gs.ValidatorKeys = append(gs.ValidatorKeys, entry)
		fmt.Printf("bound consensus %s pubkey %d bytes\n", hex.EncodeToString(entry.ConsensusAddress), len(entry.PubKey))
	}
	if err := gs.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "pqauth genesis rejected: %v\n", err)
		return 1
	}
	out, err := cdc.MarshalJSON(&gs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var pq any
	if err := json.Unmarshal(out, &pq); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	app["pqauth"] = pq
	if err := writeJSON(*genesis, root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("wrote %d validator ML-DSA keys into %s\n", len(gs.ValidatorKeys), *genesis)
	return 0
}

func bindOne(chainID string, spec valSpec) (pqauthtypes.ValidatorMLDSAKey, error) {
	cons, err := consensusAddress(spec.home)
	if err != nil {
		return pqauthtypes.ValidatorMLDSAKey{}, err
	}
	secpPub, err := secpPubKey(spec.home, spec.key)
	if err != nil {
		return pqauthtypes.ValidatorMLDSAKey{}, err
	}
	priv, err := hybrid.LoadMLDSAKey(spec.mldsa)
	if err != nil {
		return pqauthtypes.ValidatorMLDSAKey{}, err
	}
	pub, ok := priv.Public().(*mldsa87.PublicKey)
	if !ok || pub == nil {
		return pqauthtypes.ValidatorMLDSAKey{}, fmt.Errorf("ML-DSA key has no public key")
	}
	pubBytes, err := pub.MarshalBinary()
	if err != nil {
		return pqauthtypes.ValidatorMLDSAKey{}, err
	}
	payload, err := pqauthtypes.ValidatorPoPSignBytes(chainID, cons, secpPub, pubBytes)
	if err != nil {
		return pqauthtypes.ValidatorMLDSAKey{}, err
	}
	sig, err := priv.Sign(rand.Reader, payload, nil)
	if err != nil {
		return pqauthtypes.ValidatorMLDSAKey{}, fmt.Errorf("sign proof: %w", err)
	}
	if err := pqauthtypes.VerifyMLDSA(pubBytes, payload, sig); err != nil {
		return pqauthtypes.ValidatorMLDSAKey{}, fmt.Errorf("proof of possession: %w", err)
	}
	popPath := spec.home + "/config/validator-pop.hex"
	if err := os.WriteFile(popPath, []byte(hex.EncodeToString(sig)+"\n"), 0o600); err != nil {
		return pqauthtypes.ValidatorMLDSAKey{}, err
	}
	return pqauthtypes.ValidatorMLDSAKey{ConsensusAddress: cons, PubKey: pubBytes}, nil
}

func secpPubKey(home, name string) ([]byte, error) {
	enc := evmencoding.MakeConfig(9000)
	kb, err := keyring.New("evmd", keyring.BackendTest, home, os.Stdin, enc.Codec, evmkeyring.Option())
	if err != nil {
		return nil, fmt.Errorf("open keyring: %w", err)
	}
	record, err := kb.Key(name)
	if err != nil {
		return nil, fmt.Errorf("key %s: %w", name, err)
	}
	pk, err := record.GetPubKey()
	if err != nil {
		return nil, err
	}
	if pk.Type() != "eth_secp256k1" {
		return nil, fmt.Errorf("key %s has type %s", name, pk.Type())
	}
	bz := pk.Bytes()
	if len(bz) == 0 {
		return nil, fmt.Errorf("key %s has an empty public key", name)
	}
	return bz, nil
}

func consensusAddress(home string) ([]byte, error) {
	raw, err := os.ReadFile(home + "/config/priv_validator_key.json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	addr, err := hex.DecodeString(doc.Address)
	if err != nil {
		return nil, fmt.Errorf("consensus address: %w", err)
	}
	if len(addr) != pqauthtypes.ConsensusAddressSize {
		return nil, fmt.Errorf("consensus address is %d bytes", len(addr))
	}
	return addr, nil
}

func parseVal(text string) (valSpec, error) {
	var spec valSpec
	spec.key = "validator"
	for _, part := range strings.Split(text, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || v == "" {
			return spec, fmt.Errorf("bad --val entry %q", text)
		}
		switch k {
		case "home":
			spec.home = v
		case "key":
			spec.key = v
		case "mldsa":
			spec.mldsa = v
		default:
			return spec, fmt.Errorf("unknown --val field %s", k)
		}
	}
	if spec.home == "" || spec.mldsa == "" {
		return spec, fmt.Errorf("--val needs home and mldsa: %s", text)
	}
	return spec, nil
}

func runCheckBlock(args []string) int {
	fs := flag.NewFlagSet("check-block", flag.ExitOnError)
	genesis := fs.String("genesis", "", "genesis.json with validator_keys")
	chainID := fs.String("chain-id", "", "chain ID")
	blockPath := fs.String("block", "", "CometBFT /block JSON")
	_ = fs.Parse(args)
	if *genesis == "" || *chainID == "" || *blockPath == "" {
		fmt.Fprintln(os.Stderr, "check-block requires --genesis, --chain-id, and --block")
		return 2
	}
	enc := evmencoding.MakeConfig(9000)
	root, err := readJSON(*genesis)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	app := root["app_state"].(map[string]any)
	rawPQ, err := json.Marshal(app["pqauth"])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var gs pqauthtypes.GenesisState
	if err := enc.Codec.UnmarshalJSON(rawPQ, &gs); err != nil {
		fmt.Fprintf(os.Stderr, "decode pqauth genesis: %v\n", err)
		return 1
	}
	keys := map[string][]byte{}
	for _, item := range gs.ValidatorKeys {
		keys[strings.ToUpper(hex.EncodeToString(item.ConsensusAddress))] = item.PubKey
	}
	if len(keys) == 0 {
		fmt.Fprintln(os.Stderr, "genesis has no validator ML-DSA keys")
		return 1
	}

	body, err := os.ReadFile(*blockPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var rpc struct {
		Result struct {
			Block struct {
				LastCommit commitJSON `json:"last_commit"`
			} `json:"block"`
		} `json:"result"`
		Block struct {
			LastCommit commitJSON `json:"last_commit"`
		} `json:"block"`
	}
	if err := json.Unmarshal(body, &rpc); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	commit := rpc.Result.Block.LastCommit
	if commit.Height == "" && len(commit.Signatures) == 0 {
		commit = rpc.Block.LastCommit
	}
	height, err := parseInt(string(commit.Height))
	if err != nil || height < 1 {
		fmt.Fprintf(os.Stderr, "commit height %q: %v\n", commit.Height, err)
		return 1
	}
	round, err := parseInt(string(commit.Round))
	if err != nil || round < 0 {
		fmt.Fprintf(os.Stderr, "commit round %q: %v\n", commit.Round, err)
		return 1
	}
	hash, err := hex.DecodeString(commit.BlockID.Hash)
	if err != nil || len(hash) == 0 {
		fmt.Fprintf(os.Stderr, "commit block hash: %v\n", err)
		return 1
	}

	checked := 0
	for _, sig := range commit.Signatures {
		if sig.BlockIDFlag != 2 {
			continue
		}
		addr, err := hex.DecodeString(sig.ValidatorAddress)
		if err != nil {
			fmt.Fprintf(os.Stderr, "validator address: %v\n", err)
			return 1
		}
		ext, err := decodeB64(sig.Extension)
		if err != nil || len(ext) == 0 {
			fmt.Fprintf(os.Stderr, "validator %s has no vote extension\n", sig.ValidatorAddress)
			return 1
		}
		expect, ok := keys[strings.ToUpper(sig.ValidatorAddress)]
		if !ok {
			fmt.Fprintf(os.Stderr, "no genesis ML-DSA key for %s\n", sig.ValidatorAddress)
			return 1
		}
		if err := hybrid.VerifyVoteExtensionSignature(*chainID, height, int32(round), hybrid.VoteExtensionPrecommit, hash, addr, ext, expect); err != nil {
			fmt.Fprintf(os.Stderr, "live extension rejected for %s: %v\n", sig.ValidatorAddress, err)
			return 1
		}
		if err := hybrid.VerifyVoteExtensionSignature(*chainID, height, int32(round), hybrid.VoteExtensionPrecommit, hash, addr, nil, expect); err == nil {
			fmt.Fprintln(os.Stderr, "empty extension was accepted")
			return 1
		}
		wrong := append([]byte(nil), expect...)
		wrong[0] ^= 0xff
		if err := hybrid.VerifyVoteExtensionSignature(*chainID, height, int32(round), hybrid.VoteExtensionPrecommit, hash, addr, ext, wrong); err == nil {
			fmt.Fprintln(os.Stderr, "wrong public key was accepted")
			return 1
		}
		fmt.Printf("accepted %s height %d round %d; empty and wrong-key extensions rejected\n", sig.ValidatorAddress, height, round)
		checked++
	}
	if checked < 2 {
		fmt.Fprintf(os.Stderr, "checked %d commit signatures; want both validators\n", checked)
		return 1
	}
	return 0
}

// runWatchVotes reads precommit votes from the node. Canonical /block commits
// do not carry vote extensions; the Vote event does.
func runWatchVotes(args []string) int {
	fs := flag.NewFlagSet("watch-votes", flag.ExitOnError)
	genesis := fs.String("genesis", "", "genesis.json with validator_keys")
	chainID := fs.String("chain-id", "", "chain ID")
	node := fs.String("node", "tcp://127.0.0.1:36657", "CometBFT RPC")
	timeout := fs.Duration("timeout", 30*time.Second, "how long to wait for both validators")
	_ = fs.Parse(args)
	if *genesis == "" || *chainID == "" {
		fmt.Fprintln(os.Stderr, "watch-votes requires --genesis and --chain-id")
		return 2
	}
	keys, err := loadValidatorKeys(*genesis)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	client, err := rpchttp.New(*node, "/websocket")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := client.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = client.Stop() }()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	sub, err := client.Subscribe(ctx, "qomx9000net", "tm.event='Vote'", 32)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	seen := map[string]struct{}{}
	var unknown string
	for len(seen) < 2 {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "timed out with %d of 2 validator extensions\n", len(seen))
			return 1
		case msg, ok := <-sub:
			if !ok {
				fmt.Fprintln(os.Stderr, "vote subscription closed")
				return 1
			}
			vote := voteFromEvent(msg.Data)
			if vote == nil {
				if unknown == "" {
					unknown = fmt.Sprintf("%T", msg.Data)
					fmt.Fprintf(os.Stderr, "skipping event type %s\n", unknown)
				}
				continue
			}
			if int32(vote.Type) != hybrid.VoteExtensionPrecommit || len(vote.BlockID.Hash) == 0 || len(vote.Extension) == 0 {
				continue
			}
			addrHex := strings.ToUpper(hex.EncodeToString(vote.ValidatorAddress))
			if _, ok := seen[addrHex]; ok {
				continue
			}
			expect, ok := keys[addrHex]
			if !ok {
				fmt.Fprintf(os.Stderr, "no genesis ML-DSA key for %s\n", addrHex)
				return 1
			}
			if err := hybrid.VerifyVoteExtensionSignature(
				*chainID, vote.Height, vote.Round, int32(vote.Type),
				vote.BlockID.Hash, vote.ValidatorAddress, vote.Extension, expect,
			); err != nil {
				fmt.Fprintf(os.Stderr, "live extension rejected for %s: %v\n", addrHex, err)
				return 1
			}
			if err := hybrid.VerifyVoteExtensionSignature(
				*chainID, vote.Height, vote.Round, int32(vote.Type),
				vote.BlockID.Hash, vote.ValidatorAddress, nil, expect,
			); err == nil {
				fmt.Fprintln(os.Stderr, "empty extension was accepted")
				return 1
			}
			wrong := append([]byte(nil), expect...)
			wrong[0] ^= 0xff
			if err := hybrid.VerifyVoteExtensionSignature(
				*chainID, vote.Height, vote.Round, int32(vote.Type),
				vote.BlockID.Hash, vote.ValidatorAddress, vote.Extension, wrong,
			); err == nil {
				fmt.Fprintln(os.Stderr, "wrong public key was accepted")
				return 1
			}
			fmt.Printf("accepted %s height %d round %d; empty and wrong-key extensions rejected\n", addrHex, vote.Height, vote.Round)
			seen[addrHex] = struct{}{}
		}
	}
	return 0
}

func voteFromEvent(data any) *cmttypes.Vote {
	switch v := data.(type) {
	case cmttypes.EventDataVote:
		return v.Vote
	case *cmttypes.EventDataVote:
		return v.Vote
	default:
		return nil
	}
}

func loadValidatorKeys(genesis string) (map[string][]byte, error) {
	enc := evmencoding.MakeConfig(9000)
	root, err := readJSON(genesis)
	if err != nil {
		return nil, err
	}
	app, ok := root["app_state"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("genesis has no app_state object")
	}
	rawPQ, err := json.Marshal(app["pqauth"])
	if err != nil {
		return nil, err
	}
	var gs pqauthtypes.GenesisState
	if err := enc.Codec.UnmarshalJSON(rawPQ, &gs); err != nil {
		return nil, fmt.Errorf("decode pqauth genesis: %w", err)
	}
	keys := map[string][]byte{}
	for _, item := range gs.ValidatorKeys {
		keys[strings.ToUpper(hex.EncodeToString(item.ConsensusAddress))] = item.PubKey
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("genesis has no validator ML-DSA keys")
	}
	return keys, nil
}

type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	*f = flexString(b)
	return nil
}

type commitJSON struct {
	Height  flexString `json:"height"`
	Round   flexString `json:"round"`
	BlockID struct {
		Hash string `json:"hash"`
	} `json:"block_id"`
	Signatures []struct {
		BlockIDFlag      int    `json:"block_id_flag"`
		ValidatorAddress string `json:"validator_address"`
		Extension        string `json:"extension"`
	} `json:"signatures"`
}

func parseInt(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	var n int64
	_, err := fmt.Sscan(s, &n)
	return n, err
}

func decodeB64(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	if bz, err := base64.StdEncoding.DecodeString(s); err == nil {
		return bz, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func readJSON(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	return root, nil
}

func writeJSON(path string, root map[string]any) error {
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}
