package server

import (
	"context"
	"maps"
	"slices"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// activityBuffer is how many relayed activities may wait per connection;
// more are dropped, and the agent's next activity brings it up to date.
const activityBuffer = 32

// Watch is one connection's place in a room's presence (M7-01): the room
// counts its member as connected until Leave, and C receives the room's
// presence whenever it changes. Only the latest presence waits in C; an
// older one not yet read is replaced. A receives the agent activity
// members relay. Nothing here is logged.
type Watch struct {
	C      <-chan *crewbov1.Presence
	A      <-chan *crewbov1.Activity
	room   *Room
	ch     chan *crewbov1.Presence
	act    chan *crewbov1.Activity
	member string
}

// Connect counts member as connected to the room and tells every watcher,
// including the new one.
func (r *Room) Connect(ctx context.Context, member string) (*Watch, error) {
	w := &Watch{room: r, ch: make(chan *crewbov1.Presence, 1), act: make(chan *crewbov1.Activity, activityBuffer), member: member}
	w.C, w.A = w.ch, w.act
	if err := r.do(ctx.Done(), func() {
		r.online[member]++
		r.watchers[w.ch] = w.act
		r.announce()
	}); err != nil {
		return nil, err
	}
	return w, nil
}

// Leave ends the watch and tells the other watchers if its member has no
// other connection to the room. It is safe after the room has stopped.
func (w *Watch) Leave() {
	r := w.room
	_ = r.do(nil, func() {
		if _, ok := r.watchers[w.ch]; !ok {
			return
		}
		delete(r.watchers, w.ch)
		close(w.ch)
		close(w.act)
		if r.online[w.member]--; r.online[w.member] <= 0 {
			delete(r.online, w.member)
		}
		r.announce()
	})
}

// announce gives every watcher the room's presence. It runs on the room
// goroutine, the only sender, so after draining a buffered value the send
// cannot block.
func (r *Room) announce() {
	p := &crewbov1.Presence{RoomId: r.id, OnlineMemberIds: slices.Sorted(maps.Keys(r.online))}
	for ch := range r.watchers {
		select {
		case <-ch:
		default:
		}
		ch <- p
	}
}

// Relay gives every connection of the room an agent's activity, dropping
// it for a connection that is too far behind.
func (r *Room) Relay(ctx context.Context, a *crewbov1.Activity) error {
	return r.do(ctx.Done(), func() {
		for _, act := range r.watchers {
			select {
			case act <- a:
			default:
			}
		}
	})
}
