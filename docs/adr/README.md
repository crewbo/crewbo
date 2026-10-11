# Architecture Decision Records

Format: [MADR](https://adr.github.io/madr/). One file per decision, `NNNN-title-in-kebab-case.md`. ADRs record decisions that are costly to reverse; smaller decisions go only in `docs/engineering/decision-log.md`.

To change an accepted decision, write a new ADR that supersedes it and set the old one's status to `Superseded by NNNN`. Never edit the decision of an accepted ADR.

| # | Title | Status |
| --- | --- | --- |
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-single-go-binary-with-subcommands.md) | Single Go binary with subcommands | Accepted |
| [0003](0003-leased-advisory-claims-and-event-log.md) | Leased advisory claims plus append-only event log | Accepted |
| [0004](0004-hooks-enforce-mcp-queries.md) | Hooks as the enforcement layer; MCP as query surface | Accepted |
| [0005](0005-git-strategy.md) | Git strategy: task branches, `refs/crewbo/` checkpoints, daemon merge queue | Accepted |
| [0006](0006-dispatch-security.md) | Dispatch security: verbatim approval and Ed25519-signed tasks | Accepted |
| [0007](0007-protobuf-wire-go-structs-for-mcp.md) | Protobuf for daemon to server; Go structs for MCP | Accepted |
| [0008](0008-sync-server-websocket-hub-sqlite.md) | Sync server: in-binary WebSocket hub with SQLite | Accepted |
| [0009](0009-github-oauth-device-flow.md) | GitHub OAuth device flow for authentication | Accepted |
| [0010](0010-monorepo-layout-and-toolchain.md) | Monorepo layout and toolchain | Accepted |
| [0011](0011-sync-server-licence.md) | Sync server licence | Proposed |
| [0012](0012-crewbo-ide-long-term.md) | Crewbo IDE as a long-term product | Proposed |

"Accepted" for 0002 to 0010 means accepted in the PRD (2026-10-10) and pending Ronel's review of this package at Gate A.
