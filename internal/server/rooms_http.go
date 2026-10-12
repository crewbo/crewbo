package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/crewbo/crewbo/internal/auth"
)

// authenticate returns the member behind the request's bearer session, or
// writes 401 and returns false.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		http.Error(w, "missing session; run crewbo login", http.StatusUnauthorized)
		return auth.User{}, false
	}
	u, err := s.sessions.Lookup(r.Context(), token)
	if err != nil {
		if !errors.Is(err, ErrUnauthenticated) {
			s.log.Error("session lookup", "err", err)
		}
		http.Error(w, "session unknown or expired; run crewbo login", http.StatusUnauthorized)
		return auth.User{}, false
	}
	return u, true
}

// decode reads a small JSON body into v, or writes 400 and returns false.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(v); err != nil {
		http.Error(w, "body must be JSON", http.StatusBadRequest)
		return false
	}
	return true
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// roomError maps Rooms errors to status codes. A room the caller is not in
// answers 404, so room ids cannot be probed.
func (s *Server) roomError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrNotMember), errors.Is(err, ErrInviteUnknown):
		code = http.StatusNotFound
	case errors.Is(err, ErrNotLead):
		code = http.StatusForbidden
	case errors.Is(err, ErrRoomFull), errors.Is(err, ErrLeadMustStay):
		code = http.StatusConflict
	case errors.Is(err, ErrInviteExpired), errors.Is(err, ErrInviteUsedUp):
		code = http.StatusGone
	default:
		s.log.Error("rooms", "err", err)
		http.Error(w, "internal error", code)
		return
	}
	http.Error(w, strings.TrimPrefix(err.Error(), "server: "), code)
}

// CreateRoomRequest is the body of POST /v1/rooms.
type CreateRoomRequest struct {
	Name string `json:"name"`
}

// InviteRequest is the body of POST /v1/rooms/{id}/invites; zero values
// take the defaults (MaxMembers-1 uses, DefaultInviteTTL).
type InviteRequest struct {
	MaxUses       int   `json:"max_uses"`
	ExpiresInSecs int64 `json:"expires_in_secs"`
}

// InviteResponse answers POST /v1/rooms/{id}/invites.
type InviteResponse struct {
	Code          string `json:"code"`
	ExpiresUnixMs int64  `json:"expires_unix_ms"`
	MaxUses       int    `json:"max_uses"`
}

// JoinRequest is the body of POST /v1/rooms/join.
type JoinRequest struct {
	Code string `json:"code"`
}

func (s *Server) serveCreateRoom(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req CreateRoomRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" || len(req.Name) > 64 || !utf8.ValidString(req.Name) {
		http.Error(w, "name must be 1 to 64 bytes", http.StatusBadRequest)
		return
	}
	info, err := s.rooms.Create(r.Context(), u, req.Name)
	if err != nil {
		s.roomError(w, err)
		return
	}
	s.log.Info("rooms: created", "room_id", info.ID, "lead", u.Login)
	reply(w, info)
}

func (s *Server) serveListRooms(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	list, err := s.rooms.List(r.Context(), u)
	if err != nil {
		s.roomError(w, err)
		return
	}
	reply(w, list)
}

func (s *Server) serveInvite(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req InviteRequest
	if !decode(w, r, &req) {
		return
	}
	if req.MaxUses == 0 {
		req.MaxUses = MaxMembers - 1
	}
	ttl := DefaultInviteTTL
	if req.ExpiresInSecs != 0 {
		ttl = time.Duration(req.ExpiresInSecs) * time.Second
	}
	if req.MaxUses < 1 || req.MaxUses > MaxMembers-1 || ttl < time.Minute || ttl > MaxInviteTTL {
		http.Error(w, "max_uses must be 1 to 9 and the expiry 1 minute to 7 days", http.StatusBadRequest)
		return
	}
	code, expires, err := s.rooms.Invite(r.Context(), u, r.PathValue("id"), req.MaxUses, ttl)
	if err != nil {
		s.roomError(w, err)
		return
	}
	//nolint:gosec // G117: handing the new invite code to the lead is the point of this endpoint
	reply(w, InviteResponse{Code: code, ExpiresUnixMs: expires.UnixMilli(), MaxUses: req.MaxUses})
}

func (s *Server) serveJoin(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req JoinRequest
	if !decode(w, r, &req) {
		return
	}
	info, err := s.rooms.Join(r.Context(), u, req.Code)
	if err != nil {
		s.roomError(w, err)
		return
	}
	s.log.Info("rooms: joined", "room_id", info.ID, "github_login", u.Login)
	reply(w, info)
}

func (s *Server) serveLeave(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if err := s.rooms.Leave(r.Context(), u, r.PathValue("id")); err != nil {
		s.roomError(w, err)
		return
	}
	s.log.Info("rooms: left", "room_id", r.PathValue("id"), "github_login", u.Login)
	w.WriteHeader(http.StatusNoContent)
}
