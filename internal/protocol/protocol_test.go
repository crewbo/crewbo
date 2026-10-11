// Package protocol_test checks the wire contract of the generated crewbo.v1
// types (docs/architecture/protocol.md). The generated code in crewbo/v1 is
// never edited by hand; this file only exercises it.
package protocol_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

func TestEnvelopeJSONRoundTrip(t *testing.T) {
	in := &crewbov1.Envelope{
		Id: "0190a5f2-7c3e-7d4a-9b1e-123456789abc",
		Body: &crewbov1.Envelope_ClaimRequest{ClaimRequest: &crewbov1.ClaimRequest{
			RoomId: "room-1", SessionId: "session-1", Patterns: []string{"src/auth.go", "web/**"},
		}},
	}
	b, err := protojson.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out := &crewbov1.Envelope{}
	if err := protojson.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(in, out) {
		t.Errorf("round trip changed the message:\n in %v\nout %v", in, out)
	}
}

// TestUnknownFieldsIgnored pins the compatibility rule: a receiver built from
// an older protocol must accept frames from a newer sender. Decoders must set
// DiscardUnknown; strict decoding rejects the same frame.
func TestUnknownFieldsIgnored(t *testing.T) {
	frame := []byte(`{"id":"e1","hello":{"protocolVersion":1,"fieldFromTheFuture":"x"},"bodyFromTheFuture":{}}`)
	out := &crewbov1.Envelope{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(frame, out); err != nil {
		t.Fatalf("lenient decode: %v", err)
	}
	if out.GetHello().GetProtocolVersion() != 1 {
		t.Errorf("hello not decoded: %v", out)
	}
	if err := protojson.Unmarshal(frame, &crewbov1.Envelope{}); err == nil {
		t.Error("strict decode accepted unknown fields; the test no longer shows why DiscardUnknown is required")
	}
}

func TestEventCarriesClaims(t *testing.T) {
	ev := &crewbov1.Event{
		RoomId: "room-1", Seq: 42, ServerTimeUnixMs: 1791700000000,
		Body: &crewbov1.Event_ClaimsGranted{ClaimsGranted: &crewbov1.ClaimsGranted{Claims: []*crewbov1.Claim{{
			Id: "c1", Pattern: "src/auth.go", SessionId: "s1", LeaseExpiresUnixMs: 1791700300000,
		}}}},
	}
	b, err := protojson.Marshal(&crewbov1.Envelope{Id: "e2", Body: &crewbov1.Envelope_Event{Event: ev}})
	if err != nil {
		t.Fatal(err)
	}
	out := &crewbov1.Envelope{}
	if err := protojson.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
	got := out.GetEvent().GetClaimsGranted().GetClaims()
	if len(got) != 1 || got[0].GetPattern() != "src/auth.go" || out.GetEvent().GetSeq() != 42 {
		t.Errorf("event not preserved: %v", out)
	}
}
