// Package platform holds every OS-specific behaviour Crewbo needs, behind
// small types, so the rest of the code base stays OS-independent
// (docs/architecture/cross-platform.md).
package platform

import (
	"errors"
	"os"
)

// Env is the process environment the path rules read. It is injected so the
// rules can be tested without touching the real environment.
type Env struct {
	Getenv func(string) string
	Home   string // user home directory
	UID    int    // numeric user id on Unix; unused on Windows
}

// DefaultEnv returns the environment of the running process.
func DefaultEnv() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	return Env{Getenv: os.Getenv, Home: home, UID: os.Getuid()}, nil
}

// Paths are the directories Crewbo uses on this machine.
type Paths struct {
	Data    string // database, logs, key file fallback
	Config  string // user configuration
	Runtime string // daemon socket or pipe token; owner-only
}

// ErrNoBaseDir means the environment lacks the variable or home directory a
// path rule depends on.
var ErrNoBaseDir = errors.New("platform: base directory not available")

// Resolve applies this OS's path rules to env.
func Resolve(env Env) (Paths, error) {
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	return resolve(env)
}
