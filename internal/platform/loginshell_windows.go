package platform

// LoginShell reports no login shell on Windows: agents run hook commands
// there without a Unix login shell, so there are no startup files to
// check.
func LoginShell() (string, bool) { return "", false }
