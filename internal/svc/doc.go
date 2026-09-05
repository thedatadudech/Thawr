// Package svc registers the thawr binary as a system service: a systemd
// unit on Linux, a launchd daemon on macOS, a Windows service elsewhere.
// It writes the unit files itself and drives the platform's service
// manager through an injected command runner, so nothing here is
// tested against a live init system.
//
// Spec: docs/specs/009-release-and-install.md.
package svc
