package platform

import "path/filepath"

// resolve uses the per-user Local and Roaming application data folders.
// Both are private to the user by default ACL; the runtime directory holds
// only a fallback token file, since the daemon listens on a named pipe.
func resolve(env Env) (Paths, error) {
	local, roaming := env.Getenv("LOCALAPPDATA"), env.Getenv("APPDATA")
	if !filepath.IsAbs(local) || !filepath.IsAbs(roaming) {
		return Paths{}, ErrNoBaseDir
	}
	return Paths{
		Data:    filepath.Join(local, "crewbo"),
		Config:  filepath.Join(roaming, "crewbo"),
		Runtime: filepath.Join(local, "crewbo", "run"),
	}, nil
}
