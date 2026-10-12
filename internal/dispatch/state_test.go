package dispatch

import (
	"slices"
	"testing"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

func TestTransitions(t *testing.T) {
	// Every edge of the data-model.md diagram, plus Running to Finished.
	allowed := [][2]string{
		{"dispatched", "rejected"},
		{"dispatched", "expired"},
		{"dispatched", "declined"},
		{"dispatched", "approved"},
		{"approved", "failed"},
		{"approved", "running"},
		{"running", "checkpointed"},
		{"running", "failed"},
		{"running", "cancelled"},
		{"running", "finished"},
		{"checkpointed", "running"},
		{"checkpointed", "merging"},
		{"checkpointed", "cancelled"},
		{"merging", "merged"},
		{"merging", "checkpointed"},
	}
	states := []string{
		StateDispatched, StateRejected, StateExpired, StateDeclined, StateApproved, StateFailed, StateRunning,
		StateFinished, StateCheckpointed, StateMerging, StateMerged, StateCancelled,
	}
	for _, from := range states {
		for _, to := range states {
			want := slices.Contains(allowed, [2]string{from, to})
			if got := CanMove(from, to); got != want {
				t.Errorf("CanMove(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	if got := From(StateFailed); !slices.Equal(got, []string{"approved", "running"}) {
		t.Errorf("From(failed) = %v", got)
	}
	if got := From(StateDispatched); len(got) != 0 {
		t.Errorf("From(dispatched) = %v", got)
	}
	for _, s := range states {
		if StateOf(Wire(s)) != s || Wire(s) == crewbov1.TaskState_TASK_STATE_UNSPECIFIED {
			t.Errorf("%s does not round-trip: %v", s, Wire(s))
		}
	}
	if StateOf(crewbov1.TaskState(99)) != "" || StateOf(crewbov1.TaskState_TASK_STATE_UNSPECIFIED) != "" {
		t.Error("an unknown protocol state has a name")
	}
}

func TestFold(t *testing.T) {
	for name, tc := range map[string]struct {
		statuses      []Status
		state, detail string
	}{
		"nothing yet":      {nil, "dispatched", ""},
		"run to the end":   {[]Status{{"gav", "approved", ""}, {"gav", "running", ""}, {"gav", "finished", ""}}, "finished", ""},
		"declined":         {[]Status{{"gav", "declined", "busy"}}, "declined", "busy"},
		"failed run":       {[]Status{{"gav", "approved", ""}, {"gav", "failed", "exit status 1"}}, "failed", "exit status 1"},
		"not the assignee": {[]Status{{"mallory", "declined", "x"}}, "dispatched", ""},
		"impossible move":  {[]Status{{"gav", "running", ""}}, "dispatched", ""},
		"after an end":     {[]Status{{"gav", "declined", "no"}, {"gav", "approved", ""}}, "declined", "no"},
		"resent status":    {[]Status{{"gav", "approved", ""}, {"gav", "approved", ""}, {"gav", "running", ""}}, "running", ""},
	} {
		state, detail := Fold("gav", tc.statuses)
		if state != tc.state || detail != tc.detail {
			t.Errorf("%s: %s %q, want %s %q", name, state, detail, tc.state, tc.detail)
		}
	}
}
