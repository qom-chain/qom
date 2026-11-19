# Cosmos SDK client fix (`--gas auto` / `--dry-run`)

Live QOM mainnet (`qom_33303-1`) `evmd` is built against Cosmos SDK v0.54.3 with a
small **client-only** patch: `scripts/cosmos-sdk-v0.54.3-clifix.patch`.

It fixes these CLI bugs in upstream `client/` and `x/bank` help text:

1. `--dry-run --from <key-name>` failed ("a valid bech32 address must be provided in simulation mode").
2. `--gas auto --from <address>` (address not in the local keyring) failed with "no key name".
3. With `--gas-prices` and no `--fees`, the first simulation ran with a zero fee, so fee
   deduction gas (~19k on QOM) was missing from the estimate and a broadcast could run out of gas.
   The estimate now re-simulates once with the implied fee.
4. Default `--gas-adjustment` is 1.1 (upstream is 1.0). Setting `--gas` to a number still
   ignores the adjustment.
5. `--dry-run --output json` prints `{"gas_estimate":N}` on stdout instead of the text line
   `gas estimate: N` on stderr.
6. Help text that said a key name cannot be used with `--dry-run` is updated: a local key
   name or a bech32 address may be used.

The patch touches `client/flags/flags.go`, `client/cmd.go`, `client/context.go`,
`client/tx/factory.go`, `client/tx/tx.go`, `x/bank/client/cli/tx.go`, and
`x/bank/autocli.go`. It does not change state-machine, ante, or consensus code, so
binaries with and without the patch are consensus-compatible.

PQ `--gas auto` also charges the real packed ML-DSA extension size in simulate
mode (`ante/cosmos/mldsa.go` in this repository, not this SDK patch). That
change is similarly limited to `simulate == true`.

## Build the live mainnet binary

`scripts/prepare-evmd-build.sh` applies this patch together with the CometBFT
vote-extension patch. Follow
[validator-join.md, section 3](validator-join.md#3-build-the-release-binary)
for the exact release recipe and SHA-256.

## Vote extensions

Live mainnet runs the round-aware CometBFT v0.39.3 tree from
`scripts/patch-cometbft-voteext.sh`. `abci/vote_extensions.go` signs and
verifies ML-DSA vote extensions with the request's round and vote type.
See `docs/voteext-round-aware-cutover.md`.
