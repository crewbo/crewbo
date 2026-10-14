package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type client struct {
	t     *testing.T
	base  string
	token string
}

func (c client) do(method, path, body string) (int, []byte) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestRoomEndpoints(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	as := func(n int) client {
		tok, _, err := s.sessions.Create(context.Background(), user(n))
		if err != nil {
			t.Fatal(err)
		}
		return client{t, ts.URL, tok}
	}
	lead, mate, stranger, anon := as(1), as(2), as(3), client{t, ts.URL, ""}

	if code, _ := anon.do("POST", "/v1/rooms", `{"name":"x"}`); code != http.StatusUnauthorized {
		t.Errorf("create without session: %d", code)
	}
	if code, _ := lead.do("POST", "/v1/rooms", `{"name":""}`); code != http.StatusBadRequest {
		t.Errorf("empty name: %d", code)
	}
	code, b := lead.do("POST", "/v1/rooms", `{"name":"crewbo"}`)
	var room RoomInfo
	if code != http.StatusOK || json.Unmarshal(b, &room) != nil || room.Role != "lead" {
		t.Fatalf("create: %d %s", code, b)
	}
	invites := "/v1/rooms/" + room.ID + "/invites"
	for body, want := range map[string]int{`{"max_uses":10}`: 400, `{"expires_in_secs":30}`: 400, `{"expires_in_secs":864000}`: 400} {
		if code, _ := lead.do("POST", invites, body); code != want {
			t.Errorf("invite %s: %d, want %d", body, code, want)
		}
	}
	if code, _ := stranger.do("POST", invites, `{}`); code != http.StatusNotFound {
		t.Errorf("stranger invites: %d, want 404", code)
	}
	code, b = lead.do("POST", invites, `{}`)
	var inv InviteResponse
	if code != http.StatusOK || json.Unmarshal(b, &inv) != nil || !strings.HasPrefix(inv.Code, "cbi_") || inv.MaxUses != MaxMembers-1 {
		t.Fatalf("invite: %d %s", code, b)
	}
	if code, _ := mate.do("POST", "/v1/rooms/join", `{"code":"cbi_wrong"}`); code != http.StatusNotFound {
		t.Errorf("unknown code: %d", code)
	}
	join, _ := json.Marshal(JoinRequest{Code: inv.Code})
	if code, b := mate.do("POST", "/v1/rooms/join", string(join)); code != http.StatusOK || !bytes.Contains(b, []byte(`"role":"member"`)) {
		t.Errorf("join: %d %s", code, b)
	}
	// Merge mode: members by default; only the lead changes it.
	settings := "/v1/rooms/" + room.ID + "/settings"
	if !bytes.Contains(mustGet(t, mate, "/v1/rooms/"+room.ID+"/keys"), []byte(`"merge_mode":"members"`)) {
		t.Error("a new room does not merge by members")
	}
	for c, want := range map[client]int{mate: http.StatusForbidden, stranger: http.StatusNotFound, anon: http.StatusUnauthorized} {
		if code, _ := c.do("POST", settings, `{"merge_mode":"lead"}`); code != want {
			t.Errorf("settings by %q: %d, want %d", c.token, code, want)
		}
	}
	if code, _ := lead.do("POST", settings, `{"merge_mode":"everyone"}`); code != http.StatusBadRequest {
		t.Errorf("unknown mode: %d", code)
	}
	if code, b := lead.do("POST", settings, `{"merge_mode":"lead"}`); code != http.StatusOK || !bytes.Contains(b, []byte(`"merge_mode":"lead"`)) {
		t.Errorf("lead sets lead-only: %d %s", code, b)
	}
	if !bytes.Contains(mustGet(t, mate, "/v1/rooms/"+room.ID+"/keys"), []byte(`"merge_mode":"lead"`)) ||
		!bytes.Contains(mustGet(t, mate, "/v1/rooms"), []byte(`"merge_mode":"lead"`)) {
		t.Error("members do not see the lead-only mode")
	}
	if code, _ := mate.do("POST", invites, `{}`); code != http.StatusForbidden {
		t.Errorf("member invites: %d, want 403", code)
	}
	code, b = mate.do("GET", "/v1/rooms", "")
	var list []RoomInfo
	if code != http.StatusOK || json.Unmarshal(b, &list) != nil || len(list) != 1 || list[0].Members != 2 {
		t.Errorf("list: %d %s", code, b)
	}
	if code, b := stranger.do("GET", "/v1/rooms", ""); code != http.StatusOK || string(bytes.TrimSpace(b)) != "[]" {
		t.Errorf("empty list: %d %s", code, b)
	}
	leave := "/v1/rooms/" + room.ID + "/leave"
	if code, _ := lead.do("POST", leave, ""); code != http.StatusConflict {
		t.Errorf("lead leaves first: %d", code)
	}
	if code, _ := mate.do("POST", leave, ""); code != http.StatusNoContent {
		t.Errorf("mate leaves: %d", code)
	}
	// An exhausted invite answers 410 Gone.
	code, b = lead.do("POST", invites, `{"max_uses":1}`)
	if code != http.StatusOK || json.Unmarshal(b, &inv) != nil {
		t.Fatalf("one-use invite: %d %s", code, b)
	}
	join, _ = json.Marshal(JoinRequest{Code: inv.Code})
	mate.do("POST", "/v1/rooms/join", string(join))
	if code, _ := stranger.do("POST", "/v1/rooms/join", string(join)); code != http.StatusGone {
		t.Errorf("used-up invite: %d", code)
	}
}

func mustGet(t *testing.T, c client, path string) []byte {
	t.Helper()
	code, b := c.do("GET", path, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, code, b)
	}
	return b
}

// The server id is public, permanent and the same on every call.
func TestServerInfo(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	anon := client{t, ts.URL, ""}
	var first, second Info
	for _, info := range []*Info{&first, &second} {
		code, b := anon.do("GET", "/v1/info", "")
		if code != http.StatusOK || json.Unmarshal(b, info) != nil {
			t.Fatalf("info: %d %s", code, b)
		}
	}
	if !strings.HasPrefix(first.ServerID, "srv_") || first.ServerID != second.ServerID || first.ProtocolVersion != ProtocolVersion {
		t.Errorf("info %+v then %+v", first, second)
	}
}
