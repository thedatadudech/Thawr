// Package dns serves the thawr zone: <name>.thawr answers with a peer's
// overlay address, reverse lookups answer with the name, and nothing
// else is known. The data comes from an injected Source: on a client
// the netmap, on the server the peer registry filtered by the policy's
// visibility from the asking address. The hub resolver additionally
// forwards queries outside the zone to the server host's upstreams so
// phones, whose WireGuard app sends every query through the tunnel,
// keep working.
//
// Registrars tell the operating system to send .thawr queries to the
// resolver (systemd-resolved, a hosts-file block, a macOS resolver
// file, a Windows NRPT rule) through an injected command runner and
// root directory, so unit tests never touch a live system.
//
// Spec: docs/specs/010-dns-names.md.
package dns
