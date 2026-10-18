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
	// Ref and Commit are set on a checkpoint or a merge.
	Ref, Commit string
}

// Fold returns a task's state as the room sees it: from dispatched, each
// status in log order that the state machine allows and that came from
// the assignee, or from the task's author (the lead who sent it) for a
// merge or a cancel (data-model.md: the lead merges; lead or teammate
// cancels). Statuses from anyone else, and impossible moves, are ignored.
// last is the status that set the state.
func Fold(assignee, author string, statuses []Status) (last Status) {
	last = Status{State: StateDispatched}
	for _, s := range statuses {
		byLead := s.Actor == author && (s.State == StateMerging || s.State == StateMerged || s.State == StateCancelled)
		if (s.Actor == assignee || byLead) && CanMove(last.State, s.State) {
			last = s
		}
	}
	return last
}
