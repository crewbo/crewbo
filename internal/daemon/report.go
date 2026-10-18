package daemon

import (
	"context"
	"database/sql"
	"errors"
	"runtime"
	"time"
)

// Report is what `crewbo report` prints for the member to paste into the
// dogfood survey (M6-03): counts and dates only. It holds no file paths,
// task text, branch names or other members' logins, and nothing is sent.
type Report struct {
	Version string        `json:"version"`
	OS      string        `json:"os"`
	Agents  []AgentReport `json:"agents"`
	Rooms   []RoomReport  `json:"rooms"`
	// Claims is how many claims are active now, in every repository.
	Claims int `json:"claims"`
	// Tasks counts this member's tasks by kind ("own", "received", "sent")
	// and state.
	Tasks map[string]map[string]int `json:"tasks"`
}

// AgentReport is one agent this daemon has seen.
type AgentReport struct {
	Agent    string    `json:"agent"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	Sessions int       `json:"sessions"`
}

// RoomReport counts one room's activity as this daemon saw it in the room
// log. Members is -1 when the server could not be asked.
type RoomReport struct {
	Members   int `json:"members"`
	Merged    int `json:"merged"`    // tasks merged into main
	Conflicts int `json:"conflicts"` // conflict warnings that named paths
	Handoffs  int `json:"handoffs"`  // handoff requests
	Approved  int `json:"approved"`  // of those, handed off
	Overrides int `json:"overrides"` // lead overrides
}

// report builds the Report on the connection goroutine.
func (d *Daemon) report(ctx context.Context, req Request) Response {
	fail := func(err error) Response { return Response{ID: req.ID, Error: "report: " + err.Error()} }
	if d.sync == nil {
		return fail(errors.New("the daemon is not ready"))
	}
	r := Report{Version: d.cfg.Version, OS: runtime.GOOS + "/" + runtime.GOARCH, Agents: []AgentReport{}, Rooms: []RoomReport{}, Tasks: map[string]map[string]int{}}

	var live map[string]time.Time
	if err := d.onOwner(ctx, func() { live, r.Claims = d.agentsSeen(), d.activeClaims(d.cfg.Now()) }); err != nil {
		return fail(err)
	}
	rows, err := d.sync.db.QueryContext(ctx, `SELECT agent, first_unix_ms, last_unix_ms, sessions FROM agent_seen ORDER BY agent`)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var a AgentReport
		var first, last int64
		if err := rows.Scan(&a.Agent, &first, &last, &a.Sessions); err != nil {
			_ = rows.Close()
			return fail(err)
		}
		a.First, a.Last = time.UnixMilli(first), time.UnixMilli(last)
		if t := live[a.Agent]; t.After(a.Last) {
			a.Last = t
		}
		r.Agents = append(r.Agents, a)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fail(err)
	}

	rows, err = d.sync.db.QueryContext(ctx, `SELECT own, state, count(*) FROM tasks WHERE task_id IS NOT NULL GROUP BY own, state`)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var own bool
		var state string
		var n int
		if err := rows.Scan(&own, &state, &n); err != nil {
			_ = rows.Close()
			return fail(err)
		}
		kind := "received"
		switch {
		case own:
			kind = "own"
		case state == taskOther:
			kind, state = "sent", "sent"
		}
		if r.Tasks[kind] == nil {
			r.Tasks[kind] = map[string]int{}
		}
		r.Tasks[kind][state] += n
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fail(err)
	}

	sess := d.sync.session()
	for _, b := range d.sync.bindings() {
		rr := RoomReport{Members: -1}
		err := d.sync.db.QueryRowContext(ctx, `SELECT
			(SELECT count(DISTINCT task_id) FROM task_status WHERE room_id = ?1 AND state = 'merged'),
			(SELECT count(*) FROM conflict_warning WHERE room_id = ?1 AND paths != '[]' AND paths != 'null'),
			(SELECT count(*) FROM handoff WHERE room_id = ?1 AND kind = 'request'),
			(SELECT count(*) FROM handoff WHERE room_id = ?1 AND kind = 'request' AND answer = 'approved'),
			(SELECT count(*) FROM handoff WHERE room_id = ?1 AND kind = 'override')`, b.RoomID).
			Scan(&rr.Merged, &rr.Conflicts, &rr.Handoffs, &rr.Approved, &rr.Overrides)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fail(err)
		}
		if keys, err := fetchRoomKeys(ctx, sess, b.RoomID); err == nil {
			rr.Members = len(keys.Members)
		}
		r.Rooms = append(r.Rooms, rr)
	}
	return Response{ID: req.ID, Report: &r}
}
