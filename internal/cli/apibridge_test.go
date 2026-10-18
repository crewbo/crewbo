package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
)

func TestAPIBridge(t *testing.T) {
	p := isolate(t)
	if code := Run(context.Background(), []string{"api"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, BuildInfo{}); code != ExitError {
		t.Errorf("without a daemon: exit %d", code)
	}

	repo := "/r/repo"
	set := claims.NewSet()
	if err := set.Add(claims.Claim{ID: "c1", Pattern: "a.go", Holder: claims.Holder{SessionID: "s", MemberID: "riya"}, LeaseExpires: time.Now().Add(time.Hour)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = daemon.New(daemon.Config{Paths: p, Claims: map[string]*claims.Set{repo: set}}).Run(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })

	bridge := func(lines ...string) (int, []daemon.Response, string) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"api"}, strings.NewReader(strings.Join(lines, "\n")+"\n"), &stdout, &stderr, BuildInfo{})
		var out []daemon.Response
		dec := json.NewDecoder(&stdout)
		for dec.More() {
			var r daemon.Response
			if err := dec.Decode(&r); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return code, out, stderr.String()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if code, _, _ := bridge(`{"id":1,"method":"status"}`); code == ExitOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon not reachable")
		}
		time.Sleep(20 * time.Millisecond)
	}

	code, out, errOut := bridge(
		`{"id":7,"method":"claim_list","repo":"/r/repo"}`,
		`{"id":8,"method":"task_approve","task":"t-1"}`,
		`{"id":9,"method":"nope"}`,
		`{"id":10,"method":"status"}`,
	)
	if code != ExitOK || len(out) != 4 {
		t.Fatalf("bridge: %d %+v %q", code, out, errOut)
	}
	if out[0].ID != 7 || len(out[0].Claims) != 1 || out[0].Claims[0].Holder.MemberID != "riya" {
		t.Errorf("claim_list: %+v", out[0])
	}
	if out[1].ID != 8 || !strings.Contains(out[1].Error, "crewbo task approve t-1") {
		t.Errorf("task_approve is not refused: %+v", out[1])
	}
	if out[2].ID != 9 || !strings.Contains(out[2].Error, "unknown method") {
		t.Errorf("an unknown method: %+v", out[2])
	}
	if out[3].ID != 10 || out[3].Status == nil {
		t.Errorf("status after an error: %+v", out[3])
	}
	if code, _, errOut := bridge(`not json`); code != ExitError || !strings.Contains(errOut, "not a JSON request") {
		t.Errorf("bad input: %d %q", code, errOut)
	}
}
