# Crewbo PRD

Status: planning complete, v1 not started. As of 2026-10-10. Owner: Ronel Abraham Mathew (@RM1338).

Canonical living copy: the Crewbo PRD doc on claude.ai. This file is the repo snapshot. If they disagree, ask the owner.

## Summary

Crewbo lets every teammate run their own AI coding agent at the same time, on any OS, with each agent aware of what the others are doing. It is a coordination layer, not an agent manager: teammates keep Claude Code, Codex, Cursor or whatever they already use.

- **Problem:** in team projects and hackathons, only one person can drive an AI agent on the shared codebase at a time. Everyone else waits, or their agents collide at merge time.
- **Wedge:** mixed-vendor, cross-machine, cross-OS coordination that is self-hostable and keeps code off third-party servers. No shipping product does all four.
- **Mechanism:** a local Go daemon enforces leased file claims through each agent's own hooks, shares live intent through a small sync server, and warns about cross-machine conflicts before merge.
- **Team lead:** dispatches signed tasks to teammates' agents; each teammate approves before anything runs.
- **Status:** planning complete. Org `github.com/crewbo` and `crewbo.dev` secured. Next step is the one-week Phase 0 spike.

## Problem and target users

Parallel AI agents on one codebase conflict often, and far more often when the agents come from different vendors. A 2026 study of 33,596 agent-written PRs found text conflicts in 19.8% of merges when the same agent wrote both PRs and 41.7% when two different agents did. The authors attribute it to agents having no way to see each other's branches or claim files.

**Primary user (v1):** small student and hackathon teams of 2 to 10 people, on mixed operating systems (Windows, macOS, Ubuntu, Arch/Omarchy), each using a different coding agent.

**Secondary user (later):** small startups and regulated teams that want agent coordination without sending code or chats to a vendor cloud.

**What they do today:** take turns, split work verbally and hope, or resolve conflicts by hand after the fact. Existing tools coordinate one person's agents on one machine, or require everyone to move into one vendor's cloud.

## Competitive landscape

No product combines multi-human, cross-machine, any-OS, mixed-vendor and self-hostable coordination with agent-to-agent awareness. Conductor is the main funded competitor; Foremerge is the closest technical one.

| Tool | Multi-human | Cross-machine | OS | Agents | Self-host | Main gap for Crewbo to exploit |
| --- | --- | --- | --- | --- | --- | --- |
| Conductor | Yes (paid cloud) | Cloud only | Mac app | Claude, Codex, Cursor, OpenCode | No (in development) | Mac-only, chats stored on its servers, no conflict detection between workspaces |
| Claude Code Agent Teams | No | Own sessions only | All | Claude only | n/a | Same account, one vendor |
| Cursor 3 teams | One Cursor team | Cloud agents | All | Cursor | No | Locked to Cursor |
| GitHub Agent HQ | Via PRs | Yes | Web | Copilot, Claude, Codex | No | No live shared state |
| Paseo | Thin | Remote daemon | All | 30+ | Yes | Per-user, team roles undocumented |
| Vibe Kanban | Shut down cloud | Board only | All | ~10 | Yes | Company wound down Apr 2026 |
| Foremerge | No | No (roadmap) | All | Any MCP | Yes | Single machine only |

**Positioning:** not an agent manager, the coordination layer between agent managers. Vendor neutrality and self-hosting are the durable edges.

## Goals, non-goals and kill criteria (v1)

v1 is a walking skeleton: the thinnest version that runs end to end through daemon, server, hooks and extension, built in a fixed 6-week budget where scope shrinks to fit.

**Goals (v1)**

- Two agents on two different machines and OSes work in parallel without editing the same file unknowingly.
- A lead dispatches a task that runs on a teammate's agent after the teammate approves it.
- A cross-machine merge conflict is flagged before anyone merges.
- Works on Windows, macOS and Linux (including Omarchy) from one binary.

**Non-goals (v1)**

- Desktop app (VS Code-family extension plus web dashboard only)
- Symbol or API-level claims (file and glob only)
- Billing, SSO, audit export
- More than 2 agent vendors (Claude Code and Codex only)
- Rooms over 10 people
- ACP integration

**Success criteria (by week 8):** 5 real teams use Crewbo in a real hackathon or project; at least 3 of them run 2 or more agent vendors in the same room.

