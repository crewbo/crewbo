# 0007. Protobuf for daemon to server; Go structs for MCP

- Status: Accepted (PRD 2026-10-10)
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

Daemons and servers of different versions will talk to each other, so the wire contract needs compatibility checks. MCP tool schemas are JSON Schema; tooling to generate them from Protobuf is alpha or archived (research 2026-10-10).

## Decision

- Daemon to server messages are defined in `proto/crewbo/v1/*.proto`, managed with `buf`, encoded as Protobuf JSON over WebSocket text frames.
- `buf lint` and `buf breaking` (against `main`) run in CI.
- Generated Go code lives in `internal/protocol/`, is committed, never hand-edited, and checked for drift in CI.
- MCP tools use plain Go structs with the official SDK `github.com/modelcontextprotocol/go-sdk` (Apache-2.0 and MIT, v1.8.0 at 2026-10-11), which infers schemas from struct tags.

## Alternatives considered

- Protobuf generating MCP schemas: immature tooling.
- Hand-written JSON for the wire: no breaking-change detection.
- Binary Protobuf on the wire: harder to debug; JSON is cheap at rooms of 10.

## Consequences

- Two type systems (proto messages and MCP structs) with a thin mapping layer.

## Links

- [Buf breaking change detection](https://buf.build/docs/breaking/)
- `docs/architecture/protocol.md`
