package daemon

import (
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
}

// agentSeen is the latest activity of one agent session in a room.
type agentSeen struct {
	Member, Agent, Path string
	Idle                bool
	At                  time.Time
}

func newCrew() *crew {
	return &crew{online: map[string][]string{}, agents: map[string]map[string]agentSeen{}}
}

// presence records a room's connected members.
func (c *crew) presence(p *crewbov1.Presence) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.online[p.GetRoomId()] = p.GetOnlineMemberIds()
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
