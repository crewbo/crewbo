package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Binding ties a repository on this machine to a room on a sync server.
// The daemon shares claims in that repository with that room.
type Binding struct {
	Repo   string `json:"repo"` // repository root, as hooks.RepoRoot returns it
	Server string `json:"server"`
	RoomID string `json:"room_id"`
	Name   string `json:"name"`
}

// BindingsPath is where bindings are saved inside the config directory.
func BindingsPath(configDir string) string { return filepath.Join(configDir, "rooms.json") }

// LoadBindings reads the saved bindings; none saved is an empty list.
func LoadBindings(path string) ([]Binding, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path is the user's own config dir
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Binding
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("auth: %s is damaged: %w", path, err)
	}
	return out, nil
}

// SaveBindings replaces the saved bindings atomically.
func SaveBindings(path string, bs []Binding) error {
	if bs == nil {
		bs = []Binding{}
	}
	b, err := json.MarshalIndent(bs, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(path, append(b, '\n'))
}

// FindBinding returns the binding for repo, if any.
func FindBinding(bs []Binding, repo string) (Binding, bool) {
	for _, b := range bs {
		if b.Repo == repo {
			return b, true
		}
	}
	return Binding{}, false
}
