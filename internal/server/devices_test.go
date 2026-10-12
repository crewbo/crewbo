package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/keys"
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
