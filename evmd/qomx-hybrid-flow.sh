#!/usr/bin/env bash
# Demonstrate the QOMX hybrid flow against a running qomx_9000-2 node.
#
#   1. Generate an ML-DSA-87 key
#   2. Register it with MsgRegisterMLDSAKey (ethsecp256k1 only; the key is not registered yet)
#   3. Open the EVM window with MsgOpenEVMWindow
#   4. Send a Cosmos bank tx that must carry the hybrid signature
#   5. Send an EVM transfer while the window is open
#   6. Show how an EVM transfer is rejected after the window closes
#
# Steps 3 and 4 both use `evmd tx pqauth sign`. After step 2 every Cosmos tx
# from this account is rejected unless that helper attaches the ML-DSA-87 signature.
#
# The evmd binary and the running node must both include the pqauth module.
# Override the binary with EVMD=/path/to/evmd.
#
# Defaults match Validator 1 ($HOME/.qomx-testnet, JSON-RPC 8545).
# Set WAIT_FOR_CLOSE=1 to wait out the 100-block window and confirm rejection.
# With the default timeout_commit of 5s that wait is about nine minutes.

set -euo pipefail

EVMD="${EVMD:-evmd}"
HOME_DIR="${HOME_DIR:-$HOME/.qomx-testnet}"
KEY_NAME="${KEY_NAME:-validator}"
KEYRING="${KEYRING:-test}"
CHAIN_ID="${CHAIN_ID:-qomx_9000-2}"
NODE="${NODE:-tcp://127.0.0.1:26657}"
RPC="${RPC:-http://127.0.0.1:8545}"
GAS_PRICES="${GAS_PRICES:-1000000000aqomx}"
WORK="${WORK:-/tmp/qomx-hybrid-flow}"
WAIT_FOR_CLOSE="${WAIT_FOR_CLOSE:-0}"

COMMON=(
  --home "$HOME_DIR"
  --keyring-backend "$KEYRING"
  --chain-id "$CHAIN_ID"
  --node "$NODE"
)

mkdir -p "$WORK"
MLDSA_KEY="$WORK/validator-mldsa.json"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing command: $1" >&2
    exit 1
  }
}

need "$EVMD"
need jq
need cast
need curl

if ! "$EVMD" tx pqauth --help >/dev/null 2>&1; then
  echo "$EVMD has no 'tx pqauth' commands. Point EVMD at the hybrid evmd binary." >&2
  exit 1
fi

if ! "$EVMD" query pqauth params "${COMMON[@]}" >/dev/null 2>&1; then
  echo "node $NODE does not serve pqauth. Restart it with the hybrid evmd binary." >&2
  exit 1
fi

height() {
  "$EVMD" status --node "$NODE" | jq -r '.sync_info.latest_block_height'
}

wait_tx() {
  local hash="$1"
  local i code
  for i in $(seq 1 40); do
    if "$EVMD" query tx "$hash" --node "$NODE" -o json >"$WORK/tx-result.json" 2>/dev/null; then
      code=$(jq -r '.code' "$WORK/tx-result.json")
      if [[ "$code" != "0" ]]; then
        echo "tx $hash failed: $(jq -r '.raw_log' "$WORK/tx-result.json")" >&2
        exit 1
      fi
      echo "included $hash in block $(jq -r '.height' "$WORK/tx-result.json")"
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for $hash" >&2
  exit 1
}

broadcast() {
  local file="$1"
  local code hash
  "$EVMD" tx broadcast "$file" --node "$NODE" --broadcast-mode sync -o json >"$WORK/broadcast.json"
  code=$(jq -r '.code // 0' "$WORK/broadcast.json")
  hash=$(jq -r '.txhash' "$WORK/broadcast.json")
  if [[ "$code" != "0" || -z "$hash" || "$hash" == "null" ]]; then
    echo "broadcast rejected: $(jq -r '.raw_log // .' "$WORK/broadcast.json")" >&2
    exit 1
  fi
  echo "broadcast $hash (CheckTx code $code)"
  wait_tx "$hash"
}

# Sign with ethsecp256k1 plus the ML-DSA-87 extension, then broadcast.
sign_hybrid_and_broadcast() {
  local unsigned="$1"
  local signed="$2"
  "$EVMD" tx pqauth sign "$unsigned" \
    --from "$KEY_NAME" \
    --key-file "$MLDSA_KEY" \
    --sign-mode direct \
    "${COMMON[@]}" \
    -o json >"$signed"
  broadcast "$signed"
}

