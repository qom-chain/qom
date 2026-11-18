#!/bin/sh
# Prepare local, uncommitted CometBFT and Cosmos SDK trees and point evmd/go.mod
# at them with RELATIVE replace directives. A mainnet evmd must be built this
# way. The patched trees and the go.mod replaces are not committed.
#
#   sh scripts/prepare-evmd-build.sh
#   cd evmd && go build -trimpath -buildvcs=false -ldflags="-s -w" ./cmd/evmd
#   git checkout -- evmd/go.mod evmd/go.sum
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
"$root/scripts/patch-cometbft-voteext.sh"
"$root/scripts/apply-cosmos-sdk-clifix.sh"
echo 'Build with: cd evmd && go build -trimpath -buildvcs=false -ldflags="-s -w" ./cmd/evmd'
echo "Do not commit evmd/go.mod or evmd/go.sum replaces."
