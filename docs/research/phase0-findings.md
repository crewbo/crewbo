# Phase 0 findings

Status: interim, 2026-10-11. Linux (Omarchy) and Windows results are complete; macOS and team interviews are pending. Gate B is decided after those arrive.

Reproduce everything from `crewbo/spikes` (Linux: commit 68e385b; Windows: 40b2686, report in `results/windows/report.md`). Founder machine (Linux): Omarchy (Arch Linux, x86_64, i5-13420H), Claude Code 2.1.294, codex-cli 0.161.0, git 2.55.0, Go 1.27.2.

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

## Gate B status

PRD gate: go if hooks block reliably on at least 2 of 3 OSes and at least 3 of 10 teams mix vendors.

- Linux: both agents block reliably.
- Windows: both agents block reliably (Claude Code 2.1.296, Codex 0.154.0). Hook criterion (2 of 3 OSes) met.
- macOS (friend): pending.
- Interviews: pending.

Interim recommendation: **go on the hook criterion** (Linux and Windows); the vendor-mix criterion depends on the interviews. Start M1 work that does not depend on per-OS hook behaviour (claims, simulator, protocol, gitx) only after Gate B, as planned. Nothing found so far requires a superseding ADR.
