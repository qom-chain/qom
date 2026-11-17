# QOM Chain

**QOM** is a hybrid post-quantum EVM + Cosmos SDK chain.

- **EVM Chain ID**: 33303
- **Cosmos Chain ID**: qom_33303-1
- **Native Token**: QOMX (18 decimals)
- **Total Supply**: 200,000,000 QOMX

QOM combines full Ethereum compatibility (MetaMask, Solidity, JSON-RPC) with optional ML-DSA-87 (Dilithium) post-quantum signatures for accounts and consensus vote extensions.

## Key Features

- Full EVM compatibility (MetaMask, Hyperlane, Solidity)
- Hybrid quantum protection (ethsecp256k1 + optional ML-DSA-87)
- Dilithium precompile at `0x0401`
- Zero inflation
- Cosmos SDK + CometBFT consensus with vote extensions

## Quick Links

- [Mainnet parameters and genesis hash](docs/mainnet.md#chain-parameters)
- [Add QOM to MetaMask](docs/mainnet.md#add-qom-to-metamask)
- [Run a full node or public RPC](docs/mainnet.md#run-a-full-node)
- [Become a validator](docs/mainnet.md#become-a-validator)

## Getting Started

See [docs/mainnet.md](docs/mainnet.md) for the full genesis parameters, node setup, and validator instructions.

## License

Apache 2.0
