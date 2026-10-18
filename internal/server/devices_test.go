package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/crewbo/crewbo/internal/keys"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

func registerKey(t *testing.T, url, token string, req DeviceRequest) (int, string) {
	t.Helper()
	body, _ := json.Marshal(req)
	r, _ := http.NewRequest(http.MethodPost, url+"/v1/devices", bytes.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out DeviceResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.KeyID
}

func signedRequest(t *testing.T, priv ed25519.PrivateKey, token string) DeviceRequest {
	t.Helper()
	return DeviceRequest{
		PublicKey: priv.Public().(ed25519.PublicKey),
		Signature: ed25519.Sign(priv, keys.RegistrationMessage(token)),
		Name:      "laptop", OS: "linux",
	}
}

func TestRegisterDevice(t *testing.T) {
	s := newTestServer(t)
	other := "cbs_other"
	if _, err := s.sessions.db.Exec(`INSERT INTO sessions VALUES (?, 2, 'gavriel953', 0, ?)`, hashToken(other), time.Now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	want := keys.KeyID(priv.Public().(ed25519.PublicKey))

	if code, id := registerKey(t, ts.URL, testToken, signedRequest(t, priv, testToken)); code != http.StatusOK || id != want {
		t.Fatalf("register = %d %q, want 200 %q", code, id, want)
	}
	if code, id := registerKey(t, ts.URL, testToken, signedRequest(t, priv, testToken)); code != http.StatusOK || id != want {
		t.Errorf("register again = %d %q", code, id)
	}
	var owner int64
	var pub []byte
	if err := s.sessions.db.QueryRow(`SELECT github_user_id, public_key FROM device_keys WHERE key_id = ?`, want).Scan(&owner, &pub); err != nil || owner != 1 || !bytes.Equal(pub, priv.Public().(ed25519.PublicKey)) {
		t.Errorf("stored row: owner %d, %v", owner, err)
	}

	bad := func(mutate func(*DeviceRequest)) DeviceRequest {
		r := signedRequest(t, priv, testToken)
		mutate(&r)
		return r
	}
	for name, tc := range map[string]struct {
		token string
		req   DeviceRequest
		want  int
	}{
		"no session":                 {"", signedRequest(t, priv, testToken), http.StatusUnauthorized},
		"unknown session":            {"cbs_nope", signedRequest(t, priv, "cbs_nope"), http.StatusUnauthorized},
		"signed for another session": {testToken, signedRequest(t, priv, other), http.StatusForbidden},
		"short key":                  {testToken, bad(func(r *DeviceRequest) { r.PublicKey = r.PublicKey[:31] }), http.StatusBadRequest},
		"no name":                    {testToken, bad(func(r *DeviceRequest) { r.Name = "" }), http.StatusBadRequest},
		"long os":                    {testToken, bad(func(r *DeviceRequest) { r.OS = "a-very-long-os-name" }), http.StatusBadRequest},
		// Someone else presenting this key, correctly signed for their own
		// session (they would need the private key), is still refused.
		"key owned by another member": {other, signedRequest(t, priv, other), http.StatusConflict},
	} {
		if code, _ := registerKey(t, ts.URL, tc.token, tc.req); code != tc.want {
			t.Errorf("%s: %d, want %d", name, code, tc.want)
		}
	}
}

func TestRegisterDeviceConcurrently(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code, _ := registerKey(t, ts.URL, testToken, signedRequest(t, priv, testToken)); code != http.StatusOK {
				t.Errorf("concurrent register: %d", code)
			}
		}()
	}
	wg.Wait()
	var n int
	if err := s.sessions.db.QueryRow(`SELECT count(*) FROM device_keys`).Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d %v", n, err)
	}
}

