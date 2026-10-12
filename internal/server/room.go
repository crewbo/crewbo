package server

import (
	"context"
	"errors"
	"time"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// ErrRoomClosed means the room's goroutine has stopped.
var ErrRoomClosed = errors.New("server: room closed")

// subBuffer is how far a subscriber may fall behind before it is dropped;
// it then reconnects and replays from its cursor.
const subBuffer = 64

// Room is one room's owner goroutine (ADR 0008): it is the room's only
// writer, and it hands out subscriptions so a reader gets the replay and
// then every later event with no gap and no duplicate.
type Room struct {
	id   string
	log  *Log
	now  func() time.Time
	reqs chan func()
	done chan struct{}
	subs map[chan *crewbov1.Event]uint64 // subscriber to its cursor
}

// NewRoom returns a room over log. Call Run to start it.
func NewRoom(id string, log *Log, now func() time.Time) *Room {
	return &Room{id: id, log: log, now: now, reqs: make(chan func()), done: make(chan struct{}), subs: map[chan *crewbov1.Event]uint64{}}
}

// Run serves the room until ctx ends, then closes every subscription.
func (r *Room) Run(ctx context.Context) {
	defer func() {
		close(r.done)
		for ch := range r.subs {
			close(ch)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-r.reqs:
			f()
		}
	}
}

// do runs f on the room goroutine and waits for it, unless the room has
// stopped or cancel (nil: never) fires before f starts.
func (r *Room) do(cancel <-chan struct{}, f func()) error {
	finished := make(chan struct{})
	select {
	case r.reqs <- func() { f(); close(finished) }:
	case <-r.done:
		return ErrRoomClosed
	case <-cancel:
		return context.Canceled
	}
	<-finished // f runs on the room goroutine, which does not stop mid-call
	return nil
}

// Append commits a copy of ev to the room and delivers it to subscribers.
func (r *Room) Append(ctx context.Context, ev *crewbov1.Event) (*crewbov1.Event, error) {
	var out *crewbov1.Event
	var err error
	if derr := r.do(ctx.Done(), func() {
		out, err = r.log.Append(ctx, r.id, ev, r.now())
		if err != nil {
			return
		}
		for ch, after := range r.subs {
			if out.GetSeq() <= after {
				continue // cursor ahead of the log; see Subscribe
			}
			select {
			case ch <- out:
			default: // too slow: drop it; it resyncs from its cursor
				delete(r.subs, ch)
				close(ch)
			}
		}
	}); derr != nil {
		return nil, derr
	}
	return out, err
}

// Subscription is a reader's view of a room: Backlog holds the events after
// its cursor, Events every event committed after that. Events is closed
// when the subscriber falls behind, the room stops, or Close is called.
type Subscription struct {
	Backlog []*crewbov1.Event
	Events  <-chan *crewbov1.Event
	room    *Room
	ch      chan *crewbov1.Event
}

// Subscribe returns the events after seq after, then live events. A cursor
// ahead of the log gets nothing until the log passes it.
func (r *Room) Subscribe(ctx context.Context, after uint64) (*Subscription, error) {
	s := &Subscription{room: r, ch: make(chan *crewbov1.Event, subBuffer)}
	s.Events = s.ch
	var err error
	if derr := r.do(ctx.Done(), func() {
		if s.Backlog, err = r.log.Since(ctx, r.id, after); err == nil {
			r.subs[s.ch] = after
		}
	}); derr != nil {
		return nil, derr
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Close stops the subscription. It is safe after the room has stopped.
func (s *Subscription) Close() {
	_ = s.room.do(nil, func() {
		if _, ok := s.room.subs[s.ch]; ok {
			delete(s.room.subs, s.ch)
			close(s.ch)
		}
	})
}
