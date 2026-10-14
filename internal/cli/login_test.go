package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/keys"
	"github.com/crewbo/crewbo/internal/server"
	"github.com/crewbo/crewbo/internal/store"
)

func TestCheckServerURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://crewbo.example.com/": true,
		"https://x.trycloudflare.com": true,
		"http://127.0.0.1:7420":       true,
		"http://localhost:7420":       true,
		"http://[::1]:7420":           true,
		"http://192.168.1.5:7420":     false,
		"ftp://example.com":           false,
		"crewbo.example.com":          false,
		"":                            false,
	} {
		got, err := checkServerURL(raw, false)
		if (err == nil) != ok || (ok && strings.HasSuffix(got, "/")) {
			t.Errorf("checkServerURL(%q) = %q, %v", raw, got, err)
		}
	}
	if _, err := checkServerURL("http://192.168.1.5:7420", true); err != nil {
		t.Errorf("--insecure-lan: %v", err)
	}
}

func TestLoginExchangesTheGitHubToken(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"WXYZ-0001","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`))
		case "/login/oauth/access_token":
			_, _ = w.Write([]byte(`{"access_token":"gho_x","token_type":"bearer"}`))
		}
	}))
	defer github.Close()
	crewbo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req server.SessionRequest
		if r.URL.Path != "/v1/auth/session" || json.NewDecoder(r.Body).Decode(&req) != nil || req.GitHubToken != "gho_x" {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(server.SessionResponse{SessionToken: "cbs_abc", GitHubLogin: "RM1338", ExpiresUnixMs: 99})
	}))
	defer crewbo.Close()

	flow := auth.NewDeviceFlow("cid")
	flow.BaseURL = github.URL
	flow.Wait = func(context.Context, time.Duration) error { return nil }
	var out bytes.Buffer
	s, err := login(context.Background(), flow, crewbo.Client(), crewbo.URL, &out)
	if err != nil || s != (auth.Session{Server: crewbo.URL, SessionToken: "cbs_abc", GitHubLogin: "RM1338", ExpiresUnixMs: 99}) {
		t.Fatalf("login = %+v, %v", s, err)
	}
	if !strings.Contains(out.String(), "https://github.com/login/device") || !strings.Contains(out.String(), "WXYZ-0001") {
		t.Errorf("prompt: %q", out.String())
	}
	if strings.Contains(out.String(), "gho_x") || strings.Contains(out.String(), "cbs_abc") {
		t.Errorf("a token was printed: %q", out.String())
	}
	// A server that refuses the sign-in is reported, nothing returned.
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "GitHub did not accept the token", http.StatusUnauthorized)
	}))
	defer refusing.Close()
	if _, err := login(context.Background(), flow, refusing.Client(), refusing.URL, &out); err == nil || !strings.Contains(err.Error(), "GitHub did not accept") {
		t.Errorf("refused sign-in: %v", err)
	}
}

func TestLoginUsage(t *testing.T) {
	isolate(t)
	// Only cases that stop before any network call: a broken URL rule must
	// not start a real GitHub sign-in (TestCheckServerURL covers the rule).
	for _, args := range [][]string{{"login"}, {"login", "extra"}, {"login", "--server", "ftp://x"}} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, nil, &out, &errOut, BuildInfo{}); code != ExitUsage {
			t.Errorf("%v: exit %d %q", args, code, errOut.String())
		}
	}
}

func TestRegisterDeviceAgainstServer(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "server.db"), store.Server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	token, _, err := server.NewSessions(db.SQL(), time.Now).Create(ctx, auth.User{ID: 9, Login: "RM1338"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.SQL()).Handler())
	defer ts.Close()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	key := keys.Key{Private: priv, Source: keys.SourceKeychain}
	if id, err := registerDevice(ctx, ts.Client(), ts.URL, token, key); err != nil || id != key.ID() {
		t.Fatalf("registerDevice = %q, %v", id, err)
	}
	if _, err := registerDevice(ctx, ts.Client(), ts.URL, "cbs_expired", key); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("unknown session: %v", err)
	}
}

