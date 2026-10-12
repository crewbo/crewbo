package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
)

// MaxMembers is the room size limit for v1 (PRD non-functional
// requirements).
const MaxMembers = 10

// Invite defaults (decision log: 24 h, hackathon length).
const (
	DefaultInviteTTL = 24 * time.Hour
	MaxInviteTTL     = 7 * 24 * time.Hour
)

// Errors from Rooms that handlers turn into status codes.
var (
	ErrNotMember     = errors.New("server: not a member of this room")
	ErrNotLead       = errors.New("server: only the room lead can do this")
	ErrRoomFull      = fmt.Errorf("server: the room already has %d members", MaxMembers)
	ErrInviteUnknown = errors.New("server: unknown invite code")
	ErrInviteExpired = errors.New("server: the invite has expired")
	ErrInviteUsedUp  = errors.New("server: the invite has been used up")
	ErrLeadMustStay  = errors.New("server: the lead cannot leave while others are in the room")
)

// Rooms keeps rooms, their members and invites, in a database opened with
// store.Server. Every change runs in one transaction, and the server's
// single database connection serializes them, so the member cap and invite
// use limits hold under concurrent requests.
type Rooms struct {
	db  *sql.DB
	now func() time.Time
}

// NewRooms returns Rooms over db.
func NewRooms(db *sql.DB, now func() time.Time) *Rooms { return &Rooms{db: db, now: now} }

// RoomInfo describes a room to one of its members.
type RoomInfo struct {
	ID        string `json:"room_id"`
	Name      string `json:"name"`
	Role      string `json:"role"` // the caller's role: lead or member
	LeadLogin string `json:"lead_login"`
	Members   int    `json:"members"`
}

func randomID(prefix string, n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// Create makes a room with u as its lead.
func (rs *Rooms) Create(ctx context.Context, u auth.User, name string) (RoomInfo, error) {
	id, err := randomID("r_", 12)
	if err != nil {
		return RoomInfo{}, err
	}
	now := rs.now().UnixMilli()
	err = rs.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO rooms (id, name, created_unix_ms) VALUES (?, ?, ?)`, id, name, now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO members (room_id, github_user_id, github_login, role, joined_unix_ms) VALUES (?, ?, ?, 'lead', ?)`, id, u.ID, u.Login, now)
		return err
	})
	if err != nil {
		return RoomInfo{}, fmt.Errorf("create room: %w", err)
	}
	return RoomInfo{ID: id, Name: name, Role: "lead", LeadLogin: u.Login, Members: 1}, nil
}

