package platform

import "os"

// ShutdownSignals are the signals that stop the daemon cleanly. On Windows
// os.Interrupt covers Ctrl+C and Ctrl+Break; there is no SIGTERM.
func ShutdownSignals() []os.Signal { return []os.Signal{os.Interrupt} }
