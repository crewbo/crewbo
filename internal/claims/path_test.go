package claims

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name, root, p, want string
		err                 error
	}{
		{"posix absolute", "/home/u/repo", "/home/u/repo/src/a.go", "src/a.go", nil},
		{"posix relative", "/home/u/repo", "src/a.go", "src/a.go", nil},
		{"dot segments", "/home/u/repo", "./src/../src/./a.go", "src/a.go", nil},
		{"root with trailing slash", "/home/u/repo/", "/home/u/repo/a.go", "a.go", nil},
		{"windows absolute backslash", `C:\work\repo`, `C:\work\repo\src\a.go`, "src/a.go", nil},
		{"windows drive letter case", `c:\work\repo`, `C:\work\repo\a.go`, "a.go", nil},
		{"windows relative backslash", `C:\work\repo`, `src\a.go`, "src/a.go", nil},
		{"windows forward slashes", "C:/work/repo", "C:/work/repo/a.go", "a.go", nil},
		{"filename case kept", "/r", "/r/Src/A.go", "Src/A.go", nil},
		{"outside by dotdot", "/home/u/repo", "../other/a.go", "", ErrOutsideRepo},
		{"outside absolute", "/home/u/repo", "/etc/passwd", "", ErrOutsideRepo},
		{"sibling prefix", "/home/u/repo", "/home/u/repo2/a.go", "", ErrOutsideRepo},
		{"the root itself", "/home/u/repo", "/home/u/repo", "", ErrOutsideRepo},
		{"other drive", `C:\repo`, `D:\repo\a.go`, "", ErrOutsideRepo},
		{"empty", "/r", "  ", "", ErrEmptyPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.root, tt.p)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidatePattern(t *testing.T) {
	for _, ok := range []string{"a.go", "src/a.go", "src/**", "src/*.go", "docs/[ab].md"} {
		if err := ValidatePattern(ok); err != nil {
			t.Errorf("ValidatePattern(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "/abs", "C:/abs", `src\a.go`, "src//a.go", "./a", "src/../a", "a/"} {
		if err := ValidatePattern(bad); !errors.Is(err, ErrBadPattern) {
			t.Errorf("ValidatePattern(%q) = %v, want ErrBadPattern", bad, err)
		}
	}
}

// patchHeader matches apply_patch file headers. The real parser belongs to
// the Codex adapter (M2); this copy only feeds fixture paths to Normalize.
var patchHeader = regexp.MustCompile(`(?m)^\*\*\* (?:Add File|Update File|Delete File|Move to): (.+)$`)

// TestNormalizeFixtures runs every file path captured from real agents in
// Phase 0 (Linux, Windows, macOS) through Normalize.
func TestNormalizeFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "test", "fixtures", "hooks", "*", "*", "*", "*ToolUse-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	checked := 0
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // f comes from a fixed glob under test/fixtures
		if err != nil {
			t.Fatal(err)
		}
		var in struct {
			Cwd       string `json:"cwd"`
			ToolName  string `json:"tool_name"`
			ToolInput struct {
				FilePath     string `json:"file_path"`
				NotebookPath string `json:"notebook_path"`
				Command      string `json:"command"`
			} `json:"tool_input"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var targets []string
		switch in.ToolName {
		case "Edit", "Write":
			targets = append(targets, in.ToolInput.FilePath)
		case "NotebookEdit":
			targets = append(targets, in.ToolInput.NotebookPath)
		case "apply_patch":
			for _, m := range patchHeader.FindAllStringSubmatch(in.ToolInput.Command, -1) {
				targets = append(targets, m[1])
			}
		}
		for _, target := range targets {
			got, err := Normalize(in.Cwd, target)
			if err != nil {
				t.Errorf("%s: Normalize(%q, %q): %v", f, in.Cwd, target, err)
				continue
			}
			if got != path.Base(got) {
				t.Errorf("%s: got %q, want a file at the repository root", f, got)
			}
			checked++
		}
	}
	if checked < 10 {
		t.Errorf("checked %d fixture paths, want at least 10", checked)
	}
}
