# QOM Mainnet

Public genesis for chain `qom_33303-1`. The file in this repository is the
genesis every node must use.

`evmd/qom_33303-1-genesis.json`

SHA-256: `51f77bb2646e5a768562e5827c50b2416726c9a6a6249772e9baa5d27644a1bd`

```bash
sha256sum evmd/qom_33303-1-genesis.json
evmd genesis validate evmd/qom_33303-1-genesis.json
```

Genesis time: `2026-11-17T02:44:49Z`.

Private keys and the ML-DSA key file stay on the operator's machine.
They are not in this repository.

## Chain parameters

| Parameter | Value |
| --- | --- |
| Chain name / moniker | QOM |
| EVM chain ID | 33303 |
| Cosmos chain ID | `qom_33303-1` |
| Native token | QOMX, 18 decimals |
| Base denom | `aqomx` (alias `attoqomx`, display `qomx`) |
| Total supply | 200,000,000 QOMX (`200000000000000000000000000` aqomx) |
| Inflation | 0 (`inflation`, `inflation_max`, `inflation_min`, `inflation_rate_change`, and annual provisions are all zero) |
| Mint max supply | 200,000,000 QOMX |
| Block gas limit | 30,000,000 |
| Min gas price and base-fee floor | `47619047619048` aqomx |
| EVM window duration | 1000 blocks |
| `restrict_cosmos_precompiles` | false |
| Vote extensions | enabled at height 1 |
| Consensus key | Ed25519 |
| Max validators | 100 |
| Unbonding time | 14 days (`1209600s`) |
| Dilithium precompile | `0x0000000000000000000000000000000000000401` |
| Native ERC-20 (`aqomx`) | `0xD4949664cD82660AaE99bEdc034a0deA8A0bd517` (module-owned, enabled) |

A 21,000-gas transfer at the floor price costs `1000000000000008000` aqomx
(1.000000000000008 QOMX). `eth_gasPrice` reports nine eighths of the floor,
`53571428571429` aqomx. The enforced base fee stays at the floor.

Voting power uses a reduction of 10^18, so 1 QOMX of stake is 1 voting power
after integer truncation.

## Genesis allocation

| Account | Address | Amount |
| --- | --- | --- |
| CEX hot wallet | `0xF9C3b5e63416ed9e4dB661B10D66e1bAeC6b6c83` (`cosmos1l8pmte35zmkeundkvxcs6ehphtkxkmyrk9q4k8`) | 199,000,000 QOMX |
| Initial validator | `0x385BE93570680703A12d0406ed534dAc7Af601fb` (`cosmos18pd7jdtsdqrs8gfdqsrw656d43a0vq0mzsm9q7`) | 1,000,000 QOMX |

The CEX address is a funded account. This repository has no key for it.

The initial validator operator address is
`cosmosvaloper18pd7jdtsdqrs8gfdqsrw656d43a0vq0m8y0svd`.

The gentx self-delegates `999876190476190476000000` aqomx
(999,876.190476190476 QOMX). Voting power is 999876. Commission is 10%, with
a 20% maximum and a 1% maximum change rate. Minimum self-delegation is 1
aqomx.

The gentx fee is 500,000 gas at the floor price:
`23809523809524000000` aqomx (23.809523809524 QOMX). After that fee and the
self-delegation, the validator account keeps 100 QOMX liquid
(`100000000000000000000` aqomx) so it can pay fees. Rewards stay in the
distribution module until withdrawn, and inflation is zero, so the liquid
balance is the operating reserve.

Ed25519 consensus pubkey: `lCEu0UBPE0/Vn2WQFlDYzsYjO1v4Ku+r+JhOAQG8JDs=`

Consensus address: `FB64B44E95FC813C4C7632C83C18DC1C9AB99262`

The matching ML-DSA-87 public key is already in
`app_state.pqauth.validator_keys`. The genesis validator does not send
`register-validator-mldsa-key` after launch.

Node ID: `25c9967d869feb5340d85c01cfa16ff1d1591632`

Gentx memo: `25c9967d869feb5340d85c01cfa16ff1d1591632@0.0.0.0:26656`

The memo is inside the signed gentx. Leave it unchanged. `0.0.0.0` is a
placeholder because no public peer address was set at genesis. Peers dial
`25c9967d869feb5340d85c01cfa16ff1d1591632@p2p.qomx.io:26656`.

## Build

