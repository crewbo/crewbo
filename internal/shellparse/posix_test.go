package shellparse

import (
	"reflect"
	"strings"
	"testing"
)

func TestPOSIX(t *testing.T) {
	tests := []struct {
		cmd     string
		targets []string
		opaque  bool
	}{
		{"echo hi > a.txt", []string{"a.txt"}, false},
		{"echo hi >> a.txt 2>&1", []string{"a.txt"}, false},
		{"echo hi > /dev/null", nil, false},
		{"cat <<EOF > out.txt\nhello\nEOF", []string{"out.txt"}, false},
		{"cd src && echo x > a.go", []string{"src/a.go"}, false},
		{"cd src; cd ../web && touch b.ts", []string{"web/b.ts"}, false},
		{"(cd src && echo x > a.go); echo y > b.go", []string{"src/a.go", "b.go"}, false},
		{"cd /tmp && echo x > a", []string{"/tmp/a"}, true},
		{"F=a.txt; echo x > $F", []string{"a.txt"}, false},
		{"F=a.txt; echo x > \"${F}\"", []string{"a.txt"}, false},
		{"echo x > $UNSET", nil, true},
		{"echo x > $(pick)", nil, true},
		{"sed -i 's/a/b/' a.txt b.txt", []string{"a.txt", "b.txt"}, false},
		{"sed -i.bak -e 's/a/b/' -e 's/c/d/' a.txt", []string{"a.txt"}, false},
		{"sed -Ei 's/a/b/' a.txt", []string{"a.txt"}, false},
		{"sed 's/a/b/' a.txt", nil, false},
		{"perl -pi -e 's/a/b/' a.txt", []string{"a.txt"}, false},
		{"perl script.pl", nil, true},
		{"awk -i inplace '{print}' a.txt", []string{"a.txt"}, false},
		{"awk '{print}' a.txt", nil, true},
		{"echo x | tee -a a.txt b.txt", []string{"a.txt", "b.txt"}, false},
		{"cp -r src/b.txt dest/a.txt", []string{"dest/a.txt"}, false},
		{"cp -t dest a b", nil, true},
		{"mv old.txt new.txt", []string{"old.txt", "new.txt"}, false},
		{"rm -rf build -- -weird", []string{"build", "-weird"}, false},
		{"truncate -s 0 a.txt", []string{"a.txt"}, false},
		{"dd if=/dev/zero of=a.bin bs=1", []string{"a.bin"}, false},
		{"git checkout -- a.txt b.txt", []string{"a.txt", "b.txt"}, false},
		{"git restore --staged a.txt", []string{"a.txt"}, false},
		{"git status", nil, false},
		{"git apply fix.diff", nil, true},
		{"gofmt -l -w ./pkg", []string{"./pkg"}, false},
		{"gofmt -l ./pkg", nil, false},
		{"prettier --write src/a.ts", []string{"src/a.ts"}, false},
		{"clang-format -i a.c", []string{"a.c"}, false},
		{"if true; then echo x > a.txt; fi", []string{"a.txt"}, false},
		{"for f in 1 2; do echo $f >> log.txt; done", []string{"log.txt"}, false},
		{"python3 -c 'print(1)'", nil, true},
		{"./scripts/gen.sh", nil, true},
		{"/usr/bin/touch a", []string{"a"}, false},
		{"ls -la; go test ./...", nil, false},
		{"echo 'unterminated", nil, true},
		{"X=; echo y > $X", nil, true},
		{"rm $X", nil, true},
		{"cd $DIR && echo x > a", []string{"a"}, true},
		{"echo $X", nil, false},
	}
	for _, tt := range tests {
		got := POSIX(tt.cmd)
		if !reflect.DeepEqual(got.Targets, tt.targets) || got.Opaque != tt.opaque {
			t.Errorf("POSIX(%q) = %+v, want targets %q opaque %v", tt.cmd, got, tt.targets, tt.opaque)
		}
	}
}

func TestArgHelpers(t *testing.T) {
	if valueOf([]string{"-tdest"}, "-t") != "dest" || valueOf([]string{"-t", "d"}, "-t") != "d" || valueOf(nil, "-t") != "" {
		t.Error("valueOf")
	}
	if !hasLong([]string{"--write=true"}, "--write") || hasLong([]string{"--writer"}, "--write") {
		t.Error("hasLong")
	}
	if hasShort([]string{"--in-place", "-"}, 'i') {
		t.Error("hasShort matched a long flag or a lone dash")
	}
}

// FuzzPOSIX checks the parser never panics and only returns non-empty
// targets.
func FuzzPOSIX(f *testing.F) {
	for _, s := range []string{"echo x > a", "cd d && sed -i s/a/b/ f", "F=x; tee $F", "cat <<E >o\nE", "(cd a; rm b) | xargs"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, tg := range POSIX(s).Targets {
			if strings.TrimSpace(tg) == "" {
				t.Fatalf("empty target for %q", s)
			}
		}
	})
}
