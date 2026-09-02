# ADR 0003: License is Apache-2.0

Status: Accepted
Date: 2026-09-02

## Context

Thawr targets organisations that evaluate licenses before adopting
software. The dependencies are MIT, BSD-3-Clause and Apache-2.0. The
project wants contributions from companies and individuals without a
contributor license agreement, and wants an explicit patent grant so
adopters are not exposed to contributor patent claims.

## Decision

Apache License 2.0 for all code and documentation in this repository.
Copied third-party code (for example the STUN codec from Tailscale)
keeps its original BSD license header and is listed in
`docs/ARCHITECTURE.md`. No CLA; contributions are accepted under the
Developer Certificate of Origin.

## Consequences

- Compatible with every dependency in use; the combined work can be
  redistributed commercially.
- Section 4 of the license requires preserving notices; a `NOTICE` file
  is added in Phase 1 listing copied BSD code.
- The repository was initialised with an MIT `LICENSE` file; Phase 1
  replaces it with the Apache-2.0 text. This ADR records the intended
  license.
- Contributors cannot attach copyleft terms; anyone wanting a GPL fork
  can make one, which is acceptable.
