# QOM

**QOM** (Quantum Operating Machine) is a hybrid post-quantum network that
combines the Ethereum Virtual Machine with Cosmos SDK and CometBFT. Its
native token is **QOMX**.

QOM is hybrid by design. Accounts use standard secp256k1 keys, so MetaMask,
Solidity, and Ethereum JSON-RPC tooling work unchanged. Accounts and
validators can add an ML-DSA-87 (FIPS 204, Dilithium) key on top, and every
validator signs its consensus vote extensions with ML-DSA-87. The network
is built to move to full post-quantum signing as wallets support it.

| | |
| --- | --- |
| Cosmos chain ID | `qom_33303-1` |
| EVM chain ID | `33303` |
| Token | QOMX, 18 decimals (base denom `aqomx`) |
| Total supply | 200,000,000 QOMX, zero inflation |
| Public EVM JSON-RPC | `https://rpc.qomx.io` |
| Explorer | `https://scan.qomx.io` |

## Features

- Full EVM compatibility: MetaMask, Solidity, Hardhat, Foundry, viem, ethers
- Hybrid post-quantum accounts: secp256k1 plus optional ML-DSA-87
- ML-DSA-87 signed vote extensions from every validator
- ML-DSA (Dilithium) verification precompile at `0x0000000000000000000000000000000000000401`
- Cosmos SDK staking, governance, and distribution, also reachable from the EVM through precompiles

## Documentation

- [Run a validator](docs/validator-join.md): build, node setup, key security, joining, operations
- [Mainnet parameters, genesis, and full node setup](docs/mainnet.md)
- [Add QOM to MetaMask](docs/mainnet.md#add-qom-to-metamask)
- [Round-aware vote extensions](docs/voteext-round-aware-cutover.md)
- [Cosmos SDK client fix](docs/cosmos-sdk-clifix.md)
- [Changelog](CHANGELOG.md)
- [Upstream Cosmos EVM material](docs/upstream/README.md) (not QOM-specific, including the upstream audit PDF)

## Build

The mainnet binary is `evmd`, built from this repository with patched
CometBFT and Cosmos SDK trees. Follow
[docs/validator-join.md, section 3](docs/validator-join.md#3-build-the-release-binary)
for the exact, reproducible recipe and the release SHA-256. A plain
`make build` or `go build` against stock CometBFT does not produce a
usable mainnet binary.

## Security

Please report vulnerabilities privately. See [SECURITY.md](SECURITY.md).

## License

Apache 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE). QOM is built on
[Cosmos EVM](https://github.com/cosmos/evm), Cosmos SDK, and CometBFT.