// devicesCall sends method to url+path with token and returns the status
// and, for a list, the devices.
func devicesCall(t *testing.T, method, url, token string) (int, []Device) {
	t.Helper()
	r, _ := http.NewRequest(method, url, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out []Device
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// helloWith opens a connection presenting a session and a device key and
// returns it with the server's first answer.
func helloWith(t *testing.T, wsURL, token, key string) (*websocket.Conn, *crewbov1.Envelope) {
	t.Helper()
	c := dial(t, wsURL)
	sendEnv(t, c, &crewbov1.Envelope{Id: "h1", Body: &crewbov1.Envelope_Hello{Hello: &crewbov1.Hello{ProtocolVersion: ProtocolVersion, SessionToken: token, DeviceKeyId: key}}})
	env, err := recv(t, c)
	if err != nil {
		t.Fatal(err)
	}
	return c, env
}

func TestRevokeDevice(t *testing.T) {
	s := newTestServer(t)
	other := "cbs_other"
	if _, err := s.sessions.db.Exec(`INSERT INTO sessions VALUES (?, 2, 'gavriel953', 0, ?)`, hashToken(other), time.Now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/ws"
	newKey := func(token string) string {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		code, id := registerKey(t, ts.URL, token, signedRequest(t, priv, token))
		if code != http.StatusOK {
			t.Fatalf("register: %d", code)
		}
		return id
	}
	kept, lost, theirs := newKey(testToken), newKey(testToken), newKey(other)

	if code, ds := devicesCall(t, http.MethodGet, ts.URL+"/v1/devices", testToken); code != http.StatusOK || len(ds) != 2 || ds[0].KeyID != kept || ds[1].KeyID != lost || ds[0].RevokedUnixMs != 0 || ds[0].Name != "laptop" {
		t.Fatalf("list: %d %+v", code, ds)
	}
	live, welcome := helloWith(t, wsURL, testToken, lost)
	if welcome.GetWelcome() == nil {
		t.Fatalf("hello with the key: %v", welcome)
	}

	for name, id := range map[string]string{"another member's key": theirs, "an unknown key": "ed25519:nope"} {
		if code, _ := devicesCall(t, http.MethodPost, ts.URL+"/v1/devices/"+id+"/revoke", testToken); code != http.StatusNotFound {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code, _ := devicesCall(t, http.MethodPost, ts.URL+"/v1/devices/"+lost+"/revoke", testToken); code != http.StatusNoContent {
		t.Fatalf("revoke: %d", code)
	}

	// Every session of the member ended, and their live connection closed;
	// other members are untouched.
	if _, err := recv(t, live); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the live connection stayed open: %v", err)
	}
	if code, _ := devicesCall(t, http.MethodGet, ts.URL+"/v1/devices", testToken); code != http.StatusUnauthorized {
		t.Errorf("the old session still works: %d", code)
	}
	if code, ds := devicesCall(t, http.MethodGet, ts.URL+"/v1/devices", other); code != http.StatusOK || len(ds) != 1 || ds[0].RevokedUnixMs != 0 {
		t.Errorf("another member's devices: %d %+v", code, ds)
	}

	// Signed in again: the key is listed as revoked and refused for good.
	again := "cbs_again"
	if _, err := s.sessions.db.Exec(`INSERT INTO sessions VALUES (?, 1, 'tester', 0, ?)`, hashToken(again), time.Now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	_, ds := devicesCall(t, http.MethodGet, ts.URL+"/v1/devices", again)
	if len(ds) != 2 || ds[0].RevokedUnixMs != 0 || ds[1].RevokedUnixMs == 0 {
		t.Fatalf("after revoking: %+v", ds)
	}
	if _, env := helloWith(t, wsURL, again, lost); env.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_UNAUTHENTICATED {
		t.Errorf("hello with the revoked key: %v", env)
	}
	if _, env := helloWith(t, wsURL, again, kept); env.GetWelcome() == nil {
		t.Errorf("hello with the kept key: %v", env)
	}
	if code, _ := devicesCall(t, http.MethodPost, ts.URL+"/v1/devices/"+lost+"/revoke", again); code != http.StatusNoContent {
		t.Errorf("revoking again: %d", code)
	}
	var at int64
	if err := s.sessions.db.QueryRow(`SELECT revoked_unix_ms FROM device_keys WHERE key_id = ?`, lost).Scan(&at); err != nil || at != ds[1].RevokedUnixMs {
		t.Errorf("revoking again moved the revocation time: %d, was %d (%v)", at, ds[1].RevokedUnixMs, err)
	}
}
