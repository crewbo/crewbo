package claims

import "strings"

// Overlaps reports whether two claim patterns could match a common path.
//
// The check is conservative, as specified in docs/architecture/data-model.md:
// it never misses a real overlap but may report false ones. Literal patterns
// overlap when equal; otherwise patterns overlap when the literal segments
// before the first wildcard of one are a prefix of the other's. Comparison
// folds case on every OS, because a claim made on Linux must still protect
// the same file on a case-insensitive Windows or macOS checkout.
func Overlaps(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	ap, bp := literalPrefix(as), literalPrefix(bs)
	if ap == len(as) && bp == len(bs) {
		return len(as) == len(bs) && segmentsEqual(as, bs, len(as))
	}
	n := min(ap, bp)
	if !segmentsEqual(as, bs, n) {
		return false
	}
	// One side is literal: it can only match the other's wildcard part if it
	// is at least as deep as the other's literal prefix.
	if ap == len(as) {
		return len(as) > bp || hasDoubleStar(bs[bp:])
	}
	if bp == len(bs) {
		return len(bs) > ap || hasDoubleStar(as[ap:])
	}
	return true
}

// IsGlob reports whether a pattern contains wildcard syntax.
func IsGlob(pattern string) bool { return strings.ContainsAny(pattern, "*?[") }

func literalPrefix(segs []string) int {
	for i, s := range segs {
		if IsGlob(s) {
			return i
		}
	}
	return len(segs)
}

func segmentsEqual(a, b []string, n int) bool {
	for i := range n {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}

func hasDoubleStar(segs []string) bool {
	for _, s := range segs {
		if strings.Contains(s, "**") {
			return true
		}
	}
	return false
}
