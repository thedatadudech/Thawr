// Package tests holds integration tests that boot a real Thawr server and
// clients inside Linux network namespaces and verify encrypted
// connectivity end to end. They are built only with the "integration"
// tag and require CAP_NET_ADMIN.
//
// Run with: make integration
package tests
