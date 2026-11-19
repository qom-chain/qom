# Run a QOM validator

This guide takes a new operator from an empty Linux server to an active,
signing validator on QOM mainnet (`qom_33303-1`). Every command below was
run against the current release binary. Run them in order.

QOM is the network (Quantum Operating Machine). QOMX is its token. QOM is a
hybrid post-quantum chain: accounts and wallets use standard secp256k1 keys,
so MetaMask and other Ethereum tooling work unchanged, and accounts and
validators can add an ML-DSA-87 (FIPS 204, Dilithium) key on top. The
network is built to move to full post-quantum signing as wallets support it.

Every validator signs with two keys:

- an **Ed25519 consensus key** (`priv_validator_key.json`), used by CometBFT
  for votes and proposals, and
- an **ML-DSA-87 validator key** (`mldsa_priv_validator_key.json`), which
  signs every vote extension. The other validators check that signature
  against the ML-DSA-87 public key registered on chain for your consensus
  address.

A validator must have its ML-DSA-87 key registered on chain **before** it
gets voting power. The join step in section 9 creates the validator and
registers the key in one transaction.

## Contents

1. [Network reference](#1-network-reference)
2. [Server requirements](#2-server-requirements)
3. [Build the release binary](#3-build-the-release-binary)
4. [Initialize and configure the node](#4-initialize-and-configure-the-node)
5. [Run the node with systemd](#5-run-the-node-with-systemd)
6. [Sync to the chain tip](#6-sync-to-the-chain-tip)
7. [Securing your keys](#7-securing-your-keys)
8. [Fund the operator account](#8-fund-the-operator-account)
9. [Join the validator set (one transaction)](#9-join-the-validator-set-one-transaction)
10. [Verify that you are signing](#10-verify-that-you-are-signing)
11. [Recover a validator jailed for a missing ML-DSA key](#11-recover-a-validator-jailed-for-a-missing-ml-dsa-key)
12. [Downtime, double signing, and unjailing](#12-downtime-double-signing-and-unjailing)
13. [Post-quantum registration for the operator account (optional)](#13-post-quantum-registration-for-the-operator-account-optional)
14. [Day-to-day operation and upgrades](#14-day-to-day-operation-and-upgrades)
15. [Troubleshooting](#15-troubleshooting)

## 1. Network reference

All values below were read from live mainnet.

| Item | Value |
| --- | --- |
| Cosmos chain ID (`--chain-id`) | `qom_33303-1` |
| EVM chain ID (MetaMask) | `33303` |
| Token | QOMX, 18 decimals |
| Base denom | `aqomx` (1 QOMX = 10^18 aqomx) |
| Minimum gas price | `47619047619048aqomx` |
| Block time | about 5 seconds |
| Genesis file | [`evmd/qom_33303-1-genesis.json`](../evmd/qom_33303-1-genesis.json) |
| Genesis SHA-256 | `51f77bb2646e5a768562e5827c50b2416726c9a6a6249772e9baa5d27644a1bd` |
| Persistent peer | `25c9967d869feb5340d85c01cfa16ff1d1591632@p2p.qomx.io:26656` |
| Public EVM JSON-RPC | `https://rpc.qomx.io` |
| Explorer | `https://scan.qomx.io` |
| Address prefixes | `cosmos` (accounts), `cosmosvaloper` (operators), `cosmosvalcons` (consensus) |

Staking and slashing parameters:

| Parameter | Value |
| --- | --- |
| Bond denom | `aqomx` |
| Max validators | 100 |
| Unbonding time | 14 days (`336h`) |
| Minimum commission rate | 0 |
| Voting power | 1 per whole QOMX bonded |
| `signed_blocks_window` | 100 blocks |
| `min_signed_per_window` | 0.5 |
| Downtime penalty | jailed for 10 minutes, 1% of bonded stake slashed |
| Double-sign penalty | 5% of bonded stake slashed, validator tombstoned (permanently jailed) |
| Inflation | 0 (rewards come from transaction fees) |
| Community tax | 2% |

With 5-second blocks, missing more than 50 of the last 100 blocks (about
4 minutes offline) jails you. Plan every restart and upgrade to take well
under that.

## 2. Server requirements

| | Minimum | Recommended |
| --- | --- | --- |
| CPU | 4 x86_64 cores | 8 cores |
| RAM | 8 GB | 16 GB |
| Disk | 200 GB SSD | 500 GB NVMe SSD |
| Network | 100 Mbit/s, static public IP | 1 Gbit/s |
| OS | 64-bit Linux (x86_64) | Debian 13 or Ubuntu 24.04 LTS or newer |

The node stores about 20 KB per block today, roughly 130 GB a year at
5-second blocks. Leave headroom for growth and for traffic spikes.

Ports:

| Port | Service | Exposure |
| --- | --- | --- |
| 26656/tcp | CometBFT P2P | open to the internet |
| 26657/tcp | CometBFT RPC | localhost only |
| 9090/tcp | gRPC | localhost only |
| 1317/tcp | REST API (off by default) | localhost only |
| 8545, 8546/tcp | EVM JSON-RPC and WebSocket (off by default) | localhost only on a validator |
| 26660/tcp | Prometheus metrics (off by default) | your monitoring network only |

Open only P2P to the internet. With `ufw`:

```bash
sudo ufw allow 22/tcp
sudo ufw allow 26656/tcp
sudo ufw enable
```

Do not serve public RPC from a validator. Run a separate full node for
that.

Create a dedicated, unprivileged user to run the node:

```bash
sudo adduser --disabled-password --gecos "" qom
sudo -iu qom
```

Run the rest of this guide as `qom` unless a command uses `sudo`.

## 3. Build the release binary

Every validator must run the same release. Mainnet nodes start only with
the release binary; see "Why the release binary matters" below.

Install the build tools (Debian/Ubuntu):

```bash
sudo apt-get update
sudo apt-get install -y build-essential git curl jq patch python3
```

Install Go 1.25.9. Any Go 1.21 or newer can fetch the exact toolchain by
itself when `GOTOOLCHAIN` is set. Without a system Go, install 1.25.9 from
go.dev:

```bash
curl -fsSLO https://go.dev/dl/go1.25.9.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.25.9.linux-amd64.tar.gz
export PATH=/usr/local/go/bin:$PATH
```

Set the build environment. These values are part of the release recipe:

```bash
export GOTOOLCHAIN=go1.25.9
export CGO_ENABLED=1
export GOMODCACHE=/var/tmp/qom-gomodcache
go version
# go version go1.25.9 linux/amd64
```

`GOMODCACHE` must be exactly `/var/tmp/qom-gomodcache`. One dependency
records that path in the binary, so another path builds a binary that
works the same but has a different SHA-256.

Start from an empty module cache. If you have built before, clear it with
Go's own command (with `GOMODCACHE` still set as above):

```bash
go clean -modcache
```

Do not use a plain `rm -rf` on the cache. Go stores module files
read-only, so `rm` fails part way. If you must delete it by hand, make it
writable first: `chmod -R u+w /var/tmp/qom-gomodcache && rm -rf /var/tmp/qom-gomodcache`.

Clone the repository and check out the release:

```bash
mkdir -p ~/qom-build && cd ~/qom-build
git clone https://github.com/qom-chain/qom.git
cd qom
git checkout qom-v1.0.0
```

The release uses CometBFT v0.39.3 and Cosmos SDK v0.54.3 with two small
patches that live in this repository (`scripts/cometbft-voteext.patch` and
`scripts/cosmos-sdk-v0.54.3-clifix.patch`). This script downloads the
upstream modules through the Go module proxy, which checks them against
`go.sum`, applies the patches into `../src-voteext/`, and points
`evmd/go.mod` at them:

```bash
sh scripts/prepare-evmd-build.sh
```

Keep the `../src-voteext/` directory name. It is recorded in the binary.

Build, strip, and restore `go.mod`:

```bash
cd evmd
go build -trimpath -buildvcs=false -o build/evmd ./cmd/evmd
strip build/evmd
git checkout -- go.mod go.sum
sha256sum build/evmd
```

The SHA-256 must be:

```text
411d48b335215bd629f31b3303b0cccb5a67373a1426cf520e8e36537f25e7e2
```

This hash was reproduced on Debian 13 (gcc 14.2, GNU binutils 2.44). A
different C compiler or `strip` version can produce a binary with a
different hash. It is built from the same source, but you cannot confirm
it byte for byte. Build on Debian 13 (x86_64) to compare hashes.

The build needs several GB of temporary space. If `/tmp` is a small tmpfs,
run `export TMPDIR=$HOME/tmp && mkdir -p $TMPDIR` first.

Install the binary:

```bash
sudo install -m 0755 build/evmd /usr/local/bin/evmd
sha256sum /usr/local/bin/evmd
evmd version --long | grep -E '^(cosmos_sdk_version|go):'
# cosmos_sdk_version: v0.54.3
# go: go version go1.25.9 linux/amd64
```

`evmd version` prints an empty version string for this release. Use the
SHA-256 to identify the binary.

### Why the release binary matters

- **Vote extensions.** The patched CometBFT passes the vote round and type
  to the application, and validators sign their ML-DSA-87 vote extension
  over them. A binary built against stock CometBFT does not compile, and
  a validator set that mixes patched and stock nodes stops at the first
  round above 0.
- **Activation heights.** Protocol rule changes switch on at fixed block
  heights. This release has two: 33000 and 34000. On chain
  `qom_33303-1`, the node refuses to start if a binary's built-in heights
  differ from the release values. Never build a mainnet binary with `-X`
  linker overrides.
- **Same state machine everywhere.** Every validator must run the same
  release. A node on the wrong release computes a different state and
  stops with an app hash error.

## 4. Initialize and configure the node

```bash
export QOM_HOME=$HOME/.qom
export CHAIN_ID=qom_33303-1
export MONIKER="your-validator-name"

evmd init "$MONIKER" --chain-id "$CHAIN_ID" --home "$QOM_HOME"
```

`init` writes `config/priv_validator_key.json` (your Ed25519 consensus key)
and `config/node_key.json` (your P2P identity). Back them up in section 7.

Install the mainnet genesis and check it:

```bash
curl -fsSL https://raw.githubusercontent.com/qom-chain/qom/qom-v1.0.0/evmd/qom_33303-1-genesis.json \
  -o "$QOM_HOME/config/genesis.json"
echo "51f77bb2646e5a768562e5827c50b2416726c9a6a6249772e9baa5d27644a1bd  $QOM_HOME/config/genesis.json" | sha256sum -c -
evmd genesis validate --home "$QOM_HOME"
```

The genesis file is also in the source tree at
`evmd/qom_33303-1-genesis.json`.

Apply the mainnet settings. `init` writes generic defaults (a test EVM
chain ID, a zero gas price, the `flood` mempool), and these commands
replace them:

```bash
# app.toml
evmd config set app minimum-gas-prices 47619047619048aqomx --home "$QOM_HOME"
evmd config set app evm.evm-chain-id 33303 --home "$QOM_HOME"

# config.toml (-s skips a check that does not apply to config.toml)
evmd config set config mempool.type app -s --home "$QOM_HOME"
evmd config set config p2p.persistent_peers "25c9967d869feb5340d85c01cfa16ff1d1591632@p2p.qomx.io:26656" -s --home "$QOM_HOME"
evmd config set config p2p.external_address "YOUR_PUBLIC_IP:26656" -s --home "$QOM_HOME"
evmd config set config consensus.timeout_commit 5s -s --home "$QOM_HOME"

# client.toml, the defaults for evmd query and tx commands
evmd config set client chain-id "$CHAIN_ID" --home "$QOM_HOME"
evmd config set client keyring-backend file --home "$QOM_HOME"
```

Replace `YOUR_PUBLIC_IP` with your server's public IP or DNS name.

Check the result:

```bash
grep -E '^(minimum-gas-prices|evm-chain-id)' "$QOM_HOME/config/app.toml"
grep -E '^(type|persistent_peers|external_address|timeout_commit) ' "$QOM_HOME/config/config.toml"
```

Notes on the other defaults:

- EVM JSON-RPC (`[json-rpc] enable`) stays off. A validator does not need it.
- CometBFT RPC listens on `127.0.0.1:26657`, gRPC on `localhost:9090`.
  Keep them local.
- `pruning = "default"` keeps the last 100 states plus every 500th state
  and is fine for a validator.
- Mainnet nodes do not publish state-sync snapshots, so a new node syncs
  from genesis. That takes minutes at the current chain size.

## 5. Run the node with systemd

Create `/etc/systemd/system/qom.service`:

```bash
sudo tee /etc/systemd/system/qom.service > /dev/null <<'EOF'
[Unit]
Description=QOM node (evmd, qom_33303-1)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=qom
Group=qom
ExecStartPre=/bin/sh -c 'if pgrep -u qom -x evmd >/dev/null; then echo "refusing to start: evmd already running" >&2; exit 1; fi'
ExecStart=/usr/local/bin/evmd start --home /home/qom/.qom
Restart=on-failure
RestartSec=5
KillSignal=SIGINT
TimeoutStopSec=60
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now qom
```

`ExecStartPre` refuses to start while any other `evmd` process of the `qom`
user is running, which guards against two processes signing with one key on
this host. If you happen to run an `evmd` CLI command at the moment the
service starts, the start is refused and systemd retries after 5 seconds.

Follow the logs:

```bash
journalctl -u qom -f -o cat
```

At startup you should see these lines:

```text
INF pqauth validator key guard activation height height=34000 module=server
INF pqauth hardening v2 activation height height=33000 module=server
INF loaded hybrid validator consensus_key=ed25519 mldsa_key=/home/qom/.qom/config/mldsa_priv_validator_key.json module=server
```

On first start, a node with no ML-DSA-87 key on chain writes a new
`config/mldsa_priv_validator_key.json`. That file becomes your validator's
ML-DSA-87 key in section 9.

## 6. Sync to the chain tip

```bash
evmd status --home "$QOM_HOME" | jq '.sync_info | {latest_block_height, catching_up}'
```

Wait until `catching_up` is `false` and `latest_block_height` matches the
explorer. Check that you have a peer:

```bash
curl -s localhost:26657/net_info | jq '.result.n_peers'
```

Do not create the validator before the node is fully synced. A validator
that cannot sign gets jailed for downtime.

## 7. Securing your keys

A validator has four secrets. Losing one, or letting someone copy it, can
cost you your stake.

| Secret | Location | What it controls | If lost | If stolen |
| --- | --- | --- | --- | --- |
| Operator account key (eth_secp256k1) | keyring in `$QOM_HOME/keyring-file/`, recoverable from the 24-word mnemonic | your funds, self-delegation, commission, validator settings, unjail | recover from mnemonic | funds and validator control lost |
| Consensus key (Ed25519) | `config/priv_validator_key.json` | block signing | you need a new validator | double-sign risk, slashing, tombstoning |
| ML-DSA-87 validator key | `config/mldsa_priv_validator_key.json` | vote-extension signing | node will not start; you must register a new key (see below) | an attacker could sign extensions for you |
| Node key | `config/node_key.json` | P2P identity (node ID) | a new node ID; peers must update | impersonation of your node ID |

### 7.1 Choose a keyring backend

| Backend | Use |
| --- | --- |
| `file` | Recommended on servers. Keys are encrypted with a passphrase in `$QOM_HOME/keyring-file/`. Every signing command asks for the passphrase. |
| `os` | The OS credential store (Secret Service on Linux desktops, Keychain on macOS). Good on a workstation, usually not available on a headless server. |
| `test` | Unencrypted keys on disk. **Never use `test` on mainnet.** |

This guide uses `--keyring-backend file`. Pick a long, unique passphrase
of at least 8 characters and store it in a password manager.

### 7.2 Create the operator key

```bash
evmd keys add operator --keyring-backend file --home "$QOM_HOME"
```

The command asks for a new keyring passphrase twice, then prints the
address and a **24-word mnemonic**. The key type is `eth_secp256k1` with
coin type 60, so the mnemonic also works in MetaMask and other Ethereum
wallets (path `m/44'/60'/0'/0/0`).

Write the mnemonic on paper, or on a steel backup, and store it offline in
two separate places. Do not photograph it, paste it into chat or email, or
save it on the server. Clear the terminal scrollback after the command:

```bash
clear && printf '\033[3J'
```

To create the key without showing a mnemonic on screen (for example while
screen sharing), add `--no-backup`. Then the encrypted export in 7.4 and
the keyring files are your only backups. A mnemonic is the stronger backup.

Show the operator addresses:

```bash
evmd keys show operator --keyring-backend file --home "$QOM_HOME"
evmd keys show operator -a --keyring-backend file --home "$QOM_HOME"                  # cosmos1...
evmd keys show operator --bech val -a --keyring-backend file --home "$QOM_HOME"       # cosmosvaloper1...
```

The `0x` address of the same key is what MetaMask shows. Convert between
the two forms with:

```bash
evmd keys parse cosmos1...         # "bytes:" is the 0x address without the 0x
evmd keys parse <hex-without-0x>   # prints the cosmos1... and cosmosvaloper1... forms
```

### 7.3 Recover the operator key from the mnemonic

Type the mnemonic at the prompt, so it does not end up in your shell
history:

```bash
evmd keys add operator --recover --keyring-backend file --home "$QOM_HOME"
```

It asks for the mnemonic first, then for the keyring passphrase twice.
Check that `evmd keys show operator -a ...` prints the same `cosmos1...`
address as before.

`--source FILE` reads the mnemonic from a file instead. The file must hold
the 24 words with **no trailing newline**, or the command fails with
`Checksum incorrect`. Shred the file afterwards (`shred -u FILE`).

### 7.4 Encrypted export and import

An armored export is the private key encrypted with a passphrase you pick:

```bash
evmd keys export operator --keyring-backend file --home "$QOM_HOME" > operator.armor
```

It asks `Enter passphrase to encrypt the exported key:` first, then for
the keyring passphrase. The file begins with
`-----BEGIN TENDERMINT PRIVATE KEY-----`.

Import it on another machine:

```bash
evmd keys import operator operator.armor --keyring-backend file --home "$QOM_HOME"
```

It asks for the export passphrase, then for a new keyring passphrase twice.

`evmd keys unsafe-export-eth-key` prints the raw private key in hex for
wallets that cannot use a mnemonic. Avoid it on a server, and never leave
its output on disk.

### 7.5 Back up the node key files

```bash
ls -l "$QOM_HOME/config/priv_validator_key.json" \
      "$QOM_HOME/config/node_key.json" \
      "$QOM_HOME/config/mldsa_priv_validator_key.json"
```

All three must be owned by `qom` with mode `-rw-------` (0600). Fix
ownership and mode with:

```bash
chmod 700 "$QOM_HOME/config"
chmod 600 "$QOM_HOME"/config/{priv_validator_key.json,node_key.json,mldsa_priv_validator_key.json}
```

Make one encrypted archive and copy it off the server, for example to an
encrypted USB drive kept offline:

```bash
tar -C "$QOM_HOME" -czf - \
    config/priv_validator_key.json \
    config/node_key.json \
    config/mldsa_priv_validator_key.json \
  | gpg --symmetric --cipher-algo AES256 -o qom-validator-keys.tar.gz.gpg

# check that the archive opens and lists all three files
gpg -d qom-validator-keys.tar.gz.gpg | tar -tzvf -
```

Copy `qom-validator-keys.tar.gz.gpg` and `operator.armor` off the server
with `scp`, then delete the local copies with `shred -u`. Keep the gpg
passphrase in your password manager, separate from the archive.

Record these public values next to the backup. They let you confirm a
restored backup is the right one:

```bash
evmd comet show-validator --home "$QOM_HOME"       # Ed25519 consensus public key
evmd comet show-address --home "$QOM_HOME"         # cosmosvalcons1...
jq -r .address "$QOM_HOME/config/priv_validator_key.json"   # consensus address, hex
evmd comet show-node-id --home "$QOM_HOME"         # node ID
jq -r .pub_key "$QOM_HOME/config/mldsa_priv_validator_key.json" | base64 -d | sha256sum   # ML-DSA-87 public key fingerprint
```

### 7.6 The ML-DSA-87 validator key

- **How it is made.** On its first start, a node with no ML-DSA-87 key on
  chain for its consensus address writes
  `config/mldsa_priv_validator_key.json` (mode 0600). You can also make one
  yourself before the first start with
  `evmd tx pqauth gen-mldsa-key "$QOM_HOME/config/mldsa_priv_validator_key.json"`.
  The file is JSON with base64 `priv_key` and `pub_key` fields. The public
  key is 2592 bytes.
- **When it becomes binding.** Section 9 registers its public key on chain.
  From then on, the node checks the file against the on-chain key at every
  start. If the file is missing or does not match, `evmd start` stops with
  an error and never writes a replacement.
- **If you lose it after registration.** The node will not start. Restore
  the file from your backup. Without a backup, the only fix is a new key,
  registered with `register-validator-mldsa-key --key-file NEW
  --old-key-file OLD`, which needs the old file. So without a backup of
  the original file, that consensus key cannot be used as a validator
  again: you would have to create a new validator with a new consensus
  key and move your stake. **Back up this file.**
- **Never replace it** on a running validator, and never copy it from
  another validator.

### 7.7 Never run two validators with the same key

If two nodes sign with the same `priv_validator_key.json`, they will
double-sign. The penalty is a 5% slash and a permanent ban (tombstoning).

- Run exactly one `evmd start` with your keys at any time. There is no
  safe "hot standby" that shares the key.
- Keep your backups offline. Do not leave a copy of `$QOM_HOME/config` on
  a second running server.
- `data/priv_validator_state.json` records the last height and round you
  signed. CometBFT refuses to sign at or below it, which protects you
  against double-signing after a restart. Never delete or reset it on a
  live validator.

**Moving a validator to a new server:**

1. Build and verify the release binary on the new server (section 3),
   then initialize and sync it as a full node (sections 4 to 6) with the
   new node's own keys.
2. On the old server: `sudo systemctl stop qom && sudo systemctl disable qom`.
   Confirm it is stopped (`pgrep -af "evmd start"` prints nothing), and
   make sure it cannot restart, for example by powering it off.
3. On the new server: `sudo systemctl stop qom`.
4. Copy `priv_validator_key.json`, `mldsa_priv_validator_key.json`, and
   optionally `node_key.json` from the old server into the new
   `$QOM_HOME/config/` (mode 0600). Copy the old
   `data/priv_validator_state.json` over the new one, so the new node
   knows the last height signed. If the old file cannot be copied, wait
   until the chain has moved at least a few blocks past the last height the
   old node signed before you start the new one.
5. Start the new node: `sudo systemctl start qom`. Check the startup lines
   in section 5 and that you are signing (section 10).

Every minute the move takes counts toward the downtime limit (about 4
minutes). Have both servers ready before you stop the old one.

### 7.8 Remote signers and hardware keys

This release does not support remote signers (tmkms, Horcrux, or
`priv_validator_laddr`). The node always signs with the local
`priv_validator_key.json` and `mldsa_priv_validator_key.json`. Hardware
wallets were not tested with this release; use the `file` keyring.

## 8. Fund the operator account

Send QOMX to the operator `cosmos1...` address, or to its `0x` form from
MetaMask. You need:

- the self-delegation you want to bond, plus
- about 100 QOMX for fees. At the minimum gas price the join transaction
  costs 33.33 QOMX (52.38 QOMX if the operator account is PQ-registered,
  section 13), an ordinary transaction such as `unjail` or
  `edit-validator` about 5 QOMX, and a hybrid-signed transaction about 20
  to 26 QOMX.

```bash
evmd q bank balances $(evmd keys show operator -a --keyring-backend file --home "$QOM_HOME") --home "$QOM_HOME"
```

## 9. Join the validator set (one transaction)

Create the validator and register its ML-DSA-87 key in **one transaction**.
From block 34000, a validator that would join the active set without a
registered ML-DSA-87 key is jailed in the same block. If you send
`create-validator` on its own, you join jailed and must recover with
section 11. The single transaction below avoids that.

If the operator account has its own ML-DSA-87 key registered (section 13),
sign step 9.4 with `evmd tx pqauth sign` instead of `evmd tx sign`. The
section shows how.

### 9.1 Set variables

```bash
export QOM_HOME=$HOME/.qom
export CHAIN_ID=qom_33303-1
export MONIKER="your-validator-name"
export GAS_PRICES=47619047619048aqomx
export OPERATOR=$(evmd keys show operator -a --keyring-backend file --home "$QOM_HOME")
export CONS_HEX=$(jq -r .address "$QOM_HOME/config/priv_validator_key.json")
echo "$OPERATOR $CONS_HEX"
```

`CONS_HEX` is your 20-byte consensus address in hex. It is the same
address as `evmd comet show-address` (`cosmosvalcons1...`); check with
`evmd keys parse $(evmd comet show-address --home "$QOM_HOME")`, whose
`bytes:` line must equal `CONS_HEX`.

### 9.2 Describe the validator

`amount` is the self-delegation in aqomx. The example bonds 10,000 QOMX.

```bash
cat > validator.json <<EOF
{
  "pubkey": $(evmd comet show-validator --home "$QOM_HOME"),
  "amount": "10000000000000000000000aqomx",
  "moniker": "$MONIKER",
  "identity": "",
  "website": "",
  "security": "",
  "details": "",
  "commission-rate": "0.10",
  "commission-max-rate": "0.20",
  "commission-max-change-rate": "0.01",
  "min-self-delegation": "1"
}
EOF
jq . validator.json
```

- `commission-rate`, `commission-max-rate`, `commission-max-change-rate`:
  the max rate and max daily change can never be changed later. The rate
  itself can change once per 24 hours, by at most the max change rate.
- `min-self-delegation` is in aqomx. If your self-delegation falls below
  it, the validator is jailed.
- `identity` is an optional Keybase ID (16 hex characters).

### 9.3 Build the combined transaction

```bash
# 1) create-validator, with the gas limit and fee for the whole transaction
evmd tx staking create-validator validator.json \
  --from "$OPERATOR" \
  --chain-id "$CHAIN_ID" \
  --gas 700000 \
  --gas-prices "$GAS_PRICES" \
  --generate-only \
  --home "$QOM_HOME" > create-validator.json

# 2) register the ML-DSA-87 key. The key file signs a proof of possession
#    locally. --from must be the key name here (it asks for the keyring passphrase).
evmd tx pqauth register-validator-mldsa-key "$CONS_HEX" \
  --key-file "$QOM_HOME/config/mldsa_priv_validator_key.json" \
  --from operator \
  --keyring-backend file \
  --chain-id "$CHAIN_ID" \
  --generate-only \
  --home "$QOM_HOME" > register-key.json

# 3) append the key registration to the create-validator transaction
jq --slurpfile r register-key.json '.body.messages += $r[0].body.messages' \
  create-validator.json > join-unsigned.json

jq '{messages: [.body.messages[]."@type"], fee: .auth_info.fee}' join-unsigned.json
```

The last command must show both messages and a 700000 gas limit:

```json
{
  "messages": [
    "/cosmos.staking.v1beta1.MsgCreateValidator",
    "/cosmos.evm.pqauth.v1.MsgRegisterValidatorMLDSAKey"
  ],
  "fee": {
    "amount": [{ "denom": "aqomx", "amount": "33333333333333600000" }],
    "gas_limit": "700000",
    "payer": "",
    "granter": ""
  }
}
```

That fee is 33.33 QOMX. The combined transaction uses about 600,000 gas,
most of it to verify the ML-DSA-87 proof of possession. Cosmos
transactions on QOM do not refund unused gas, and a transaction that runs
out of gas still pays the full fee and creates nothing, so do not lower
the limit. `--gas auto` cannot be used here, because each message is
generated separately.

### 9.4 Sign and broadcast

```bash
evmd tx sign join-unsigned.json \
  --from operator \
  --keyring-backend file \
  --chain-id "$CHAIN_ID" \
  --home "$QOM_HOME" > join-signed.json

evmd tx broadcast join-signed.json --home "$QOM_HOME"
```

`broadcast` returns `code: 0` and a `txhash` once the node accepts the
transaction. That is not inclusion yet. Wait one block, then check the
result:

```bash
evmd q tx <txhash> --home "$QOM_HOME" -o json | jq '{height, code, raw_log, gas_used, gas_wanted}'
```

`code` must be `0`.

## 10. Verify that you are signing

```bash
VALOPER=$(evmd keys show operator --bech val -a --keyring-backend file --home "$QOM_HOME")

evmd q staking validator "$VALOPER" --home "$QOM_HOME" -o json \
  | jq '.validator | {moniker: .description.moniker, status, jailed, tokens}'
```

Expect `"status": "BOND_STATUS_BONDED"` and no `"jailed": true` after the
next block.

Check that the ML-DSA-87 key on chain is the one in your file. The
registration event in your join transaction carries the public key in hex:

```bash
evmd q tx <txhash> --home "$QOM_HOME" -o json \
  | jq -j '.events[] | select(.type=="register_validator_mldsa_key") | .attributes[] | select(.key=="mldsa_pub_key") | .value' \
  | sha256sum
jq -r .pub_key "$QOM_HOME/config/mldsa_priv_validator_key.json" | base64 -d | od -An -v -tx1 | tr -d ' \n' | sha256sum
```

The two fingerprints must be equal. A restart is a second check: once the
key is on chain, `evmd start` refuses to run with a file that does not
match it.

Check your signing record:

```bash
evmd q slashing signing-info "$(evmd comet show-validator --home "$QOM_HOME")" --home "$QOM_HOME" -o json
```

`missed_blocks_counter` should stay at or near 0. The field is left out
of the output while it is 0.

Check that your node is in the active set and voting:

```bash
curl -s localhost:26657/validators | jq --arg a "$CONS_HEX" '.result.validators[] | select(.address==$a)'
```

## 11. Recover a validator jailed for a missing ML-DSA key

If a validator was created without registering its ML-DSA-87 key (for
example with a plain `create-validator`), it is jailed in the block it
would have joined. The jail event looks like this:

```bash
evmd q block-results <height> --home "$QOM_HOME" -o json \
  | jq '.finalize_block_events[] | select(.type=="jail_validator_missing_mldsa_key")'
```

Your stake is not slashed. To recover, register the key first, then
unjail. An `unjail` sent before the key is registered is accepted and
charged, but the validator is jailed again in the same block.

```bash
evmd tx pqauth register-validator-mldsa-key "$CONS_HEX" \
  --key-file "$QOM_HOME/config/mldsa_priv_validator_key.json" \
  --from operator \
  --keyring-backend file \
  --chain-id "$CHAIN_ID" \
  --gas auto \
  --gas-prices "$GAS_PRICES" \
  --home "$QOM_HOME"

# wait for the registration to be included (evmd q tx <txhash>), then:
evmd tx slashing unjail \
  --from operator \
  --keyring-backend file \
  --chain-id "$CHAIN_ID" \
  --gas auto \
  --gas-prices "$GAS_PRICES" \
  --home "$QOM_HOME"
```

The registration uses about 480,000 gas (about 25 QOMX), the unjail about
92,000 gas (about 5 QOMX). After the unjail is included, the validator
returns to the active set at the end of that block. Verify as in section
10.

## 12. Downtime, double signing, and unjailing

- **Downtime.** Each validator must sign at least 50 of every 100 blocks.
  Below that, it is jailed for 10 minutes and loses 1% of its bonded stake
  (delegators included). After 10 minutes, fix the node and unjail:

  ```bash
  evmd tx slashing unjail --from operator --keyring-backend file \
    --chain-id "$CHAIN_ID" --gas auto --gas-prices "$GAS_PRICES" --home "$QOM_HOME"
  ```

  Unjailing before the 10 minutes are up fails.

- **Double signing.** Signing two different blocks at the same height
  slashes 5% and tombstones the validator: it can never be unjailed. Its
  delegators must redelegate. See 7.7.

- **Missing ML-DSA key.** See section 11. There is no slash.

## 13. Post-quantum registration for the operator account (optional)

Section 9 registers the **validator's** ML-DSA-87 key, which every
validator needs. Your **operator account** can also add its own
ML-DSA-87 key. This is optional. It makes the account hybrid: every Cosmos
transaction from it must carry both a secp256k1 and an ML-DSA-87
signature, and EVM transactions (MetaMask) from it only go through while
an EVM window is open.

Before you register, know the costs:

- **Registration is permanent.** It can be rotated to a new ML-DSA-87 key
  (`rotate-mldsa-key`), but not removed.
- **Lose the account ML-DSA-87 key file and the account is frozen.** No
  Cosmos transaction from it can be signed again, and no EVM window can be
  opened, so its funds, its self-delegation, and its validator settings
  can no longer be changed.
- **Higher fees.** Each hybrid-signed Cosmos transaction uses about
  400,000 to 550,000 gas: about 20 to 26 QOMX at the minimum gas price.
- **Two-step signing.** `evmd tx ... --generate-only`, then
  `evmd tx pqauth sign`, then `evmd tx broadcast`.
- Registration deletes authz grants and fee allowances that the account
  has given to others, and the CLI cannot complete authz or feegrant
  spends for a registered granter.

### 13.1 Create and back up the account key

Keep the account key file apart from the validator key file:

```bash
evmd tx pqauth gen-mldsa-key "$QOM_HOME/operator-mldsa.json"
chmod 600 "$QOM_HOME/operator-mldsa.json"
```

The command prints the public key in hex. Back up the file now, encrypted
and offline, the same way as in 7.5:

```bash
gpg --symmetric --cipher-algo AES256 -o operator-mldsa.json.gpg "$QOM_HOME/operator-mldsa.json"
gpg -d operator-mldsa.json.gpg | jq -r .pub_key | base64 -d | sha256sum
jq -r .pub_key "$QOM_HOME/operator-mldsa.json" | base64 -d | sha256sum
```

The two fingerprints must match. Copy the `.gpg` file off the server.

### 13.2 Register the account key

The account is not registered yet, so this transaction uses only the
secp256k1 signature:

```bash
evmd tx pqauth register-mldsa-key \
  --key-file "$QOM_HOME/operator-mldsa.json" \
  --from operator \
  --keyring-backend file \
  --chain-id "$CHAIN_ID" \
  --gas auto \
  --gas-prices "$GAS_PRICES" \
  --home "$QOM_HOME"
```

It uses about 490,000 gas (about 26 QOMX at the minimum gas price with the
default `--gas-adjustment 1.1`).

View the registered public key:

```bash
evmd q pqauth mldsa-pub-key "$OPERATOR" --home "$QOM_HOME" -o json
```

Compare it with your file. The query returns the key in base64, the same
encoding as the file:

```bash
evmd q pqauth mldsa-pub-key "$OPERATOR" --home "$QOM_HOME" -o json | jq -r .pub_key | base64 -d | sha256sum
jq -r .pub_key "$QOM_HOME/operator-mldsa.json" | base64 -d | sha256sum
```

### 13.3 Send a hybrid-signed Cosmos transaction

Every Cosmos transaction from the registered account follows this
pattern. Example: update the validator description.

```bash
evmd tx staking edit-validator \
  --details "QOM validator" \
  --from "$OPERATOR" \
  --chain-id "$CHAIN_ID" \
  --gas auto \
  --gas-prices "$GAS_PRICES" \
  --generate-only \
  --home "$QOM_HOME" > unsigned.json

evmd tx pqauth sign unsigned.json \
  --from operator \
  --key-file "$QOM_HOME/operator-mldsa.json" \
  --keyring-backend file \
  --chain-id "$CHAIN_ID" \
  --home "$QOM_HOME" > signed.json

evmd tx broadcast signed.json --home "$QOM_HOME"
```

With `--generate-only`, `--gas auto` simulates the transaction (including
the ML-DSA signature it will carry) and prints `gas estimate: N` on stderr;
the unsigned JSON on stdout carries the limit and fee. A hybrid-signed
transaction uses about 400,000 to 550,000 gas, about 20 to 26 QOMX.

To join as a validator from a registered operator account, build
`join-unsigned.json` as in 9.3 but with `--gas 1100000` (the transaction
uses about 930,000 gas; the fee is 52.38 QOMX), then sign it with
`evmd tx pqauth sign` as above instead of `evmd tx sign`.

### 13.4 Open the EVM window

To use the account from MetaMask or any EVM tool, open its EVM window. The
window stays open for 1000 blocks (about 83 minutes) from the block that
includes the transaction. Sending it again extends the window.

```bash
evmd tx pqauth open-evm-window \
  --from "$OPERATOR" \
  --chain-id "$CHAIN_ID" \
  --gas auto \
  --gas-prices "$GAS_PRICES" \
  --generate-only \
  --home "$QOM_HOME" > open-window.json

evmd tx pqauth sign open-window.json \
  --from operator \
  --key-file "$QOM_HOME/operator-mldsa.json" \
  --keyring-backend file \
  --chain-id "$CHAIN_ID" \
  --home "$QOM_HOME" > open-window-signed.json

evmd tx broadcast open-window-signed.json --home "$QOM_HOME"

evmd q pqauth evm-window "$OPERATOR" --home "$QOM_HOME" -o json
```

The query shows `expiry_height` and `open: true` while the window is
open. Opening a window needs only the account's two keys and enough QOMX
for the fee. It does not need any token approval.

```bash
evmd q pqauth params --home "$QOM_HOME" -o json
```

## 14. Day-to-day operation and upgrades

**Monitoring.** At minimum, alert on:

- `evmd status --home "$QOM_HOME" | jq -r .sync_info.catching_up` becoming `true`, or
  `latest_block_height` not increasing for 30 seconds;
- `missed_blocks_counter` in `evmd q slashing signing-info` increasing;
- `jailed: true` in `evmd q staking validator "$VALOPER"`;
- free disk space, and the `qom` service restarting.

For Prometheus, set `instrumentation.prometheus true` in `config.toml`
(`evmd config set config instrumentation.prometheus true -s --home "$QOM_HOME"`)
and scrape port 26660 from your monitoring network only.

**Logs.** `journalctl -u qom -f -o cat`. The default level is `info`; set
`log_level` in `config.toml` to change it.

**Edit validator details or commission:**

```bash
evmd tx staking edit-validator \
  --new-moniker "$MONIKER" \
  --website "https://example.com" \
  --details "..." \
  --commission-rate 0.09 \
  --from operator --keyring-backend file \
  --chain-id "$CHAIN_ID" --gas auto --gas-prices "$GAS_PRICES" --home "$QOM_HOME"
```

**Upgrades.** QOM releases are announced with a tag, a binary SHA-256,
and, for rule changes, an activation height. Every validator must install
the new release **before** its activation height. A node still on the old
release when the new rules activate stops with an app hash error.

1. Build the new tag as in section 3 into a new directory and check its
   SHA-256 against the release notes.
2. Back up the running binary:
   `sudo cp /usr/local/bin/evmd /usr/local/bin/evmd.$(date +%Y%m%d-%H%M%S)`.
3. Swap and restart, keeping downtime short:

   ```bash
   sudo systemctl stop qom
   sudo install -m 0755 build/evmd /usr/local/bin/evmd
   sudo systemctl start qom
   ```

4. Check the startup lines (section 5), then `catching_up: false` and your
   signing record (section 10).

Never roll back to an older release after a new activation height has
passed.

## 15. Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| `panic: pqauth: this binary has hardening v2 activation height ... but qom_33303-1 requires ...` (or the same for the validator key guard) at start | Not the release binary (built with an override or from the wrong commit). Rebuild from the release tag (section 3). |
| `vote-extension round patch missing` at start | Built against stock CometBFT. Run `sh scripts/prepare-evmd-build.sh` before `go build`. |
| `undefined: state.QOMXABCIVoteExtensionRoundPatched` during the build | The same: the patched CometBFT tree is missing. Run the prepare script. |
| `ML-DSA key file ... is missing, but consensus address ... has an on-chain or genesis ML-DSA binding; refusing to auto-generate a new key` | Your validator key file is gone. Restore `config/mldsa_priv_validator_key.json` from backup (7.5), mode 0600. |
| `ML-DSA key file ... does not match the on-chain or genesis binding` | Wrong file in place, for example from another node or a new `gen-mldsa-key`. Restore the registered file. |
| `wrong Block.Header.AppHash` / `CONSENSUS FAILURE` | Wrong binary for this height. Install the current release and restart. If the node had already stored the bad state, run `evmd rollback --home "$QOM_HOME"` once with the correct binary, then start. |
| Logs show rejected vote extensions from your validator | Your on-chain ML-DSA-87 key does not match the running node's file, or the node runs a different release. Check section 10 and the binary SHA-256. |
| `insufficient fee` / `gas price ... below minimum` | Use `--gas-prices 47619047619048aqomx` or higher. |
| `out of gas` | Raise `--gas` or the `--gas-adjustment`. The full fee is charged even when out of gas. |
| Broadcast shows `code: 0` but nothing happens | Broadcast only means the node accepted the transaction. Query `evmd q tx <hash>` after a block; a failed transaction shows a non-zero `code`. |
| `account ... has a registered ML-DSA-87 key but the transaction has no ML-DSA signature for it` | The account is registered (section 13). Build with `--generate-only`, sign with `evmd tx pqauth sign`, then `evmd tx broadcast`. |
| Node has no peers | Check `persistent_peers`, that outbound TCP 26656 is allowed, and `curl -s localhost:26657/net_info`. |
| `refusing to start: evmd already running` | Another `evmd` process of the `qom` user is running (`pgrep -af evmd`). Stop it first. Never run two nodes with one key. |
| `unjail` fails with `validator still jailed` | The 10-minute downtime jail has not passed yet. |
| Validator still jailed after a successful `unjail`, block results show `jail_validator_missing_mldsa_key` | No ML-DSA-87 key registered for the consensus address. Register it, then unjail again (section 11). |

For other questions, open a GitHub issue. Report security problems
privately as described in [SECURITY.md](../SECURITY.md).
