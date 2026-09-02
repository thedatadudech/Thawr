// Package api exposes the Thawr server over the network: the gRPC
// Control service used by clients (enroll, netmap sync, endpoint reports,
// key rotation) and the REST API used by the embedded admin UI and the
// admin CLI.
//
// Handlers validate every request at the boundary and translate wire
// types to calls into package control. They contain no business logic.
// Protobuf definitions and generated code live under internal/api/proto.
//
// Design: docs/ARCHITECTURE.md §5.
package api
