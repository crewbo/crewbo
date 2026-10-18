package platform

import (
	"os"
	"strconv"
	"strings"
)

// LoginShell returns the account's login shell, which Codex runs hook
// commands with (CLAUDE.md gotchas): the user's /etc/passwd entry, else
// $SHELL (accounts from LDAP and the like are not in /etc/passwd).
func LoginShell() (string, bool) {
	if b, err := os.ReadFile("/etc/passwd"); err == nil {
		if sh, ok := passwdShell(string(b), os.Getuid()); ok {
			return sh, true
		}
	}
	sh := os.Getenv("SHELL")
	return sh, sh != ""
}

// passwdShell finds uid's shell in an /etc/passwd file
// (name:password:uid:gid:gecos:home:shell, passwd(5)).
func passwdShell(passwd string, uid int) (string, bool) {
	want := strconv.Itoa(uid)
	for _, line := range strings.Split(passwd, "\n") {
		f := strings.Split(strings.TrimSpace(line), ":")
		if len(f) == 7 && f[2] == want && f[6] != "" {
			return f[6], true
		}
	}
	return "", false
}
