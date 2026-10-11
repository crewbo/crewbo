//go:build unix

package platform

// AgentShellIsPowerShell reports whether coding agents on this OS run shell
// commands through PowerShell. On Unix they use a POSIX shell.
func AgentShellIsPowerShell() bool { return false }
