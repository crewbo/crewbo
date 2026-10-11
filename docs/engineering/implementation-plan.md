# Implementation plan

Timeline shifted 1 day earlier than the PRD at Ronel's request (decision log 2026-10-11). Budget about 22 effective hours per week. Scope shrinks to fit; dates do not move.

| Milestone | Dates (2026) | Gate |
| --- | --- | --- |
| Phase A: planning package | Oct 11 | Gate A |
| M0: Phase 0 spikes | Oct 12 to Oct 17 | Gate B (go / no-go Oct 17) |
| M1: protocol and daemon skeleton | Oct 18 to Oct 24 | Gate C |
| M2: Claude Code and Codex hook adapters | Oct 25 to Oct 31 | Gate C |
| M3: sync server and auth | Nov 1 to Nov 7 | Gate C |
| M4: lead dispatch | Nov 8 to Nov 14 | Gate C |
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
| M1-04 | `internal/claims`: claim set, lease expiry, path normalisation, conservative glob overlap, `Decide` | Pure, no I/O; 85%+ coverage | Table tests; property tests for overlap symmetry | none |
| M1-05 | `test/sim`: seeded deterministic simulator for claims and leases | Reproducible from a seed; invariants checked (no overlapping active claims, leases expire) | `go test ./test/sim -run Sim -seed N` | M1-04 |
| M1-06 | `internal/platform` IPC listener and dialer (Unix socket; Windows per U-16) | Same-user only; round trip under 10 ms locally | Integration test on 3 OSes | M1-02 |
| M1-07 | `internal/daemon` lifecycle: owner goroutine, local API (Decide, Status), graceful shutdown | `crewbo daemon` starts, answers `crewbo status`, stops on signal | Integration test spawning the daemon | M1-04, M1-06 |
| M1-08 | `internal/store` SQLite (pure Go) with migrations for the daemon queue | Migrations idempotent; WAL mode | Unit tests with temp dirs | M1-01 |
| M1-09 | `internal/gitx`: version check (>= 2.38), worktree add and remove, merge-tree | Refuses old git; worktrees created with long-path flag on Windows | Integration tests with real git in temp repos | M1-01 |

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

## M3: sync server and auth

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M3-01 | `crewbo serve` HTTP and WebSocket skeleton, TLS, health | Serves `/healthz`; refuses plain HTTP except localhost or `--insecure-lan` | Integration test | M1-03 |
| M3-02 | Room goroutine, event log with gap-free seq, SQLite persistence | Restart preserves log; seq gap-free under concurrency | Unit plus `-race` stress test | M3-01, M1-08 |
| M3-03 | Auth: GitHub device flow in CLI, session exchange on server | `crewbo login` works against a real OAuth app | Manual test plus unit tests with a fake GitHub | M3-01 |
| M3-04 | Device keys: generate, keychain with 0600 fallback, register | Fallback path taken without secret service | Unit tests with fake keyring; manual on Omarchy | M3-03 |
| M3-05 | Daemon sync client: hello, replay, local queue, reconnect | Resync after server restart and after offline edits | e2e: two daemons, one server, kill and restart | M3-02 |
| M3-06 | Rooms: create, invite, join, leave, 10-member cap, lead role | Invite expiry and use limits enforced | Unit and e2e | M3-02, M3-03 |
| M3-07 | Claims over the wire with server-time leases; propagation under 2 s | Measured p95 under 2 s on LAN | e2e timing test | M3-05, M1-04 |

## M4: lead dispatch

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M4-01 | Task signing and verification (exact-bytes Ed25519) | Rejects tampered, unknown, revoked, non-lead, expired, wrong-room, replayed | Table tests; 85%+ coverage | M3-04 |
| M4-02 | `crewbo task create` and store-and-forward | Offline teammate receives task on next hello | e2e | M4-01, M3-05 |
| M4-03 | Approval UX in CLI: full verbatim text, approve or decline | Nothing launches without approval | Unit tests; manual | M4-02 |
| M4-04 | Claude Code launcher: cwd = worktree, explicit permission mode, strip `ANTHROPIC_API_KEY`, never `--bare` | Flags asserted in tests; preflight `claude auth status` | Unit tests with a fake binary; manual run | M4-03, M1-09 |
| M4-05 | Codex launcher: `codex exec -C <wt> --sandbox workspace-write --json` | Flags asserted; JSONL parsed | Unit tests with a fake binary; manual run | M4-03, M1-09 |
| M4-06 | Task state machine and status events | All transitions in data-model.md enforced | Table tests | M4-02 |

## M5: conflict warnings and extension v0

| ID | Issue | Acceptance criteria | Test plan | Deps |
| --- | --- | --- | --- | --- |
| M5-01 | Checkpoint on stop: commit, push `refs/crewbo/...` (fallback per U-06) | Ref visible on remote | Integration test against a local bare remote | M1-09, M2-01 |
| M5-02 | Cross-machine merge-tree check and `ConflictWarning` event | Warning reaches all room members | e2e with two clones | M5-01, M3-05 |
| M5-03 | Escalation ladder: handoff request, owner decision, lead override | All ladder steps logged | e2e | M3-07 |
| M5-04 | Serial merge queue on the lead | Re-runs merge-tree against current main; refuses conflicts | Integration test | M5-02 |
| M5-05 | Extension v0: claims, intents, warnings, approve task | Works in VS Code; lists live state | Manual test plan; unit tests for the client | M3-05 |
| M5-06 | Dashboard v0 served by the daemon | Localhost only, token, Origin check | Integration tests | M1-07 |

## M6: dogfood

Install on 2 to 5 machines across 3 OSes, run a real hackathon, collect issues, measure the PRD success criteria.
