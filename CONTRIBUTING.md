# Contributing to Thawr

Thawr is small on purpose: one binary, one config file, one policy
file, and a control plane someone can read in an afternoon. The rules
below keep it that way. They are short; please read them once.

## Before you start

- **Bugs and small fixes**: open an issue or a pull request directly.
- **Features**: open an issue first. Thawr follows written specs
  (`docs/specs/`), one per feature, with acceptance criteria and test
  cases. A feature that does not fit `docs/VISION.md` (in particular
  the non-goals) or contradicts an ADR in `docs/adr/` will be declined,
  however good the code is. Discussing it first saves both of us time.
- **Cryptography**: pull requests that add cryptographic code are
  declined on principle (ADR 0004). WireGuard does the tunnel; the Go
  standard library and `golang.org/x/crypto` do the rest.

## Development setup

Go 1.26 or newer, `golangci-lint`, and on Linux `iproute2` plus
`CAP_NET_ADMIN` for the integration tests. No CGO, no Node, no Docker.

```
make build        # bin/thawr
make test         # go test -race ./...
make lint         # gofmt, go vet, golangci-lint
make integration  # netns tests, Linux only, needs root
```

`docs/TESTING.md` describes what each layer of tests covers and the
manual checklists per platform.

## Rules of the code

Read `CLAUDE.md`: it is the working agreement for every session,
human or AI-assisted, and lists the fixed architecture decisions. The
ones that come up most:

1. Never log or print a private key, node secret, token secret or
   password. Log key fingerprints. Tests generate their keys.
2. No hardcoded secrets. Secrets come from environment variables or
   files with mode 0600; config values that are secrets accept `_file`
   variants.
3. Validate at every boundary and reject; never sanitize silently.
4. Wrap errors with `fmt.Errorf("...: %w", err)` and context (peer id,
   token id, path). No blanket `recover`, no `panic` outside `main`.
5. Dependencies are injected through constructors. No package-level
   mutable state, no `init()` that does work, time is injected wherever
   expiry is checked.
6. Platform code lives in `_linux.go`, `_darwin.go`, `_windows.go`
   files behind a shared interface; the rest compiles everywhere.
7. A new dependency needs a permissive license (Apache-2.0, MIT, BSD),
   pure Go, and a row in the dependency table of `docs/ARCHITECTURE.md`.
8. Exported identifiers have doc comments. TODOs are written
   `TODO(YYYY-MM-DD): text`.

## Pull requests

- One logical change per commit, semantic messages: `feat:`, `fix:`,
  `refactor:`, `docs:`, `chore:`, `test:`, with a scope when useful
  (`feat(control): one-time enrollment tokens`).
- `make test lint` must be clean before every commit. CI runs the same
  plus race tests on Linux, macOS and Windows, cross-builds, a
  reproducibility check of the release archives and `govulncheck`.
- Control-plane logic (`internal/control`, `internal/store`,
  `internal/config`, `internal/api`) comes with unit tests, table-driven
  where natural. Store tests open a SQLite file in `t.TempDir()`.
- Update the docs that the change touches: `README.md`,
  `docs/ARCHITECTURE.md`, the spec, `TASKS.md` for decisions worth
  remembering.
- Sign off every commit (`git commit -s`). Contributions are accepted
  under the [Developer Certificate of Origin](https://developercertificate.org/)
  and the project's Apache-2.0 license.

## Reporting security issues

Not in a public issue. See `SECURITY.md`.

## Code of conduct

Be kind and precise. `CODE_OF_CONDUCT.md` applies to every project
space.
