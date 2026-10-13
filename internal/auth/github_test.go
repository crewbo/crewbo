package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGitHub answers the device endpoints from a script of token replies.
type fakeGitHub struct {
	t       *testing.T
	mu      sync.Mutex
	replies []string // JSON bodies for successive token polls
	polls   int
}

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("Accept") != "application/json" {
		g.t.Errorf("%s %s Accept=%q", r.Method, r.URL.Path, r.Header.Get("Accept"))
	}
	if err := r.ParseForm(); err != nil || r.PostForm.Get("client_id") != "cid" {
		g.t.Errorf("form %v %v", r.PostForm, err)
	}
	switch r.URL.Path {
	case "/login/device/code":
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"ABCD-1234","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`))
	case "/login/oauth/access_token":
		if r.PostForm.Get("device_code") != "dc" || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			g.t.Errorf("token form %v", r.PostForm)
		}
		g.mu.Lock()
		reply := g.replies[min(g.polls, len(g.replies)-1)]
		g.polls++
		g.mu.Unlock()
		if reply == "HANG" { // longer than the client waits
			time.Sleep(300 * time.Millisecond)
			return
		}
		if strings.HasPrefix(reply, "HTTP ") {
			http.Error(w, reply, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(reply))
	default:
		http.NotFound(w, r)
	}
}

// flow returns a DeviceFlow against a fake GitHub and the waits it made.
func flow(t *testing.T, replies ...string) (*DeviceFlow, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(&fakeGitHub{t: t, replies: replies})
	t.Cleanup(srv.Close)
	var waits []time.Duration
	f := NewDeviceFlow("cid")
	f.BaseURL = srv.URL
	f.Wait = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	return f, &waits
}

func TestDeviceFlowApproved(t *testing.T) {
	f, waits := flow(t,
		`{"error":"authorization_pending"}`,
		`{"error":"slow_down","interval":10}`,
		`{"error":"authorization_pending"}`,
		`{"error":"slow_down"}`, // no interval: add 5 s
		`{"access_token":"gho_x","token_type":"bearer","scope":""}`,
	)
	ctx := context.Background()
	c, err := f.Start(ctx)
	if err != nil || c.UserCode != "ABCD-1234" || c.VerificationURI != "https://github.com/login/device" {
		t.Fatalf("Start = %+v, %v", c, err)
	}
	tok, err := f.Poll(ctx, c)
	if err != nil || tok != "gho_x" {
		t.Fatalf("Poll = %q, %v", tok, err)
	}
	s := time.Second
	if want := []time.Duration{5 * s, 5 * s, 10 * s, 10 * s, 15 * s}; !reflect.DeepEqual(*waits, want) {
		t.Errorf("waits = %v, want %v", *waits, want)
	}
}

func TestDeviceFlowEnds(t *testing.T) {
	for reply, want := range map[string]error{
		`{"error":"expired_token"}`: ErrExpired,
		`{"error":"access_denied"}`: ErrDenied,
	} {
		f, _ := flow(t, reply)
		if _, err := f.Poll(context.Background(), Code{DeviceCode: "dc", Interval: 1}); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", reply, err, want)
		}
	}
	for _, reply := range []string{`{"error":"device_flow_disabled","error_description":"Device Flow must be explicitly enabled"}`, `HTTP 500`, `{"token_type":"bearer"}`} {
		f, _ := flow(t, reply)
		_, err := f.Poll(context.Background(), Code{DeviceCode: "dc", Interval: 1})
		if err == nil || errors.Is(err, ErrExpired) || errors.Is(err, ErrDenied) {
			t.Errorf("%s: %v", reply, err)
		}
	}
	f, _ := flow(t, `{"error":"device_flow_disabled"}`)
	if _, err := f.Poll(context.Background(), Code{DeviceCode: "dc"}); err == nil || !strings.Contains(err.Error(), "device_flow_disabled") {
		t.Errorf("disabled flow error = %v", err)
	}
}

func TestDeviceFlowCodeExpires(t *testing.T) {
	f, _ := flow(t, `{"error":"authorization_pending"}`)
	f.Wait = sleep
	// An outer deadline, so a missing expiry check fails on time instead of
	// hanging; it must not be what ends the poll.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	start := time.Now()
	_, err := f.Poll(ctx, Code{DeviceCode: "dc", ExpiresIn: 1, Interval: 0})
	if !errors.Is(err, ErrExpired) || time.Since(start) > 4*time.Second {
		t.Errorf("Poll after expiry = %v in %v", err, time.Since(start))
	}
}

func TestStartRejectsErrorsAndGaps(t *testing.T) {
	for _, body := range []string{`{"error":"device_flow_disabled"}`, `{"user_code":"A"}`, `not json`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		f := NewDeviceFlow("cid")
		f.BaseURL = srv.URL
		if _, err := f.Start(context.Background()); err == nil {
			t.Errorf("Start accepted %s", body)
		}
		srv.Close()
	}
}

func TestLookupUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" || r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != apiVersion {
			t.Errorf("%s headers %v", r.URL.Path, r.Header)
		}
		switch r.Header.Get("Authorization") {
		case "Bearer good":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 4242, "login": "RM1338", "name": "x"})
		case "Bearer empty":
			_, _ = w.Write([]byte(`{"login":""}`))
		case "Bearer down":
			http.Error(w, "boom", http.StatusBadGateway)
		default:
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	if u, err := LookupUser(ctx, srv.Client(), srv.URL, "good"); err != nil || u != (User{ID: 4242, Login: "RM1338"}) {
		t.Errorf("good = %+v %v", u, err)
	}
	if _, err := LookupUser(ctx, srv.Client(), srv.URL, "bad"); !errors.Is(err, ErrBadToken) {
		t.Errorf("bad token = %v", err)
	}
	for _, tok := range []string{"empty", "down"} {
		if _, err := LookupUser(ctx, srv.Client(), srv.URL, tok); err == nil || errors.Is(err, ErrBadToken) {
			t.Errorf("%s = %v", tok, err)
		}
	}
}

// A poll lost on the network (here: the client times out) is retried at
// the next interval instead of ending the sign-in.
func TestDeviceFlowRetriesLostPolls(t *testing.T) {
	f, waits := flow(t, "HANG", `{"error":"authorization_pending"}`, "HANG", `{"access_token":"gho_x"}`)
	f.HTTP.Timeout = 100 * time.Millisecond
	tok, err := f.Poll(context.Background(), Code{DeviceCode: "dc", Interval: 1})
	if err != nil || tok != "gho_x" || len(*waits) != 4 {
		t.Fatalf("Poll = %q, %v after %d polls", tok, err, len(*waits))
	}
	// If the code expires while GitHub cannot be reached, the error says so.
	f, _ = flow(t, "HANG")
	f.HTTP.Timeout = 100 * time.Millisecond
	f.Wait = func(ctx context.Context, _ time.Duration) error { // poll often until the code expires
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return nil
		}
	}
	_, err = f.Poll(context.Background(), Code{DeviceCode: "dc", ExpiresIn: 1, Interval: 1})
	if !errors.Is(err, ErrExpired) || !strings.Contains(err.Error(), "the last request to GitHub failed") {
		t.Errorf("expiry while unreachable: %v", err)
	}
}
