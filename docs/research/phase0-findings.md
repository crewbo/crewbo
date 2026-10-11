# Phase 0 findings

Status: Gate B closed 2026-10-11. Conditional go, then **go** the same day once Ronel confirmed both conditions (10 interviews; same-repository use).

Reproduce everything from `crewbo/spikes` (Linux: commit 68e385b; Windows: 40b2686, report in `results/windows/report.md`; macOS: 2f6f70d, report in `results/macos/report.md`). Founder machine (Linux): Omarchy (Arch Linux, x86_64, i5-13420H), Claude Code 2.1.294, codex-cli 0.161.0, git 2.55.0, Go 1.27.2.

## Summary

| Checklist item (PRD) | Linux result | Effect on plan |
| --- | --- | --- |
| Claude Code hook blocks an edit and injects context | Pass | None. Exec-form hooks work; deny holds even in `bypassPermissions` |
| Codex hook does the same, incl. trust and `apply_patch` parsing | Pass for blocking and context. Trust (U-02) pending | Use absolute-path parsing for `apply_patch` headers |
| Shell-edit corpus | Parser 71%, parser plus heuristic 85%, 0 false positives | Keep both layers plus post-run hash check (ADR 0004 holds) |
| Post a message into a running Claude session from an external process | Pass for `-p` with `crossSessionInbound: accept` | Socket usable as a push channel for dispatched sessions |
| Push and fetch `refs/crewbo/...` on GitHub | Pass; 12 rapid pushes, no throttling | ADR 0005 holds; `wip/` fallback not needed so far |
| Cross-machine merge-tree warning | Pass with two clones on one machine; two-laptop run pending | ADR 0005 holds |
| Keyring on Omarchy with and without a secret service | Works with gnome-keyring (installed by default here). **Hangs** when D-Bus activation fails | Wrap every keyring call in a hard timeout |
| Interview 10 teams | Pending (Ronel) | Gate criterion |

## Details

### S1: Claude Code hooks (U-04, U-12, U-14, U-15)

- Exec form (`command` plus `args`) runs the binary with no shell. Supported since Claude Code 2.1.139 ([CHANGELOG](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md): "Added hook `args: string[]` field (exec form)"). U-04 resolved.
- Denied: `Edit`, `Write`, and a `Bash` redirect (`echo shell >> blocked.txt`). Allowed edits went through. The model sees the reason prefixed with `PreToolUse:<Tool> hook error:`.
- Deny holds under `--permission-mode bypassPermissions` (U-14, Claude part resolved on Linux).
- With `SessionStart` and `UserPromptSubmit` context ("blocked.txt is claimed by alice"), the model did not even attempt the edits. Context injection alone changed behaviour; the deny is the backstop.
- `NotebookEdit` sends `tool_input.notebook_path` (absolute), plus `cell_id` and `new_source`. U-12 resolved.
- Payload fields observed beyond the docs: `prompt_id` on `UserPromptSubmit` and `Stop`; `effort` on `Stop`. `SessionStart` has no `permission_mode`.
- Hook binary spawn latency: p50 2.3 ms, p95 3.3 ms over 200 runs (U-15 baseline; daemon IPC not yet included).

### S2: Codex hooks (U-03, U-07, U-13)

- Hooks load from `-c hooks.<Event>=[...]` overrides, so tests need not touch `~/.codex`. Spikes used `--dangerously-bypass-hook-trust`; real trust flow (U-02) is a manual test.
- Denied: `apply_patch` and `Bash` (`echo shell >> blocked.txt`). The model sees `Command blocked by PreToolUse hook: <reason>`.
- `apply_patch` headers carry **absolute** paths in 0.161.0 (`*** Update File: /abs/path/blocked.txt`). The adapter must accept absolute and relative paths.
- `permission_mode` is reported as `bypassPermissions` for `codex exec --sandbox workspace-write` (exec defaults to no approvals). The field does not reflect the sandbox.
- `transcript_path` is `null` with `--ephemeral`.
- Field names (U-13 resolved): `UserPromptSubmit` has `prompt`; `Stop` has `stop_hook_active` and `last_assistant_message`; every event has `session_id`, `cwd`, `model`, `permission_mode`; tool events add `turn_id`, `tool_use_id`.
- Hook command shell on Linux: `/usr/bin/bash -c <command>` (no `-l`). The research's `$SHELL -lc` claim does not hold for 0.161.0 here. Whether Codex follows `$SHELL` or the login shell from passwd is unresolved because `/bin/sh` is bash on Arch; the macOS run (zsh) will tell. U-03 Linux part resolved.
- `codex exec` exit code is 1 when the turn fails (`turn.failed`, here a rejected model). U-07 exit-code part resolved; App Server `turn/steer` not tested (timeboxed, fallback hooks-only for Codex in v1).
- The founder's configured default model (`gpt-6.1-sol`) is rejected for ChatGPT-account auth. Crewbo should not override the user's model, but `crewbo doctor` should surface this error clearly.