Every node on `qom_33303-1` must run the release binary. Build it exactly as
described in [validator-join.md, section 3](validator-join.md#3-build-the-release-binary):
Go 1.26.11, `CGO_ENABLED=1`, `GOMODCACHE=/var/tmp/qom-gomodcache`, the
patched CometBFT and Cosmos SDK trees from `scripts/prepare-evmd-build.sh`,
`go build -trimpath -buildvcs=false`, then `strip`. The section lists the
release SHA-256.

The release carries round-aware vote extensions: they are signed and
verified with the actual CometBFT vote round and vote type. A binary built
against stock CometBFT does not compile (`evmd` references
`QOMXABCIVoteExtensionRoundPatched`), and a validator set that mixes
patched and stock nodes stops at round 1 or higher. See
`docs/voteext-round-aware-cutover.md`.

## Add QOM to MetaMask

| Field | Value |
| --- | --- |
| Network name | QOM |
| RPC URL | the node's JSON-RPC endpoint, port 8545 |
| Chain ID | 33303 |
| Currency symbol | QOMX |
| Decimals | 18 |
| Block explorer | https://scan.qomx.io |

The chain ID in MetaMask is the EVM chain ID, 33303. The Cosmos chain ID
`qom_33303-1` is the `--chain-id` flag on `evmd`.

## Run a full node

`evmd init` writes example-chain defaults (`0aatom`, EVM chain ID `262144`,
mempool type `flood`, JSON-RPC disabled, gas cap 25,000,000, tx-fee cap 1).
Replace the genesis and correct those settings. A tx-fee cap of 1 QOMX
rejects an ordinary transfer, because that transfer costs about 1 QOMX.

```bash
export HOME_DIR="$HOME/.qom"
export CHAIN_ID="qom_33303-1"

evmd init QOM --chain-id "$CHAIN_ID" --home "$HOME_DIR"
cp evmd/qom_33303-1-genesis.json "$HOME_DIR/config/genesis.json"

# CometBFT mempool: the EVM app owns the mempool.
sed -i '/^\[mempool\]/,/^\[/ s/^type = "flood"/type = "app"/' "$HOME_DIR/config/config.toml"

# app.toml
sed -i 's/minimum-gas-prices = "0aatom"/minimum-gas-prices = "47619047619048aqomx"/' "$HOME_DIR/config/app.toml"
sed -i 's/evm-chain-id = 262144/evm-chain-id = 33303/' "$HOME_DIR/config/app.toml"
sed -i '/^\[json-rpc\]/,/^\[/ s/^enable = false/enable = true/' "$HOME_DIR/config/app.toml"
sed -i '/^\[json-rpc\]/,/^\[/ s/^gas-cap = 25000000/gas-cap = 30000000/' "$HOME_DIR/config/app.toml"
sed -i '/^\[json-rpc\]/,/^\[/ s/^txfee-cap = 1/txfee-cap = 2000/' "$HOME_DIR/config/app.toml"

evmd genesis validate --home "$HOME_DIR"
```

Confirm the hash before the first start:

```bash
sha256sum "$HOME_DIR/config/genesis.json"
# 51f77bb2646e5a768562e5827c50b2416726c9a6a6249772e9baa5d27644a1bd
```

Start command. These flags match the genesis even when `app.toml` is still
at the init defaults. JSON-RPC listens on localhost until you change the address.

```bash
evmd start \
  --home "$HOME_DIR" \
  --chain-id "$CHAIN_ID" \
  --minimum-gas-prices 47619047619048aqomx \
  --evm.evm-chain-id 33303 \
  --json-rpc.enable \
  --json-rpc.address 127.0.0.1:8545 \
  --json-rpc.ws-address 127.0.0.1:8546 \
  --json-rpc.api eth,net,web3 \
  --json-rpc.gas-cap 30000000 \
  --json-rpc.txfee-cap 2000
```

Default ports on a dedicated host are P2P `26656`, Comet RPC `26657`, and
JSON-RPC `8545`. To publish JSON-RPC, set `--json-rpc.address 0.0.0.0:8545`
and put a reverse proxy in front of it.

Peer configuration:

```toml
# config.toml
persistent_peers = "25c9967d869feb5340d85c01cfa16ff1d1591632@p2p.qomx.io:26656"
external_address = "YOUR_PUBLIC_IP:26656"   # only if peers should dial you
```

A full node that only copies the genesis gets its own node key. It does not
use the genesis validator's `priv_validator_key.json`. Mainnet does not
publish state-sync snapshots; new nodes sync from genesis.

## Genesis validator

The gentx in the committed genesis creates the first validator at chain
start. Its Ed25519 key, node key, and ML-DSA-87 key were created with that
gentx and are kept outside this repository.

On the machine that signs for this validator, the home directory must already
contain all three of these files before the first `evmd start`:

- `config/priv_validator_key.json` (Ed25519 consensus key)
- `config/node_key.json` (node ID `25c9967d869feb5340d85c01cfa16ff1d1591632`)
- `config/mldsa_priv_validator_key.json` (ML-DSA-87 vote-extension key)

`evmd start` auto-generates `config/mldsa_priv_validator_key.json` only
when this consensus address has no on-chain or genesis pqauth binding
(a fresh node). This genesis validator already has a binding, so a
missing or mismatched key file is a hard startup error and no new file
is written. Copy the original file into place first, mode `0600`. Back
up the three key files and the account keyring. Replacing
`priv_validator_key.json` on a synced home also requires deleting
`data/priv_validator_state.json`, which is a separate recovery step and
is not part of a normal start.

Set `external_address` on this node to the public `p2p.qomx.io:26656` that peers
should dial. The signed gentx memo stays `0.0.0.0`.

## Become a validator

Follow [docs/validator-join.md](validator-join.md). It covers the build,
node setup, key security, and the single transaction that creates the
validator and registers its ML-DSA-87 key. From block 34000, a validator
that would enter the active set without a registered ML-DSA-87 key is
jailed in the same block, so do not send `create-validator` on its own.

## Account-level ML-DSA-87 (hybrid accounts)

An account can register its own ML-DSA-87 key with
`evmd tx pqauth register-mldsa-key`. After that, every Cosmos transaction
from the account needs both signatures (`--generate-only`, then
`evmd tx pqauth sign`, then `evmd tx broadcast`), and EVM transactions from
it go through only while its EVM window is open
(`evmd tx pqauth open-evm-window`, 1000 blocks). Registration is permanent
and the key file cannot be recovered if lost. See
[validator-join.md, section 13](validator-join.md#13-post-quantum-registration-for-the-operator-account-optional)
for the commands.

```bash
evmd query pqauth evm-window "$ACCOUNT" --home "$HOME_DIR"
evmd query pqauth mldsa-pub-key "$ACCOUNT" --home "$HOME_DIR"
evmd query pqauth params --home "$HOME_DIR"
```

`restrict_cosmos_precompiles` is false. While an account's EVM window is
open, that account's secp256k1 key can call the bank, staking, and gov
precompiles.

The CLI cannot attach a second ML-DSA signature for a granter. A registered
granter cannot complete a feegrant or authz spend from this CLI. Registration
also deletes authz grants and fee allowances where the account is the granter.
GenericAuthorization is rejected for a registered account.

## Broadcasting and gas

`--gas auto` multiplies the simulated gas by `--gas-adjustment` (default
1.1) to set the transaction gas limit. Setting `--gas` to a number ignores
the adjustment. `--dry-run --output json` prints `{"gas_estimate":N}`. A
local key name or a bech32 address may be used with `--dry-run`.

`--broadcast-mode sync` (the default) returns when the node accepts the
transaction into the mempool. The reply has `height` 0, `gas_wanted` 0,
and `gas_used` 0 even when CheckTx succeeded (`code` 0). That is not
inclusion. Query the result with:

```bash
evmd q tx <hash> --home "$HOME_DIR"
```

Repeat until the transaction is in a block. The included result has the
real height, `gas_wanted`, and `gas_used`.

Cosmos transactions charge the fee declared at submission (`gas` ×
`--gas-prices`, or the `--fees` amount). Unused gas is not refunded. EVM
transactions refund unused gas. If the Cosmos gas limit is too low, the
transaction fails with out-of-gas, still pays the full declared fee, and
still consumes the account sequence. Accurate `--gas auto` estimates
matter because a slightly short limit can cost a full fee with no state
change.

## Gas price

Set every Cosmos transaction to `--gas-prices 47619047619048aqomx` or higher.
Below the floor, CheckTx rejects the transaction.

For EVM transactions, set the gas price to at least `47619047619048` wei
(hex `0x2b4f2c6af9e8`). The JSON-RPC server can accept a transaction priced
under the base fee. The block builder skips it, and that nonce stays occupied
until a replacement priced at least about 10% higher is included.
