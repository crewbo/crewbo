package platform

import (
	"path/filepath"
	"strconv"
)

// resolve follows Apple's convention of keeping application data and
// settings under ~/Library/Application Support. The runtime directory lives
// in the per-user $TMPDIR, which keeps socket paths under macOS's 104-byte
// limit for Unix socket addresses.
func resolve(env Env) (Paths, error) {
	if env.Home == "" {
		return Paths{}, ErrNoBaseDir
	}
	support := filepath.Join(env.Home, "Library", "Application Support", "crewbo")
	tmp := env.Getenv("TMPDIR")
	if !filepath.IsAbs(tmp) {
		tmp = "/tmp"
	}
	return Paths{
		Data:    support,
		Config:  support,
		Runtime: filepath.Join(tmp, "crewbo-"+strconv.Itoa(env.UID)),
	}, nil
}
