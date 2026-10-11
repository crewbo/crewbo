package daemon

import (
	"time"

	"github.com/crewbo/crewbo/internal/claims"
)

// The local API is newline-delimited JSON over the daemon socket. The first
// line of every connection is a Hello carrying the token from the runtime
// directory; each later line is a Request answered by one Response.

// Hello authenticates a connection.
type Hello struct {
	Token string `json:"token"`
}

// Methods of the local API.
const (
	MethodDecide = "decide"
	MethodStatus = "status"
)

// Request is one local API call.
type Request struct {
	ID      int64    `json:"id"`
	Method  string   `json:"method"`
	Session string   `json:"session,omitempty"` // decide: the agent session
	Paths   []string `json:"paths,omitempty"`   // decide: repository-relative paths
}

// Response answers the Request with the same ID.
type Response struct {
	ID    int64  `json:"id"`
	Error string `json:"error,omitempty"`

	// decide
	Allow     bool          `json:"allow,omitempty"`
	Path      string        `json:"path,omitempty"`
	BlockedBy *claims.Claim `json:"blocked_by,omitempty"`

	// status
	Status *Status `json:"status,omitempty"`
}

// Status describes the running daemon.
type Status struct {
	Version      string    `json:"version"`
	PID          int       `json:"pid"`
	Started      time.Time `json:"started"`
	ActiveClaims int       `json:"active_claims"`
}
