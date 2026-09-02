// Package relay implements the DERP-style packet relay built into the
// Thawr server and the client-side proxy that uses it.
//
// The server side accepts authenticated connections over an HTTP Upgrade
// on the HTTPS listener and forwards opaque WireGuard packets between
// peers that are mutually visible in the current policy. The client side
// maintains one relay connection and, per relayed peer, a local UDP
// socket that the WireGuard device uses as the peer endpoint.
//
// The relay never holds session keys and never inspects payloads.
//
// Spec: docs/specs/005-relay-fallback.md.
package relay
