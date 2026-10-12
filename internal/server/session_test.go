package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/crewbo/crewbo/internal/auth"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

func TestSessionsStoreOnlyAHash(t *testing.T) {
	ctx := context.Background()
	s := newTestServer(t)
	now := time.Now()
	s.sessions.now = func() time.Time { return now }
	token, expires, err := s.sessions.Create(ctx, auth.User{ID: 7, Login: "RM1338"})
	if err != nil || !strings.HasPrefix(token, tokenPrefix) || len(token) < 40 || !expires.Equal(now.Add(SessionTTL)) {
		t.Fatalf("Create = %q %v %v", token, expires, err)
	}
	var n int
	if err := s.sessions.db.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash = ? OR CAST(token_hash AS TEXT) = ? OR github_login = ?`, []byte(token), token, token).Scan(&n); err != nil || n != 0 {
		t.Errorf("token stored in clear: %d %v", n, err)
	}
	if u, err := s.sessions.Lookup(ctx, token); err != nil || u != (auth.User{ID: 7, Login: "RM1338"}) {
		t.Errorf("Lookup = %+v %v", u, err)
	}
	if _, err := s.sessions.Lookup(ctx, token+"x"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("unknown token: %v", err)
	}
	now = now.Add(SessionTTL)
	if _, err := s.sessions.Lookup(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("expired token: %v", err)
	}
}

func postSession(t *testing.T, url, body string) (int, SessionResponse) {
	t.Helper()
	resp, err := http.Post(url+"/v1/auth/session", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out SessionResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

func TestSessionEndpoint(t *testing.T) {
	s := newTestServer(t)
	var seen []string
	s.lookupUser = func(_ context.Context, token string) (auth.User, error) {
		seen = append(seen, token)
		switch token {
		case "gho_good":
			return auth.User{ID: 4242, Login: "RM1338"}, nil
		case "gho_bad":
			return auth.User{}, auth.ErrBadToken
		}
		return auth.User{}, errors.New("network down")
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	code, got := postSession(t, ts.URL, `{"github_token":"gho_good"}`)
	if code != http.StatusOK || got.GitHubLogin != "RM1338" || !strings.HasPrefix(got.SessionToken, tokenPrefix) || got.ExpiresUnixMs <= time.Now().UnixMilli() {
		t.Fatalf("exchange: %d %+v", code, got)
	}
	// The new session opens a WebSocket.
	c := dial(t, "ws"+strings.TrimPrefix(ts.URL, "http")+"/v1/ws")
	sendEnv(t, c, &crewbov1.Envelope{Id: "h", Body: &crewbov1.Envelope_Hello{Hello: &crewbov1.Hello{ProtocolVersion: 1, SessionToken: got.SessionToken}}})
	if env, err := recv(t, c); err != nil || env.GetWelcome() == nil {
		t.Fatalf("hello with the new session: %v %v", env, err)
	}
	for body, want := range map[string]int{
		`{"github_token":"gho_bad"}`:  http.StatusUnauthorized,
		`{"github_token":"gho_down"}`: http.StatusBadGateway,
		`{"github_token":""}`:         http.StatusBadRequest,
		`not json`:                    http.StatusBadRequest,
	} {
		if code, _ := postSession(t, ts.URL, body); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
	if len(seen) != 3 {
		t.Errorf("GitHub asked %d times (%v), want 3", len(seen), seen)
	}
	resp, err := http.Get(ts.URL + "/v1/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", resp.StatusCode)
	}
}

func TestHelloNeedsAValidSession(t *testing.T) {
	s := newTestServer(t)
	expired := "cbs_expired"
	if _, err := s.sessions.db.Exec(`INSERT INTO sessions VALUES (?, 2, 'old', 0, 1)`, hashToken(expired)); err != nil {
		t.Fatal(err)
	}
	url := testServer(t, s)
	for _, token := range []string{"", "cbs_wrong", expired} {
		c := dial(t, url)
		sendEnv(t, c, &crewbov1.Envelope{Id: "h", Body: &crewbov1.Envelope_Hello{Hello: &crewbov1.Hello{ProtocolVersion: 1, SessionToken: token}}})
		env, err := recv(t, c)
		if env.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_UNAUTHENTICATED || env.GetError().GetRequestId() != "h" {
			t.Fatalf("token %q: %v %v", token, env, err)
		}
		if _, err := recv(t, c); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
			t.Errorf("token %q: close %v", token, err)
		}
	}
}
