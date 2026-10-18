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

// devicePublicKey returns a device key's public key if it is registered to
// the account and not revoked.
func (s *Server) devicePublicKey(ctx context.Context, keyID string, userID int64) (ed25519.PublicKey, error) {
	var pub []byte
	err := s.sessions.db.QueryRowContext(ctx, `SELECT public_key FROM device_keys WHERE key_id = ? AND github_user_id = ? AND revoked_unix_ms IS NULL`, keyID, userID).Scan(&pub)
	return pub, err
}

// Device is one of the caller's registered device keys (GET /v1/devices).
type Device struct {
	KeyID         string `json:"key_id"`
	Name          string `json:"name"`
	OS            string `json:"os"`
	CreatedUnixMs int64  `json:"created_unix_ms"`
	RevokedUnixMs int64  `json:"revoked_unix_ms,omitempty"`
}

// serveListDevices lists the caller's device keys, revoked ones included.
func (s *Server) serveListDevices(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	rows, err := s.sessions.db.QueryContext(r.Context(), `SELECT key_id, name, os, created_unix_ms, coalesce(revoked_unix_ms, 0) FROM device_keys WHERE github_user_id = ? ORDER BY created_unix_ms`, u.ID)
	if err != nil {
		s.log.Error("devices: list", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = rows.Close() }()
	out := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.KeyID, &d.Name, &d.OS, &d.CreatedUnixMs, &d.RevokedUnixMs); err != nil {
			s.log.Error("devices: list", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("devices: list", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	reply(w, out)
}

// serveRevokeDevice revokes one of the caller's device keys for good
// (data-model.md: revoked keys are never accepted again), then ends all
// of the caller's sessions and closes their live connections: a session
// is not bound to a key, so a lost device's session would otherwise keep
// working until it expires. The caller signs in again on each device they
// still have.
func (s *Server) serveRevokeDevice(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	res, err := s.sessions.db.ExecContext(r.Context(), `UPDATE device_keys SET revoked_unix_ms = coalesce(revoked_unix_ms, ?) WHERE key_id = ? AND github_user_id = ?`,
		s.sessions.now().UnixMilli(), id, u.ID)
	if err != nil {
		s.log.Error("devices: revoke", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "no device key "+id+" is registered to you", http.StatusNotFound)
		return
	}
	if err := s.sessions.EndAll(r.Context(), u.ID); err != nil {
		s.log.Error("devices: end sessions", "err", err)
		http.Error(w, "the key is revoked, but ending your sessions failed; try again", http.StatusInternalServerError)
		return
	}
	s.closeConns(u.ID)
	s.log.Info("devices: revoked", "github_login", u.Login, "key_id", id)
	w.WriteHeader(http.StatusNoContent)
}

// track registers a live connection of a member until the returned func
// is called.
func (s *Server) track(userID int64, cancel *context.CancelFunc) func() {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.conns == nil {
		s.conns = map[int64]map[*context.CancelFunc]struct{}{}
	}
	if s.conns[userID] == nil {
		s.conns[userID] = map[*context.CancelFunc]struct{}{}
	}
	s.conns[userID][cancel] = struct{}{}
	return func() {
		s.connsMu.Lock()
		defer s.connsMu.Unlock()
		delete(s.conns[userID], cancel)
	}
}

// closeConns closes every live connection of a member.
func (s *Server) closeConns(userID int64) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	for cancel := range s.conns[userID] {
		(*cancel)()
	}
}