**Kill or pivot criteria:** fewer than 3 mixed-vendor teams by week 8; or Phase 0 shows hooks cannot reliably block claimed edits on 2 of 3 OSes.

**North-star metric:** share of active rooms running two or more agent vendors.

## Roadmap beyond v1

Each version starts only when the gate before it is passed. Gate numbers are targets to validate. Dates after v1 are targets, not commitments.

| Version | Goals | Non-goals (deferred to) | Exit gate |
| --- | --- | --- | --- |
| v1 Skeleton (Oct to Dec 2026) | Claude Code + Codex adapters; leased file and glob claims; lead dispatch with approval; cross-machine merge-tree warnings; VS Code-family extension and web dashboard; self-hosted sync server | Other agents (v2); semantic claims (v2); billing (v3); SSO (v4); desktop app (v2 if demanded) | 5 real teams, at least 3 mixing vendors |
| v2 Public beta (Dec 2026 to Feb 2027) | Adapters for Cursor, Gemini CLI, Antigravity and Copilot CLI; semantic claims (symbol, API, schema, migration); optional auto-revert of shell edits; signed installers (winget, Homebrew, AUR, Open VSX); free hosted relay; docs and open-source launch | Billing and paid tiers (v3); rooms over 25 (v3); ACP host mode (v3); mobile app (not planned) | 50 weekly active rooms; 30% of rooms mix vendors |
| v3 Teams (Mar to Jun 2027) | Paid hosted Team tier with PPP pricing for India; lead policies; agent activity analytics and conflict reports; roles and permissions; rooms up to 50; Postgres and horizontally scaled server; Slack, Linear and GitHub Issues integrations; optional ACP host mode | SSO, SCIM, audit export (v4); on-prem licence (v4) | 10 paying teams |
| v4 Enterprise (Jul to Dec 2027) | SAML SSO and SCIM; audit log export; licensed on-prem and air-gapped deployment; data residency; claim schema published as an open spec; interop with GitHub Agent HQ and Conductor workspaces | Anything on the permanent list | First enterprise contract or pilot |

