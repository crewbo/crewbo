package platform

import (
	"errors"
	"testing"
)

func TestResolveDarwin(t *testing.T) {
	get := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	got, err := Resolve(Env{Getenv: get(map[string]string{"TMPDIR": "/var/folders/ab/xyz/T/"}), Home: "/Users/u", UID: 501})
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		Data:    "/Users/u/Library/Application Support/crewbo",
		Config:  "/Users/u/Library/Application Support/crewbo",
		Runtime: "/var/folders/ab/xyz/T/crewbo-501",
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	got, err = Resolve(Env{Getenv: get(nil), Home: "/Users/u", UID: 501})
	if err != nil || got.Runtime != "/tmp/crewbo-501" {
		t.Errorf("no TMPDIR: got %+v, %v", got, err)
	}
	if _, err := Resolve(Env{}); !errors.Is(err, ErrNoBaseDir) {
		t.Errorf("no home: err = %v, want ErrNoBaseDir", err)
	}
}
