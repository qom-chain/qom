//go:build premainnet

// Command qom9000premainnet is a one-off checker for the qom_9000-1 simulation.
//
//	go run -tags premainnet ./scripts/qom9000premainnet.go dilithium valid.hex invalid.hex
//	go run -tags premainnet ./scripts/qom9000premainnet.go watch --genesis FILE --chain-id ID --node tcp://127.0.0.1:46657
package main

import (
	"context"
	"crypto/rand"
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
	evmencoding "github.com/cosmos/evm/encoding"
	pqauthtypes "github.com/cosmos/evm/x/pqauth/types"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: qom9000premainnet dilithium|watch")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "dilithium":
		os.Exit(runDilithium(os.Args[2:]))
	case "watch":
		os.Exit(runWatch(os.Args[2:]))
	default:
		fmt.Fprintln(os.Stderr, "usage: qom9000premainnet dilithium|watch")
		os.Exit(2)
	}
}

func runDilithium(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "dilithium requires valid-hex and invalid-hex paths")
		return 2
	}
	pub, priv, err := mldsa87.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	msg := []byte("qom-dilithium-precompile-v1")
	sig, err := priv.Sign(rand.Reader, msg, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	pubBytes, err := pub.MarshalBinary()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !mldsa87.Verify(pub, msg, nil, sig) {
		fmt.Fprintln(os.Stderr, "local ML-DSA verify rejected a signature it just created")
		return 1
	}
	bad := append([]byte(nil), sig...)
	bad[0] ^= 0xff
	if mldsa87.Verify(pub, msg, nil, bad) {
		fmt.Fprintln(os.Stderr, "local ML-DSA verify accepted a flipped signature")
		return 1
	}
	valid := append(append(append([]byte{}, msg...), pubBytes...), sig...)
	invalid := append(append(append([]byte{}, msg...), pubBytes...), bad...)
	if err := os.WriteFile(args[0], []byte(hex.EncodeToString(valid)+"\n"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(args[1], []byte(hex.EncodeToString(invalid)+"\n"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("dilithium message %d pubkey %d signature %d\n", len(msg), len(pubBytes), len(sig))
	return 0
}

func runWatch(args []string) int {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	genesis := fs.String("genesis", "", "genesis.json with validator_keys")
	chainID := fs.String("chain-id", "", "chain ID")
	node := fs.String("node", "tcp://127.0.0.1:46657", "CometBFT RPC")
	timeout := fs.Duration("timeout", 45*time.Second, "how long to wait for one precommit")
	_ = fs.Parse(args)
	if *genesis == "" || *chainID == "" {
		fmt.Fprintln(os.Stderr, "watch requires --genesis and --chain-id")
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
	sub, err := client.Subscribe(ctx, "qom9000premainnet", "tm.event='Vote'", 32)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "timed out waiting for a precommit vote extension")
			return 1
		case msg, ok := <-sub:
			if !ok {
				fmt.Fprintln(os.Stderr, "vote subscription closed")
				return 1
			}
			vote := voteFromEvent(msg.Data)
			if vote == nil || int32(vote.Type) != hybrid.VoteExtensionPrecommit || len(vote.BlockID.Hash) == 0 || len(vote.Extension) == 0 {
				continue
			}
			addrHex := strings.ToUpper(hex.EncodeToString(vote.ValidatorAddress))
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
			return 0
		}
	}
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
	raw, err := os.ReadFile(genesis)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
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
