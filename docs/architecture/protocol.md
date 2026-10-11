# Daemon to server protocol

Draft for Gate A. Since M1-03 (#12) the source of truth is `proto/crewbo/v1/*.proto`; the sketch below is the original design and may lag behind it. Tasks, intents, presence and conflict messages are added in later milestones as new fields and oneof members.

## Transport

- One WebSocket per daemon per room set, at `wss://<server>/v1/ws`. TLS required except on `localhost` and when the operator passes an explicit `--insecure-lan` flag (for hackathon LANs; logged as a warning).
- Each text frame carries one `Envelope` in Protobuf JSON encoding, with the default lowerCamelCase JSON field names. Receivers decode with `protojson.UnmarshalOptions{DiscardUnknown: true}`; strict decoding would reject frames from newer senders (`internal/protocol/protocol_test.go`).
- Times on the wire are `int64` Unix milliseconds (`*_unix_ms`), not `google.protobuf.Timestamp`, to keep the JSON plain.
- Authentication: the Crewbo session token in the first frame (`Hello`), not in the URL.
- Keepalive: WebSocket ping every 20 s; a connection with no pong for 60 s is closed. (Values to be tuned; decision log.)

## Message sketch

```proto
syntax = "proto3";
package crewbo.v1;

message Envelope {
  string id = 1;              // client-generated UUIDv7, for idempotency and acks
  oneof body {
    Hello hello = 10;
    Welcome welcome = 11;
    Ack ack = 12;
    Error error = 13;
    ClaimRequest claim_request = 20;
    ClaimRelease claim_release = 21;
    ClaimRenew claim_renew = 22;
    HandoffRequest handoff_request = 23;
    HandoffDecision handoff_decision = 24;
    LeadOverride lead_override = 25;
    IntentUpdate intent_update = 30;
    Presence presence = 31;
    DispatchTask dispatch_task = 40;
    TaskStatus task_status = 41;
    Checkpoint checkpoint = 50;
    ConflictWarning conflict_warning = 51;
    ClaimViolation claim_violation = 52;
    Event event = 60;          // server to daemon: one committed log entry
  }
}

message Hello {
  int32 protocol_version = 1; // major version, currently 1
  string session_token = 2;
  string device_key_id = 3;
  repeated RoomCursor rooms = 4;
}
message RoomCursor { string room_id = 1; uint64 last_seq = 2; }

message Event {
  string room_id = 1;
  uint64 seq = 2;
  int64 server_time_unix_ms = 3;
  string actor_member_id = 4;
  oneof body { /* committed forms of the requests above */ }
}

message SignedTask {
  bytes body = 1;             // TaskBody, serialized once by the signer
  bytes signature = 2;        // Ed25519 over the exact bytes of body
  string key_id = 3;
}
```

Verifiers check the signature over the received `body` bytes, then decode them. Nobody re-encodes a task before verifying, because Protobuf serialization is not canonical across implementations. The signed `TaskBody` includes `room_id`, `assignee`, `expires_at` and a unique `task_id`, so a signature cannot be replayed into another room, to another member, or after expiry.

## Versioning and compatibility

- Package `crewbo.v1`. Field numbers are never reused; removed fields are `reserved`.
- `buf breaking` with the `FILE` rule set runs against `main` in CI.
- Additive changes (new optional fields, new oneof members) stay in v1. Receivers ignore unknown fields and unknown oneof members, replying `Error(unsupported)` only for requests.
- A breaking change means `crewbo.v2` and a server that serves both for at least one minor release.
- `Hello.protocol_version` lets the server refuse a too-old daemon with a clear upgrade message.

## Ordering and delivery

- The server is the single writer of each room's log and assigns `seq` gap-free per room.
- Requests are idempotent by `Envelope.id`: the server keeps recent ids per device for 10 min and replays the same `Ack` or `Error`.
- Every state change reaches daemons as an `Event`; a daemon applies events strictly in `seq` order and never applies its own request optimistically for claims (it waits for the `Event`). The hook path reads the daemon's last applied state, so enforcement may lag the server by one propagation (target under 2 s).
- No ordering guarantee across rooms.

## Reconnect and resync

1. On disconnect, the daemon keeps enforcing its last known claim set and appends outgoing requests to its local queue (SQLite).
2. Reconnect with exponential backoff (1 s doubling to 30 s, with jitter).
3. Send `Hello` with each room's `last_seq`.
4. The server replays events after `last_seq`. If the gap exceeds the retained window (the server compacts after a snapshot), it sends a `Welcome` with a full snapshot and the snapshot's `seq` instead.
5. The daemon applies replayed events, then flushes its local queue in original order. Each queued request is re-validated by the server against current state; requests that conflict (for example a claim taken by someone else while offline) come back as `Error(conflict)` and the user is told.
6. Leases that expired while disconnected are not resurrected; the daemon re-requests them.

## Size limits

Envelope max 64 KiB; task text max 32 KiB; paths per claim request max 200. Oversized frames close the connection with a policy error.
