#!/bin/sh
# Build a local Cosmos SDK v0.54.3 module carrying the QOMX client-only CLI fix
# (--gas auto / --dry-run key resolution, default gas-adjustment 1.1, JSON dry-run
# output, fee-aware gas estimation, and updated dry-run help) and point
# evmd/go.mod at it with a RELATIVE replace directive.
#
# The patch only touches client/ code (CLI tx building and simulation). It does
# not change any state machine, ante, or consensus code, so nodes built with or
# without it are consensus-compatible. Live mainnet evmd is built with it.
#
# Usage: scripts/apply-cosmos-sdk-clifix.sh [dest-dir]
#   dest-dir defaults to ../src/cosmos-sdk-v0.54.3-qomx (relative to repo root),
#   mirroring scripts/patch-cometbft-voteext.sh. The tree is not committed.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
dest=${1:-"$root/../src/cosmos-sdk-v0.54.3-qomx"}
patchfile="$root/scripts/cosmos-sdk-v0.54.3-clifix.patch"
version=v0.54.3

# Prefer the Go module cache copy; otherwise download it.
mod=$(go env GOMODCACHE)/github.com/cosmos/cosmos-sdk@$version
if [ ! -d "$mod" ]; then
	(cd "$root/evmd" && go mod download github.com/cosmos/cosmos-sdk@$version)
fi
if [ ! -d "$mod" ]; then
	echo "missing $mod; could not download github.com/cosmos/cosmos-sdk $version" >&2
	exit 1
fi

rm -rf "$dest"
mkdir -p "$(dirname "$dest")"
cp -a "$mod" "$dest"
chmod -R u+w "$dest"
patch -d "$dest" -p1 < "$patchfile"

# Add a relative replace to evmd/go.mod (idempotent).
dest_abs=$(CDPATH= cd -- "$dest" && pwd)
rel=$(python3 -c 'import os,sys; print(os.path.relpath(sys.argv[1], sys.argv[2]))' "$dest_abs" "$root/evmd")
(cd "$root/evmd" && go mod edit -replace "github.com/cosmos/cosmos-sdk=$rel")
echo "patched Cosmos SDK $version at $dest"
echo "evmd/go.mod: replace github.com/cosmos/cosmos-sdk => $rel"
echo "build: cd evmd && go build ./cmd/evmd   (do not commit the replace)"
