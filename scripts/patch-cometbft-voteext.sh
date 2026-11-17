#!/bin/sh
# Rebuild the local CometBFT module that carries vote round and vote type on
# the in-process ABCI requests. Socket and gRPC ABCI still drop those fields.
# The 74MB tree is not committed. Both go.mod files replace cometbft with it.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
dest=${1:-"$root/../src/cometbft-v0.39.3-voteext"}
mod=$(go env GOPATH)/pkg/mod/github.com/cometbft/cometbft@v0.39.3

if [ ! -d "$mod" ]; then
	echo "missing $mod; download github.com/cometbft/cometbft v0.39.3 first" >&2
	exit 1
fi

rm -rf "$dest"
mkdir -p "$(dirname "$dest")"
cp -a "$mod" "$dest"
chmod -R u+w "$dest"
patch -d "$dest" -p1 < "$root/scripts/cometbft-voteext.patch"
echo "patched CometBFT at $dest"
