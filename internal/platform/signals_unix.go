//go:build unix

package platform

import (
	"os"
	"syscall"
)

// ShutdownSignals are the signals that stop the daemon cleanly.
func ShutdownSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }
