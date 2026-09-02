// Package config loads the single YAML configuration file of the Thawr
// server, applies defaults so that a file containing only public_addr is
// complete, and validates every value before anything else starts.
//
// Spec: docs/specs/001-server-bootstrap.md. Example: config/server.example.yaml.
package config
