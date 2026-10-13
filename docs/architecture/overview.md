# Architecture overview

Status: draft for Gate A, 2026-10-11. Source of truth for product scope: [PRD](../product/PRD.md). Decisions: [ADRs](../adr/).

## Level 1: system context

Presentation copy: [Lucid](https://lucid.app/lucidchart/edbff025-4e98-4417-99fa-6327ce8b1f06/view). Mermaid below is the source of truth.

Crewbo sits between each teammate's coding agent and the team. It never hosts code: code moves only through the team's git remote. The sync server sees metadata only.

```mermaid
flowchart LR
  lead([Team lead])
  mate([Teammate])
  subgraph crewbo[Crewbo]
    sys[Crewbo daemon + hooks + sync server]
  end
  cc[Claude Code]
  cx[Codex CLI]
  ed[VS Code-family editor]
  gh[(GitHub: OAuth + git remote)]

  lead -- dispatch, review, merge --> sys
  mate -- approve, claim, observe --> sys
  cc -- hook events --> sys
  cx -- hook events --> sys
  sys -- deny / context --> cc
  sys -- deny / context --> cx
  sys -- headless launch --> cc
  sys -- headless launch --> cx
  ed -- extension --> sys
  sys -- device flow login --> gh
  sys -- push / fetch branches and refs --> gh
```

| Actor or system | Interaction |
| --- | --- |
| Team lead | Creates the room, dispatches signed tasks, reviews conflict warnings, merges through the merge queue. One lead per room. |
| Teammate | Joins with an invite, approves tasks after reading them verbatim, works with their own agent. |
| Claude Code, Codex CLI | Call `crewbo hook` through their native hook systems; are launched headless for dispatched tasks. |
| Editor | Shows claims, intents and conflict warnings through the extension. |
| GitHub | OAuth device flow identity; the git remote that carries all code. |

## Level 2: containers

Presentation copy: [Lucid](https://lucid.app/lucidchart/f964aecb-7153-4893-b402-cf9300435660/view). Mermaid below is the source of truth.

```mermaid
flowchart TB
  subgraph laptop[Each teammate laptop]
    agent[Coding agent<br/>Claude Code or Codex]
    hook[crewbo hook<br/>short-lived process]
    daemon[crewbo daemon<br/>claims, worktrees, merge-tree,<br/>event queue, local API]
    mcp[crewbo mcp<br/>stdio MCP server]
    ext[Editor extension<br/>TypeScript]
    web[Web dashboard<br/>served by daemon]
    git[(Local git repo<br/>+ worktrees)]
    kc[(OS keychain<br/>or 0600 file)]
    ldb[(Daemon SQLite<br/>event queue)]
  end
  subgraph srv[Sync server host: hosted, self-hosted, or lead laptop]
    serve[crewbo serve<br/>WebSocket hub]
    sdb[(SQLite<br/>event log, rooms, keys)]
  end
  remote[(Git remote)]

  agent -- "stdin JSON / stdout JSON" --> hook
  hook -- "local IPC, deadline ~300 ms" --> daemon
  agent -- stdio --> mcp
  mcp -- local IPC --> daemon
  ext -- local IPC --> daemon
  web -- HTTP on localhost --> daemon
  daemon -- "WebSocket, Protobuf JSON" --> serve
  serve --- sdb
  daemon --- ldb
  daemon --- kc
  daemon -- "system git" --> git
  git -- push / fetch --> remote
```

| Container | Process lifetime | Responsibility |
| --- | --- | --- |
| `crewbo hook` | One process per hook event | Decode agent payload, ask the daemon, print the agent's response. Fails open. |
| `crewbo daemon` | Long-running, one per user per machine | Claims and leases, intents, worktrees, merge-tree checks, merge queue (on the lead), headless agent launch, local API, offline queue. |
| `crewbo mcp` | Started by the agent over stdio | Read-only MCP tools backed by the daemon. |
| `crewbo serve` | Long-running | Rooms, membership, device keys, presence, the event log, store-and-forward of tasks. |
| Extension, dashboard | Editor or browser session | Display state, approve tasks, request handoffs. |

Local IPC is a Unix domain socket on every OS, including Windows (AF_UNIX, Windows 10 1803 or later), and every connection starts with a token from a user-only file (see [cross-platform.md](cross-platform.md)).

## What leaves a laptop

File paths, globs, task text, branch names, agent vendor, presence. Never file contents. No telemetry by default.

## Related documents

- [components.md](components.md): packages inside the daemon and server.
- [flows.md](flows.md): sequence diagrams for the five v1 flows.
- [data-model.md](data-model.md), [protocol.md](protocol.md), [hooks.md](hooks.md).
- [threat-model.md](threat-model.md), [failure-modes.md](failure-modes.md), [cross-platform.md](cross-platform.md).
