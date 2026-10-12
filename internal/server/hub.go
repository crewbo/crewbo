package server

import (
	"context"
	"fmt"
	"time"

	"github.com/coder/websocket"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// room returns the running goroutine for a room, starting it on first use.
// ponytail: room goroutines run until Close; stop idle rooms if a server
// ever holds many.
func (s *Server) room(id string) *Room {
	s.hubMu.Lock()
	defer s.hubMu.Unlock()
	if r, ok := s.live[id]; ok {
		return r
	}
	r := NewRoom(id, s.events, time.Now)
	s.live[id] = r
	s.hubWG.Add(1)
	go func() { defer s.hubWG.Done(); r.Run(s.hubCtx) }()
	return r
}

// Close stops every room goroutine. Run calls it on shutdown; tests that
// use Handler directly call it themselves.
func (s *Server) Close() {
	s.hubStop()
	s.hubWG.Wait()
}

// subscribe checks membership for each Hello cursor and subscribes to those
// rooms. On failure it has closed what it opened.
func (s *Server) subscribe(ctx context.Context, userID int64, cursors []*crewbov1.RoomCursor) ([]*Subscription, crewbov1.ErrorCode, string) {
	var subs []*Subscription
	fail := func(code crewbov1.ErrorCode, msg string) ([]*Subscription, crewbov1.ErrorCode, string) {
		for _, sub := range subs {
			sub.Close()
		}
		return nil, code, msg
	}
	for _, cur := range cursors {
		ok, err := s.rooms.IsMember(ctx, cur.GetRoomId(), userID)
		if err != nil {
			s.log.Error("ws: membership", "err", err)
			return fail(crewbov1.ErrorCode_ERROR_CODE_UNSPECIFIED, "internal error")
		}
		if !ok {
			return fail(crewbov1.ErrorCode_ERROR_CODE_NOT_MEMBER, fmt.Sprintf("not a member of room %s", cur.GetRoomId()))
		}
		sub, err := s.room(cur.GetRoomId()).Subscribe(ctx, cur.GetLastSeq())
		if err != nil {
			s.log.Error("ws: subscribe", "room_id", cur.GetRoomId(), "err", err)
			return fail(crewbov1.ErrorCode_ERROR_CODE_UNSPECIFIED, "internal error")
		}
		subs = append(subs, sub)
	}
	return subs, 0, ""
}

// forward sends a room's live events until the subscription ends. If it
// ends while the connection is alive, the daemon fell behind (or the
// server is stopping): close so it reconnects and replays from its cursor.
func (s *Server) forward(ctx context.Context, c *websocket.Conn, sub *Subscription) {
	for ev := range sub.Events {
		if err := send(ctx, c, &crewbov1.Envelope{Body: &crewbov1.Envelope_Event{Event: ev}}); err != nil {
			return
		}
	}
	if ctx.Err() == nil {
		_ = c.Close(websocket.StatusTryAgainLater, "fell behind; reconnect and replay")
	}
}