**Permanent non-goals:** building our own coding agent or model; forking an IDE or editor; hosting or proxying customer code (code moves only through the team's git remote); running agents in our own cloud sandboxes; proxying model inference or reselling tokens.

## v1 scope: core flows

v1 ships exactly five flows:

1. Join a room (GitHub device-flow login, room invite).
2. Lead dispatches a task (signed, with owned paths).
3. Teammate approves (task text shown verbatim) and their agent claims files and runs in a fresh worktree.
4. An edit to a claimed file is blocked and the owner notified (escalation ladder).
5. A cross-machine merge-tree warning before merge.

Dispatch sequence, by actor:

| Step | Actor | Action |
| --- | --- | --- |
| 1 | Lead | Dispatch task: signed, owned paths |
| 2 | Sync server | Store and forward until teammate is online |
| 3 | Teammate + daemon | Approve task (shown verbatim first). Security gate. |
| 4 | Teammate + daemon | Claim files, create worktree, launch agent headless |
| 5 | Teammate agent | Edit code; hooks check every edit against claims. If claimed elsewhere: escalation ladder (wait, ask handoff) |
| 6 | Teammate + daemon | On stop: checkpoint, run merge-tree against others' work |
| 7 | Sync server | Notify room of conflict warnings |
| 8 | Lead | Review and merge through the daemon's serial merge queue |

## Architecture

Every laptop runs the same `crewbo` binary. Hooks, daemon, MCP server and the sync server (`crewbo serve`) are subcommands. The sync server can run hosted, self-hosted, or on the lead's laptop over LAN or Tailscale during a hackathon. SQLite is enough for v1.

- **Coding agent** (Claude Code, Codex) calls `crewbo hook <event>` through its native hook system.
- **crewbo daemon** (per laptop): leased claims, worktrees, merge-tree checks, MCP server, local event queue.
- **Editor extension** (VS Code family): ghost edits and diffs from live state supplied by the daemon.
- **Sync server** (`crewbo serve`): presence, claims, intents, signed tasks, append-only event log. Receives metadata only, over WebSocket.
- **Git remote** (GitHub): task branches and `refs/crewbo/` checkpoints. Code moves only through git; the sync server never sees file contents.

## Key design decisions

| Area | Decision | Rejected | Evidence | Overturned if |
| --- | --- | --- | --- | --- |
| Claims | Leased advisory file and glob claims (about 5 min, renewed on each hook event) plus an append-only shared event log | Hard locks; awareness only | Locks plus shared state cut duplicate work from 64% to 0% in a simulated study (arXiv 2606.19616); leases bound the wait when a holder crashes (Gray and Cheriton); Git LFS hard locks need force-unlock and are skipped silently | Real LLM agents ignore claims despite hooks |
| Enforcement | Hooks block edits to claimed files; MCP is only the query surface | Relying on MCP or rules files | MCP 2026-07-28 removed server-initiated requests; rules files followed only 55 to 82% of the time | n/a |
| Git | One short-lived branch per task; checkpoint refs under `refs/crewbo/`; daemon's own serial merge queue | One shared branch; long-lived branches; GitHub merge queue as primary | GitHub recommends at most 6 pushes per minute per repo; merge queue is org-repo only | GitHub rejects custom ref pushes |
| Dispatch security | Verbatim approval gate, Ed25519-signed tasks, fresh worktree, teammate's own permissions | Auto-execute | Lethal trifecta: no reliable prompt-injection defence exists, so mitigation must be structural | n/a |
| Contracts | Protobuf for daemon to server; plain Go structs for MCP tools; `buf breaking` in CI | Protobuf generating MCP schemas | Proto to JSON Schema tooling is alpha or archived | n/a |
| Conflict UX | Escalation ladder: inform, agent chooses wait / work elsewhere / request handoff, owner approves, lead override with reason | Hard block; last-write-wins | Workspace awareness reduces need for locks (Gutwin and Greenberg); code cannot merge per property like Figma | Ladder feels slow in a real hackathon |
| Sync server | Same binary as the daemon (`crewbo serve`), Go WebSocket hub, SQLite | Centrifugo; P2P | Rooms of 10 or fewer need no broker; P2P fails behind campus NAT | Hosted scale demands it |
| Auth | GitHub OAuth device flow | Passwords | Built for CLI tools | n/a |

## Agent adapter specs

One compiled command, `crewbo hook <event>`, serves both agents. Every agent is normalised into five events: `session_start`, `prompt`, `pre_edit`, `post_edit`, `stop`. All details below come from research on 2026-10-10 and MUST be re-verified against official docs and the installed CLI before implementation.

| Concern | Claude Code | Codex CLI |
| --- | --- | --- |
| Hook config | `hooks` key in user, project or local settings; merged, no restart | `hooks.json` or `[hooks]` in `config.toml` under `~/.codex/` or `<repo>/.codex/` |
| Block an edit | PreToolUse returns `permissionDecision: "deny"`, or exit 2 | Same field, or exit 2 with reason on stderr |
| Edit tools | `Edit`, `Write`, `NotebookEdit` (`file_path`) | `apply_patch`: parse paths from `*** Add/Update/Delete File:` and `*** Move to:` lines |
| Shell edits | `Bash` / `PowerShell`, full command in `tool_input.command` | `Bash`, `tool_input.command` |
| Inject context | SessionStart, UserPromptSubmit, PostToolUse | `additionalContext` |
| Windows | Use exec form (`command` + `args`): runs the `.exe` with no shell; shell form goes through Git Bash and mangles backslashes | Runs via `cmd.exe /C`; set `commandWindows` separately |
| Trust | None | Every hook is hash-pinned; user must trust it once via `/hooks` |
| Fail mode | Timeout or failure lets the call through unless `onFailure: "block"` | Unsupported output fields fail the hook open |
| Headless dispatch | `claude -p --permission-mode <explicit>`, started in the worktree; never `--bare` | `codex exec -C <dir> --sandbox workspace-write --json` |
| Native push | Messaging socket via `CLAUDE_CODE_MESSAGING_SOCKET` (v2.1.224+, v2.1.234+ on Windows); format undocumented, so wake-up only | App Server `turn/steer`, only on threads the daemon started |
| MCP registration | `claude mcp add --scope user --transport stdio crewbo -- crewbo mcp` | `codex mcp add crewbo -- crewbo mcp` |

- **Shell-edit hole:** parse each shell command before it runs and deny writes to claimed paths; after it runs, compare hashes of claimed files and alert and record (v1 does not auto-revert).
- **Billing trap:** an `ANTHROPIC_API_KEY` in the environment silently moves `claude -p` to API billing. The daemon strips it unless the teammate opts in.
- **Daemon unreachable:** hooks allow the edit with a visible warning, never block all work.

## Non-functional requirements, privacy and failure modes

**Targets (v1):** rooms up to 10 people; claim and intent propagation under 2 seconds; daemon memory under 100 MB; Windows 10+, macOS 13+, Linux x86_64 and arm64; Git 2.38 or later.

**Privacy**

- Leaves the machine: file paths, globs, task text, branch names, agent vendor, presence. Never file contents.
- Code moves only through the team's own git remote.
- No telemetry by default; any later telemetry is opt-in and documented.
- Signing keys: Ed25519 seed in the OS keychain via `zalando/go-keyring`; on Linux without a secret service (common on Omarchy), fail fast and fall back to a 0600 file.

| Situation | Behaviour |
| --- | --- |
| Daemon crashes or laptop sleeps holding claims | Leases expire in about 5 minutes; on wake, the daemon re-claims or warns |
| Sync server down | Daemons queue events locally and keep enforcing known claims; resync on reconnect |
| Network split | Each side keeps its own claims; conflicts surface at resync and at merge-tree check |
| Stale claim on a file someone needs | Escalation ladder: request handoff, then lead override with reason |
| Two people acting as lead | One lead role per room, held by the room creator; transfer is explicit |
| Force-pushed task branch | Merge queue re-runs merge-tree against the new head |
| Hook fails or times out | Edit allowed with a visible warning |

## Risks and mitigations

| Risk | Mitigation | Residual risk |
| --- | --- | --- |
| Agents ignore team state (MCP is pull-based) | Four layers: context injected at session start and per prompt; pre-edit hook denies claimed writes; native push where it exists; file watcher and pre-commit backstop | Shell edits the parser misses; hook formats change monthly |
| A vendor ships cross-machine teams | Stay vendor-neutral and cross-account; import vendor team features as data sources; publish the claim schema as an open spec | Single-vendor teams default to their vendor |
| Conductor adds local multiplayer or Windows/Linux | Own agent-level claim and intent coordination across machines; free self-host; local-first | Conductor has $24M and a team |
| Foremerge adds multi-machine support | Ship cross-machine first; consider interop with its claim model | Technical lead could vanish |
| Solo maturity gap | Reuse proven parts (Go MCP SDK, system `git merge-tree`, go-keyring, GoReleaser); launch with 2 adapters; dogfood in real hackathons | Bus factor of one |
| Monetization | Keep sync free; charge the lead for governance | No conversion data for this category |
| Prompt injection through dispatched tasks | Verbatim approval, signed tasks, scoped worktree, no elevated permissions | Approved text can still be malicious if unread |

## Business model and licensing

Open core priced by buyer: coordination stays free, the team lead pays for control.

| Tier | Price | Includes |
| --- | --- | --- |
| Free / self-host | $0 | Daemon, adapters, sync server, extension, unlimited rooms |
| Hosted Team | About $8 to $20 per user per month (to validate); PPP pricing for India | Hosted relay, lead policies, agent activity analytics, conflict reports |
| Enterprise | Custom | SSO, SCIM, audit export, on-prem support |

**Licensing:** Apache-2.0 for adapters, protocol and extension; AGPL or FSL for the sync server (decision pending, owner decides before first public release); commercial licence for enterprise features.

## Unknowns register and fallbacks

Rule during the build: any new unknown gets a 90-minute timebox; unresolved by then, take the fallback and log it.

| # | Unknown | Resolved by | Fallback |
| --- | --- | --- | --- |
| 1 | Message format and auth on Claude's messaging socket; whether an external daemon's messages get held | Phase 0 | Hook-injected context only; socket used as wake-up |
| 2 | Codex re-asks for trust when only the hook binary updates | Phase 0 | Installer prompts the user to re-trust after upgrades |
| 3 | `cmd.exe` quoting and noisy shell profiles corrupting hook output | Phase 0 | Install binary to a path with no spaces; hook prints only JSON |
| 4 | Minimum Claude Code version for exec-form hooks | Phase 0 | Require that version; refuse install below it |
| 5 | Share of shell edits caught by parser plus hash check | Phase 0 (test corpus) | Alert-and-record only; documented limitation |
| 6 | GitHub accepts pushes to `refs/crewbo/...` and real throttling | Phase 0 | `wip/<user>/<task>` branches |
| 7 | Codex App Server `turn/steer` on daemon-owned threads; `codex exec` exit code on failure | Phase 0 | Hooks-only for Codex; parse JSONL for failure |
| 8 | Whether Omarchy runs a secret service by default | Phase 0 | 0600 key file |
| 9 | Lease behaviour when a laptop sleeps mid-claim | Phase 0 | Expire and warn on wake |
| 10 | Whether teams actually mix agent vendors | Phase 0 interviews (10 teams) | Narrow positioning to cross-machine, single-vendor teams |

## Phase 0 spikes and timeline

Phase 0 is one week of throwaway code that decides go or no-go before any product code. Dates assume a start on Monday 12 Oct 2026 and about 22 effective hours a week.

| Dates (2026) | Work |
| --- | --- |
| Oct 12 to Oct 18 | Phase 0 spikes. Go / no-go gate on Oct 18 |
| Oct 19 to Oct 25 | Protocol + daemon skeleton |
| Oct 26 to Nov 1 | Claude Code + Codex hook adapters |
| Nov 2 to Nov 8 | Sync server + auth |
| Nov 9 to Nov 15 | Lead dispatch |
| Nov 16 to Nov 22 | Conflict warnings + extension v0 |
| Nov 23 to Nov 29 | Dogfood in a real hackathon |
| Dec 6 | Week 8 review against success criteria |

**Phase 0 checklist** (private repo `crewbo/spikes`)

- [ ] Claude Code hook blocks an edit to a listed file and injects one line of context, on Windows, macOS and Omarchy
- [ ] Codex hook does the same, including the trust prompt and `apply_patch` path parsing
- [ ] Shell-edit corpus: share of `sed -i`, redirects and scripts caught by parser plus hash check
- [ ] Post a message into a running Claude session through the messaging socket from an external process
- [ ] Push and fetch `refs/crewbo/...` on github.com; observe throttling
- [ ] Cross-machine `git merge-tree --write-tree` warning between two laptops
- [ ] Keyring probe on Omarchy with and without a secret service
- [ ] Interview 10 hackathon teams: do they mix agent vendors?

**Gate:** go if hooks block reliably on at least 2 of 3 OSes and at least 3 of 10 teams mix vendors; otherwise revisit positioning before week 2.

## Sources

Full research reports: `docs/research/risk-mitigations.md` and `docs/research/hook-and-dispatch-internals.md`.

- [Claude Code agent teams](https://code.claude.com/docs/en/agent-teams) and [cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging)
- [Conductor Cloud](https://www.conductor.build/docs/cloud) and [Conductor multiplayer](https://www.conductor.build/docs/cloud/collaboration)
- [Distributed Agent Teams feature request, anthropics/claude-code #79507](https://github.com/anthropics/claude-code/issues/79507)
- [Foremerge README](https://raw.githubusercontent.com/naw103/foremerge/HEAD/README.md)
- [Before the Pull Request (arXiv 2606.19616)](https://arxiv.org/pdf/2606.19616)
- [Gray and Cheriton, Leases (summary)](https://blog.acolyer.org/2014/10/31/leases-an-efficient-fault-tolerant-mechanism-for-distributed-file-cache-consistency/)
- [Git LFS file locking](https://github.com/git-lfs/git-lfs/wiki/File-Locking)
- [GitHub merge queue](https://docs.github.com/en/enterprise-server@3.19/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue)
- [GitHub OAuth device flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps)
- [The lethal trifecta, Simon Willison](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/)
- [Buf breaking change detection](https://buf.build/docs/breaking/)
- [Gutwin and Greenberg, Workspace Awareness](https://www.cs.usask.ca/faculty/gutwin/1999/WA-theory/Theory-submitted-TR.html)
- [Figma multiplayer](https://www.figma.com/blog/how-figmas-multiplayer-technology-works/)
- [Vibe Kanban Cloud launch](https://vibekanban.com/blog/introducing-vibe-kanban-cloud)
- [Paseo vs Conductor](https://paseo.sh/docs/alternatives/conductor)
- [Shape Up: appetite](https://basecamp.com/shapeup/1.2-chapter-03)
