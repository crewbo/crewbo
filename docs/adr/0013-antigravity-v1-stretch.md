# 0013. Antigravity adapter as a v1 stretch goal

- Status: Accepted (Ronel, 2026-10-11)
- Date: 2026-10-11
- Decision owner: Ronel Abraham Mathew

## Context

The PRD limits v1 to Claude Code and Codex and plans Antigravity for v2. Ten interviewed teams (41 members) changed the picture: Antigravity is used by 15 members, as many as Codex, and by 9 of the 11 members v1 cannot cover. A member whose agent has no adapter is never blocked by claims, which weakens enforcement for the whole team. A docs check and a live spike (S9, agy 1.3.3) showed the same hook design works: PreToolUse deny with a reason, context injection before every model call, a Stop event, the model name on every event, and a headless CLI on all three OSes (`docs/research/antigravity-hooks.md`).

## Proposal

- Add an Antigravity adapter to v1 as a **stretch goal**, scheduled after M5 and before the M6 dogfood, only if M1 to M5 are on time.
- Scope: hook adapter (decode, encode, install one named entry in `~/.gemini/config/hooks.json`), MCP registration, and fixtures. Headless dispatch to Antigravity stays in v2 unless U-28 is resolved cheaply.
- If accepted, the PRD's v1 non-goal "More than 2 agent vendors" becomes "More than 3 agent vendors", and the v2 adapter list drops Antigravity.

## Trade-offs

- For: raises interviewed member coverage from 73% to 95% (39 of 41); two teams (3 and 5) become usable; the design and tests carry over from the Codex adapter.
- Against: about three more PRs in a 6-week budget; a third hook schema to keep current; Antigravity on Windows and macOS untested; headless writes need a permission rule (U-28).

## Decision

Accepted by Ronel on 2026-10-11. The PRD v1 non-goals, roadmap and adapter section are updated accordingly.
