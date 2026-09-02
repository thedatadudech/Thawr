# ADR 0002: SQLite via modernc.org/sqlite is the only store

Status: Accepted
Date: 2026-09-02

## Context

The server persists peers, users, enrollment tokens and a generation
counter. Volume is tiny (thousands of rows at most), writes are rare,
and the single most important operational property is that the whole
server state lives in one directory that can be backed up with `cp`.
ADR 0001 forbids CGO, which excludes `mattn/go-sqlite3`.

## Decision

`modernc.org/sqlite` (pure Go transpilation of SQLite, BSD-3-Clause),
WAL journal mode, one database file `thawr.db` in `data_dir`. Migrations
are numbered SQL files embedded in `internal/store` and applied in a
transaction at startup. No other database backend, no ORM.

## Consequences

- Zero external services. Backup is a file copy while the server is
  stopped, or `VACUUM INTO` while running (exposed as
  `thawr admin backup` in phase 2).
- One server process per database; horizontal scaling of the control
  plane is out of scope, which matches the target users.
- `modernc.org/sqlite` is slower than the C library. At Thawr's write
  rate this is irrelevant.
- The `store` package exposes small interfaces, so a future PostgreSQL
  backend would be possible, but it is explicitly not planned.
