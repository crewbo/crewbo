package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
)

// SessionTTL is how long a Crewbo session token is valid. M3-04 binds
// sessions to device keys; until then a member signs in again after this.
const SessionTTL = 7 * 24 * time.Hour

// ErrUnauthenticated means a session token is unknown or expired.
var ErrUnauthenticated = errors.New("server: session token unknown or expired")

// tokenPrefix marks Crewbo session tokens so they are recognisable in
// secret scanners and never confused with GitHub tokens.
const tokenPrefix = "cbs_"

// Sessions issues and checks Crewbo session tokens, stored as SHA-256
// hashes in a database opened with store.Server.
type Sessions struct {
	db  *sql.DB
	now func() time.Time
}

// NewSessions returns Sessions over db.
func NewSessions(db *sql.DB, now func() time.Time) *Sessions {
	return &Sessions{db: db, now: now}
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// Create issues a session token for u and returns it with its expiry.
func (s *Sessions) Create(ctx context.Context, u auth.User) (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	now := s.now()
	expires := now.Add(SessionTTL)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, github_user_id, github_login, created_unix_ms, expires_unix_ms) VALUES (?, ?, ?, ?, ?)`,
		hashToken(token), u.ID, u.Login, now.UnixMilli(), expires.UnixMilli())
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expires, nil
}

// Lookup returns the member a session token belongs to.
func (s *Sessions) Lookup(ctx context.Context, token string) (auth.User, error) {
	var u auth.User
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT github_user_id, github_login, expires_unix_ms FROM sessions WHERE token_hash = ?`, hashToken(token)).Scan(&u.ID, &u.Login, &expires)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && s.now().UnixMilli() >= expires) {
		return auth.User{}, ErrUnauthenticated
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("look up session: %w", err)
	}
	return u, nil
}

// SessionRequest is the body of POST /v1/auth/session.
type SessionRequest struct {
	GitHubToken string `json:"github_token"`
}

// SessionResponse answers a successful POST /v1/auth/session.
type SessionResponse struct {
	SessionToken  string `json:"session_token"`
	GitHubLogin   string `json:"github_login"`
	ExpiresUnixMs int64  `json:"expires_unix_ms"`
}

// serveSession exchanges a GitHub token for a Crewbo session token. The
// GitHub token is used once to learn who it belongs to and is not kept.
func (s *Server) serveSession(w http.ResponseWriter, r *http.Request) {
	var req SessionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil || req.GitHubToken == "" {
		http.Error(w, "body must be JSON with github_token", http.StatusBadRequest)
		return
	}
	u, err := s.lookupUser(r.Context(), req.GitHubToken)
	switch {
	case errors.Is(err, auth.ErrBadToken):
		http.Error(w, "GitHub did not accept the token", http.StatusUnauthorized)
		return
	case err != nil:
		s.log.Warn("auth: GitHub user lookup failed", "err", err)
		http.Error(w, "could not reach GitHub", http.StatusBadGateway)
		return
	}
	token, expires, err := s.sessions.Create(r.Context(), u)
	if err != nil {
		s.log.Error("auth: create session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.log.Info("auth: signed in", "github_login", u.Login)
	w.Header().Set("Content-Type", "application/json")
	//nolint:gosec // G117: returning the new session token to its owner is the point of this endpoint
	_ = json.NewEncoder(w).Encode(SessionResponse{SessionToken: token, GitHubLogin: u.Login, ExpiresUnixMs: expires.UnixMilli()})
}
