package shellparse

import (
	"bufio"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
)

// TestPowerShellCorpus mirrors TestCorpus for PowerShell. Expected targets
// are file names; Windows separators are normalised before comparing.
func TestPowerShellCorpus(t *testing.T) {
	f, err := os.Open("testdata/powershell.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var writes, parsed, heuristic, falsePos int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		want, cmd, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		r := PowerShell(cmd)
		hit := false
		for _, tg := range r.Targets {
			if path.Base(strings.ReplaceAll(tg, `\`, "/")) == want {
				hit = true
			}
		}
		if want == "-" {
			if len(r.Targets) > 0 || (r.Opaque && strings.Contains(cmd, "a.txt")) {
				falsePos++
				t.Errorf("false positive: %q -> %+v", cmd, r)
			}
			continue
		}
		writes++
		switch {
		case hit:
			parsed++
		case r.Opaque && strings.Contains(cmd, want):
			heuristic++
		default:
			t.Errorf("missed: %q -> %+v", cmd, r)
		}
	}
	t.Logf("PowerShell writes %d: parser %d, parser+heuristic %d; false positives %d", writes, parsed, parsed+heuristic, falsePos)
}

func TestPowerShell(t *testing.T) {
	tests := []struct {
		cmd     string
		targets []string
		opaque  bool
	}{
		{"Add-Content -LiteralPath .\\blocked.txt -Value 'shell'", []string{`.\blocked.txt`}, false},    // Phase 0 Windows, Codex
		{"Add-Content -Path blocked.txt -Value 'shell' -Encoding utf8", []string{"blocked.txt"}, false}, // Phase 0, Claude Code
		{"Set-Content -Path 'my notes.txt' -Value x", []string{"my notes.txt"}, false},
		{"Set-Content -Path \"$dir\\a.txt\" -Value x", nil, true},
		{"Set-Content $env:TEMP x", nil, true},
		{"Copy-Item -Path b.txt -Destination a.txt -Force", []string{"a.txt"}, false},
		{"Get-Date | Out-File a.txt; Get-Date >> b.txt", []string{"a.txt", "b.txt"}, false},
		{"Write-Output x *> all.log", []string{"all.log"}, false},
		{"Get-Date 2>&1 | Out-Null", nil, false},
		{"Set-Content -Path a.txt -Value x && Remove-Item b.txt", []string{"a.txt", "b.txt"}, false},
		{"Set-Content -WhatIf -Path a.txt -Value x", []string{"a.txt"}, false},
		{"Set-Content -Path", nil, false},
		{"& .\\build.ps1", nil, true},
		{"iex 'Set-Content a.txt x'", nil, true},
		{"git checkout -- a.txt", nil, true},
		{"$x = Get-Item a.txt; Remove-Item $x", nil, true},
		{"$p = [IO.Path]::Combine('a','b')", nil, true},
		{"Set-Content `\n -Path a.txt -Value x", []string{"a.txt"}, false},
		{"cd src; Set-Content a.txt x", []string{"src/a.txt"}, false},
		{"Set-Location -Path .\\web; Remove-Item app.ts", []string{"web/app.ts"}, false},
		{"cd C:\\other; Set-Content a.txt x", []string{"C:/other/a.txt"}, true},
		{"cd $home; Set-Content a.txt x", []string{"a.txt"}, true},
		{"", nil, false},
	}
	for _, tt := range tests {
		got := PowerShell(tt.cmd)
		if !reflect.DeepEqual(got.Targets, tt.targets) || got.Opaque != tt.opaque {
			t.Errorf("PowerShell(%q) = %+v, want targets %q opaque %v", tt.cmd, got, tt.targets, tt.opaque)
		}
	}
}

func FuzzPowerShell(f *testing.F) {
	for _, s := range []string{"Set-Content -Path a -Value x", "'a''b' | Out-File c", "(1)>x", "$a = 'b'; sc $a", "\"`\"\" 2>&1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, tg := range PowerShell(s).Targets {
			if strings.TrimSpace(tg) == "" {
				t.Fatalf("empty target for %q", s)
			}
		}
	})
}
