# 0004. Hooks as the enforcement layer; MCP as query surface

- Status: Accepted (PRD 2026-10-10)
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

An agent that never calls a Crewbo tool must still respect claims. MCP is pull-based: the 2026-07-28 revision removed server-initiated requests ([MCP changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog)). Claude Code and Codex both expose a PreToolUse hook that can deny a tool call with a reason shown to the model (verified 2026-10-11: [Claude Code hooks](https://code.claude.com/docs/en/hooks), [Codex hooks](https://learn.chatgpt.com/docs/hooks)).

## Decision

- `crewbo hook` is the enforcement point. It denies edits to paths claimed by another session, with a reason naming the holder and task.
- Shell commands are parsed before they run; claimed-file hashes are compared after they run (alert and record, no auto-revert in v1).
- Team state is injected as `additionalContext` at session start and on each prompt.
- MCP (`crewbo mcp`) answers queries only: team state, claims, intents, tasks. It never enforces.
- If the daemon is unreachable, the hook allows the edit and shows a warning (fail open).

## Alternatives considered

- MCP tools plus rules files: skipped 18 to 45% of the time.
- ACP proxy: requires users to launch agents through Crewbo; Antigravity is absent; v1 non-goal.
- Fail closed: punishes teammates for Crewbo bugs and makes adoption risky.

## Consequences

- Per-agent adapters are permanent maintenance; hook schemas change between releases.
- Shell-edit coverage is partial. Phase 0 measures it.
- Codex requires the user to trust each hook once via `/hooks`; whether updating the binary re-triggers trust is unknown U-02.

## Links

- `docs/architecture/hooks.md`
- `docs/research/risk-mitigations.md`, Risk 1
