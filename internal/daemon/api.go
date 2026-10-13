package daemon

import (
	"strings"
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
	MethodDecide       = "decide"
	MethodEvent        = "event"
	MethodStatus       = "status"
	MethodClaimAdd     = "claim_add"
	MethodClaimList    = "claim_list"
	MethodClaimRelease = "claim_release"
	MethodTaskCreate   = "task_create"
	MethodTaskList     = "task_list"
	MethodTaskApprove  = "task_approve"
	MethodTaskDecline  = "task_decline"
	MethodTaskMerge    = "task_merge"
)

// TerminalSession holds claims made with `crewbo claim add`. It is not an
// agent session, so its claims block every agent, including the user's own.
const TerminalSession = "terminal"

// IsTerminalSession reports whether a claim's session is a terminal one:
// "terminal" locally, or "<device key id>/terminal" once shared through a
// room (#84).
func IsTerminalSession(id string) bool {
	return id == TerminalSession || strings.HasSuffix(id, "/"+TerminalSession)
}

// Claim leases for claims made from the command line, which no hook event
// renews (agent claims last claims.DefaultLease and renew on every event).
const (
	DefaultCLILease = 30 * time.Minute
	MaxCLILease     = 8 * time.Hour
)

// Request is one local API call.
type Request struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`

	// decide and event: the agent session sending the hook event. Every
	// decide or event call registers the session and renews its leases.
	Session string `json:"session,omitempty"`
	Agent   string `json:"agent,omitempty"` // "claude-code", "codex"; task_approve: the agent to run (Claude Code when empty)
	Repo    string `json:"repo,omitempty"`  // repository root the agent works in
	Event   string `json:"event,omitempty"` // event: normalised hook event name

	Paths []string `json:"paths,omitempty"` // decide: repository-relative paths
	// decide for shell commands: the command line, and whether its writes
	// could not be parsed (then a literal mention of a claimed file denies).
	Command string `json:"command,omitempty"`
	Opaque  bool   `json:"opaque,omitempty"`
	// ToolUse pairs a shell decide with the post_edit event after it, for
	// the hash check of other sessions' claimed files.
	ToolUse string `json:"tool_use,omitempty"`

	// claim_add: canonical patterns for Repo, held by Session (and Member).
	Patterns []string `json:"patterns,omitempty"`
	Member   string   `json:"member,omitempty"`
	// Task is the task a claim belongs to; for task_approve and
	// task_decline, the task answered (Text is the decline reason).
	// KeepAPIKey lets an approved task's agent keep ANTHROPIC_API_KEY.
	Task         string `json:"task,omitempty"`
	LeaseSeconds int64  `json:"lease_seconds,omitempty"` // DefaultCLILease when 0
	KeepAPIKey   bool   `json:"keep_api_key,omitempty"`
	// task_merge: other tasks whose conflicting checkpoints the member
	// confirmed merging over (ADR 0015).
	Despite []string `json:"despite,omitempty"`
	// claim_release: claim ids.
	IDs []string `json:"ids,omitempty"`
	// task_create: the teammate's GitHub login, the task text and how long
	// it stays valid; Patterns are the owned paths.
	Assignee   string `json:"assignee,omitempty"`
	Text       string `json:"text,omitempty"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
	// OwnerPrefix limits claim_release to claims whose session starts with
	// it (this device's claims in a room). Set by the daemon, not clients.
	OwnerPrefix string `json:"-"`
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

	// claim_add (granted claims) and claim_list (active claims, by id)
	Claims []claims.Claim `json:"claims,omitempty"`
	// claim_release
	Released    int      `json:"released,omitempty"`
	ReleasedIDs []string `json:"released_ids,omitempty"`
	// TaskID is the id of a created task.
	TaskID string `json:"task_id,omitempty"`
	// task_list: tasks sent to this member, newest first.
	Tasks []Task `json:"tasks,omitempty"`
	// Note tells the member what happened beyond the result, for example
	// that a claim is queued for the sync server.
	Note string `json:"note,omitempty"`

	// event post_edit: claimed files the session's last shell command
	// changed although another session holds them.
	Violations []Violation `json:"violations,omitempty"`
}

// Violation is a claimed file changed by a session that does not hold it,
// detected after the fact (alert and record, no revert; ADR 0004).
type Violation struct {
	Path       string       `json:"path"`
	Claim      claims.Claim `json:"claim"`
	Session    string       `json:"session"`
	Command    string       `json:"command"`
	DetectedAt time.Time    `json:"detected_at"`
}

// Status describes the running daemon.
type Status struct {
	Version      string    `json:"version"`
	PID          int       `json:"pid"`
	Started      time.Time `json:"started"`
	ActiveClaims int       `json:"active_claims"`
	Sessions     int       `json:"sessions"`
	Violations   int       `json:"violations"`
	// Sync describes the connection to the sync server.
	Sync string `json:"sync,omitempty"`
	// Refused counts claims the sync server turned down after they were
	// granted locally (made offline, taken by someone else meanwhile).
	Refused int `json:"refused,omitempty"`
}

// Task is a task sent to this member. A rejected task has only its State,
// Detail, Room and Received set: nothing in it was trusted. Detail is the
// rejection or decline reason.
type Task struct {
	ID         string    `json:"id,omitempty"`
	Room       string    `json:"room"` // room id
	Author     string    `json:"author,omitempty"`
	Text       string    `json:"text,omitempty"`
	OwnedPaths []string  `json:"owned_paths,omitempty"`
	BaseCommit string    `json:"base_commit,omitempty"`
	Created    time.Time `json:"created,omitzero"`
	Expires    time.Time `json:"expires,omitzero"`
	State      string    `json:"state"` // a dispatch.State* name
	// Sent marks a task this member sent; State is then what its Assignee
	// reported.
	Sent     bool   `json:"sent,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	// Conflicts lists the other tasks this task's latest checkpoint
	// conflicts with, from the room's conflict warnings (M5-02).
	Conflicts []Conflict `json:"conflicts,omitempty"`
	Detail    string     `json:"detail,omitempty"`
	Received  time.Time  `json:"received"`
}

// Conflict is another task whose checkpoint conflicts with a task's, and
// the conflicting paths.
type Conflict struct {
	Task  string   `json:"task"`
	Paths []string `json:"paths"`
}

// Session is an agent session the daemon has heard from.
type Session struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	Repo      string    `json:"repo"`
	LastSeen  time.Time `json:"last_seen"`
	LastEvent string    `json:"last_event"`
}
