# 0008. Sync server: in-binary WebSocket hub with SQLite

- Status: Accepted (PRD 2026-10-10)
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

Rooms hold at most 10 people. The server must run hosted, self-hosted, or on the lead's laptop during a hackathon. Peer-to-peer fails behind campus NAT. The research report suggested Centrifugo; the PRD rejected it.

## Decision

- `crewbo serve` runs a Go WebSocket hub inside the same binary.
- One goroutine owns each room's state; connections send to it over channels.
- Persistence in SQLite through a pure-Go driver (candidate `modernc.org/sqlite`, BSD-3-Clause, v1.60.1 at 2026-10-11), because the binary builds with `CGO_ENABLED=0` (ADR 0002). WAL mode.
- The server stores metadata only: presence, claims, intents, signed tasks, the event log. Never file contents.

## Alternatives considered

- Centrifugo: an extra process to deploy; unnecessary at 10 users per room.
- Peer-to-peer: fails behind NAT.
- Postgres in v1: heavier to self-host; planned for v3.

## Consequences

- One server process is a single point of failure. Daemons queue locally and keep enforcing known claims while it is down.
- Horizontal scaling needs a redesign (v3).

## Links

- PRD, "Key design decisions"
- `docs/architecture/components.md`
