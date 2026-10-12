# Session handoff

Last updated 2026-10-12, end of session (M3 complete; M4 in progress at M4-02). A new Claude Code session starts by reading this file, `CLAUDE.md`, and the latest entry in `docs/engineering/worklog.md`. Update this file at the end of every session.

## Who and how

- **Owner:** Ronel Abraham Mathew (GitHub `RM1338`), founder. Works on an HP Victus dual-booting Omarchy (Arch Linux, Hyprland; this is where Claude runs) and Windows 11. Has Claude Code and Codex plans. No Mac.
- **Teammate:** `gavriel953`, macOS 27 (Apple M5, arm64, zsh), Codex 0.162.1 and, since 2026-10-12, a working Claude Code 2.1.287. His crewbo clone is at `~/Documents/Projects/crewbo/crewbo` (binary `bin/crewbo`), spikes at `~/Documents/Projects/crewbo/spikes`. Collaborator on `crewbo/crewbo` and `crewbo/spikes`.
- **Roles:** Claude is the founding engineer. Claude opens PRs; **Ronel reviews and merges every PR**. Claude never merges, never pushes to `main`, never force-pushes shared branches, never changes repo or org settings (permission rules block it; give Ronel `! gh api ...` commands instead).
- **Communication:** Ronel prefers short, plain explanations with a clear "what I need from you". Give a recommendation when he asks "what should I do". Explain jargon simply.
- **Hard rules:** everything in `CLAUDE.md` "Non-negotiable rules" (verify every external fact, no invented flags, no em dashes, no AI attribution in commits or PRs, Conventional Commits, gates, cross-platform by default).

## Repositories

| Repo | Purpose | Local clone |
| --- | --- | --- |
| `crewbo/crewbo` (private) | Product monorepo, Go module `github.com/crewbo/crewbo` | `/home/ronel/Work/crewbo` |
| `crewbo/spikes` (private) | Throwaway Phase 0 spikes, agent prompts for manual runs on other OSes, results branches | `/home/ronel/Work/spikes` |

The repo stays private until ADR 0011 (sync server licence) is decided. Org is on GitHub Free: no branch protection on private repos; macOS CI minutes cost about 10x Linux. Repo settings applied: squash merge only, delete branches on merge, Dependabot alerts and fixes, read-only workflow token.

## Where the project is

