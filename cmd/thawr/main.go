// Command thawr is the single binary for the Thawr private network. It
// runs as the control server (`thawr server`), as the node client
// (`thawr client`), and as the administration CLI (`thawr admin`).
package main

import (
	"fmt"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := newRootCmd(os.Stdout, os.Stderr).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "thawr:", err)
		os.Exit(1)
	}
}
