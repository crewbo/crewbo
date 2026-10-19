// Package mcpserver serves Crewbo's MCP tools over stdio (`crewbo mcp`):
// agents can ask which files are claimed and who is in the crew before
// they plan work, and draft tasks for teammates. MCP is a query and draft
// surface only: enforcement stays in the hooks (ADR 0004), and a draft is
// sent only by the room lead, from the editor (M7-07). Tools are plain Go
// structs with the official SDK (github.com/modelcontextprotocol/go-sdk,
// ADR 0007).
package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
	"github.com/crewbo/crewbo/internal/platform"
)

// Config tells the server where the daemon is and which repository the
// agent works in (the MCP server's working directory).
type Config struct {
	Paths   platform.Paths
	Cwd     string
	Version string
}

// Claim is one active claim as the tools report it.
type Claim struct {
	ID      string    `json:"id"`
	Pattern string    `json:"pattern" jsonschema:"repository-relative path or glob"`
	HeldBy  string    `json:"held_by" jsonschema:"teammate, or agent session when no teammate is known"`
	Task    string    `json:"task,omitempty"`
	Until   time.Time `json:"until" jsonschema:"when the claim lapses unless renewed"`
}

// ClaimsOut is the result of crewbo_claims.
type ClaimsOut struct {
	Repo   string  `json:"repo"`
	Claims []Claim `json:"claims"`
}

// WhoHoldsIn is the input of crewbo_who_holds.
type WhoHoldsIn struct {
	Paths []string `json:"paths" jsonschema:"file paths to check, relative to the repository root or absolute"`
}

// PathHolder answers crewbo_who_holds for one path.
type PathHolder struct {
	Path    string `json:"path"`
	Claimed bool   `json:"claimed"`
	Claim   *Claim `json:"claim,omitempty"`
}

// WhoHoldsOut is the result of crewbo_who_holds.
type WhoHoldsOut struct {
	Results []PathHolder `json:"results"`
}

// CrewOut is the result of crewbo_crew.
type CrewOut struct {
	Room    string              `json:"room"`
	Members []daemon.CrewMember `json:"members" jsonschema:"each member: online or not, their agents' latest activity, the files they hold"`
}

// DraftIn is the input of crewbo_draft_task.
type DraftIn struct {
	To    string   `json:"to" jsonschema:"the teammate's GitHub login, from crewbo_crew"`
	Text  string   `json:"text" jsonschema:"the task for their agent, complete enough to act on without asking back"`
	Paths []string `json:"paths,omitempty" jsonschema:"repository-relative files or globs the task may change; their agent gets claims on them"`
}

// DraftOut is the result of crewbo_draft_task.
type DraftOut struct {
	Draft daemon.Draft `json:"draft"`
	Note  string       `json:"note"`
}

// New returns the MCP server with Crewbo's tools.
func New(cfg Config) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "crewbo", Version: cfg.Version}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "crewbo_claims",
		Description: "List the files and globs teammates' agents have claimed in this repository. Edits to files claimed by others are blocked; check before planning changes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ClaimsOut, error) {
		repo, active, err := list(ctx, cfg)
		if err != nil {
			return nil, ClaimsOut{}, err
		}
		out := ClaimsOut{Repo: repo, Claims: []Claim{}}
		for _, c := range active {
			out.Claims = append(out.Claims, view(c))
		}
		return nil, out, nil
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "crewbo_who_holds",
		Description: "For each given path, say whether a teammate's agent has claimed it and who. Use before editing files you are unsure about.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in WhoHoldsIn) (*mcp.CallToolResult, WhoHoldsOut, error) {
		repo, active, err := list(ctx, cfg)
		if err != nil {
			return nil, WhoHoldsOut{}, err
		}
		out := WhoHoldsOut{Results: []PathHolder{}}
		for _, p := range in.Paths {
			if !filepath.IsAbs(p) {
				p = filepath.Join(repo, p)
			}
			rel, err := claims.Normalize(repo, p)
			if err != nil {
				out.Results = append(out.Results, PathHolder{Path: p})
				continue
			}
			r := PathHolder{Path: rel}
			for _, c := range active {
				if claims.Overlaps(rel, c.Pattern) {
					v := view(c)
					r.Claimed, r.Claim = true, &v
					break
				}
			}
			out.Results = append(out.Results, r)
		}
		return nil, out, nil
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "crewbo_crew",
		Description: "List the members of this repository's room: who is online, what their agents are doing, and which files they hold. Use it to split work around what teammates are already doing.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, CrewOut, error) {
		_, resp, err := ask(ctx, cfg, daemon.Request{Method: daemon.MethodCrew})
		if err != nil {
			return nil, CrewOut{}, err
		}
		return nil, CrewOut{Room: resp.RoomName, Members: resp.Crew}, nil
	})
	mcp.AddTool(s, &mcp.Tool{
		Name: "crewbo_draft_task",
		Description: "Draft a task for one teammate's agent. It is NOT sent: the room lead reviews every draft in the editor, may edit or drop it, and sends it; the teammate then reads and approves it themselves. " +
			"Use crewbo_crew first, give each teammate a separate piece of work with its own paths, and avoid files others hold.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DraftIn) (*mcp.CallToolResult, DraftOut, error) {
		_, resp, err := ask(ctx, cfg, daemon.Request{Method: daemon.MethodTaskDraft, Assignee: in.To, Text: in.Text, Patterns: in.Paths})
		if err != nil {
			return nil, DraftOut{}, err
		}
		return nil, DraftOut{Draft: resp.Drafts[0], Note: "Drafted, not sent. The room lead reviews it in the Crewbo view before it goes to " + in.To + "."}, nil
	})
	return s
}

// ask sends req to the daemon for the agent's repository.
func ask(ctx context.Context, cfg Config, req daemon.Request) (string, daemon.Response, error) {
	repo, ok := hooks.RepoRoot(cfg.Cwd)
	if !ok {
		return "", daemon.Response{}, errors.New("crewbo: this agent is not working inside a git repository")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := daemon.Dial(ctx, cfg.Paths)
	if err != nil {
		return "", daemon.Response{}, errors.New("crewbo: the Crewbo daemon is not running; start it with `crewbo daemon`")
	}
	defer func() { _ = c.Close() }()
	req.Repo = repo
	resp, err := c.Call(ctx, req)
	return repo, resp, err
}

// list returns the repository root and its active claims from the daemon.
func list(ctx context.Context, cfg Config) (string, []claims.Claim, error) {
	repo, ok := hooks.RepoRoot(cfg.Cwd)
	if !ok {
		return "", nil, errors.New("crewbo: this agent is not working inside a git repository")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c, err := daemon.Dial(ctx, cfg.Paths)
	if err != nil {
		return "", nil, errors.New("crewbo: the Crewbo daemon is not running, so claims are unknown; start it with `crewbo daemon`")
	}
	defer func() { _ = c.Close() }()
	resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodClaimList, Repo: repo})
	if err != nil {
		return "", nil, err
	}
	return repo, resp.Claims, nil
}

func view(c claims.Claim) Claim {
	held := c.Holder.MemberID
	switch {
	case daemon.IsTerminalSession(c.Holder.SessionID) && held != "":
		held += " (from a terminal)"
	case held == "":
		held = "agent session " + c.Holder.SessionID
	default:
		held += "'s agent"
	}
	return Claim{ID: c.ID, Pattern: c.Pattern, HeldBy: held, Task: c.TaskID, Until: c.LeaseExpires}
}
