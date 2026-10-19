package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// crew keeps what rooms share live (M7-01): which members are connected
// and what their agents are doing. The syncer goroutine fills it and the
// local API reads it, so it has its own lock. Nothing is stored: a new
// daemon learns it again from the server.
type crew struct {
	mu     sync.Mutex
	online map[string][]string             // room id to connected members
	agents map[string]map[string]agentSeen // room id to session id
	// left is when each member was last seen connected, by room, and
	// members the room's member list as last fetched.
	left    map[string]map[string]time.Time
	members map[string]roomMembers
}

// roomMembers is a room's members and lead from the room keys.
type roomMembers struct {
	lead   string
	logins []string
	at     time.Time
}

// agentSeen is the latest activity of one agent session in a room.
type agentSeen struct {
	Member, Agent, Path string
	Idle                bool
	At                  time.Time
}

func newCrew() *crew {
	return &crew{
		online: map[string][]string{}, agents: map[string]map[string]agentSeen{},
		left: map[string]map[string]time.Time{}, members: map[string]roomMembers{},
	}
}

// presence records a room's connected members, and when those who just
// left were last connected, at local time now.
func (c *crew) presence(p *crewbov1.Presence, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := p.GetRoomId()
	if c.left[room] == nil {
		c.left[room] = map[string]time.Time{}
	}
	for _, m := range c.online[room] {
		if !slices.Contains(p.GetOnlineMemberIds(), m) {
			c.left[room][m] = now
		}
	}
	c.online[room] = p.GetOnlineMemberIds()
}

// disconnected forgets who was online in every room when this daemon's
// connection ends: it can no longer tell, so they show as last seen now.
func (c *crew) disconnected(now time.Time) {
	c.mu.Lock()
	rooms := slices.Collect(maps.Keys(c.online))
	c.mu.Unlock()
	for _, room := range rooms {
		c.presence(&crewbov1.Presence{RoomId: room}, now)
	}
}

// activity records an agent's activity, at local time now.
func (c *crew) activity(a *crewbov1.Activity, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.agents[a.GetRoomId()]
	if room == nil {
		room = map[string]agentSeen{}
		c.agents[a.GetRoomId()] = room
	}
	room[a.GetSessionId()] = agentSeen{Member: a.GetMemberId(), Agent: a.GetAgent(), Path: a.GetPath(), Idle: a.GetIdle(), At: now}
}

// CrewMember is one member of a room as the crew panel shows them (M7-02).
type CrewMember struct {
	Login  string `json:"login"`
	You    bool   `json:"you,omitempty"`
	Lead   bool   `json:"lead,omitempty"`
	Online bool   `json:"online"`
	// OfflineSince is when this daemon last saw the member connected; zero
	// when it has not since it started.
	OfflineSince time.Time `json:"offline_since,omitzero"`
	// Agents are the member's agents seen in the last hour while the member
	// is online, most recent first.
	Agents []CrewAgent `json:"agents,omitempty"`
	// Claims are the patterns the member holds in this repository.
	Claims []string `json:"claims,omitempty"`
}

// CrewAgent is the latest activity of one of a member's agents.
type CrewAgent struct {
	Agent string    `json:"agent"`
	Path  string    `json:"path,omitempty"`
	Idle  bool      `json:"idle"`
	At    time.Time `json:"at"`
}

// membersStale is how long a room's fetched member list is used before
// the crew view fetches it again; the panel asks every 2 seconds.
const membersStale = time.Minute

// agentStale is how long an agent's last activity keeps it in the view.
const agentStale = time.Hour

// crewView answers `crew` for req.Repo's room on the connection goroutine:
// every member (you first, then by latest activity), online or since
// when not, their agents and their claims.
func (d *Daemon) crewView(ctx context.Context, req Request) Response {
	fail := func(err error) Response { return Response{ID: req.ID, Error: fmt.Sprintf("crew: %v", err)} }
	if d.sync == nil {
		return fail(fmt.Errorf("the daemon is not ready"))
	}
	b, _, login, ok := d.sync.binding(req.Repo)
	if !ok {
		return fail(errors.New("this repository is not in a room; join one with `crewbo room join` or create one with `crewbo room create`"))
	}
	c, now := d.sync.crew, time.Now()
	c.mu.Lock()
	rm, online := c.members[b.RoomID], slices.Clone(c.online[b.RoomID])
	c.mu.Unlock()
	if stale := now.Sub(rm.at) > membersStale || slices.ContainsFunc(online, func(m string) bool { return !slices.Contains(rm.logins, m) }); stale {
		fctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rk, err := fetchRoomKeys(fctx, d.sync.session(), b.RoomID)
		cancel()
		if err == nil {
			rm = roomMembers{lead: rk.LeadLogin, at: now}
			for _, m := range rk.Members {
				rm.logins = append(rm.logins, m.Login)
			}
			c.mu.Lock()
			c.members[b.RoomID] = rm
			c.mu.Unlock()
		}
	}
	held := map[string][]string{}
	if err := d.onOwner(ctx, func() {
		for _, cl := range d.set(b.Repo).Active(d.cfg.Now()) {
			held[cl.Holder.MemberID] = append(held[cl.Holder.MemberID], cl.Pattern)
		}
	}); err != nil {
		return fail(err)
	}

	logins := slices.Clone(rm.logins)
	for _, m := range append(online, login) { // offline fetch failed, or not yet listed
		if !slices.Contains(logins, m) {
			logins = append(logins, m)
		}
	}
	c.mu.Lock()
	out := make([]CrewMember, 0, len(logins))
	for _, m := range logins {
		cm := CrewMember{Login: m, You: m == login, Lead: m == rm.lead, Online: slices.Contains(online, m), Claims: held[m]}
		if !cm.Online {
			cm.OfflineSince = c.left[b.RoomID][m]
		}
		for _, a := range c.agents[b.RoomID] {
			if cm.Online && a.Member == m && now.Sub(a.At) < agentStale {
				cm.Agents = append(cm.Agents, CrewAgent{Agent: a.Agent, Path: a.Path, Idle: a.Idle, At: a.At})
			}
		}
		slices.SortFunc(cm.Agents, func(x, y CrewAgent) int { return y.At.Compare(x.At) })
		out = append(out, cm)
	}
	c.mu.Unlock()
	latest := func(m CrewMember) time.Time {
		if len(m.Agents) == 0 {
			return time.Time{}
		}
		return m.Agents[0].At
	}
	slices.SortStableFunc(out, func(x, y CrewMember) int {
		switch {
		case x.You != y.You:
			if x.You {
				return -1
			}
			return 1
		case x.Online != y.Online:
			if x.Online {
				return -1
			}
			return 1
		}
		return cmp.Or(latest(y).Compare(latest(x)), cmp.Compare(x.Login, y.Login))
	})
	return Response{ID: req.ID, Crew: out}
}
