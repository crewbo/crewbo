package platform

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"
)

// LoginShell returns the account's login shell, which Codex runs hook
// commands with: `dscl . -read /Users/<name> UserShell` (macOS keeps
// accounts in Directory Services, not /etc/passwd), else $SHELL.
// UNVERIFIED on macOS: see docs/engineering/unknowns.md U-35.
func LoginShell() (string, bool) {
	if u, err := user.Current(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "dscl", ".", "-read", "/Users/"+u.Username, "UserShell").Output() //nolint:gosec // a fixed tool; the name is this account's own, from the system
		if err == nil {
			if sh, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "UserShell:"); ok && strings.TrimSpace(sh) != "" {
				return strings.TrimSpace(sh), true
			}
		}
	}
	sh := os.Getenv("SHELL")
	return sh, sh != ""
}
