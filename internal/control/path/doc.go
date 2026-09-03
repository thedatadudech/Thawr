// Package path decides how a client reaches each peer: the candidate
// order both sides compute identically from the netmap, and the
// per-peer state machine that walks it with WireGuard handshakes as
// the success signal. It is pure: the daemon feeds it clock, stats and
// traffic intent and executes the actions it returns.
package path
