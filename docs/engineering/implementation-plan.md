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
| M5: conflict warnings and extension v0 | Nov 15 to Nov 21 | Gate C |
| M6: dogfood in a real hackathon | Nov 22 to Nov 28 | |
| Week 8 review | Dec 5 | PRD success criteria |

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
| M5-04 | `crewbo task merge`: the assignee (or the lead) merges a task through the conflict check (ADR 0014); optional lead-only room mode | Re-runs merge-tree against current main and every other active task branch; refuses a conflict with main, and with an active branch unless the lead overrides; a push rejected because main moved re-fetches, re-checks and retries; in lead-only mode members are refused | Integration tests with a local bare remote, including two members merging at once | M5-02 |
| M5-05 | Extension v0: claims, intents, warnings, approve task | Works in VS Code; lists live state | Manual test plan; unit tests for the client | M3-05 |
| M5-06 | Dashboard v0 served by the daemon | Localhost only, token, Origin check | Integration tests | M1-07 |
| M5-07 | Self-started tasks (ADR 0014): `crewbo task start "<text>" --path <glob>...` and registering an existing branch | A member gets a task branch and worktree, claims on its paths, checkpoints and merge-tree checks without a lead or approval; the room event carries no lead signature and dispatched-task verification is unchanged; a registered branch is checkpointed and checked like a task branch | Unit tests; e2e with two daemons | M5-01, M4-06 |
| M5-08 | Move a session and its rooms to a new server address | `crewbo login --server <new>` (or a room command) moves this machine's room bindings from the old address when the server still knows the rooms, so a restarted tunnel needs no hand edits (M4 check finding 4) | Unit tests; manual with a restarted tunnel | M3-04, M3-06 |

Carried over from M4, proposed for M5 if time allows (otherwise M6): `crewbo task cancel` (the Cancelled state exists, no command), and the device key rotation runbook the threat model promised (`docs/runbooks/` is empty).

## M5b: Antigravity adapter (stretch, ADR 0013)

Only if M1 to M5 are on time. Contract: `docs/research/antigravity-hooks.md`; fixtures from spike S9.

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M5b-01 | `hooks/antigravity` decode and encode | Decodes all S9 fixtures (`TargetFile`, `CommandLine`, `Cwd`, camelCase common fields); deny prints `{"decision":"deny","reason":...}`; unblocked calls print nothing (never `{}`, never `allow`); `PreInvocation` returns `injectSteps` with an `ephemeralMessage` | Golden fixture tests | M2-01 |
| M5b-02 | Install and uninstall one named `crewbo` entry in `~/.gemini/config/hooks.json`; MCP entry in `~/.gemini/config/mcp_config.json` | Idempotent; other named hooks untouched; refuses a binary path with spaces | Unit tests on sample configs | M5b-01, M2-07 |
| M5b-03 | Crew panel and doctor support for Antigravity sessions (`conversationId`, `modelName`) | Antigravity sessions appear with their model; doctor detects hooks that never fire | Unit tests; manual run with agy | M5b-02, M5-05 |

## M6: dogfood

Install on 2 to 5 machines across 3 OSes, run a real hackathon, collect issues, measure the PRD success criteria.