// Invite issues an invite code for roomID; only the lead may. The code is
// 128 random bits; only its hash is stored.
func (rs *Rooms) Invite(ctx context.Context, u auth.User, roomID string, maxUses int, ttl time.Duration) (string, time.Time, error) {
	code, err := randomID("cbi_", 16)
	if err != nil {
		return "", time.Time{}, err
	}
	now := rs.now()
	expires := now.Add(ttl)
	err = rs.tx(ctx, func(tx *sql.Tx) error {
		role, err := memberRole(ctx, tx, roomID, u.ID)
		if err != nil {
			return err
		}
		if role != "lead" {
			return ErrNotLead
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO invites (code_hash, room_id, created_by, created_unix_ms, expires_unix_ms, max_uses) VALUES (?, ?, ?, ?, ?, ?)`,
			hashToken(code), roomID, u.ID, now.UnixMilli(), expires.UnixMilli(), maxUses)
		return err
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// Join adds u to the room an invite code belongs to. Joining a room u is
// already in succeeds without using the invite up.
func (rs *Rooms) Join(ctx context.Context, u auth.User, code string) (RoomInfo, error) {
	var roomID string
	err := rs.tx(ctx, func(tx *sql.Tx) error {
		var expires int64
		var uses, maxUses int
		err := tx.QueryRowContext(ctx, `SELECT room_id, expires_unix_ms, uses, max_uses FROM invites WHERE code_hash = ?`, hashToken(code)).Scan(&roomID, &expires, &uses, &maxUses)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrInviteUnknown
		case err != nil:
			return err
		}
		if _, err := memberRole(ctx, tx, roomID, u.ID); err == nil {
			return nil // already a member
		} else if !errors.Is(err, ErrNotMember) {
			return err
		}
		switch {
		case rs.now().UnixMilli() >= expires:
			return ErrInviteExpired
		case uses >= maxUses:
			return ErrInviteUsedUp
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM members WHERE room_id = ? AND left_unix_ms IS NULL`, roomID).Scan(&n); err != nil {
			return err
		}
		if n >= MaxMembers {
			return ErrRoomFull
		}
		if _, err := tx.ExecContext(ctx, `UPDATE invites SET uses = uses + 1 WHERE code_hash = ?`, hashToken(code)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO members (room_id, github_user_id, github_login, role, joined_unix_ms) VALUES (?, ?, ?, 'member', ?)`,
			roomID, u.ID, u.Login, rs.now().UnixMilli())
		return err
	})
	if err != nil {
		return RoomInfo{}, err
	}
	return rs.info(ctx, u, roomID)
}

// Leave removes u from the room. The lead can leave only as the last
// member, since v1 has no lead transfer yet.
func (rs *Rooms) Leave(ctx context.Context, u auth.User, roomID string) error {
	return rs.tx(ctx, func(tx *sql.Tx) error {
		role, err := memberRole(ctx, tx, roomID, u.ID)
		if err != nil {
			return err
		}
		if role == "lead" {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM members WHERE room_id = ? AND left_unix_ms IS NULL`, roomID).Scan(&n); err != nil {
				return err
			}
			if n > 1 {
				return ErrLeadMustStay
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE members SET left_unix_ms = ? WHERE room_id = ? AND github_user_id = ? AND left_unix_ms IS NULL`, rs.now().UnixMilli(), roomID, u.ID)
		return err
	})
}

// IsMember reports whether the account is an active member of the room.
func (rs *Rooms) IsMember(ctx context.Context, roomID string, userID int64) (bool, error) {
	var n int
	err := rs.db.QueryRowContext(ctx, `SELECT count(*) FROM members WHERE room_id = ? AND github_user_id = ? AND left_unix_ms IS NULL`, roomID, userID).Scan(&n)
	return n > 0, err
}

// List returns the rooms u is a member of.
func (rs *Rooms) List(ctx context.Context, u auth.User) ([]RoomInfo, error) {
	rows, err := rs.db.QueryContext(ctx, `SELECT room_id FROM members WHERE github_user_id = ? AND left_unix_ms IS NULL ORDER BY joined_unix_ms`, u.ID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := []RoomInfo{}
	for _, id := range ids {
		info, err := rs.info(ctx, u, id)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

func (rs *Rooms) info(ctx context.Context, u auth.User, roomID string) (RoomInfo, error) {
	info := RoomInfo{ID: roomID}
	err := rs.db.QueryRowContext(ctx, `
		SELECT r.name,
		       (SELECT role FROM members WHERE room_id = r.id AND github_user_id = ? AND left_unix_ms IS NULL),
		       (SELECT github_login FROM members WHERE room_id = r.id AND role = 'lead' AND left_unix_ms IS NULL),
		       (SELECT count(*) FROM members WHERE room_id = r.id AND left_unix_ms IS NULL)
		FROM rooms r WHERE r.id = ?`, u.ID, roomID).Scan(&info.Name, &info.Role, &info.LeadLogin, &info.Members)
	if err != nil {
		return RoomInfo{}, fmt.Errorf("room %s: %w", roomID, err)
	}
	return info, nil
}

func memberRole(ctx context.Context, tx *sql.Tx, roomID string, userID int64) (string, error) {
	var role string
	err := tx.QueryRowContext(ctx, `SELECT role FROM members WHERE room_id = ? AND github_user_id = ? AND left_unix_ms IS NULL`, roomID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotMember
	}
	return role, err
}

func (rs *Rooms) tx(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := rs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}
