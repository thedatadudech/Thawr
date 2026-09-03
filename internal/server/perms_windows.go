package server

import (
	"net"
	"os"
)

// checkDirPerms is a no-op on Windows; ACLs are outside v1's scope.
func checkDirPerms(string, os.FileInfo) error { return nil }

// secureSocket is a no-op on Windows; the socket inherits the
// directory's ACL.
func secureSocket(string, net.Listener) error { return nil }
