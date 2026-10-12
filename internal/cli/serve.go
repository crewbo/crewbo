package cli

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/crewbo/crewbo/internal/server"
	"github.com/crewbo/crewbo/internal/store"
)

const serveUsage = `Usage:
  crewbo serve [--addr HOST:PORT] [--tls-cert FILE --tls-key FILE] [--insecure-lan] [--data DIR]

Runs the Crewbo sync server in the foreground. Without TLS it listens only
on a loopback address, unless --insecure-lan is given for a trusted network.
Rooms, events and sessions are kept in DIR/server.db (default: Crewbo's
data directory).
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
	dataDir := fs.String("data", "", "directory for server.db (default: Crewbo's data directory)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 {
		return write(stderr, ExitUsage, "crewbo: unexpected argument %q\n\n%s", fs.Arg(0), serveUsage)
	}
	if *dataDir == "" {
		p, err := paths()
		if err != nil {
			return write(stderr, ExitError, "crewbo: %v\n", err)
		}
		*dataDir = p.Data
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	db, err := store.Open(ctx, filepath.Join(*dataDir, "server.db"), store.Server)
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	defer func() { _ = db.Close() }()
	log := slog.New(slog.NewTextHandler(stderr, nil))
	if err := server.New(log, db.SQL()).Run(ctx, cfg, nil); err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return ExitOK
}
