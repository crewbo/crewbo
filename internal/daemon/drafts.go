package daemon

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/dispatch"
)

// Draft is a task an agent proposed for a teammate (M7-07). Nothing is
// sent: the room lead reviews drafts in the editor and sends each one
// through task_create, or drops it. Drafts live only in this daemon's
// memory, so a restart forgets them.
type Draft struct {
	ID       string    `json:"id"`
	Repo     string    `json:"repo"`
	Assignee string    `json:"assignee"`
	Text     string    `json:"text"`
	Patterns []string  `json:"patterns,omitempty"`
	Created  time.Time `json:"created"`
}

// maxDrafts bounds how many drafts wait, so a runaway agent cannot pile
// them up.
const maxDrafts = 20

// draft runs task_draft, task_drafts and task_draft_drop on the owner
// goroutine.
func (d *Daemon) draft(req Request, now time.Time) Response {
	switch req.Method {
	case MethodTaskDrafts:
		return Response{ID: req.ID, Drafts: slices.Clone(d.drafts)}
	case MethodTaskDraftDrop:
		d.drafts = slices.DeleteFunc(d.drafts, func(x Draft) bool { return slices.Contains(req.IDs, x.ID) })
		return Response{ID: req.ID}
	}
	text := strings.TrimSpace(req.Text)
	switch {
	case req.Repo == "" || req.Assignee == "":
		return Response{ID: req.ID, Error: "task_draft: a repository and a teammate are required"}
	case text == "" || len(text) > dispatch.MaxText:
		return Response{ID: req.ID, Error: fmt.Sprintf("task_draft: the text is 1 to %d bytes", dispatch.MaxText)}
	case len(d.drafts) >= maxDrafts:
		return Response{ID: req.ID, Error: fmt.Sprintf("task_draft: %d drafts already wait for the lead; send or drop some first", maxDrafts)}
	}
	d.draftSeq++
	x := Draft{ID: fmt.Sprintf("d%d", d.draftSeq), Repo: req.Repo, Assignee: req.Assignee, Text: text, Patterns: req.Patterns, Created: now}
	d.drafts = append(d.drafts, x)
	return Response{ID: req.ID, Drafts: []Draft{x}}
}
