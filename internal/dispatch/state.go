package dispatch

import (
	"slices"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// Task states as daemons store and list them, following the task state
// machine in data-model.md (plus finished, until checkpoints in M5-01).
const (
	StateDispatched   = "dispatched"
	StateRejected     = "rejected"
	StateExpired      = "expired"
	StateDeclined     = "declined"
	StateApproved     = "approved"
	StateFailed       = "failed"
	StateRunning      = "running"
	StateFinished     = "finished"
	StateCheckpointed = "checkpointed"
	StateMerging      = "merging"
	StateMerged       = "merged"
	StateCancelled    = "cancelled"
)

// transitions is every allowed move (data-model.md, task state machine).
var transitions = map[string][]string{
	StateDispatched:   {StateRejected, StateExpired, StateDeclined, StateApproved},
	StateApproved:     {StateFailed, StateRunning},
	StateRunning:      {StateCheckpointed, StateFailed, StateCancelled, StateFinished},
	StateCheckpointed: {StateRunning, StateMerging, StateCancelled},
	StateMerging:      {StateMerged, StateCheckpointed},
}

// CanMove reports whether a task may go from one state to another.
func CanMove(from, to string) bool { return slices.Contains(transitions[from], to) }

// From returns the states a task may reach state to from.
func From(to string) []string {
	var out []string
	for from, tos := range transitions {
		if slices.Contains(tos, to) {
			out = append(out, from)
		}
	}
	slices.Sort(out)
	return out
}

var wire = map[string]crewbov1.TaskState{
	StateDispatched: crewbov1.TaskState_TASK_STATE_DISPATCHED, StateRejected: crewbov1.TaskState_TASK_STATE_REJECTED,
	StateExpired: crewbov1.TaskState_TASK_STATE_EXPIRED, StateDeclined: crewbov1.TaskState_TASK_STATE_DECLINED,
	StateApproved: crewbov1.TaskState_TASK_STATE_APPROVED, StateFailed: crewbov1.TaskState_TASK_STATE_FAILED,
	StateRunning: crewbov1.TaskState_TASK_STATE_RUNNING, StateFinished: crewbov1.TaskState_TASK_STATE_FINISHED,
	StateCheckpointed: crewbov1.TaskState_TASK_STATE_CHECKPOINTED, StateMerging: crewbov1.TaskState_TASK_STATE_MERGING,
	StateMerged: crewbov1.TaskState_TASK_STATE_MERGED, StateCancelled: crewbov1.TaskState_TASK_STATE_CANCELLED,
}

// Wire returns the protocol value of a state.
func Wire(state string) crewbov1.TaskState { return wire[state] }

// StateOf returns the name of a protocol state, or "" for one this build
// does not know.
func StateOf(s crewbov1.TaskState) string {
	for name, w := range wire {
		if w == s {
			return name
		}
	}
	return ""
}

// Status is one task status from the room log.
type Status struct {
	Actor  string // GitHub login of the member who sent it
	State  string
	Detail string
}

// Fold returns a task's state as its lead sees it: from dispatched, each
// status the assignee sent, in log order, that the state machine allows.
// Statuses from anyone else, and impossible moves, are ignored.
func Fold(assignee string, statuses []Status) (state, detail string) {
	state = StateDispatched
	for _, s := range statuses {
		if s.Actor == assignee && CanMove(state, s.State) {
			state, detail = s.State, s.Detail
		}
	}
	return state, detail
}
