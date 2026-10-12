// Package dispatch signs the tasks a room lead sends to a teammate's agent
// and verifies them on the teammate's machine before anything is shown or
// run (ADR 0006).
package dispatch

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/keys"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// Limits from data-model.md and protocol.md.
const (
	MaxText       = 32 << 10
	MaxOwnedPaths = 200
	// ClockSkew is how far a task's creation time may be ahead of the
	// verifier's clock.
	ClockSkew = 5 * time.Minute
)

// Verification failures. Callers record the reason as a REJECTED task
// status; none of them shows the task to the member.
var (
	ErrUnknownKey    = errors.New("dispatch: signed by a key this room does not know")
	ErrRevokedKey    = errors.New("dispatch: signed by a revoked key")
	ErrNotLead       = errors.New("dispatch: not signed by the room lead")
	ErrBadSignature  = errors.New("dispatch: the signature does not match the task")
	ErrMalformed     = errors.New("dispatch: the signed task cannot be read")
	ErrMismatch      = errors.New("dispatch: the task names a different author than its signature")
	ErrWrongRoom     = errors.New("dispatch: the task is for another room")
	ErrNotForYou     = errors.New("dispatch: the task is for another member")
	ErrExpired       = errors.New("dispatch: the task has expired")
	ErrNotYetValid   = errors.New("dispatch: the task is dated in the future")
	ErrReplayed      = errors.New("dispatch: this task was already received")
	ErrInvalidFields = errors.New("dispatch: the task is not well formed")
)

// NewTaskID returns a UUIDv7 task id (data-model.md).
func NewTaskID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// Sign serializes body once and signs exactly those bytes with key. The
// body's author key must be key.
func Sign(key keys.Key, body *crewbov1.TaskBody) (*crewbov1.SignedTask, error) {
	if body.GetAuthorKeyId() != key.ID() {
		return nil, fmt.Errorf("%w: author key %q is not the signing key %q", ErrMismatch, body.GetAuthorKeyId(), key.ID())
	}
	b, err := proto.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &crewbov1.SignedTask{Body: b, Signature: ed25519.Sign(key.Private, b), KeyId: key.ID()}, nil
}

// Key is a registered device key as the verifier knows it.
type Key struct {
	Member  string // GitHub login of the key's owner
	Public  ed25519.PublicKey
	Revoked bool
}

// Check is everything a verifier needs besides the task.
type Check struct {
	RoomID string
	Lead   string         // the room's current lead (GitHub login)
	Keys   map[string]Key // key id to key, for the room's members
	Me     string         // this member's GitHub login
	Now    time.Time
	// Seen reports whether a task id was received before.
	Seen func(taskID string) bool
}

// Verify checks st and returns its body. The signature is checked over the
// received bytes before they are decoded; nothing is trusted until every
// check passes.
func Verify(st *crewbov1.SignedTask, c Check) (*crewbov1.TaskBody, error) {
	k, ok := c.Keys[st.GetKeyId()]
	switch {
	case !ok:
		return nil, ErrUnknownKey
	case k.Revoked:
		return nil, ErrRevokedKey
	case k.Member != c.Lead:
		return nil, ErrNotLead
	case keys.KeyID(k.Public) != st.GetKeyId():
		return nil, ErrUnknownKey // the registry entry does not match its id
	case !ed25519.Verify(k.Public, st.GetBody(), st.GetSignature()):
		return nil, ErrBadSignature
	}
	body := &crewbov1.TaskBody{}
	if err := proto.Unmarshal(st.GetBody(), body); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	now := c.Now.UnixMilli()
	switch {
	case body.GetAuthorKeyId() != st.GetKeyId() || body.GetAuthorMemberId() != k.Member:
		return nil, ErrMismatch
	case body.GetRoomId() != c.RoomID:
		return nil, ErrWrongRoom
	case body.GetAssigneeMemberId() != c.Me:
		return nil, ErrNotForYou
	case now >= body.GetExpiresUnixMs():
		return nil, ErrExpired
	case body.GetCreatedUnixMs() > now+ClockSkew.Milliseconds():
		return nil, ErrNotYetValid
	}
	if err := wellFormed(body); err != nil {
		return nil, err
	}
	if c.Seen != nil && c.Seen(body.GetTaskId()) {
		return nil, ErrReplayed
	}
	return body, nil
}

func wellFormed(b *crewbov1.TaskBody) error {
	if _, err := uuid.Parse(b.GetTaskId()); err != nil {
		return fmt.Errorf("%w: task id: %w", ErrInvalidFields, err)
	}
	// Protobuf already refused text that is not UTF-8 while decoding.
	if b.GetText() == "" || len(b.GetText()) > MaxText {
		return fmt.Errorf("%w: text must be 1 byte to %d KiB", ErrInvalidFields, MaxText>>10)
	}
	if len(b.GetOwnedPaths()) > MaxOwnedPaths {
		return fmt.Errorf("%w: at most %d owned paths", ErrInvalidFields, MaxOwnedPaths)
	}
	for _, p := range b.GetOwnedPaths() {
		if err := claims.ValidatePattern(p); err != nil {
			return fmt.Errorf("%w: owned path: %w", ErrInvalidFields, err)
		}
	}
	return nil
}
