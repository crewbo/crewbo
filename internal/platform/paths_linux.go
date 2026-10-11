package platform

import (
	"path/filepath"
	"strconv"
)

// resolve follows the XDG Base Directory specification: relative values in
// XDG_* variables are ignored (https://specifications.freedesktop.org/basedir-spec/latest/).
func resolve(env Env) (Paths, error) {
	if env.Home == "" {
		return Paths{}, ErrNoBaseDir
	}
	xdg := func(name, fallback string) string {
		if v := env.Getenv(name); filepath.IsAbs(v) {
			return v
		}
		return fallback
	}
	runtime := filepath.Join(xdg("TMPDIR", "/tmp"), "crewbo-"+strconv.Itoa(env.UID))
	if dir := xdg("XDG_RUNTIME_DIR", ""); dir != "" {
		runtime = filepath.Join(dir, "crewbo")
	}
	return Paths{
		Data:    filepath.Join(xdg("XDG_DATA_HOME", filepath.Join(env.Home, ".local", "share")), "crewbo"),
		Config:  filepath.Join(xdg("XDG_CONFIG_HOME", filepath.Join(env.Home, ".config")), "crewbo"),
		Runtime: runtime,
	}, nil
}
