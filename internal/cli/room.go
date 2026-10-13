package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/hooks"
	"github.com/crewbo/crewbo/internal/server"
)

const roomUsage = `Usage:
  crewbo room create [--name NAME]
  crewbo room invite [--uses N] [--for DURATION]
  crewbo room join CODE
  crewbo room leave
  crewbo room status
  crewbo room set --merge lead-only|members

Run inside a git repository. A room shares this repository's claims with
your teammates; each teammate joins from their own clone. Sign in first
with crewbo login.

set --merge (lead only) chooses who may merge tasks into main with crewbo
task merge: members, each their own task (the default), or lead-only. It
is a Crewbo rule, not a lock: plain git push still works unless the
repository protects its main branch.
`

// roomCmd holds what the room subcommands need, so tests can supply it.
type roomCmd struct {
	http      *http.Client
	configDir string
	cwd       string
	stdout    io.Writer
	stderr    io.Writer
}

func runRoom(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	rc := roomCmd{http: &http.Client{Timeout: 30 * time.Second}, configDir: p.Config, cwd: cwd, stdout: stdout, stderr: stderr}
	return rc.run(ctx, args)
}

func (rc roomCmd) run(ctx context.Context, args []string) int {
	if len(args) == 0 || !map[string]bool{"create": true, "invite": true, "join": true, "leave": true, "status": true, "set": true}[args[0]] {
		return write(rc.stderr, ExitUsage, "%s", roomUsage)
	}
	fs := flag.NewFlagSet("room "+args[0], flag.ContinueOnError)
	fs.SetOutput(rc.stderr)
	name := fs.String("name", "", "room name (default: the repository folder name)")
	uses := fs.Int("uses", 0, "how many people the invite admits (default 9)")
	ttl := fs.Duration("for", 0, "how long the invite is valid (default 24h)")
	merge := fs.String("merge", "", "set: who may merge tasks, members or lead-only")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage
	}
	repo, ok := hooks.RepoRoot(rc.cwd)
	if !ok {
		return write(rc.stderr, ExitError, "crewbo: run this inside a git repository\n")
	}
	sess, err := auth.LoadSession(auth.SessionPath(rc.configDir))
	if err != nil {
		return write(rc.stderr, ExitError, "crewbo: %v\n", err)
	}
	bindingsPath := auth.BindingsPath(rc.configDir)
	bindings, err := auth.LoadBindings(bindingsPath)
	if err != nil {
		return write(rc.stderr, ExitError, "crewbo: %v\n", err)
	}
	bound, isBound := auth.FindBinding(bindings, repo)
	if isBound && bound.Server != sess.Server && args[0] != "status" {
		return write(rc.stderr, ExitError, "crewbo: this repository's room %s is on %s, but you are signed in to %s\n", bound.Name, bound.Server, sess.Server)
	}
	call := func(method, path string, in, out any) error {
		return callServer(ctx, rc.http, sess.Server, sess.SessionToken, method, path, in, out)
	}
	bind := func(info server.RoomInfo) error {
		return auth.SaveBindings(bindingsPath, append(bindings, auth.Binding{Repo: repo, Server: sess.Server, RoomID: info.ID, Name: info.Name}))
	}

	switch {
	case args[0] == "create" && fs.NArg() == 0:
		if isBound {
			return write(rc.stderr, ExitError, "crewbo: this repository is already in room %s; run crewbo room leave first\n", bound.Name)
		}
		if *name == "" {
			*name = filepath.Base(repo)
		}
		var info server.RoomInfo
		if err := call(http.MethodPost, "/v1/rooms", server.CreateRoomRequest{Name: *name}, &info); err != nil {
			return write(rc.stderr, ExitError, "crewbo: %v\n", err)
		}
		if err := bind(info); err != nil {
			return write(rc.stderr, ExitError, "crewbo: room %s was created, but saving it failed: %v\n", info.ID, err)
		}
		return write(rc.stdout, ExitOK, "Created room %s for %s. You are its lead.\nInvite teammates with: crewbo room invite\n", info.Name, repo)

	case args[0] == "invite" && fs.NArg() == 0:
		if !isBound {
			return write(rc.stderr, ExitError, "crewbo: this repository is not in a room; run crewbo room create or crewbo room join first\n")
		}
		var inv server.InviteResponse
		req := server.InviteRequest{MaxUses: *uses, ExpiresInSecs: int64(ttl.Seconds())}
		if err := call(http.MethodPost, "/v1/rooms/"+bound.RoomID+"/invites", req, &inv); err != nil {
			return write(rc.stderr, ExitError, "crewbo: %v\n", err)
		}
		return write(rc.stdout, ExitOK, "Invite for room %s, valid until %s for up to %d people:\n\n  crewbo room join %s\n\nTeammates run that inside their own clone of this repository, after crewbo login --server %s\n",
			bound.Name, time.UnixMilli(inv.ExpiresUnixMs).Format("2006-01-02 15:04"), inv.MaxUses, inv.Code, sess.Server)

	case args[0] == "join" && fs.NArg() == 1:
		if isBound {
			return write(rc.stderr, ExitError, "crewbo: this repository is already in room %s; run crewbo room leave first\n", bound.Name)
		}
		var info server.RoomInfo
		if err := call(http.MethodPost, "/v1/rooms/join", server.JoinRequest{Code: fs.Arg(0)}, &info); err != nil {
			return write(rc.stderr, ExitError, "crewbo: %v\n", err)
		}
		if err := bind(info); err != nil {
			return write(rc.stderr, ExitError, "crewbo: joined room %s, but saving it failed: %v\n", info.ID, err)
		}
		return write(rc.stdout, ExitOK, "Joined room %s (lead: %s, %d members) for %s.\n", info.Name, info.LeadLogin, info.Members, repo)

	case args[0] == "leave" && fs.NArg() == 0:
		if !isBound {
			return write(rc.stderr, ExitError, "crewbo: this repository is not in a room\n")
		}
		err := call(http.MethodPost, "/v1/rooms/"+bound.RoomID+"/leave", nil, nil)
		var se *serverError
		// A room the server no longer has us in is left locally too.
		alreadyOut := errors.As(err, &se) && se.Status == http.StatusNotFound
		if err != nil && !alreadyOut {
			return write(rc.stderr, ExitError, "crewbo: %v\n", err)
		}
		kept := bindings[:0]
		for _, b := range bindings {
			if b.Repo != repo {
				kept = append(kept, b)
			}
		}
		if err := auth.SaveBindings(bindingsPath, kept); err != nil {
			return write(rc.stderr, ExitError, "crewbo: %v\n", err)
		}
		return write(rc.stdout, ExitOK, "Left room %s. Claims in %s are local only again.\n", bound.Name, repo)

	case args[0] == "status" && fs.NArg() == 0:
		if !isBound {
			return write(rc.stdout, ExitOK, "%s is not in a room. Signed in to %s as %s.\n", repo, sess.Server, sess.GitHubLogin)
		}
		line := fmt.Sprintf("%s is in room %s (%s) on %s. Signed in as %s.\n", repo, bound.Name, bound.RoomID, bound.Server, sess.GitHubLogin)
		var rooms []server.RoomInfo
		if bound.Server == sess.Server && call(http.MethodGet, "/v1/rooms", nil, &rooms) == nil {
			for _, r := range rooms {
				if r.ID == bound.RoomID {
					line += "Tasks are merged by " + mergedBy(r.MergeMode) + ".\n"
				}
			}
		}
		return write(rc.stdout, ExitOK, "%s", line)

	case args[0] == "set" && fs.NArg() == 0 && *merge != "":
		if !isBound {
			return write(rc.stderr, ExitError, "crewbo: this repository is not in a room\n")
		}
		mode := map[string]string{"members": server.MergeMembers, "lead-only": server.MergeLead}[*merge]
		if mode == "" {
			return write(rc.stderr, ExitUsage, "crewbo: --merge must be members or lead-only\n")
		}
		var info server.RoomInfo
		if err := call(http.MethodPost, "/v1/rooms/"+bound.RoomID+"/settings", server.SettingsRequest{MergeMode: mode}, &info); err != nil {
			return write(rc.stderr, ExitError, "crewbo: %v\n", err)
		}
		return write(rc.stdout, ExitOK, "In room %s, tasks are now merged by %s.\n", info.Name, mergedBy(info.MergeMode))
	}
	return write(rc.stderr, ExitUsage, "%s", roomUsage)
}

// mergedBy describes a room merge mode.
func mergedBy(mode string) string {
	if mode == server.MergeLead {
		return "the lead only"
	}
	return "their members"
}
