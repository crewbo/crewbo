package cli

import (
	"context"
	"flag"
	"io"
	"log/slog"

	"github.com/crewbo/crewbo/internal/server"
)

const serveUsage = `Usage:
  crewbo serve [--addr HOST:PORT] [--tls-cert FILE --tls-key FILE] [--insecure-lan]

Runs the Crewbo sync server in the foreground. Without TLS it listens only
on a loopback address, unless --insecure-lan is given for a trusted network.
`

// defaultServeAddr is loopback so a bare `crewbo serve` never exposes plain
// HTTP to the network.
const defaultServeAddr = "127.0.0.1:7420"

func runServe(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _ = write(stderr, ExitUsage, "%s", serveUsage) }
	var cfg server.Config
	fs.StringVar(&cfg.Addr, "addr", defaultServeAddr, "address to listen on")
	fs.StringVar(&cfg.CertFile, "tls-cert", "", "TLS certificate file (PEM)")
	fs.StringVar(&cfg.KeyFile, "tls-key", "", "TLS private key file (PEM)")
	fs.BoolVar(&cfg.InsecureLAN, "insecure-lan", false, "allow plain HTTP on a non-loopback address")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 {
		return write(stderr, ExitUsage, "crewbo: unexpected argument %q\n\n%s", fs.Arg(0), serveUsage)
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	if err := server.New(log).Run(ctx, cfg, nil); err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return ExitOK
}
