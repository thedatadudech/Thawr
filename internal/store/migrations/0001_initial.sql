-- Schema version 1: metadata, users, peers, enrollment tokens.
-- See docs/ARCHITECTURE.md §3 and §6.

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    role          TEXT NOT NULL CHECK (role IN ('admin', 'member')),
    password_hash TEXT NOT NULL,
    disabled      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL
);

CREATE TABLE peers (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    kind             TEXT NOT NULL CHECK (kind IN ('human', 'server', 'agent')),
    mode             TEXT NOT NULL CHECK (mode IN ('agent', 'static')),
    owner_id         TEXT REFERENCES users(id) ON DELETE SET NULL,
    tags             TEXT NOT NULL DEFAULT '[]',
    public_key       TEXT NOT NULL UNIQUE,
    ipv4             TEXT NOT NULL UNIQUE,
    ipv6             TEXT,
    node_secret_hash TEXT,
    created_at       TEXT NOT NULL,
    last_seen_at     TEXT,
    expires_at       TEXT
);
CREATE INDEX peers_owner_id ON peers(owner_id);

CREATE TABLE enrollment_tokens (
    id              TEXT PRIMARY KEY,
    secret_hash     TEXT NOT NULL UNIQUE,
    owner_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL CHECK (kind IN ('human', 'server', 'agent')),
    tags            TEXT NOT NULL DEFAULT '[]',
    peer_name       TEXT,
    created_by      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    used_at         TEXT,
    used_by_peer_id TEXT REFERENCES peers(id) ON DELETE SET NULL
);
CREATE INDEX enrollment_tokens_expires_at ON enrollment_tokens(expires_at);

INSERT INTO meta (key, value) VALUES ('netmap_generation', '0');
