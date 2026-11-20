# Upstream Cosmos EVM material

QOM is built on [Cosmos EVM](https://github.com/cosmos/evm). The files in
this directory come from upstream Cosmos EVM and are kept for reference.
They are **not QOM-specific**: they describe the upstream example chain
and upstream library versions, not QOM mainnet (`qom_33303-1`).

For running a QOM node or validator, use
[docs/validator-join.md](../validator-join.md) and
[docs/mainnet.md](../mainnet.md).

| File | What it is |
| --- | --- |
| [CHANGELOG.md](CHANGELOG.md) | Upstream Cosmos EVM changelog. QOM releases are in the top-level [CHANGELOG.md](../../CHANGELOG.md). |
| [AGENTS.md](AGENTS.md) | Upstream contributor guidance for the Cosmos EVM repository. |
| [migrations/](migrations/) | Upstream Cosmos EVM library migration guides (v0.3 to v0.8). QOM node operators do not need them. |

Upstream files that stay in their original place:

- [`docs/audits/sherlock_2025_07_28_final.pdf`](../audits/sherlock_2025_07_28_final.pdf)
  is an upstream audit of Cosmos EVM. It is not an audit of QOM.
- [`local_node.sh`](../../local_node.sh) and
  [`docker-compose.yml`](../../docker-compose.yml) start the upstream
  example chain for local development and tests. The test scripts and
  `Makefile` use them. They are not a QOM mainnet setup.
