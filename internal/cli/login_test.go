package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/server"
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
