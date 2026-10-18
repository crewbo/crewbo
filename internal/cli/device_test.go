package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/server"
	"github.com/crewbo/crewbo/internal/store"
)

func TestDeviceCommands(t *testing.T) {
	p := isolate(t)
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "server.db"), store.Server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ts := httptest.NewServer(server.New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.SQL()).Handler())
	defer ts.Close()
	for _, k := range []string{"ed25519:aaa1", "ed25519:aaa2", "ed25519:bbb"} {
		if _, err := db.SQL().Exec(`INSERT INTO device_keys (key_id, github_user_id, public_key, name, os, created_unix_ms) VALUES (?, 1, zeroblob(32), ?, 'linux', 0)`, k, "laptop "+k[8:]); err != nil {
			t.Fatal(err)
		}
	}
	tok, _, err := server.NewSessions(db.SQL(), time.Now).Create(context.Background(), auth.User{ID: 1, Login: "RM1338"})
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.SaveSession(auth.SessionPath(p.Config), auth.Session{Server: ts.URL, SessionToken: tok, GitHubLogin: "RM1338", DeviceKeyID: "ed25519:bbb"}); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), append([]string{"device"}, args...), nil, &out, &errOut, BuildInfo{})
		return code, out.String(), errOut.String()
	}

	code, out, _ := run("list")
	if code != ExitOK || strings.Count(out, "\n") != 4 || !strings.Contains(out, "ed25519:bbb   laptop bbb") || !strings.Contains(out, "active, this computer") {
		t.Fatalf("list: %d\n%s", code, out)
	}
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{nil, ExitUsage, "Usage:"},
		{[]string{"revoke"}, ExitUsage, "Usage:"},
		{[]string{"list", "x"}, ExitUsage, "Usage:"},
		{[]string{"revoke", "ed25519:zzz"}, ExitError, "no device key ed25519:zzz is registered to you"},
		{[]string{"revoke", "ed25519:aaa"}, ExitError, "matches 2 keys"},
		{[]string{"revoke", "ed25519:bbb"}, ExitError, "is this computer's own key"},
	} {
		if code, _, errOut := run(tc.args...); code != tc.code || !strings.Contains(errOut, tc.want) {
			t.Errorf("%q: %d %q", tc.args, code, errOut)
		}
	}
	code, out, errOut := run("revoke", "ed25519:aaa1")
	if code != ExitOK || !strings.Contains(out, "Revoked key ed25519:aaa1 (laptop aaa1, linux)") || !strings.Contains(out, "run crewbo login here") {
		t.Fatalf("revoke: %d %q %q", code, out, errOut)
	}
	var revoked int64
	if err := db.SQL().QueryRow(`SELECT coalesce(revoked_unix_ms, 0) FROM device_keys WHERE key_id = 'ed25519:aaa1'`).Scan(&revoked); err != nil || revoked == 0 {
		t.Errorf("not revoked on the server: %v", err)
	}
	if code, _, errOut := run("list"); code != ExitError || !strings.Contains(errOut, "401") {
		t.Errorf("the session survived the revocation: %d %q", code, errOut)
	}
}
