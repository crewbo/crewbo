# Session handoff

Last updated 2026-10-12 (M3 nearly done: only M3-07 remains). A new Claude Code session starts by reading this file, `CLAUDE.md`, and the latest entry in `docs/engineering/worklog.md`. Update this file at the end of every session.

## Who and how

- **Owner:** Ronel Abraham Mathew (GitHub `RM1338`), founder. Works on an HP Victus dual-booting Omarchy (Arch Linux, Hyprland; this is where Claude runs) and Windows 11. Has Claude Code and Codex plans. No Mac.
- **Teammate:** `gavriel953`, macOS (Apple M5, zsh), **Codex plan only, no Claude Code plan**: never ask him to run Claude Code. Collaborator on `crewbo/crewbo` and `crewbo/spikes`.
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
- **Phase 0** (spikes on Linux, Windows, macOS + 10 interviews): done. **Gate B: go.** Findings: `docs/research/phase0-findings.md`. Antigravity live spike done too (`docs/research/antigravity-hooks.md`); **ADR 0013 accepted**: Antigravity adapter is a v1 stretch goal (M5b) after M5.
- **M1** (protocol and daemon skeleton, issues #10 to #19, PRs #20 to #34): **complete**.
- **M2** (Claude Code and Codex hook adapters, issues #35 to #44, PRs #45 to #54, follow-ups #57, #69, #71): **complete.**
- **Gate C for M3 approved** 2026-10-11. Order changed with Ronel: M3-06 before M3-05 (the hub needs membership).
- **M3** (sync server and auth, milestone 3, issues #59 to #65):
  - M3-01 `crewbo serve` (#67), M3-02 event log and room goroutine (#70), M3-03 `crewbo login` with GitHub device flow (#72, #73), M3-04 device keys (#74, #75), M3-06 rooms (#76, #77), M3-05 hub and daemon sync (#78, #79): **merged**.
  - **M3-07 claims over the wire (#65): in progress.** Server half PR #80. Daemon half next: apply room events to the daemon's claim sets (roomstate.Apply with a clock from Welcome's server time), send `crewbo claim add/release` for bound repositories as ClaimRequest/ClaimRelease with `<device key id>/<session>` session ids and client-chosen claim ids, throttled ClaimRenew on hook events, offline claims granted locally plus outbox drain and reconcile on reconnect, replay each room from seq 0 on daemon start (in-memory cursors; `room_cursor` unused until snapshots), e2e with two daemons and a p95 under 2 s timing test. Then the two-machine live check with gavriel953 (Codex, macOS) through a Cloudflare quick tunnel (`cloudflared tunnel --url http://localhost:7420`; verify WebSocket support first; it is not documented).
- **GitHub OAuth app** "Crewbo" on the crewbo org: client ID `Ov23liMSnQmA2q3VqyiK` (constant in `internal/auth`), device flow on, no client secret.

### What works today (verified live on Omarchy and Windows)

`crewbo daemon`, `crewbo status`, `crewbo claim add|list|release`, `crewbo hooks install|uninstall`, `crewbo hook <claude-code|codex> <event>`, `crewbo mcp`, `crewbo version`. A terminal claim blocks Claude Code (Edit, Write, NotebookEdit, Bash, PowerShell) and Codex (apply_patch, Bash) on Linux and Windows; shell writes are parsed (POSIX 81%, PowerShell 87%, 90% with the daemon's literal-mention check); a post-shell hash check reports writes nothing could see beforehand; MCP tools `crewbo_claims` and `crewbo_who_holds` answer from the daemon.

### Package map (what exists)

`internal/claims` (pure claim/lease logic, 100%), `test/sim` (seeded simulator), `internal/platform` (paths, AF_UNIX IPC on all OSes, shutdown signals, agent shell dialect), `internal/protocol` + `proto/crewbo/v1` (generated, never edit), `internal/store` (SQLite, migrations; not yet used by the daemon), `internal/gitx` (system git; merge-tree; not yet used), `internal/daemon` (owner goroutine, token-authenticated JSON local API: decide, event, status, claim_add, claim_list, claim_release; per-repo claim sets; post-shell snapshots), `internal/hooks` (+ `claudecode`, `codex`, `install`), `internal/shellparse` (POSIX via mvdan.cc/sh, PowerShell tokenizer), `internal/mcpserver`, `internal/cli`.

## Open follow-ups

1. Hooks install findings from the Windows M2 run: fixed in PR #57 (issue #56), waiting for Ronel to merge.
2. Windows M2 results recorded in `docs/research/m2-windows-check.md` (docs PR with the Gate C list). U-21 resolved; U-20 and U-22 open.
3. **macOS:** gavriel953 runs `agent-prompts/macos-m2.md` (Codex only). Review his `results/macos-m2` PR when it arrives, as was done for Windows.
4. Close milestone "M2" once #56 is closed (Ronel).

## Gate C for M3 (presented, waiting for approval)

The refined list is in `docs/engineering/implementation-plan.md` (M3 section). After approval: create issues M3-01 to M3-07 in a milestone "M3: sync server and auth", then start with M3-01. Original notes:

Base list: `docs/engineering/implementation-plan.md` M3-01 to M3-07 (serve skeleton with TLS, room goroutine + gap-free event log, GitHub device-flow auth, device keys with keychain timeout fallback, daemon sync client with offline outbox and resync, rooms and invites with 10-member cap, claims over the wire with server-time leases and under 2 s propagation). Refine with what M1 and M2 taught: claims are now per repository in the daemon (map rooms to repositories); the local `crewbo claim` terminal session must become a member identity; `store` outbox and `room_cursor` tables already exist; `protocol` has Envelope/Hello/Welcome/Ack/Error/claims/Event; WebSocket library candidate `github.com/coder/websocket` (ISC; verify before use); keyring calls need a hard timeout (U-18). M3 needs Ronel to register a GitHub OAuth app (device flow enabled). Create the issues only after approval.

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

## Decisions waiting on Ronel (not blocking M3)

- ADR 0011 sync server licence (AGPL-3.0 vs FSL) before the repo goes public.
- ADR 0012 long-term IDE (Proposed).
- Code of conduct contact email (on crewbo.dev once set up).
- Mirror PRD edits (Antigravity stretch, Windows 10 1803+) into the canonical PRD doc on claude.ai.
- Optional: Windows Codex rerun (`agent-prompts/windows-rerun.md`) for U-20 and U-22; a Mac user with a Claude Code plan for U-23.

## Estimate

As of end of day 1: M1 and M2 done. Remaining v1 code: M3 (7 issues), M4 (6), M5 (6, includes the VS Code extension and dashboard), M5b (3, stretch). Earlier estimate: about 10 to 15 focused days for all of v1; M1 and M2 took one day, but M3 onward has more moving parts (network, auth, UI). Re-estimate at each gate. The v1 gate itself depends on a real hackathon (dogfood M6) and 5 teams.
