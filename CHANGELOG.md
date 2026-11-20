# QOM changelog

Releases of QOM, the network (`qom_33303-1`). QOMX is its token. Build
steps for every release are in
[docs/validator-join.md, section 3](docs/validator-join.md#3-build-the-release-binary).
The upstream Cosmos EVM changelog is kept in
[docs/upstream/CHANGELOG.md](docs/upstream/CHANGELOG.md).

## qom-v1.0.3

- Dependency updates: Cosmos SDK v0.54.5, CometBFT v0.39.4, grpc v1.85.0,
  golang.org/x/net v0.63.0.
- No state change, no gas change, no activation height. Stop the node,
  replace the binary, restart.
- Replays the whole chain from genesis.
- Reproducible build SHA-256:
  `dc9ecf84ffe647582fb0faf0c012144860cfd3e5e719fdd70fe89f1cecdf4dae`

## qom-v1.0.2

- Upstream Cosmos EVM maintenance fixes, active from block 40000.
  Below that height, behaviour is the same as v1.0.1.
- Startup log line: `upstream fixes activation height height=40000`.
- No chain parameter changes.
- Reproducible build SHA-256:
  `bed715bc4cb6d838b2a1837619cc419edbb04853da3ab9e380c0b6981f56cc66`
- Superseded by v1.0.3.

## qom-v1.0.1

- Mempool stability updates from upstream.
- No consensus, state or parameter changes.
- Reproducible build SHA-256:
  `b1d311a17600f4f0874100d771fc9fa762347df8b00aab5d556b21f57d5626eb`
- Superseded. Cannot sync past block 40000.

## qom-v1.0.0

- Mainnet code for `qom_33303-1`.
- Post-quantum hardening (pqauth v2), active from block 33000.
- Validator ML-DSA-87 key guard, active from block 34000.
- Round-aware ML-DSA-87 vote extensions.
- Reproducible build SHA-256:
  `411d48b335215bd629f31b3303b0cccb5a67373a1426cf520e8e36537f25e7e2`
- Superseded. Cannot sync past block 40000.
