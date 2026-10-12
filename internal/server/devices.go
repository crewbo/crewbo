package server

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/crewbo/crewbo/internal/keys"
)

// DeviceRequest is the body of POST /v1/devices. Signature is Ed25519 over
// keys.RegistrationMessage(the caller's session token), proving the caller
// holds the private key. Byte fields are standard base64 in JSON.
type DeviceRequest struct {
	PublicKey []byte `json:"public_key"`
	Signature []byte `json:"signature"`
	Name      string `json:"name"`
	OS        string `json:"os"`
}

// DeviceResponse answers a successful POST /v1/devices.
type DeviceResponse struct {
	KeyID string `json:"key_id"`
}

// serveDevice registers the caller's device public key against their
// GitHub account. Registering the same key again is accepted; a key
// already registered to someone else is refused.
func (s *Server) serveDevice(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	var req DeviceRequest
	if !decode(w, r, &req) {
		return
	}
	switch {
	case len(req.PublicKey) != ed25519.PublicKeySize:
		http.Error(w, "public_key must be 32 bytes", http.StatusBadRequest)
		return
	case req.Name == "" || len(req.Name) > 64 || !utf8.ValidString(req.Name) || req.OS == "" || len(req.OS) > 16:
		http.Error(w, "name (1 to 64 bytes) and os (1 to 16 bytes) are required", http.StatusBadRequest)
		return
	case !ed25519.Verify(req.PublicKey, keys.RegistrationMessage(token), req.Signature):
		http.Error(w, "signature does not prove the key is yours", http.StatusForbidden)
		return
	}
	id := keys.KeyID(req.PublicKey)
	// Insert if absent, then check the owner: no window between the two
	// for a concurrent registration of the same key.
	res, err := s.sessions.db.ExecContext(r.Context(),
		`INSERT INTO device_keys (key_id, github_user_id, public_key, name, os, created_unix_ms) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (key_id) DO NOTHING`,
		id, u.ID, req.PublicKey, req.Name, req.OS, s.sessions.now().UnixMilli())
	var owner int64
	if err == nil {
		err = s.sessions.db.QueryRowContext(r.Context(), `SELECT github_user_id FROM device_keys WHERE key_id = ?`, id).Scan(&owner)
	}
	if err != nil {
		s.log.Error("devices: register", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if owner != u.ID {
		http.Error(w, "this key is registered to another account", http.StatusConflict)
		return
	}
	if n, _ := res.RowsAffected(); n == 1 {
		s.log.Info("devices: registered", "github_login", u.Login, "key_id", id, "os", req.OS)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(DeviceResponse{KeyID: id})
}

// deviceOwned reports whether a device key is registered to the account and
// not revoked.
func (s *Server) deviceOwned(ctx context.Context, keyID string, userID int64) (bool, error) {
	var n int
	err := s.sessions.db.QueryRowContext(ctx, `SELECT count(*) FROM device_keys WHERE key_id = ? AND github_user_id = ? AND revoked_unix_ms IS NULL`, keyID, userID).Scan(&n)
	return n > 0, err
}
