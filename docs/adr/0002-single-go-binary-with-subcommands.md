# 0002. Single Go binary with subcommands

- Status: Accepted (PRD 2026-10-10)
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

Crewbo needs a hook handler that agents call on every edit, a long-running daemon per laptop, an MCP server, a sync server and user commands. It must run on Windows 10+, macOS 13+ and Linux (x86_64, arm64), installed by students in minutes. Claude Code exec-form hooks on Windows require a real executable; `.cmd` and `.bat` shims cannot be spawned without a shell ([Claude Code hooks reference](https://code.claude.com/docs/en/hooks)).

## Decision

Ship one statically linked Go binary, `crewbo`, with subcommands: `daemon`, `hook <agent> <event>`, `mcp`, `serve`, and user commands (`login`, `room`, `claim`, `task`, `version`). Build with `CGO_ENABLED=0` so cross-compilation needs no C toolchain.

## Alternatives considered

- Separate binaries per role: more install steps, version skew between hook and daemon.
- Shell scripts as hooks: break on Windows (Git Bash strips backslashes; cmd.exe quoting), and source user profiles that can corrupt JSON on stdout.
- Node or Python: needs a runtime on every machine; npm shims cannot be exec-form hooks on Windows.

## Consequences

- One version number covers every component, so hook and daemon cannot drift.
- `CGO_ENABLED=0` rules out C SQLite bindings; the store uses a pure-Go driver (ADR 0008).
- The hook path must start fast. The hook subcommand must not initialise daemon or server state.

## Links

- PRD, "Architecture"
- `docs/research/hook-and-dispatch-internals.md`
