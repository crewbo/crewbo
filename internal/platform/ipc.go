package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// SocketName is the daemon's socket file inside Paths.Runtime.
const SocketName = "daemon.sock"

// maxSocketPath is the longest socket path accepted on every OS: macOS
// limits sun_path to 104 bytes including the terminating NUL (Linux and
// Windows allow 108).
const maxSocketPath = 103

// Errors returned by Listen.
var (
	ErrAlreadyRunning    = errors.New("platform: another process is listening on the daemon socket")
	ErrSocketPathTooLong = errors.New("platform: socket path is too long")
)

// SocketPath returns the daemon socket path for these directories.
func (p Paths) SocketPath() string { return filepath.Join(p.Runtime, SocketName) }

// Listen opens the daemon's local socket. It uses AF_UNIX on every OS: Go's
// net package supports it on Windows 10 build 17063 and later
// (internal/syscall/windows.SupportUnixSocket), so no named-pipe library is
// needed. Same-user access comes from the owner-only runtime directory
// (Paths.Ensure) and, on Unix, a 0600 socket file.
//
// A socket file left by a crashed daemon is replaced; a live one returns
// ErrAlreadyRunning.
func Listen(path string) (net.Listener, error) {
	if len(path) > maxSocketPath {
		return nil, fmt.Errorf("%w: %d bytes (max %d): %s", ErrSocketPathTooLong, len(path), maxSocketPath, path)
	}
	if _, err := os.Lstat(path); err == nil {
		if c, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
			_ = c.Close()
			return nil, ErrAlreadyRunning
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale socket %s: %w", path, err)
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", path, err)
	}
	if err := restrictSocket(path); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// Dial connects to the daemon socket. Callers on the hook path pass a short
// deadline in ctx so an absent daemon fails fast and the hook fails open.
func Dial(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}
