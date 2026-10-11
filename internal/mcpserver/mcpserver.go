// Package mcpserver serves Crewbo's read-only MCP tools over stdio
// (`crewbo mcp`): agents can ask which files are claimed before they plan
// work. MCP is a query surface only; enforcement stays in the hooks
// (ADR 0004). Tools are plain Go structs with the official SDK
// (github.com/modelcontextprotocol/go-sdk, ADR 0007).
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

// New returns the MCP server with Crewbo's read-only tools.
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
	return s
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
	case c.Holder.SessionID == daemon.TerminalSession && held != "":
		held += " (from a terminal)"
	case held == "":
		held = "agent session " + c.Holder.SessionID
	default:
		held += "'s agent"
	}
	return Claim{ID: c.ID, Pattern: c.Pattern, HeldBy: held, Task: c.TaskID, Until: c.LeaseExpires}
}
