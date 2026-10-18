package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
)

// openDashboard asks the daemon for the dashboard and returns its base
// address and token.
func openDashboard(t *testing.T, call func(Request) (Response, error)) (base, token string) {
	t.Helper()
	resp, err := call(Request{Method: MethodDashboard})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(resp.URL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "/" {
		t.Fatalf("dashboard address %q: %v", resp.URL, err)
	}
	frag, _ := url.ParseQuery(u.Fragment)
	if len(frag.Get("token")) != 64 {
		t.Fatalf("token in %q", resp.URL)
	}
	return "http://" + u.Host, frag.Get("token")
}

type got struct {
	code int
	body string
	hdr  http.Header
}

func fetch(t *testing.T, base, path string, set func(*http.Request)) got {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if set != nil {
		set(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return got{resp.StatusCode, string(b), resp.Header}
}

func TestDashboardGuardsItsState(t *testing.T) {
	p := testPaths(t)
	set := claims.NewSet()
	if err := set.Add(claims.Claim{ID: "c1", Pattern: "src/a.go", TaskID: "t-1", Holder: claims.Holder{SessionID: "terminal", MemberID: "riya"}, LeaseExpires: time.Now().Add(time.Hour)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := start(t, Config{Paths: p, Log: quietLog(), Claims: map[string]*claims.Set{"/r/repo": set, "/r/empty": claims.NewSet()}})
	call := func(req Request) (Response, error) {
		c, err := dial(t, p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return c.Call(ctx, req)
	}
	base, token := openDashboard(t, call)
	if again, _ := call(Request{Method: MethodDashboard}); again.URL != base+"/#token="+token {
		t.Errorf("a second call gave %q", again.URL)
	}
	auth := func(req *http.Request) { req.Header.Set("Authorization", "Bearer "+token) }

	page := fetch(t, base, "/", nil)
	if page.code != http.StatusOK || !strings.Contains(page.body, "<title>Crewbo</title>") || !strings.Contains(page.hdr.Get("Content-Security-Policy"), "default-src 'self'") || page.hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("page: %d %v", page.code, page.hdr)
	}
	if f := fetch(t, base, "/fonts/instrument-sans-latin-400-normal.woff2", nil); f.code != http.StatusOK || len(f.body) < 1000 {
		t.Errorf("font: %d", f.code)
	}

	for name, tc := range map[string]struct {
		set  func(*http.Request)
		want int
	}{
		"no token":         {nil, http.StatusUnauthorized},
		"wrong token":      {func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+strings.Repeat("0", 64)) }, http.StatusUnauthorized},
		"token in query":   {func(r *http.Request) { r.URL.RawQuery = "token=" + token }, http.StatusUnauthorized},
		"other origin":     {func(r *http.Request) { auth(r); r.Header.Set("Origin", "http://evil.example") }, http.StatusForbidden},
		"localhost name":   {func(r *http.Request) { auth(r); r.Host = strings.Replace(r.Host, "127.0.0.1", "localhost", 1) }, http.StatusMisdirectedRequest},
		"rebinding host":   {func(r *http.Request) { auth(r); r.Host = "evil.example" }, http.StatusMisdirectedRequest},
		"same origin":      {func(r *http.Request) { auth(r); r.Header.Set("Origin", base) }, http.StatusOK},
		"token, no origin": {auth, http.StatusOK},
	} {
		if g := fetch(t, base, "/api/state", tc.set); g.code != tc.want {
			t.Errorf("%s: %d, want %d (%s)", name, g.code, tc.want, g.body)
		}
	}

	g := fetch(t, base, "/api/state", auth)
	var st dashboardState
	if err := json.Unmarshal([]byte(g.body), &st); err != nil {
		t.Fatalf("state %q: %v", g.body, err)
	}
	if len(st.Repos) != 1 || st.Repos[0].Root != "/r/repo" || len(st.Repos[0].Claims) != 1 || st.Repos[0].Claims[0].Holder.MemberID != "riya" || st.Status.PID == 0 {
		t.Errorf("state: %+v", st)
	}
	if !strings.Contains(g.body, `"handoffs":[]`) || !strings.Contains(g.body, `"tasks":[]`) || g.hdr.Get("Cache-Control") != "no-store" {
		t.Errorf("empty lists must be [] for the page: %s %v", g.body, g.hdr)
	}

	// The dashboard stops with the daemon.
	r.cancel()
	<-r.done
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
		t.Error("the dashboard still answers after the daemon stopped")
	}
}

// In a room, the dashboard shows the room's name and its handoff steps.
func TestDashboardShowsTheRoom(t *testing.T) {
	f := newCrossFixture(t)
	a, b := f.a, f.b
	if _, err := b.claim("src/a.go"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "A to see B's claim", func() bool { ok, _ := a.agentMay("src/a.go"); return !ok })
	base, token := openDashboard(t, a.call)
	auth := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
	if g := fetch(t, base, "/api/state", auth); !strings.Contains(g.body, `"room":"crewbo"`) || !strings.Contains(g.body, `"handoffs":[]`) {
		t.Errorf("a room with no handoff steps: %s", g.body)
	}
	if _, err := a.call(Request{Method: MethodClaimRequest, Repo: a.repo, Paths: []string{"src/a.go"}, Text: "need it"}); err != nil {
		t.Fatal(err)
	}
	var st dashboardState
	eventually(t, "the request on A's dashboard", func() bool {
		g := fetch(t, base, "/api/state", auth)
		return json.Unmarshal([]byte(g.body), &st) == nil && len(st.Repos) == 1 && len(st.Repos[0].Handoffs) == 1
	})
	r := st.Repos[0]
	if r.Room != "crewbo" || r.Root != a.repo || len(r.Claims) != 1 || r.Handoffs[0].Path != "src/a.go" || !r.Handoffs[0].Open || st.Status.Login != "RM1338" {
		t.Errorf("A's dashboard: %+v", st)
	}
}
