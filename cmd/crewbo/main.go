// Command crewbo is the single Crewbo binary. It only wires build
// information into internal/cli; all behaviour lives in internal packages.
package main

import (
	"os"

	"github.com/crewbo/crewbo/internal/cli"
)

// Set at link time by GoReleaser: -X main.version=... -X main.commit=...
var (
	version = "dev"
	commit  = "none"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, cli.BuildInfo{Version: version, Commit: commit}))
}