// M5-08: a restarted tunnel gives the same server a new address; logging
// in there moves the session and its rooms without a GitHub sign-in.
func TestLoginMovesToANewAddress(t *testing.T) {
	p := isolate(t)
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "server.db"), store.Server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	h := server.New(slog.New(slog.DiscardHandler), db.SQL()).Handler()
	oldAddr, newAddr := httptest.NewServer(h), httptest.NewServer(h) // one server, two addresses
	defer oldAddr.Close()
	defer newAddr.Close()
	token, expires, err := server.NewSessions(db.SQL(), time.Now).Create(ctx, auth.User{ID: 2, Login: "gavriel953"})
	if err != nil {
		t.Fatal(err)
	}
	var info server.Info
	if err := callServer(ctx, oldAddr.Client(), oldAddr.URL, "", http.MethodGet, "/v1/info", nil, &info); err != nil {
		t.Fatal(err)
	}
	prev := auth.Session{Server: oldAddr.URL, SessionToken: token, GitHubLogin: "gavriel953", ExpiresUnixMs: expires.UnixMilli(), ServerID: info.ServerID}
	if err := auth.SaveSession(auth.SessionPath(p.Config), prev); err != nil {
		t.Fatal(err)
	}
	if err := auth.SaveBindings(auth.BindingsPath(p.Config), []auth.Binding{
		{Repo: "/src/app", Server: oldAddr.URL, RoomID: "r_app", Name: "app"},
		{Repo: "/src/other", Server: "https://elsewhere.example", RoomID: "r_other", Name: "other"},
	}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runLogin(ctx, []string{"--server", newAddr.URL}, &out, &errOut); code != ExitOK || !strings.Contains(out.String(), "no new sign-in needed") || !strings.Contains(out.String(), "1 room(s)") {
		t.Fatalf("login: %d %q %q", code, out.String(), errOut.String())
	}
	s, err := auth.LoadSession(auth.SessionPath(p.Config))
	if err != nil || s.Server != newAddr.URL || s.SessionToken != token || s.ServerID != info.ServerID {
		t.Errorf("session after the move: %+v %v", s, err)
	}
	bs, _ := auth.LoadBindings(auth.BindingsPath(p.Config))
	if len(bs) != 2 || bs[0].Server != newAddr.URL || bs[1].Server != "https://elsewhere.example" {
		t.Errorf("bindings after the move: %+v", bs)
	}
	// Something that is not a Crewbo server is refused before anything else.
	notCrewbo := httptest.NewServer(http.NotFoundHandler())
	defer notCrewbo.Close()
	if code := runLogin(ctx, []string{"--server", notCrewbo.URL}, &out, &errOut); code != ExitError || !strings.Contains(errOut.String(), "does not answer like a Crewbo server") {
		t.Errorf("login to a non-Crewbo server: %d %q", code, errOut.String())
	}
}

func TestSameServerNewAddress(t *testing.T) {
	now := time.Now()
	valid := auth.Session{Server: "https://old", ServerID: "srv_a", ExpiresUnixMs: now.Add(time.Hour).UnixMilli()}
	if !sameServerNewAddress(valid, server.Info{ServerID: "srv_a"}, "https://new", now) {
		t.Error("the same server at a new address was not recognised")
	}
	for name, tc := range map[string]struct {
		prev auth.Session
		id   string
		base string
	}{
		"another server":     {valid, "srv_b", "https://new"},
		"no saved server id": {auth.Session{Server: "https://old", ExpiresUnixMs: valid.ExpiresUnixMs}, "srv_a", "https://new"},
		"the same address":   {valid, "srv_a", "https://old"},
		"an expired session": {auth.Session{Server: "https://old", ServerID: "srv_a", ExpiresUnixMs: now.Add(-time.Minute).UnixMilli()}, "srv_a", "https://new"},
	} {
		if sameServerNewAddress(tc.prev, server.Info{ServerID: tc.id}, tc.base, now) {
			t.Errorf("%s: treated as a move", name)
		}
	}
}

// After a full sign-in, rooms the server already has move to its address;
// rooms it does not know stay where they were.
func TestMoveKnownRooms(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "server.db"), store.Server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ts := httptest.NewServer(server.New(slog.New(slog.DiscardHandler), db.SQL()).Handler())
	defer ts.Close()
	u := auth.User{ID: 2, Login: "gavriel953"}
	room, err := server.NewRooms(db.SQL(), time.Now).Create(ctx, u, "app")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := server.NewSessions(db.SQL(), time.Now).Create(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	cfg := t.TempDir()
	if err := auth.SaveBindings(auth.BindingsPath(cfg), []auth.Binding{
		{Repo: "/src/app", Server: "https://old-tunnel.example", RoomID: room.ID, Name: "app"},
		{Repo: "/src/other", Server: "https://elsewhere.example", RoomID: "r_unknown", Name: "other"},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := moveKnownRooms(ctx, ts.Client(), ts.URL, token, cfg)
	if err != nil || n != 1 {
		t.Fatalf("moved %d, %v", n, err)
	}
	bs, _ := auth.LoadBindings(auth.BindingsPath(cfg))
	if bs[0].Server != ts.URL || bs[1].Server != "https://elsewhere.example" {
		t.Errorf("bindings %+v", bs)
	}
}
