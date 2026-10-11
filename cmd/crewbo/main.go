// Command crewbo is the single Crewbo binary. It only wires build
// information into internal/cli; all behaviour lives in internal packages.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/crewbo/crewbo/internal/cli"
	"github.com/crewbo/crewbo/internal/platform"
)

// Set at link time by GoReleaser: -X main.version=... -X main.commit=...
var (
	version = "dev"
	commit  = "none"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), platform.ShutdownSignals()...)
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.BuildInfo{Version: version, Commit: commit})
	stop()
	os.Exit(code)
}
