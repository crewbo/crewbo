package claims

import "testing"

func TestOverlaps(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"src/a.go", "src/a.go", true},
		{"src/a.go", "SRC/A.go", true}, // case folded on every OS
		{"src/a.go", "src/b.go", false},
		{"src", "src/a.go", false}, // literal paths name files, not directories
		{"src/a.go", "src/*.go", true},
		{"src/a.go", "lib/*.go", false},
		{"src", "src/*.go", false},
		{"src", "src/**", true},
		{"src/deep/a.go", "src/**", true},
		{"*.go", "src/a.go", true}, // conservative false positive
		{"src/*.go", "src/*_test.go", true},
		{"src/*.go", "lib/*.go", false},
		{"src/*", "src/a/*.go", true}, // conservative false positive
		{"**", "anything/at/all", true},
		{"docs/[ab].md", "docs/a.md", true},
		{"docs/[ab].md", "web/a.md", false},
	}
	for _, tt := range tests {
		if got := Overlaps(tt.a, tt.b); got != tt.want {
			t.Errorf("Overlaps(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestOverlapsSymmetric(t *testing.T) {
	patterns := []string{
		"a.go", "src/a.go", "src/b.go", "src", "src/*.go", "src/**", "*.go", "**",
		"lib/*.go", "src/a/*.go", "SRC/A.GO", "docs/[ab].md", "docs/a.md", "src/*/x.go",
	}
	for _, a := range patterns {
		if !Overlaps(a, a) {
			t.Errorf("Overlaps(%q, %q) = false, want true", a, a)
		}
		for _, b := range patterns {
			if Overlaps(a, b) != Overlaps(b, a) {
				t.Errorf("Overlaps not symmetric for %q, %q", a, b)
			}
		}
	}
}
