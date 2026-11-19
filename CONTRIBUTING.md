# Contributing to QOM

We appreciate community contributions! To ensure a smooth process, please follow these guidelines.

Report security problems privately as described in [SECURITY.md](SECURITY.md),
not in issues or pull requests. Changes to consensus code (anything that can
change state, gas, or block validity) need a coordinated release with an
activation height; see [docs/validator-join.md](docs/validator-join.md#14-day-to-day-operation-and-upgrades).

---

## Requirements for Pull Requests

1. **Issue Link Required**
   - All pull requests must be linked to an existing [GitHub Issue](https://github.com/qom-chain/qom/issues).
   - PRs without a corresponding issue **will not be reviewed**.
   - Issues should include:
      - **Reproducibility**: If the issue is a bug, describe steps to reproduce the problem clearly.
      - **Context & Explanation**: Provide enough background so others can understand the problem or motivation.
      - **Potential Impact**: Explain how this affects the project or user experience, referencing:
         - severity
         - user scope
         - downstream effects

2. **Signed Commits**
   - All commits must be [signed](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits).
   - Unsigned commits will be rejected.

3. **Conventional Commit PR Titles**
    - PR titles should use the [conventional commit](https://www.conventionalcommits.org/en/v1.0.0/#summary) format.

4. **Documentation Contributions**
   - We only accept documentation PRs that make **substantial or impactful changes**.
   - Minor typo or style-only fixes in documentation will not be accepted.

---

## Getting Started

- Fork the repo and create your branch from `main`.
- Run tests and linters before submitting.
- Make sure your code adheres to the project's style and conventions.

---

Thank you for helping improve QOM!
