package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crewbo/crewbo/internal/doctor"
)

func TestPrintDoctor(t *testing.T) {
	var out bytes.Buffer
	code := printDoctor(&out, []doctor.Result{
		{Name: "daemon", Level: doctor.OK, Detail: "running"},
		{Name: "room", Level: doctor.Fail, Detail: "/w is in no room", Fix: "`crewbo room join <code>`"},
		{Name: "hooks: codex", Level: doctor.Warn, Detail: "no event", Fix: "trust them"},
		{Name: "login shell", Level: doctor.Skip, Detail: "none"},
	})
	s := out.String()
	for _, want := range []string{"ok    daemon", "FAIL  room", "fix: `crewbo room join <code>`", "WARN  hooks: codex", "skip  login shell", "1 problem(s)"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if code != ExitError {
		t.Errorf("exit %d with a failure", code)
	}
	out.Reset()
	if code := printDoctor(&out, []doctor.Result{{Name: "daemon", Level: doctor.OK}, {Name: "x", Level: doctor.Warn, Fix: "f"}}); code != ExitOK || !strings.Contains(out.String(), "No problems found.") {
		t.Errorf("warnings only: %d %q", code, out.String())
	}
}

func TestDoctorRuns(t *testing.T) {
	isolate(t)
	t.Setenv("PATH", t.TempDir()) // no agents, no git: every outside check fails or skips cleanly
	var out, errOut bytes.Buffer
	code := Run(t.Context(), []string{"doctor"}, nil, &out, &errOut, BuildInfo{})
	if code != ExitError || !strings.Contains(out.String(), "FAIL  daemon") || !strings.Contains(out.String(), "FAIL  sign-in") || errOut.Len() != 0 {
		t.Errorf("doctor without a daemon or sign-in: %d\n%s\n%s", code, out.String(), errOut.String())
	}
}