### S3: shell-edit coverage (U-05)

Corpus of 57 commands (48 writes, 9 read-only) in `s3-shellparse/corpus.tsv`, parsed with `mvdan.cc/sh/v3` v3.14.1:

| Layer | Writes caught |
| --- | --- |
| Static parser (redirects, heredocs to files, `sed -i`, `perl -pi`, `tee`, `cp`, `mv`, `rm`, `truncate`, `touch`, `install`, `ln`, `git checkout/restore`) | 34 / 48 (71%) |
| Plus heuristic (write-capable program such as `python`, `node`, `awk`, `dd`, `find`, `xargs`, `gofmt`, `prettier` and the claimed name appears literally) | 41 / 48 (85%) |
| False positives on read-only commands | 0 / 9 |

Not caught statically: variables (`F=a.txt; echo x > $F`), command substitution, scripts (`./scripts/gen.sh`), `npm run`, `patch` and `git apply` (targets live inside the patch file). These are caught only by the post-run hash check (alert and record). Cheap improvements for M2: resolve same-command variable assignments, add `gofmt -w` and `prettier --write` to known writers, parse `-i` in `awk -i inplace`, read patch files for `git apply` and `patch`.

### S4: Claude messaging socket (U-01)

- A `-p` session started with `--settings '{"crossSessionInbound":"accept"}'` bound a socket under `/run/user/1000/cc-socks/` and exported `CLAUDE_CODE_MESSAGING_SOCKET` and `CLAUDE_CODE_MESSAGING_TOKEN` to the `SessionStart` hook ([docs](https://code.claude.com/docs/en/cross-session-messaging)).
- An unrelated process sent `{"type":"auth","token":"..."}` then `{"type":"user","message":{"role":"user","content":"CREWBO-S4: ..."}}`, one per line. The session fired `UserPromptSubmit` with the content as `prompt`, and the model quoted the message verbatim in its final answer. No reply is written on the socket.
- The message frame is still undocumented; it matches the format the research observed. Interactive sessions without `accept` were not tested (expected: held for approval).
- Plan effect: dispatched sessions can receive urgent pushes (for example "your claim was overridden") through the socket; hook-injected context stays primary for everything else.

### S5: custom refs on GitHub (U-06)

Push to `refs/crewbo/ronel/task-1` on `github.com/crewbo/spikes` was accepted as `[new reference]`, listed by `ls-remote`, and fetched by a second clone. 12 consecutive pushes to the same ref all succeeded, with no throttling observed. Test ref deleted afterwards. U-06 resolved; checkpoint-at-stop policy stays.

### S6: merge-tree (git 2.55.0)

Two clones of a bare remote, each with a task branch pushed to `refs/crewbo/<member>/<task>`. `git merge-tree --write-tree --name-only --no-messages HEAD crewbo/alice/t1` exited 1 and listed `shared.txt` for the conflicting pair, and exited 0 for a clean pair, matching the [documented exit status](https://git-scm.com/docs/git-merge-tree#_exit_status). Two-laptop run pending.

### S7: keyring (U-08)

- This Omarchy install runs `gnome-keyring-daemon` (package `gnome-keyring` 50.0) exposing `org.freedesktop.secrets`. `zalando/go-keyring` v0.2.8 set, got and deleted a secret in 10 ms.
- Dead session bus: fails in 65 microseconds.
- Fresh session bus with no secret service (`dbus-run-session`): the call **blocked for over 60 s** while D-Bus tried to activate the service. go-keyring does not fail fast in this case.
- Plan effect: wrap every keyring call in a hard timeout (proposed 3 s) and fall back to the 0600 file on timeout. Recorded in the decision log; ADR 0006 stands because its decision already says "fail fast".

## Windows results

Run by a Claude Code agent on Ronel's dual-boot laptop following `agent-prompts/windows.md`: Windows 11 Home build 26200, i5-13420H, Claude Code 2.1.296, Codex 0.154.0 (0.162.1 for U-02), git 2.51.0, Go 1.27.0. Full report: `crewbo/spikes` `results/windows/report.md`. Fixtures: `test/fixtures/hooks/*/*/windows/`.

| Item | Result | Effect on plan |
| --- | --- | --- |
| S1 Claude Code | Pass. `Edit` and the shell edit denied; Claude used its `PowerShell` tool, not `Bash` | `Bash\|PowerShell` matcher confirmed necessary |
| S2 Codex | Denies pass for `apply_patch` and `Bash`. The allowed `apply_patch` then failed inside the Codex Windows sandbox (`orchestrator_helper_incomplete`) | New unknown U-20: `--sandbox workspace-write` dispatch on Windows |
| `apply_patch` paths | Relative (`*** Update File: blocked.txt`) in 0.154.0 on Windows; absolute in 0.161.0 on Linux | Adapter accepts both (already decided) |
| Codex `SessionStart` | Never fired under `codex exec` 0.154.0 | New unknown U-21; U-03 has no Windows data |
| Hook path with a space | Unquoted `C:/dev/spikes test/bin/probe.exe`: **no hook ran and Codex reported no error** (fails open silently) | Installer refuses paths with spaces; quoted-path behaviour is U-22; `crewbo doctor` must detect hooks that never fire |
| U-02 Codex trust after binary swap | Not detected: `/hooks` stayed `Trusted`, stored `trusted_hash` unchanged, new binary ran (0.162.1) | Resolved: upgrades need no re-trust. Threat model note: trust does not pin the executable |
| S7 keyring | wincred, 7.3 ms, no prompt | None |
| Spawn latency (U-15) | p50 13.71 ms, p95 23.23 ms spawn to exit (100 runs); in-process p50 1.84 ms | Within budget; about 6x Linux |
| UTF-8 BOM on stdin | The test harness (PowerShell 5.1) prepended a BOM; the probe parsed nothing and allowed | Hook strips a leading BOM; undecodable input is logged and surfaced, never a silent allow |

## macOS results

Run by a Codex agent on a teammate's Mac following `agent-prompts/macos.md`: macOS 27.0 (26A428), Apple M5, `/bin/zsh`, Codex 0.162.1, git 2.54.0 (Apple Git-157), Go 1.27.2. Full report: `crewbo/spikes` `results/macos/report.md`. Fixtures: `test/fixtures/hooks/codex/0.162.1/macos/`.

| Item | Result | Effect on plan |
| --- | --- | --- |
| S2 Codex | Pass: `apply_patch` and `Bash` (`echo shell >> blocked.txt`) denied, allowed edit landed | Codex blocks on all three OSes |
| `apply_patch` paths | Relative, as on Windows 0.154.0 (absolute on Linux 0.161.0) | Not a version or OS rule; the adapter accepts both |
| Codex hook shell (U-19) | `/bin/zsh -c <command>` with `SHELL=/bin/zsh` and again with `SHELL=/bin/bash` | Codex ignores `$SHELL`; with Linux (`bash -c`) it appears to use the account's login shell with `-c`. zsh reads `~/.zshenv` for every shell ([zsh manual, Startup/Shutdown Files](https://zsh.sourceforge.io/Doc/Release/Files.html)), so output printed there would corrupt hook JSON. `crewbo doctor` should check |
| S6 merge-tree | Pass (two clones, one machine) | None |
| S1 Claude Code | Not run: the teammate has no Claude Code plan (their install pointed at a proxy that returned 401) | Claude Code on macOS untested (U-23) |
| S7 keyring | Not run: `go run` failed TLS verification of `proxy.golang.org` (`certificate signed by unknown authority`; `curl` exit 60). HTTPS was intercepted, possibly by the Codex sandbox or the network | New unknown U-24: daemon to sync server TLS on intercepting networks |
| Latency | Not measured (driver needed a Claude Code payload) | Rerun with a Codex payload |

## Gate B status

PRD gate: go if hooks block reliably on at least 2 of 3 OSes and at least 3 of 10 teams mix vendors.

- Linux: both agents block reliably.
- Windows: both agents block reliably (Claude Code 2.1.296, Codex 0.154.0). Hook criterion (2 of 3 OSes) met.
- macOS: Codex blocks reliably (0.162.1). Claude Code on macOS untested: no available Mac has a Claude Code plan (U-23).
- Interviews: 4 of 10 done, 3 mix vendors (below).

### Interviews (S8, U-10)

Collected by Ronel, 2026-10-11. Ronel is not a member of any interviewed team. Members are listed without names.

| Team | Size | Operating systems | Agents per member | Mixes vendors |
| --- | --- | --- | --- | --- |
| 1 | 6 | Omarchy, Ubuntu x3, macOS, Windows | Claude Code + Codex; Codex; Claude Code x4 | Yes |
| 2 | 4 | Omarchy, Ubuntu, macOS, Windows | Claude Code + Codex; Claude Code; Codex; Codex | Yes |
| 3 | 3 | macOS, Windows x2 | Antigravity x3 | No (single vendor, not supported in v1) |
| 4 | 4 | Ubuntu x4 | Claude Code x3; Claude Code + Codex | Yes, through one member |
| 5 | 4 | macOS, Windows x2, Ubuntu | Antigravity + Kiro + GitHub Copilot + Cursor; Kiro + Antigravity x2; Freebuff + OpenCode | Yes (no v1 agent) |
| 6 | 6 | Ubuntu x4, macOS, Windows | Antigravity + Freebuff + OpenCode; Codex + Antigravity + Freebuff + OpenCode + local LLM; Freebuff; Claude Code + Antigravity; Claude Code + Kiro + Antigravity + Cursor + Freebuff + OpenCode; Claude Code | Yes |
| 7 | 3 | Windows x3 | Codex; Antigravity; Claude Code | Yes |
| 8 | 4 | macOS x4 (one Intel) | Claude Code; Claude Code + Codex; Claude Code + Codex + Antigravity; Claude Code + Codex + Cursor | Yes |
| 9 | 3 | Windows x2, macOS | Claude Code + Codex; Antigravity; Cursor + Codex | Yes |
| 10 | 4 | macOS x2 (one Intel), Windows x2 | Codex; Claude Code + Antigravity; Cursor + Claude Code; Claude Code + Codex + Antigravity | Yes |

Observations:
- 3 of 4 teams include more than one agent vendor; 3 of 4 span two or more operating systems; every team has at least 3 people.
- The interviews recorded which agents members use, not whether two agents worked on the same repository in the same project. Team 4's mix rests on one member.
- 1 of 4 teams uses only Antigravity, supporting the v2 adapter plan.
- Sample: 4 teams against a planned 10.

### Full interview set (10 teams, 2026-10-11)

Teams 5 to 10 were added after the Gate B decision, completing condition 1 (10 interviews). Counts below were computed from the table with a script, not by hand.

| Measure | Result |
| --- | --- |
| Teams with more than one agent vendor | 9 of 10 (all but team 3) |
| Teams with both a Claude Code and a Codex user | 8 of 10 |
| Teams fully covered by v1 (every member uses Claude Code or Codex) | 5 of 10 (teams 1, 2, 4, 8, 10) |
| Teams with no member covered by v1 | 2 of 10 (teams 3, 5) |
| Members covered by v1 | 30 of 41 (73%) |
| Members using each agent | Claude Code 23, Codex 15, Antigravity 15, Cursor 5, Freebuff 5, Kiro 4, OpenCode 4, GitHub Copilot 1, local LLM 1 |
| Uncovered members who use Antigravity | 9 of 11 |
| Teams spanning two or more operating systems | 8 of 10 |

Observations:
- The vendor-mix criterion (at least 3 of 10) is met by a wide margin on agents used.
- Condition 2 met: Ronel confirmed that in every team the listed agents were used in the same project.
- Antigravity is used by as many members as Codex and appears in 7 of 10 teams. An Antigravity adapter would raise member coverage from 73% to 95% (39 of 41). It is planned for v2; moving it earlier is a PRD scope question for Ronel (U-26).
- A member whose agent has no adapter is not blocked by claims and does not renew leases, so partial coverage weakens enforcement for the whole team.

### Decision

**Conditional go** (Ronel, 2026-10-11). The hook criterion is met (Claude Code on Linux and Windows, Codex on all three OSes). The vendor criterion is met on count (3 mixed-vendor teams) with a small sample. Conditions:

1. Complete 6 more interviews with independent teams by the end of M1, asking explicitly whether two or more agents worked on the same repository in their last project.
2. Confirm that question for teams 1, 2 and 4.
3. Re-check at the Gate C review before M2. If fewer than 3 of 10 teams confirm same-repository vendor mixing, apply the U-10 fallback (narrow positioning to cross-machine, single-vendor teams). That is a PRD change and needs Ronel's decision and a superseding ADR.

M1 (claims, simulator, protocol, gitx, daemon skeleton) does not depend on the vendor question, so it can start once its Gate C issue list is approved. Nothing in Phase 0 requires a superseding ADR.
