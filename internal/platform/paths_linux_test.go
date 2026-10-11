package platform

import (
	"errors"
	"testing"
)

func envOf(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestResolveLinux(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want Paths
	}{
		{"defaults", nil, Paths{
			Data: "/home/u/.local/share/crewbo", Config: "/home/u/.config/crewbo", Runtime: "/tmp/crewbo-1000",
		}},
		{"xdg set", map[string]string{
			"XDG_DATA_HOME": "/d", "XDG_CONFIG_HOME": "/c", "XDG_RUNTIME_DIR": "/run/user/1000",
		}, Paths{Data: "/d/crewbo", Config: "/c/crewbo", Runtime: "/run/user/1000/crewbo"}},
		{"relative xdg ignored", map[string]string{
			"XDG_DATA_HOME": "d", "XDG_CONFIG_HOME": "./c", "XDG_RUNTIME_DIR": "run",
		}, Paths{Data: "/home/u/.local/share/crewbo", Config: "/home/u/.config/crewbo", Runtime: "/tmp/crewbo-1000"}},
		{"tmpdir fallback", map[string]string{"TMPDIR": "/var/tmp"}, Paths{
			Data: "/home/u/.local/share/crewbo", Config: "/home/u/.config/crewbo", Runtime: "/var/tmp/crewbo-1000",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(Env{Getenv: envOf(tt.vars), Home: "/home/u", UID: 1000})
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
	if _, err := Resolve(Env{}); !errors.Is(err, ErrNoBaseDir) {
		t.Errorf("no home: err = %v, want ErrNoBaseDir", err)
	}
}
