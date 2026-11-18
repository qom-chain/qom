# Round-aware vote extensions

Mainnet (`qom_33303-1`) runs the round-aware evmd build. Every validator
must use it. Mixing a stock CometBFT evmd into a patched validator set
halts at round ≥ 1.

## What changed

- `scripts/patch-cometbft-voteext.sh` patches CometBFT v0.39.3 so
  in-process `RequestExtendVote` and `RequestVerifyVoteExtension` carry
  `Round` and `VoteType`. The script also writes
  `state/qomx_round_patch.go` with `QOMXABCIVoteExtensionRoundPatched`.
- `abci/vote_extensions.go` signs and verifies with `req.Round` and
  `req.VoteType`.
- `HybridPrivValidator.SignVote` still rewrites a packed extension whose
  prefix does not match `vote.Round`. After the handlers use `req.Round`,
  that rewrite is a no-op on the in-process patched tree. It is kept for
  stock or socket/gRPC CometBFT, where those request fields are dropped.
- On start, evmd calls `assertVoteExtensionRoundPatch()`. A binary built
  against stock CometBFT will not compile. A binary whose ABCI request
  types lack a `Round` field will refuse to start.

Socket and gRPC ABCI still drop those fields. Mainnet uses in-process ABCI.

Build steps are in `docs/mainnet.md`.

## ML-DSA key file vs binding

`AttachMLDSAKey` auto-generates `config/mldsa_priv_validator_key.json` only
for a fresh node whose consensus address has no on-chain or genesis pqauth
binding. If a binding exists and the file is missing or does not match,
startup fails and no new key is written. Committed app state wins over
genesis, so a later key rotation is honoured. Validators that already have
a binding must place the matching key file at that path, mode `0600`,
before `evmd start`.

## Single-validator cutover

A single-validator chain can move to this binary with a restart and no
genesis or consensus-parameter change. Historical mainnet commits were
round 0. Those extensions were packed and signed with round 0, which
matches `last_commit.round`. Replay and block-sync do not re-run
`VerifyVoteExtension`, so a round-aware binary can sync and verify old
blocks after restart.

Recommended steps:

1. Pass an isolated multi-validator testnet gate (a 4-validator set must
   commit heights at round ≥ 1 and continue; a 2-validator stock-CometBFT
   set must halt at round ≥ 1; a mixed patched+stock set must break;
   missing or mismatched bound ML-DSA key files must refuse to start).
2. Build with `scripts/prepare-evmd-build.sh` and
   `cd evmd && go build -trimpath -buildvcs=false -ldflags="-s -w" ./cmd/evmd`.
   The prepare script writes `../src-voteext/...` trees so the binary
   hash matches the live mainnet build. Record the sha256.
3. Copy the running `evmd` to a dated backup. Do not overwrite earlier
   backups.
4. Stop the node, install the new binary, start the node.
5. Confirm replay finishes, `catching_up` is false, blocks continue, and
   logs have no startup-guard or vote-extension errors. Off-chain checks
   of post-cutover extended commits should show a valid ML-DSA extension
   whose packed round matches the commit round and whose public key
   matches the on-chain binding.

A future multi-validator network must coordinate the upgrade. Stock and
patched nodes disagree on extension validity at round > 0.
