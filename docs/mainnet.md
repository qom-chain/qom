# QOM Mainnet

## Chain Parameters

| Parameter                  | Value                          |
|---------------------------|--------------------------------|
| EVM Chain ID              | 33303                          |
| Cosmos Chain ID           | qom_33303-1                    |
| Native Token              | QOMX                           |
| Decimals                  | 18                             |
| Base Denom                | aqomx                          |
| Total Supply              | 200,000,000 QOMX               |
| Inflation                 | 0                              |
| Block Gas Limit           | 30,000,000                     |
| Min Gas Price / Base Fee  | 47619047619048 aqomx           |
| EVM Window Duration       | 1000 blocks                    |
| Vote Extensions           | Enabled at height 1            |
| Max Validators            | 100                            |
| Unbonding Time            | 14 days                        |
| Dilithium Precompile      | 0x0000000000000000000000000000000000000401 |

## Genesis Allocation

- 199,000,000 QOMX → CEX hot wallet: `0xF9C3b5e63416ed9e4dB661B10D66e1bAeC6b6c83`
- 1,000,000 QOMX → Initial validator (self-delegated via gentx)

## Add QOM to MetaMask

- Network Name: QOM
- RPC URL: (public RPC endpoint)
- Chain ID: 33303
- Currency Symbol: QOMX
- Block Explorer: https://scan.qomx.io

## Run a Full Node / Public RPC

```bash
# Initialize
evmd init qom-node --chain-id qom_33303-1 --home ~/.qom

# Download genesis (will be provided after launch)
# wget -O ~/.qom/config/genesis.json https://.../genesis.json

# Start
evmd start --home ~/.qom --chain-id qom_33303-1 --json-rpc.enable --json-rpc.api eth,net,web3
```

## Become a Validator

1. Run a full node and sync to the tip.
2. Create a key and fund it with QOMX.
3. Register your ML-DSA key (optional but recommended for hybrid protection).
4. Create and broadcast a `create-validator` transaction with self-delegation.

Detailed gentx and validator setup instructions will be added after mainnet genesis is finalized.

## Quantum Features

- Accounts remain ethsecp256k1 by default (MetaMask compatible).
- Optional registration of an ML-DSA-87 public key via `MsgRegisterMLDSAKey`.
- After registration, Cosmos transactions require a dual signature.
- EVM spends require an open authorization window (opened with a hybrid-signed message).
- Consensus uses Ed25519 + ML-DSA-87 vote extensions.

No private keys or personal information are included in this repository.
