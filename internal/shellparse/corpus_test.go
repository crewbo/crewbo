package shellparse

import (
	"bufio"
	"os"
	"path"
	"strings"
	"testing"
)

// TestCorpus replays the Phase 0 S3 corpus (crewbo/spikes
// s3-shellparse/corpus.tsv): "<expected target>\t<command>", "-" for a
// read-only command, "\n" escaped as `\n`. The heuristic column mirrors the
// daemon's check: an opaque command that mentions the claimed name.
func TestCorpus(t *testing.T) {
	f, err := os.Open("testdata/corpus.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var writes, parsed, heuristic, readOnly, falsePos int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		want, cmd, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		cmd = strings.ReplaceAll(cmd, `\n`, "\n")
		r := POSIX(cmd)
		hit := false
		for _, tg := range r.Targets {
			if path.Base(tg) == want {
				hit = true
			}
		}
		if want == "-" {
			readOnly++
			if len(r.Targets) > 0 || (r.Opaque && strings.Contains(cmd, "a.txt")) {
				falsePos++
				t.Logf("false positive: %q -> %+v", cmd, r)
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
			t.Logf("missed: %q -> %+v", cmd, r)
		}
	}
	t.Logf("writes %d: parser %d (%.0f%%), parser+heuristic %d (%.0f%%); read-only %d, false positives %d",
		writes, parsed, pct(parsed, writes), parsed+heuristic, pct(parsed+heuristic, writes), readOnly, falsePos)
	// Phase 0 S3 baseline: 34 and 41 of 48, no false positives.
	if parsed < 34 || parsed+heuristic < 41 || falsePos != 0 {
		t.Errorf("below the Phase 0 baseline (34 parser, 41 with heuristic, 0 false positives)")
	}
}

func pct(a, b int) float64 { return 100 * float64(a) / float64(b) }