ADDR=$("$EVMD" keys show "$KEY_NAME" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
echo "account $ADDR"
echo "balance:"
"$EVMD" query bank balances "$ADDR" --node "$NODE"

echo
echo "1. Generate an ML-DSA-87 key"
if [[ -f "$MLDSA_KEY" ]]; then
  echo "reusing $MLDSA_KEY"
else
  "$EVMD" tx pqauth gen-mldsa-key "$MLDSA_KEY"
fi

echo
echo "2. Register the ML-DSA-87 public key"
registered=$("$EVMD" query pqauth mldsa-pub-key "$ADDR" "${COMMON[@]}" -o json | jq -r '.registered')
if [[ "$registered" == "true" ]]; then
  echo "already registered; skipping MsgRegisterMLDSAKey"
else
  "$EVMD" tx pqauth register-mldsa-key \
    --key-file "$MLDSA_KEY" \
    --from "$KEY_NAME" \
    --gas 600000 \
    --gas-prices "$GAS_PRICES" \
    --broadcast-mode sync \
    -y -o json \
    "${COMMON[@]}" >"$WORK/register.json"
  code=$(jq -r '.code // 0' "$WORK/register.json")
  hash=$(jq -r '.txhash' "$WORK/register.json")
  if [[ "$code" != "0" || -z "$hash" || "$hash" == "null" ]]; then
    echo "register rejected: $(jq -r '.raw_log // .' "$WORK/register.json")" >&2
    exit 1
  fi
  echo "broadcast $hash (CheckTx code $code)"
  wait_tx "$hash"
fi
"$EVMD" query pqauth mldsa-pub-key "$ADDR" "${COMMON[@]}" -o json | jq '{registered, pub_key_bytes: (.pub_key | length)}'

echo
echo "3. Open the EVM authorisation window (hybrid signature required)"
"$EVMD" tx pqauth open-evm-window \
  --from "$KEY_NAME" \
  --gas 500000 \
  --gas-prices "$GAS_PRICES" \
  --generate-only \
  -o json \
  "${COMMON[@]}" >"$WORK/open-unsigned.json"
sign_hybrid_and_broadcast "$WORK/open-unsigned.json" "$WORK/open-signed.json"
"$EVMD" query pqauth evm-window "$ADDR" "${COMMON[@]}" -o json | tee "$WORK/window.json"
expiry=$(jq -r '.expiry_height' "$WORK/window.json")
echo "window open until height $expiry (that height is the first closed block)"

echo
echo "4. Cosmos bank send with the hybrid signature"
"$EVMD" tx bank send "$KEY_NAME" "$ADDR" 1aqomx \
  --from "$KEY_NAME" \
  --gas 500000 \
  --gas-prices "$GAS_PRICES" \
  --generate-only \
  -o json \
  "${COMMON[@]}" >"$WORK/send-unsigned.json"
sign_hybrid_and_broadcast "$WORK/send-unsigned.json" "$WORK/send-signed.json"

echo
echo "5. EVM transfer while the window is open"
PK=$("$EVMD" keys unsafe-export-eth-key "$KEY_NAME" --keyring-backend "$KEYRING" --home "$HOME_DIR")
ETH=$(cast wallet address --private-key "$PK")
echo "eth address $ETH"
open_now=$("$EVMD" query pqauth evm-window "$ADDR" "${COMMON[@]}" -o json | jq -r '.open')
if [[ "$open_now" != "true" ]]; then
  echo "window is already closed; refusing to send the success-path EVM tx" >&2
  exit 1
fi
cast send "$ETH" \
  --value 1wei \
  --private-key "$PK" \
  --rpc-url "$RPC" \
  --chain 9000
echo "EVM transfer accepted"

echo
echo "6. EVM transfer after the window closes"
cat <<EOF
The window closes at height $expiry. A transfer from $ETH then fails CheckTx:

  cast send $ETH --value 1wei --private-key "\$PK" --rpc-url $RPC --chain 9000

or, with a raw transaction built by cast:

  RAW=\$(cast mktx $ETH --value 1wei --private-key "\$PK" --rpc-url $RPC --chain 9000)
  curl -s $RPC -H 'content-type: application/json' \\
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_sendRawTransaction","params":["'"\$RAW"'"]}'

The node error contains:
  EVM authorisation window is closed for $ADDR
EOF

if [[ "$WAIT_FOR_CLOSE" != "1" ]]; then
  echo
  echo "Skipping the wait. Re-run with WAIT_FOR_CLOSE=1 to send the rejected transaction."
  echo "done"
  exit 0
fi

echo "waiting until height $expiry"
while true; do
  now=$(height)
  echo "height $now, expiry $expiry"
  if [[ "$now" -ge "$expiry" ]]; then
    break
  fi
  sleep 5
done

set +e
reject_out=$(cast send "$ETH" \
  --value 1wei \
  --private-key "$PK" \
  --rpc-url "$RPC" \
  --chain 9000 2>&1)
reject_code=$?
set -e
echo "$reject_out"
if [[ "$reject_code" -eq 0 ]]; then
  echo "expected the closed-window transfer to be rejected" >&2
  exit 1
fi
if ! grep -q "EVM authorisation window is closed" <<<"$reject_out"; then
  echo "transfer was rejected, but not by the closed EVM window" >&2
  exit 1
fi
echo "closed-window EVM transfer was rejected"
echo "done"
