# Security policy

**Do not open public issues, pull requests, or discussions for security
vulnerabilities.**

## Reporting a vulnerability in QOM

Report privately through GitHub: open the repository's **Security** tab
and choose **Report a vulnerability**
(<https://github.com/qom-chain/qom/security/advisories/new>).

Please include:

- the affected component (for example `x/pqauth`, the ante handlers, the
  vote-extension code, JSON-RPC, or a precompile),
- the release or commit you tested,
- steps to reproduce, and the impact you expect.

We will acknowledge your report and keep you informed while we work on a
fix. Please give us reasonable time to fix
the issue and upgrade the network before any public disclosure.

## Scope

- The latest release tag and the `main` branch of this repository.
- The QOM mainnet (`qom_33303-1`) protocol rules implemented here.

Testing must not disrupt QOM mainnet, its validators, or its users. Use a
local devnet.

## Upstream components

QOM is built on Cosmos EVM, Cosmos SDK, CometBFT, and go-ethereum. If a
vulnerability is in unmodified upstream code, also report it to that
project through its own security policy.
