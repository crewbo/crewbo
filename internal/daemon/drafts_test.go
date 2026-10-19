package daemon

import (
	"context"
	"strings"
	"testing"
)

// M7-07: agents draft tasks; drafts wait, are listed and dropped, and are
// bounded. Nothing here sends a task.
func TestDrafts(t *testing.T) {
	p := testPaths(t)
	start(t, Config{Paths: p})
	c, err := dial(t, p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx := context.Background()
	draft := func(to, text string) (Response, error) {
		return c.Call(ctx, Request{Method: MethodTaskDraft, Repo: "/repo/A", Assignee: to, Text: text, Patterns: []string{"src/auth/**"}})
	}
	a, err := draft("riya", "  Build the login page  ")
	if err != nil || len(a.Drafts) != 1 || a.Drafts[0].Text != "Build the login page" || a.Drafts[0].Assignee != "riya" {
		t.Fatalf("draft = %+v, %v", a, err)
	}
	if _, err := draft("dev", "Add the rate limiter"); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][2]string{"no teammate": {"", "x"}, "no text": {"riya", "  "}} {
		if _, err := draft(bad[0], bad[1]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	list, _ := c.Call(ctx, Request{Method: MethodTaskDrafts})
	if len(list.Drafts) != 2 {
		t.Fatalf("drafts = %+v", list.Drafts)
	}
	if _, err := c.Call(ctx, Request{Method: MethodTaskDraftDrop, IDs: []string{a.Drafts[0].ID}}); err != nil {
		t.Fatal(err)
	}
	list, _ = c.Call(ctx, Request{Method: MethodTaskDrafts})
	if len(list.Drafts) != 1 || list.Drafts[0].Assignee != "dev" {
		t.Fatalf("after dropping one: %+v", list.Drafts)
	}
	for range maxDrafts - 1 {
		if _, err := draft("dev", "more"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := draft("dev", "one too many"); err == nil || !strings.Contains(err.Error(), "send or drop") {
		t.Errorf("over the limit: %v", err)
	}
}
