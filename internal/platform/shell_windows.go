package platform

// AgentShellIsPowerShell reports whether coding agents on this OS run shell
// commands through PowerShell. Phase 0 on Windows: Codex 0.154.0 reported
// its shell tool as "Bash" but ran PowerShell (`Add-Content -Path ...`).
func AgentShellIsPowerShell() bool { return true }

// CodexDispatchSupported reports whether dispatched tasks may run with
// Codex here. Not on Windows until U-20 is resolved: an allowed
// apply_patch failed under --sandbox workspace-write (Codex 0.154.0), and
// Crewbo never falls back to danger-full-access.
func CodexDispatchSupported() bool { return false }
