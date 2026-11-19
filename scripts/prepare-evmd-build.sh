#!/bin/sh
# Prepare local, uncommitted CometBFT and Cosmos SDK trees and point evmd/go.mod
# at them with RELATIVE replace directives. A mainnet evmd must be built this
# way. The patched trees and the go.mod replaces are not committed.
#
# Default dest layout (required to reproduce the live mainnet evmd hash):
#   ../src-voteext/cometbft-v0.39.3-voteext
#   ../src-voteext/cosmos-sdk-v0.54.3-qomx
# Those directory names are recorded in the binary's build info.
#
# Release build (reproduces the published sha256; see docs/build.md):
#   export GOTOOLCHAIN=go1.26.11 CGO_ENABLED=1
#   sh scripts/prepare-evmd-build.sh
#   cd evmd && go build -trimpath -buildvcs=false -o build/evmd ./cmd/evmd
#   strip build/evmd
#   git checkout -- go.mod go.sum   # do not commit the local replaces
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
# Invoke via sh so the documented `sh scripts/prepare-evmd-build.sh` path works
# even if a checkout is missing the executable bit.
sh "$root/scripts/patch-cometbft-voteext.sh"
sh "$root/scripts/apply-cosmos-sdk-clifix.sh"
echo 'Build with: cd evmd && go build -trimpath -buildvcs=false -o build/evmd ./cmd/evmd && strip build/evmd'
echo "Do not commit evmd/go.mod or evmd/go.sum replaces."
