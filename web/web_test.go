package web

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrandCopies fails when a brand file copied here differs from its
// source in docs/brand (CLAUDE.md: never invent brand values).
func TestBrandCopies(t *testing.T) {
	brand := filepath.Join("..", "docs", "brand")
	copies := map[string]string{
		"tokens.css":       filepath.Join(brand, "tokens", "tokens.css"),
		"lockup-light.svg": filepath.Join(brand, "assets", "svg", "logo", "lockup-light.svg"),
	}
	fonts, err := fs.Glob(Files, "fonts/*.woff2")
	if err != nil || len(fonts) != 5 {
		t.Fatalf("fonts: %v %v", fonts, err)
	}
	for _, f := range fonts {
		copies[f] = filepath.Join(brand, "assets", "fonts", strings.TrimPrefix(f, "fonts/"))
	}
	for name, src := range copies {
		got, err := Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("web/%s differs from %s; copy it again", name, src)
		}
	}
}
