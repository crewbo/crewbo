package platform

// AgentShellIsPowerShell reports whether coding agents on this OS run shell
// commands through PowerShell. Phase 0 on Windows: Codex 0.154.0 reported
// its shell tool as "Bash" but ran PowerShell (`Add-Content -Path ...`).
func AgentShellIsPowerShell() bool { return true }
