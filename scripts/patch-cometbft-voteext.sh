#!/bin/sh
# Rebuild the local CometBFT module that carries vote round and vote type on
# the in-process ABCI requests. Socket and gRPC ABCI still drop those fields.
# The 74MB tree is not committed. Default dest is ../src-voteext/cometbft-v0.39.3-voteext
# (relative to the repo root). Both go.mod files replace cometbft with it.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
dest=${1:-"$root/../src-voteext/cometbft-v0.39.3-voteext"}
version=v0.39.3

# Prefer the Go module cache copy; otherwise download it from the Go module
# proxy (the download is verified against go.sum / sum.golang.org).
mod=$(go env GOMODCACHE)/github.com/cometbft/cometbft@$version
if [ ! -d "$mod" ]; then
	(cd "$root/evmd" && go mod download github.com/cometbft/cometbft@$version)
fi
if [ ! -d "$mod" ]; then
	echo "missing $mod; could not download github.com/cometbft/cometbft $version" >&2
	exit 1
fi

rm -rf "$dest"
mkdir -p "$(dirname "$dest")"
cp -a "$mod" "$dest"
chmod -R u+w "$dest"
patch -d "$dest" -p1 < "$root/scripts/cometbft-voteext.patch"

# Compile-time sentinel: evmd's startup guard references this const, so a
# build against stock CometBFT v0.39.3 fails to compile instead of starting
# with a silent round-0 binding.
cat > "$dest/state/qomx_round_patch.go" << 'EOF'
package state

// QOMXABCIVoteExtensionRoundPatched is true when this CometBFT tree injects
// vote.Round and vote.Type into in-process ABCI ExtendVote and
// VerifyVoteExtension requests. evmd refuses to start unless this const is
// present, so a build against stock v0.39.3 will not compile the guard.
const QOMXABCIVoteExtensionRoundPatched = true
EOF

# Add a relative replace to evmd/go.mod (idempotent). Do not commit the replace.
dest_abs=$(CDPATH= cd -- "$dest" && pwd)
rel=$(python3 -c 'import os,sys; print(os.path.relpath(sys.argv[1], sys.argv[2]))' "$dest_abs" "$root/evmd")
(cd "$root/evmd" && go mod edit -replace "github.com/cometbft/cometbft=$rel")

echo "patched CometBFT at $dest"
echo "evmd/go.mod: replace github.com/cometbft/cometbft => $rel"
echo "(do not commit the replace)"