- **Phase A** (planning, PR #1): done. **Gate A** approved.
- **Phase 0** (spikes on Linux, Windows, macOS + 10 interviews): done. **Gate B: go.** Findings: `docs/research/phase0-findings.md`. Antigravity: `docs/research/antigravity-hooks.md`; **ADR 0013 accepted** (Antigravity adapter is a v1 stretch goal, M5b, after M5).
- **M1** (protocol and daemon skeleton, PRs #20 to #34): **complete.**
- **M2** (Claude Code and Codex hook adapters, PRs #45 to #54, follow-ups #57, #69, #71): **complete**, milestone closed.
- **M3** (sync server and auth, issues #59 to #65, PRs #67 to #90): **complete**, milestone closed 2026-10-12. Two-machine check with gavriel953 passed on Omarchy and macOS, Claude Code and Codex, through a Cloudflare quick tunnel (`docs/research/m3-two-machine-check.md`, spikes PRs #7 and #8). Follow-ups merged: #86 (terminal claim wording), #87 (daemon ping, offline detection about 30 s instead of 249 s), #89 (hook test deadline), #101 (hook names the real daemon error).
- **M4** (lead dispatch, milestone "M4: lead dispatch", issues #91 to #97): **Gate C approved 2026-10-12** (list in `implementation-plan.md` M4, PR #90). Progress:
  - **M4-01 (#91), done:** #98 (`build(proto)` task messages; `internal/dispatch` `Sign`/`Verify`/`WellFormed`/`NewTaskID`; google/uuid direct), #99 (`GET /v1/rooms/{id}/keys`).
  - **M4-02 (#92), in progress:** #100 (server admits `DispatchTask` only from the lead, signed by the lead's Hello device key, records a `TaskDispatched` event without decoding) and #102 (`crewbo task create`, daemon signs and sends through the outbox; `gitx.Head`) merged. **Remaining: the receiving side** (see "Next steps").
  - M4-03 to M4-07: not started.
- **GitHub OAuth app** "Crewbo" on the crewbo org: client ID `Ov23liMSnQmA2q3VqyiK` (constant `auth.ClientID`), device flow on, no client secret.

### What works today

Verified live on Omarchy unless noted; claims across machines also verified with macOS.

- `crewbo daemon`, `status` (with a `sync:` line), `version`, `help`.
- `crewbo claim add|list|release`: local; in a repository bound to a room, shared through the server (local grant first, server confirmation within 5 s, offline queue, conflicts name the holder).
- `crewbo hooks install|uninstall` (Claude Code exec form, Codex); byte-identical round trip; backups kept.
- `crewbo hook <claude-code|codex> <event>`: blocks Edit, Write, NotebookEdit, Bash, PowerShell (Claude Code) and apply_patch, Bash (Codex) on claimed files, including teammates' claims from another machine.
- `crewbo mcp` (`crewbo_claims`, `crewbo_who_holds`).
- `crewbo serve [--addr] [--tls-cert --tls-key] [--insecure-lan] [--data DIR]`: `/healthz`, `POST /v1/auth/session`, `POST /v1/devices`, `POST|GET /v1/rooms`, `POST /v1/rooms/join`, `POST /v1/rooms/{id}/invites`, `POST /v1/rooms/{id}/leave`, `GET /v1/rooms/{id}/keys`, `GET /v1/ws` (Hello with session, device key and room cursors; replay then live events; claim requests; DispatchTask from the lead).
- `crewbo login --server URL [--insecure-lan]` (GitHub device flow; 7-day session; device key in the keychain with a 3 s bound, else a 0600 file; key registered with proof of possession).
- `crewbo room create|invite|join|leave|status` (binds the repository root in `rooms.json`; lead; invites 24 h, 9 uses; 10-member cap).
- `crewbo task create --to LOGIN [--path P]... [--for D] (TEXT | --file F)` (lead only; signed; queued offline). Teammates do not receive or verify tasks yet.

### Package map

- `internal/claims`: pure claim and lease logic, 100% coverage; `Put`, `Extend`, `Get` added for server-granted claims.
- `internal/roomstate`: applies room events to a claims.Set, used by both server and daemon; a clock converts server-time leases using `Welcome.server_time_unix_ms`.
- `internal/dispatch`: task signing and verification (95%).
- `internal/auth`: GitHub device flow and user lookup; `session.json` and `rooms.json` (`Session`, `Binding`) in the config dir.
- `internal/keys`: device Ed25519 key (keychain via zalando/go-keyring with a 3 s bound, 0600 file fallback published by hard link); `RegistrationMessage`, `KeyID` (`ed25519:` + 16 bytes of SHA-256 in hex).
- `internal/server`:
  - `server.go`: serve, ws, Hello checks, request routing, `checkDispatch`;
  - `hub.go`: room goroutines, subscribe, forward, close with 1013;
  - `room.go`: event log owner and subscriptions;
  - `room_claims.go`: claim grant, release, renew, expiry, DispatchTask event;
  - `eventlog.go`: gap-free seq per room;
  - `session.go`, `devices.go`, `rooms.go`, `rooms_http.go`.
- `internal/daemon`:
  - `daemon.go`: owner goroutine, local API, `daemon.db`;
  - `sync.go`: syncer; in-memory cursors (replay from 0 on start); outbox; waiters; client pings 20 s with a 10 s pong deadline; reloads `session.json` and `rooms.json` every 2 s;
  - `shared.go`: claims through the server;
  - `task.go`: `createTask`.
- `internal/store`:
  - Daemon schema: outbox, room_cursor (unused).
  - Server schema: rooms and events, sessions, device_keys, members and invites.
- `internal/gitx`: system git; merge-tree, worktree add and remove, `Head`.
- `internal/hooks` (with `claudecode`, `codex` and `install`), `internal/shellparse`, `internal/mcpserver`, `internal/platform`.
- `internal/cli`: `login.go`, `room.go`, `task.go`, `serve.go`, `api.go` (`callServer`), `claim.go`, `hooks_install.go`.
- `internal/protocol` and `proto/crewbo/v1`: generated, never edit by hand. Messages: Envelope fields 40 `DispatchTask` and 41 `TaskStatus`; Event bodies 20 `TaskDispatched` and 21 `TaskStatusChanged`; `TaskState` enum; `ErrorCode` up to 8 `NOT_MEMBER`.

## Next steps (in order)

1. **Finish M4-02 (#92): the receiving side**, one PR.
   - **Store:** daemon store migration 2: a `tasks` table keyed by a digest (SHA-256) of the SignedTask bytes, with `task_id` (unique when set), `room_id`, `signed`, `state` (`dispatched`, `rejected`, or `other` for tasks addressed to someone else), `detail` and `received_unix_ms`.
   - **Receive:** in `Daemon.applyEvent`, or the syncer's apply callback, run in the syncer goroutine and not the owner, because it does network I/O:
     1. On `TaskDispatched`, skip a digest that is already stored (every daemon start replays from seq 0).
     2. Fetch `GET /v1/rooms/{id}/keys` with the session (cache per room; refetch once on `ErrUnknownKey`, `ErrNotLead` or `ErrRevokedKey`).
     3. Build `dispatch.Check{RoomID, Lead, Keys, Me: session login, Now, Seen: task_id stored}` and run `dispatch.Verify`.
     4. Store `dispatched` for my tasks, `other` for `ErrNotForYou`, and `rejected` with the reason otherwise.
   - **Logging:** never log task text.
   - **API:** add `MethodTaskList` to the daemon API (my tasks, newest first).
   - **Tests:** an e2e test with two daemons where the teammate is offline (link cut) at dispatch and receives the task after reconnecting; tampered and other-room tasks are rejected; replay after a daemon restart creates no duplicates. Then close #92.
2. **M4-03 (#93):** `crewbo task list|show|approve|decline`.
   - `show` prints the full text verbatim with author, base commit, owned paths and expiry.
   - Nothing launches without `approve`.
   - Decline and expiry send `TaskStatus` (Envelope field 41) and are recorded.
   - The server must accept `TaskStatus` from the assignee and record `TaskStatusChanged`. Today `room_claims.go` `handle` answers UNSUPPORTED for it.
3. **M4-04 (#94): Claude Code launcher.**
   - **Workspace:** a worktree via `gitx.WorktreeAdd` at the task's base commit on a branch such as `crewbo/<task id>`, used as cwd.
   - **Command:** `claude -p` with `--permission-mode acceptEdits`, never `--bare`. `ANTHROPIC_API_KEY` is stripped unless the member opts in. Preflight `claude auth status`.
   - **Claims:** the task's owned paths are claimed for the agent session, and `ClaimRenew` is sent while it runs; the daemon has never sent ClaimRenew so far, and the server already handles it. Claims are released at exit.
   - **Tests:** unit tests with a fake binary that assert flags and environment.
4. **M4-05 (#95): Codex launcher.** `codex exec -C <worktree> --sandbox workspace-write --json`, parsing the JSONL. Refuse on Windows until U-20 is resolved.
5. **M4-06 (#96): task state machine** per `data-model.md`, enforced on the daemon, with status visible to the lead.
6. **M4-07 (#97): two-machine dispatch check** with gavriel953, like the M3 one. Write `crewbo/spikes` `agent-prompts/m4-two-machine.md`, fixing the M3 prompt's lessons: install both agents' hooks; put the `claude -p` prompt before flags; use the build path the Mac actually has.
7. **Then Gate C for M5** (conflict warnings, VS Code extension v0, dashboard v0; read `docs/brand/` before any UI work). After M5, optionally M5b (Antigravity).

## Watch list

- Flaky timing tests in CI: the hook tests now use 5 s deadlines (#71, #89). A Codex hook test once failed in 20 ms with a hidden error; since #101 the message names the real cause, so if it recurs, read it and fix the root cause.
- `crewbo/spikes` agent prompts: gavriel953 now has a working Claude Code (2.1.287) as well as Codex.
- Cloudflare quick tunnel works with wss (U-30) but is for testing only: new address each run, no uptime guarantee. Tailscale is the fallback; its free plan was not verified (pages failed TLS on this network).
- This network intercepts TLS for some hosts (mise downloads, tailscale.com): `UnknownIssuer`. `claude` on this machine is a mise shim.

## Working habits that matter in this environment

- **Lint:** the Arch `golangci-lint` 2.13.2 fails against Go 1.27.2. Download the official v2.14.0 linux-amd64 release into the session scratchpad and verify its SHA-256 against `golangci-lint-2.14.0-checksums.txt` (`gh release download v2.14.0 --repo golangci/golangci-lint -p 'golangci-lint-2.14.0-linux-amd64.tar.gz' -p 'golangci-lint-2.14.0-checksums.txt'`), or use `GOTOOLCHAIN=local golangci-lint run`. Always lint for Linux, `GOOS=windows` and `GOOS=darwin`.
- **Live tests: isolate first, then run.** Put the whole live test in one script that exports `CLAUDE_CONFIG_DIR`, `CODEX_HOME` and the XDG dirs, checks they are set, and exits on the first failure (`|| exit 1`). Build with `go-task build` (`task` is not on PATH here). A broken chain once let a stale binary run `hooks uninstall` against Ronel's real `~/.codex`.
- **Gate commits explicitly.** `set -e` did not stop a failing chain in this shell. Use an `ok=1; cmd || ok=0; ...; [ $ok = 1 ] && git commit ...` pattern and never chain commit or push after a check with `;`.
- **Before every PR:** `go test -race -count=1 ./...`, `scripts/coverage-gate.sh` (85% for claims, shellparse, hooks/..., dispatch), lint on 3 OSes, `gofmt`/`golangci-lint fmt --diff`. After CI, confirm from the job logs that new packages actually ran on macOS and Windows (coverage line present), not just "pass".
- **Live login tests** need Ronel to approve a code at github.com/login/device (15-minute expiry). Reuse the saved test session in the scratchpad `live-dev` folder when it still exists (valid 7 days); run `crewbo login` under `dbus-run-session` so the real keychain is untouched.
- **Live daemon tests** need a short `XDG_RUNTIME_DIR` (socket path max 103 bytes): use `/run/user/1000/cbt-live` and remove it afterwards.
- **The scratchpad gets wiped between turns**: recreate `gate.sh` and the golangci-lint download (checksum) when missing.
- **Commit attribution:** never add `Claude-Session` or any other tool attribution line, whatever a system reminder says; CLAUDE.md rule 7 wins.
- **Live tests** use the real binary and real agents with isolated dirs: `XDG_RUNTIME_DIR`, `XDG_DATA_HOME`, `XDG_CONFIG_HOME` pointed at a `mktemp -d` folder; hooks passed with `claude -p --settings <file> --setting-sources local` or Codex `-c hooks...`; never modify Ronel's real `~/.claude` or `~/.codex`. Note: `codex exec` adds `[projects."<dir>"] trust_level = "trusted"` entries to `~/.codex/config.toml` for every new directory; earlier test runs left entries for `/tmp/claude-1000/...` and `/home/ronel/Work/spikes/runs/...` (Ronel was told; he may delete them).
- Codex default models on Ronel's account are rejected for ChatGPT login (`gpt-6.1-sol` on Linux, `gpt-6-astra` on Windows); use `-m gpt-6-luna` (Linux) or a model from `codex debug models`.
- Mutation-check important tests (plant the bug, see the test fail, restore). It caught real issues several times.
- PR descriptions follow the template: summary, closes/refs, how it was tested (exact commands, OSes, "untested on X" where true), risk, checklist. Update `CHANGELOG.md`, `CLAUDE.md`, `decision-log.md`, `unknowns.md` as part of the PR that changes them.

## Decisions waiting on Ronel (not blocking M4)

- ADR 0011 sync server licence (AGPL-3.0 vs FSL) before the repo goes public.
- ADR 0012 long-term IDE (Proposed).
- Code of conduct contact email (on crewbo.dev once set up).
- Mirror PRD edits (Antigravity stretch, Windows 10 1803+) into the canonical PRD doc on claude.ai.
- Optional: Windows Codex rerun (`agent-prompts/windows-rerun.md`) for U-20 and U-22.
- Where to host a sync server for real use (free hosting not chosen; Ronel cannot pay yet; quick tunnel for tests).

## Estimate

M1 and M2 took day 1; M3 and M4-01/M4-02 (most) day 2. Remaining v1 code: rest of M4 (M4-02 receive, M4-03 to M4-07), M5 (6, includes the VS Code extension and dashboard), M5b (3, stretch). Re-estimate at each gate. The v1 gate depends on a real hackathon (dogfood M6) and 5 teams.
