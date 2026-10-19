# Implementation plan

Timeline shifted 1 day earlier than the PRD at Ronel's request (decision log 2026-10-11). Budget about 22 effective hours per week. Scope shrinks to fit; dates do not move.

| Milestone | Dates (2026) | Gate |
| --- | --- | --- |
| Phase A: planning package | Oct 11 | Gate A |
| M0: Phase 0 spikes | Oct 11 (finished early) | Gate B closed Oct 11: go (conditions met) |
| M1: protocol and daemon skeleton | Oct 11 to Oct 24 (started early, Gate C approved Oct 11; issues #10 to #19) | Gate C |
| M2: Claude Code and Codex hook adapters | Oct 25 to Oct 31 | Gate C |
| M3: sync server and auth | Nov 1 to Nov 7 (finished early, complete Oct 12) | Gate C |
| M4: lead dispatch | Nov 8 to Nov 14 (finished early, complete Oct 13) | Gate C |
| M5: conflict warnings and extension v0 | Nov 15 to Nov 21 (finished early, complete Oct 19) | Gate C |
| M5b: Antigravity adapter | finished Oct 18 | Gate C |
| M6: dogfood in a real hackathon | Oct 18 to the review (see the launch plan below) | Gate C approved Oct 18 |
| M7: the crew and the brand in the editor | Oct 20 to Nov 1 | Gate C |
| M8: Crewbo IDE preview (ADR 0016) | Nov 2 to Nov 15 | Gate C |
| **Launch: v0.1.0, extension in both stores, Crewbo IDE preview, website** | **target Nov 22** | release checklist |
| Dogfood hackathon (M6-09) | a hackathon between Nov 28 and Dec 13 (Ronel to pick) | |
| Week 8 review (M6-10) | one week after the hackathon | PRD success criteria |

Launch plan (Ronel, 2026-10-19): the IDE ships at launch as a preview (ADR 0016), so the launch moves to about Nov 22 and the dogfood hackathon follows it. This replaces "dates do not move" for M6 to M8: the order holds, and scope inside M7 and M8 shrinks first if a date is at risk (the caret rider, then extra IDE package formats). A hackathon on Oct 30 is too early for the launch; it can serve as an optional friendly test with one or two teams on the current build.

Issue size target: one PR, under 400 changed lines excluding generated code. IDs are stable; GitHub issues are created at each Gate C. "Deps" lists issues that must merge first.

## M0: Phase 0 spikes (repo `crewbo/spikes`)

| ID | Spike | Acceptance | Who |
| --- | --- | --- | --- |
| S1 | Claude Code hook blocks an edit to a listed file and injects one line of context | Deny observed for Edit, Write, Bash redirect; context visible to the model; payloads captured; on Omarchy, Windows, macOS | Claude on Omarchy; checklist for Ronel (Windows) and a teammate (macOS) |
| S2 | Codex hook: same, including trust prompt and `apply_patch` parsing; U-02, U-03, U-14 | Deny observed; trust behaviour after binary swap recorded; payloads captured | Same split |
| S2b | `codex exec` exit codes and JSONL; App Server `turn/steer` (U-07) | Documented with raw output | Claude |
| S3 | Shell-edit corpus: share of `sed -i`, redirects, heredocs, scripts caught by a mvdan/sh parser plus hash check (U-05) | Table of 50+ commands with caught / missed | Claude |
| S4 | Post a message into a running Claude session through the messaging socket from an external process (U-01) | Works or not, with frames captured | Claude |
| S5 | Push and fetch `refs/crewbo/...` on github.com; observe throttling (U-06) | Push accepted or rejected; rate observations | Claude |
| S6 | Cross-machine `git merge-tree --write-tree` warning between two laptops; lease across sleep (U-09) | Conflict detected across two clones; sleep behaviour recorded | Claude plus Ronel |
| S7 | Keyring probe on Omarchy with and without a secret service (U-08) | Result per configuration | Claude |
| S8 | Interview 10 hackathon teams: do they mix vendors? (U-10) | 10 interview notes | Ronel |

Deliverables: `crewbo/spikes` README per spike; `docs/research/phase0-findings.md` in this repo; golden fixtures in `test/fixtures/hooks/`; updated unknowns; superseding ADRs if needed.

## M1: protocol and daemon skeleton

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M1-01 | `cmd/crewbo` with subcommand router and `crewbo version` | `crewbo version` prints version and commit; unknown subcommand exits 2 with usage | Unit test of router; CI builds on 3 OSes | none |
| M1-02 | `internal/platform` paths (data, runtime, config) per OS | Correct dirs per OS; created with 0700 | Table tests with injected env; run on 3 OSes in CI | M1-01 |
| M1-03 | `proto/crewbo/v1` envelope, hello, claims, events; `buf generate` | `buf lint` clean; generated code committed in its own commit; drift check in CI | CI drift job | M1-01 |
| M1-04 | `internal/claims`: claim set, lease expiry, path normalisation, conservative glob overlap, `Decide` | Pure, no I/O; 85%+ coverage. Normalisation covers every path form seen in Phase 0 fixtures: absolute POSIX, absolute Windows with backslashes, and relative `apply_patch` paths; case-fold match on Windows and macOS | Table tests; property tests for overlap symmetry | none |
| M1-05 | `test/sim`: seeded deterministic simulator for claims and leases | Reproducible from a seed; invariants checked (no overlapping active claims, leases expire) | `go test ./test/sim -run Sim -seed N` | M1-04 |
| M1-06 | `internal/platform` IPC listener and dialer (Unix socket; Windows per U-16, library choice logged in the decision log) | Same-user only; round trip under 10 ms locally (hook spawn alone is p50 13.7 ms on Windows, so IPC must stay small) | Integration test on 3 OSes | M1-02 |
| M1-07 | `internal/daemon` lifecycle: owner goroutine, local API (Decide, Status), graceful shutdown | `crewbo daemon` starts, answers `crewbo status`, stops on signal | Integration test spawning the daemon | M1-04, M1-06 |
| M1-08 | `internal/store` SQLite (pure Go) with migrations for the daemon queue | Migrations idempotent; WAL mode | Unit tests with temp dirs | M1-01 |
| M1-09 | `internal/gitx`: version check (>= 2.38), worktree add and remove, merge-tree | Refuses old git; worktrees created with long-path flag on Windows; merge-tree exit 0 clean, 1 conflict with paths, other = error (as in Phase 0 S6) | Integration tests with real git in temp repos | M1-01 |
| M1-10 | CI: per-package coverage report and 85% threshold for `claims` (later `shellparse`, `hooks`, `dispatch`) | Job fails below threshold for listed packages that exist | CI run on a PR | M1-04 |

## M2: Claude Code and Codex hook adapters

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M2-01 | `internal/hooks` normalised event model and hook main loop (stdin cap, deadline, fail open) | Any error yields allow plus warning; stdout is only JSON | Unit tests with fake daemon; timeout test | M1-07 |
| M2-02 | `hooks/claudecode` decode and encode | All Phase 0 fixtures decode; deny output matches docs | Golden fixture tests | M2-01, M0 |
| M2-03 | `hooks/codex` decode, `apply_patch` header parser, strict encoder | Never emits unsupported fields; all fixtures pass | Golden fixtures; fuzz test of patch parser | M2-01, M0 |
| M2-04 | `internal/shellparse` POSIX write targets | Catches the S3 corpus share measured in Phase 0 or better | Corpus test; fuzz | none |
| M2-05 | `internal/shellparse` PowerShell subset | Set-Content, Add-Content, Out-File, Remove-Item, Move-Item, `>` | Table tests | M2-04 |
| M2-06 | `post_edit` hash check of claimed files | Detects writes the parser missed; records violation | Integration test with real files | M2-01 |
| M2-07 | `crewbo hooks install` / `uninstall` for both agents (user-level config, exec form, stable entries) | Idempotent; preserves existing user hooks; backs up config | Unit tests on sample configs from 3 OSes | M2-02, M2-03 |
| M2-08 | `crewbo mcp` read-only tools (team state, claims, intents) | Registered with both CLIs; tools return daemon state | Integration test with the SDK client | M1-07 |
| M2-09 | Daemon `event` API: register sessions, renew leases on every hook event (#43) | `decide` also renews; status counts sessions | Daemon tests with an injected clock | M1-07 |
| M2-10 | Local `crewbo claim add / list / release` before the sync server (#44) | Demo: a terminal claim blocks a Claude Code edit in the same repo | CLI tests; manual demo | M2-09 |

## M3: sync server and auth

Refined for Gate C on 2026-10-11 with what M1 and M2 taught. Unchanged from the base plan: ADR 0008 (Go WebSocket hub in the same binary, one goroutine per room, SQLite) and ADR 0009 (GitHub device flow, server-issued session token, no GitHub token stored).

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M3-01 | `crewbo serve`: HTTP and WebSocket skeleton, TLS, `/healthz`, Envelope framing | Serves `/healthz`; refuses plain HTTP except on loopback or with `--insecure-lan`; WebSocket upgrade exchanges `Hello`/`Welcome`/`Error` Envelopes. Dependency `github.com/coder/websocket` (ISC, v1.8.15, 2026-06-15) gets a decision-log entry after checking its API at that tag | Integration tests with `httptest` and a real WebSocket client | M1-03 |
| M3-02 | Room goroutine and gap-free event log in the server's own SQLite file (rooms, members, events) | Restart preserves the log; `seq` gap-free under concurrent writers; replay from any cursor | Unit plus `-race` stress test | M3-01, M1-08 |
| M3-03 | `crewbo login`: GitHub device flow in the CLI; server verifies the GitHub token once and issues its own session token | `crewbo login` works against Ronel's OAuth app; honours `interval` and `slow_down` (+5 s); code expiry handled; GitHub token never stored by the server | Unit tests against a fake GitHub; manual login on Omarchy | M3-01 |
| M3-04 | Device keys: Ed25519 generate, keychain with a hard 3 s timeout (U-18), 0600 file fallback, register public key with the server | Fallback taken when no secret service answers; never blocks over 3 s | Unit tests with a fake keyring; manual on Omarchy | M3-03 |
| M3-05 | Daemon sync client: hello with room cursors, replay, `outbox` drain, reconnect with backoff; one room bound to one repository | Resync after server restart and after offline claims; local-only mode (M2 behaviour) when not logged in or not bound | e2e: two daemons, one server, kill and restart | M3-02, M3-03 |
| M3-06 | Rooms: create, invite, join, leave, 10-member cap, lead role; `crewbo room` subcommands (create, invite, join, leave) bind the current repository | Invite expiry and use limits enforced; 11th member refused | Unit and e2e | M3-02, M3-03 |
| M3-07 | Claims over the wire: server-time leases, terminal and agent claims carry the member identity, conflicts across machines denied by the hooks | A claim on daemon A blocks an edit through daemon B; propagation p95 under 2 s on LAN; offline daemon keeps its own claims and reconciles on reconnect | e2e timing test; manual two-machine check (Omarchy plus gavriel953 on macOS with Codex) | M3-05, M3-06, M1-04 |

Notes from M1 and M2 that shape M3:

- The daemon already keeps claims per repository, so a room maps to one repository; the binding lives in daemon state, not in the repo.
- The `crewbo claim` terminal holder becomes the logged-in member; without login everything stays local as today.
- `internal/store` already has `outbox` and `room_cursor`; `proto/crewbo/v1` already has Envelope, Hello, Welcome, Ack, Error, Claim messages and claim events. New messages go in their own `build(proto)` commit.
- Needs from Ronel: a GitHub OAuth app under the `crewbo` org with device flow enabled (client ID only; the device flow needs no client secret, per docs.github.com "Authorizing OAuth apps", fetched 2026-10-11), and a reachable host for the two-machine check in M3-07.

## M4: lead dispatch

Refined for Gate C on 2026-10-12 with what M3 taught. Unchanged: ADR 0006 (Ed25519 device keys, sign the exact task bytes, verify against the lead's registered key before showing anything, verbatim text, explicit approval, fresh worktree, the teammate's own agent and login, never a bypass permission mode, task text is data).

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M4-01 | Task signing and verification, and the key lookup it needs: `build(proto)` for `TaskBody`, `SignedTask` and task status; a server endpoint listing room members' registered device keys (with revocation) and the current lead | Verification rejects tampered bytes, unknown, revoked and non-lead keys, expired, wrong room, wrong assignee, replayed task ids; nothing is decoded before the signature checks | Table tests over every rejection; 85%+ coverage | M3-04, M3-06 |
| M4-02 | `crewbo task create` (lead only) and delivery through the room event log | A teammate offline at dispatch receives the task on the next connection (replay); the server never alters task bytes | e2e: two daemons, one offline at dispatch | M4-01, M3-05 |
| M4-03 | Approval in the CLI: `crewbo task list`, `show`, `approve`, `decline` | The full text is shown verbatim before approval; nothing launches without `approve`; decline and expiry are recorded as task status events | Unit tests; manual | M4-02 |
| M4-04 | Claude Code launcher: fresh worktree (`gitx`), cwd in it, explicit `--permission-mode acceptEdits`, `ANTHROPIC_API_KEY` stripped unless the member opts in, never `--bare`; the task's owned paths claimed for the agent session, with `ClaimRenew` sent while it runs (deferred from M3-07) | Flags and environment asserted; preflight `claude auth status`; claims renewed and released at exit | Unit tests with a fake binary; manual run | M4-03, M1-09 |
| M4-05 | Codex launcher: `codex exec -C <worktree> --sandbox workspace-write --json` | Flags asserted; JSONL parsed for status; Windows refused with a clear message until U-20 is resolved | Unit tests with a fake binary; manual run on Linux and macOS | M4-03, M1-09 |
| M4-06 | Task state machine and status events | Every transition in data-model.md enforced on the daemon and visible to the lead (`crewbo task list`) | Table tests | M4-02 |
| M4-07 | Two-machine dispatch check (lead on Omarchy, teammate on macOS through the tunnel), like the M3 check | A signed task reaches the teammate, is shown verbatim, runs only after approval, in a worktree, with claims, and reports status back | Manual, both machines, written reports | M4-01 to M4-06 |

Notes from M3 that shape M4:

- Delivery is the room log: a dispatched task is a room event, so replay from M3-05 gives store-and-forward to offline teammates without new transport.
- Verification needs keys the daemon does not have today: the server must list members' public keys and revocation, and the current lead (M4-01).
- Agent-held claims expire in 5 minutes unless renewed; the daemon starts sending `ClaimRenew` for running dispatched tasks (M4-04).
- Windows Codex dispatch stays unsupported until U-20 is retested; the launcher refuses it with a clear message.
- Nothing in M4 changes how members sign in or how claims work for people at a terminal.

## M5: conflict warnings and extension v0

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M5-01 | Checkpoint on stop: commit, push `refs/crewbo/...` (fallback per U-06) | Ref visible on remote | Integration test against a local bare remote | M1-09, M2-01 |
| M5-02 | Cross-machine merge-tree check and `ConflictWarning` event | Warning reaches all room members | e2e with two clones | M5-01, M3-05 |
| M5-03 | Escalation ladder: handoff request, owner decision, lead override | All ladder steps logged | e2e | M3-07 |
| M5-04 | `crewbo task merge`: the assignee (or the lead) merges a task through the conflict check (ADR 0014); optional lead-only room mode | Re-runs merge-tree against current main and every other active task branch; refuses a conflict with main, and with another task's checkpoint unless the member confirms it with `--despite` (ADR 0015); a push rejected because main moved re-fetches, re-checks and retries; in lead-only mode members are refused | Integration tests with a local bare remote, including two members merging at once | M5-02 |
| M5-05 | Extension v0: claims, intents, warnings, approve task | Works in VS Code; lists live state | Manual test plan; unit tests for the client | M3-05 |
| M5-06 | Dashboard v0 served by the daemon | Localhost only, token, Origin check | Integration tests | M1-07 |
| M5-07 | Self-started tasks (ADR 0014): `crewbo task start "<text>" --path <glob>...` and registering an existing branch | A member gets a task branch and worktree, claims on its paths, checkpoints and merge-tree checks without a lead or approval; the room event carries no lead signature and dispatched-task verification is unchanged; a registered branch is checkpointed and checked like a task branch | Unit tests; e2e with two daemons | M5-01, M4-06 |
| M5-08 | Move a session and its rooms to a new server address | `crewbo login --server <new>` (or a room command) moves this machine's room bindings from the old address when the server still knows the rooms, so a restarted tunnel needs no hand edits (M4 check finding 4) | Unit tests; manual with a restarted tunnel | M3-04, M3-06 |

Carried over from M4, proposed for M5 if time allows (otherwise M6): `crewbo task cancel` (the Cancelled state exists, no command), and the device key rotation runbook the threat model promised (`docs/runbooks/` is empty).

## M5b: Antigravity adapter (stretch, ADR 0013)

Only if M1 to M5 are on time. Contract: `docs/research/antigravity-hooks.md` (S9 on agy 1.3.3, and the docs re-check of 2026-10-18, which found a `SessionStart` event, a `PostInvocation` event and new `PreToolUse` fields). Revised for Gate C on 2026-10-18: M5b-03 no longer depends on the crew panel (deferred in M5-05) or `crewbo doctor` (never scheduled). Headless dispatch to Antigravity stays out of v1 (ADR 0013; U-28 open).

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M5b-01 | `hooks/antigravity` decode and encode, with fixtures recaptured on the installed agy (1.3.5) | Decodes every fixture (`TargetFile` absolute, `CommandLine`, `Cwd`, camelCase common fields, `SessionStart` if it fires); `SessionStart` (or the first `PreInvocation`) maps to `session_start`, `PreInvocation` to `prompt`, `PreToolUse` on the write tools to `pre_edit` and on `run_command` to `pre_shell`, `PostToolUse` to `post_edit`, `Stop` to `stop`; a deny prints `{"decision":"deny","reason":...}`; an unblocked call prints nothing (never `{}`, never `allow` or `auto_approve`); `PreInvocation` returns `injectSteps` with an `ephemeralMessage` only when there is something to say | Golden fixture tests; the S9 probe rerun on agy 1.3.5 (Omarchy), recording what changed | M2-01 |
| M5b-02 | `crewbo hooks install` / `uninstall --agent antigravity` (and `all`): one named `crewbo` entry in `~/.gemini/config/hooks.json`; MCP entry in `~/.gemini/config/mcp_config.json` | Idempotent; other named hooks and MCP servers untouched; backup as for the other agents; refuses a binary path with spaces (shell-form commands only); uninstall leaves no empty file | Unit tests on sample configs; one live install on Omarchy with the real file backed up first | M5b-01, M2-07 |
| M5b-03 | Live check and docs: an Antigravity agent blocked by a teammate's claim | With the hooks installed, an `agy` session on Omarchy is denied an edit to a file claimed in the room (Crewbo's reason reaches the model), edits a free file, and the daemon log records its session (`agent session registered ... agent=antigravity`); README and the user docs cover install and MCP registration; Windows run if Ronel boots Windows, else marked untested | Manual run with agy, recorded in `docs/research/` | M5b-02 |

Crew panel rows and `crewbo doctor` checks for Antigravity move to whenever those features are built (not scheduled).

## M6: dogfood

Install on 2 to 5 machines across 3 OSes, run a real hackathon, collect issues, measure the PRD success criteria (5 real teams, at least 3 mixing agent vendors). Proposed for Gate C on 2026-10-18 with Ronel's decisions: the repository goes public under ADR 0011 (AGPL-3.0 for the sync server), the hackathon sync server runs on Ronel's laptop behind a Cloudflare named tunnel, the extension is packaged as a `.vsix` with `@vscode/vsce` (build-time only, approved by Ronel), and success is measured with a survey plus an opt-in report members paste themselves (no telemetry, CLAUDE.md rule 12).

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M6-01 | Licences and public readiness | `internal/server/LICENSE` holds the AGPL-3.0 text (from gnu.org); README and release notes state which licence covers what and that the binary as a whole is AGPL-3.0; a full-history secret scan and a personal-data review (interview notes, emails, tunnel addresses, test logins) with fixes or Ronel's sign-off; contribution policy (DCO or CLA) decided; Ronel then makes the repository public (CLAUDE.md rule 6) | Scan report in the PR; checklist signed off by Ronel | ADR 0011 |
| M6-02 | `crewbo doctor` | One line per check with a fix: daemon running, signed in, room for this repository, server reachable, hooks installed per agent and seen firing (decision log 2026-10-11), git identity set, binary path without spaces, login shell startup files print nothing (U-25); exit code non-zero when a check fails | Unit tests per check; manual run on Omarchy | M2-07, M5b-02 |
| M6-03 | `crewbo report` (opt-in) | Prints, for the member to paste into the survey: Crewbo version, OS, agents seen by this daemon (from hook events), rooms and member counts, claims, tasks, handoffs, conflict warnings; no file paths or task text; sends nothing | Unit tests; output reviewed against rule 12 | M6-02 |
| M6-04 | Getting started and privacy notice (`docs/user/`) | Install, sign in, create or join a room, install hooks (Claude Code, Codex, Antigravity), extension, dashboard, claims and handoffs, tasks and merging, `crewbo doctor`; what leaves the machine and what the server stores; troubleshooting from the M3 to M5 checks | A teammate who has not used Crewbo follows it on a fresh machine | M6-02 |
| M6-05 | Extension `.vsix` | `@vscode/vsce` pinned as a dev dependency (decision log: licence, Ronel's approval); `task ext:package` builds `crewbo-<version>.vsix` with the brand icon; the release workflow attaches it | Install the `.vsix` in VS Code on Omarchy; the in-editor check passes | M5-05 |
| M6-11 | Publish the extension to the Visual Studio Marketplace and Open VSX (added 2026-10-18, Ronel; moved from v2) | Store listing fields in `extension/package.json` (preview flag, links, keywords, brand banner and icon); `docs/runbooks/publishing-extension.md` with the one-time account steps and the publish commands; Crewbo installable by searching in VS Code and in Cursor | Search and install from both stores after the first publish | M6-01, M6-05 |
| M6-06 | Release v0.1.0 | Release PR moves Unreleased to 0.1.0; GoReleaser artifacts for 3 OSes, 2 architectures, checksums, signature decision (cosign keyless or minisign); smoke test on Linux, macOS and Windows before publishing | Release checklist in `release-process.md` | M6-01, M6-05, M6-08 |
| M6-07 | Hackathon sync server runbook | `docs/runbooks/hackathon-server.md`: `crewbo serve` behind a Cloudflare named tunnel on a stable hostname (Ronel's domain), start and stop, backup of `server.db`, network requirements (port 7844 outbound), what to do when the laptop sleeps or the network blocks the tunnel, Tailscale fallback | Dry run with the M5 check machines | M3-04 |
| M6-08 | Cross-OS verification | Once GitHub Actions minutes return (1 November): CI green on Windows and macOS for everything since #127; a manual Windows run (Claude Code, Codex and Antigravity hooks, dispatch, U-31, U-34) | CI runs; manual report in `docs/research/` | none |
| M6-12 | Website | One static landing page on the brand, in its own repository `crewbo/crewbo.github.io` (GitHub publishes it at `https://crewbo.github.io` with no build or CI; a `CNAME` file moves it to `crewbo.dev` later): what Crewbo is, the dashboard and crew panel images, install steps for the CLI, the extension and the IDE, links to docs and GitHub; no trackers or third-party scripts (rule 12) | Brand checklist (`docs/brand/09-checklist.md`); phone and desktop widths; links checked | M6-04 |
| M6-09 | Dogfood event | Onboarding checklist for teams, a survey form, a `dogfood` issue label, daily triage during the event, fixes as their own PRs | Event report | M6-01 to M6-07 |
| M6-10 | Week-8 review | Report against the PRD success criteria and kill or pivot criteria, from the survey and `crewbo report`; recommendation for v2 | Review with Ronel | M6-09 |

## M7: the crew and the brand in the editor

The brand's crew panel and Bo in the editor (`docs/brand/05-interface.md` sections 3.1 to 3.5, `06-bo-character.md`), and the room data they need. Only vendor and presence are new data leaving a machine, both allowed by rule 12; the privacy page lists them.

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M7-01 | Room members, presence and agent vendor (protocol and server) | The server sends each daemon the room's members and who is connected, and updates on join, leave, connect and disconnect; a claim carries the agent vendor of the session that holds it (`claude-code`, `codex`, `antigravity`, or none for a terminal); old daemons and servers ignore the new fields | Server tests; cross-daemon test; `buf breaking` | none |
| M7-02 | Crew view in the daemon | `crew` in the local API: each member with online state (and since when offline), their agents and the files their agents hold, the member's own row first; `crewbo crew` prints it | Daemon tests; CLI test | M7-01 |
| M7-03 | Crew panel in the extension | A webview view with one row per member: scene Bo at 64 px (ported unchanged from `bo.js`), name, status (`Writing <file>`, `Idle`, `Offline since <time>`), agent name; offline Bo grey; reduced motion respected (`workbench.reduceMotion` and `prefers-reduced-motion`); no model shown unless known (BDR-017) | In-editor check; brand checklist with screenshots against `docs/brand/reference/` | M7-02 |
| M7-04 | Bo badge on claims in the Crewbo view, and the working-on-this-file line | Claim rows use the 16 px badge Bo in the holder's colour as their icon; a file another member's agent holds shows a quiet line on its first line, `riya · Codex is working on this file` (an `after` decoration, never over code) | In-editor check; brand checklist | M7-02 |
| M7-05 | Caret rider for the member's own agent (stretch) | While the member's own agent edits an open file, an 18 px Bo rides the changed line (`05-interface.md` 3.4), only for agent edits, never on the human's caret | In-editor check | M7-03 |

## M8: Crewbo IDE preview (ADR 0016)

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M8-01 | IDE build from VSCodium's scripts | A repository `crewbo/ide` (MIT, from VSCodium's scripts) builds Code OSS with Crewbo's `product.json` (name, icons from `docs/brand/assets`, Open VSX gallery, telemetry off) and the Crewbo `.vsix` built in; a local Linux build starts and shows the Crewbo view | Local build on Omarchy | M6-05 |
| M8-02 | IDE packages from CI | CI builds Linux (`tar.gz`, `deb`, AppImage), macOS (arm64, x64) and Windows (x64) on a tag and attaches them to a draft release with checksums and attestations | A tagged dry run | M8-01, M6-08 |
| M8-03 | Signing and first-run defaults | Windows signed through SignPath Foundation's free code signing for open-source projects if approved in time, else unsigned with the "More info, Run anyway" step on the download page; macOS unsigned unless Ronel approves the Apple Developer Program, with the "Open Anyway" step on the download page; the crew panel open on first run | Install on all three OSes (Ronel: Linux and Windows; gavriel953: macOS) | M8-02, M7-03 |
| M8-04 | IDE docs and download page | `docs/user/ide.md` and the website: what the IDE is, preview status, Open VSX instead of the Visual Studio Marketplace (and which extensions that leaves out), how to update | Read-through; links checked | M8-03, M6-12 |
