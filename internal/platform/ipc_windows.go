package platform

// restrictSocket is a no-op on Windows: the socket file inherits the
// user-only ACL of %LOCALAPPDATA%\crewbo\run (U-29 tracks verifying that
// this blocks other users' connections).
func restrictSocket(string) error { return nil }
