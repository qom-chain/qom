# Cosmos SDK client fix (`--gas auto` / `--dry-run`)

Live QOM mainnet (`qom_33303-1`) `evmd` is built against Cosmos SDK v0.54.3 with a
small **client-only** patch: `scripts/cosmos-sdk-v0.54.3-clifix.patch`.

It fixes three CLI bugs in upstream `client/` code:

1. `--dry-run --from <key-name>` failed ("a valid bech32 address must be provided in simulation mode").
2. `--gas auto --from <address>` (address not in the local keyring) failed with "no key name".
3. With `--gas-prices` and no `--fees`, the first simulation ran with a zero fee, so fee
   deduction gas (~19k on QOM) was missing from the estimate and a broadcast could run out of gas.
   The estimate now re-simulates once with the implied fee.

Only `client/cmd.go`, `client/context.go`, `client/tx/factory.go` and `client/tx/tx.go` change.
No state-machine, ante, or consensus code is touched, so binaries with and without the patch
are consensus-compatible.

## Build the live mainnet binary

```sh
sh scripts/apply-cosmos-sdk-clifix.sh       # clones SDK v0.54.3 to ../src/cosmos-sdk-v0.54.3-qomx, applies the patch,
                                            # adds a relative replace to evmd/go.mod
cd evmd && go build ./cmd/evmd              # Go 1.25.x (live mainnet: go1.25.9)
git checkout go.mod go.sum                  # do not commit the local replace
```

Tip: Go builds of `evmd` need several GB of temp space. If `/tmp` is a small tmpfs, set
`TMPDIR` to a directory on disk.

## Vote extensions

Live mainnet uses **stock CometBFT v0.39.3**, and `abci/vote_extensions.go` signs/verifies
ML-DSA vote extensions with round `0` and vote type `2` (precommit). Do **not** apply
`scripts/patch-cometbft-voteext.sh` for mainnet builds. The round-aware variant is preserved
on branch `qomx/voteext-round-aware` for a future, coordinated upgrade (all validators must
switch at the same height; mixing the two versions breaks extension verification for round > 0).
