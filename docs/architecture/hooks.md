# Hook event model and agent mapping

Verified on 2026-10-11 against official docs and the CLIs installed on the founder's Omarchy machine (Claude Code 2.1.294, codex-cli 0.161.0), then confirmed on Linux by Phase 0 captures in `test/fixtures/hooks/` (see [phase0-findings.md](../research/phase0-findings.md)). Anything not confirmed is marked UNVERIFIED and tracked in [unknowns.md](../engineering/unknowns.md).

Sources:

- CC: [Claude Code hooks reference](https://code.claude.com/docs/en/hooks), [tools reference](https://code.claude.com/docs/en/tools-reference)
- CX: [Codex hooks](https://learn.chatgpt.com/docs/hooks)

## Normalised events

| Crewbo event | Purpose | Daemon action | Response to agent |
| --- | --- | --- | --- |
| `session_start` | Register session, renew holder leases | Record `AgentSession`; return team digest | `additionalContext`: short digest of claims and intents |
| `prompt` | Fresh team state before each turn | Renew leases; diff since last digest | `additionalContext`: changes since last prompt |
| `pre_edit` | Gate a file-tool write | `claims.Decide` on each target path | Deny with reason, or no decision |
| `pre_shell` (sub-kind of `pre_edit`) | Gate a shell command | `shellparse.WriteTargets`, then `Decide` | Deny with reason, or no decision |
| `post_edit` | Detect shell writes the parser missed; renew leases | Compare hashes of claimed files touched since `pre_shell` | `additionalContext` naming any crossed claim; record `ClaimViolation` |
| `stop` | Agent finished a turn | Checkpoint, push ref, trigger merge-tree | None (never blocks stopping) |

All responses are JSON on stdout only. The hook never prints anything else to stdout. Diagnostics go to `hook.log` in the data directory.

Main loop (`internal/hooks`, #35): read stdin (1 MiB cap), decode with the agent adapter, find the repository root by walking up to `.git` (directory or file; no git process), normalise target paths, then one daemon call (`decide` for edits with targets, `event` otherwise) within 300 ms. Outside a git repository the hook stays silent. Every failure allows the action with a warning that starts "Crewbo could not check claims for this action, so it was allowed:". An unknown agent or event name exits 0 with no output. Deny reasons name the path, the holder and the task, and only suggest actions that exist.

Shell commands (#38): `internal/shellparse` parses POSIX commands with `mvdan.cc/sh/v3` (redirects, heredocs, `sed -i`, `perl -pi`, `awk -i inplace`, `tee`, `cp`, `mv`, `rm`, `truncate`, `touch`, `dd of=`, `git checkout/restore/rm/mv`, `gofmt -w`, `prettier --write`, `clang-format -i`), following `cd` and simple variable assignments in the same command. Commands that run something unparseable (interpreters, scripts, `patch`, `xargs`, `git apply`) or write to computed paths are marked opaque; the daemon then denies if the command mentions another session's literal claimed path or file name. On the Phase 0 corpus: 39/48 writes from the parser, 43/48 with the mention check, 0 false positives. PowerShell commands (#39) are tokenised: `Set-Content`, `Add-Content`, `Clear-Content`, `Out-File`, `Tee-Object`, `New-Item`, `Rename-Item`, `Remove-Item`, `Copy-Item`, `Move-Item` and their built-in aliases, `-Path`/`-LiteralPath`/`-FilePath`/`-Destination` (abbreviated or `-Name:value`), redirects (`>`, `>>`, `2>`, `*>`), simple `$var` assignments and `Set-Location`. Script calls, `iex`, .NET calls such as `[IO.File]::WriteAllText` and computed paths are opaque. PowerShell corpus: 26/30 writes from the parser, 30/30 with the mention check, 0 false positives. Not yet run live on Windows.

Post-shell hash check (#40): every allowed shell command makes the daemon fingerprint (SHA-256) the literal files other sessions claim in that repository (at most 200 files, 10 MiB each); the matching `post_edit` event (paired by `tool_use_id`) re-hashes them. A changed file is recorded as a violation and the agent is told which claimed file its command changed and who holds it. Nothing is reverted (ADR 0004). Glob claims are not fingerprinted yet.

Input handling: strip a leading UTF-8 byte order mark before decoding (a PowerShell 5.1 test harness added one and the probe silently allowed). Input that still fails to decode is logged and answered with an allow plus a visible warning, never a silent allow.

Observed on Windows (Phase 0): Claude Code routes shell commands through its `PowerShell` tool, so the `Bash|PowerShell` matcher is required. Codex 0.154.0 sends relative paths in `apply_patch` headers there (absolute in 0.161.0 on Linux); a Codex hook command whose unquoted path contains a space does not run and Codex reports no error (U-22).

## Per-agent mapping

| Crewbo event | Claude Code event and matcher | Codex event and matcher |
| --- | --- | --- |
| `session_start` | `SessionStart` (source: `startup`, `resume`, `clear`, `compact`, `fork`) [CC] | `SessionStart` [CX] |
| `prompt` | `UserPromptSubmit`; also fires for scheduled tasks, background subagent reports, and cross-session messages [CC] | `UserPromptSubmit` [CX] |
| `pre_edit` | `PreToolUse`, matcher `Edit\|Write\|NotebookEdit` [CC] | `PreToolUse`, tool `apply_patch` (matchers `Edit` or `Write` also match it) [CX] |
| `pre_shell` | `PreToolUse`, matcher `Bash\|PowerShell` [CC] | `PreToolUse`, tool `Bash` [CX] |
| `post_edit` | `PostToolUse`, same matchers [CC] | `PostToolUse` [CX] |
| `stop` | `Stop` [CC] | `Stop` [CX] |

`MultiEdit` appears in the research report's matcher but is not in the current hooks or tools reference. The matcher is a regex, so including it is harmless; v1 omits it unless Phase 0 captures it.

## Input fields used

| Field | Claude Code | Codex |
| --- | --- | --- |
| Common | `session_id`, `transcript_path`, `cwd`, `permission_mode`, `hook_event_name` [CC] | `session_id`, `transcript_path`, `cwd`, `hook_event_name`, `model`, `permission_mode` [CX] |
| Tool | `tool_name`, `tool_input`, `tool_use_id` [CC] | `turn_id`, `tool_name`, `tool_use_id`, `tool_input` [CX] |
| Edit target | `tool_input.file_path` for `Write` and `Edit`, always absolute, backslash separators on Windows [CC]. `NotebookEdit` uses `tool_input.notebook_path`, absolute (captured in Phase 0). | Paths parsed from `tool_input.command` patch headers `*** Add File:`, `*** Update File:`, `*** Delete File:`, `*** Move to:`. Captured in Phase 0 with absolute paths in 0.161.0; accept relative paths too |
| Shell command | `tool_input.command` for `Bash` and `PowerShell` [CC] | `tool_input.command` for `Bash` [CX] |
| Prompt | `prompt` (pasted content may appear between `<pasted_content>` markers), plus `prompt_id` (captured) [CC] | `prompt` (captured) |
| Stop | `stop_hook_active`, `last_assistant_message`, `background_tasks`, `session_crons` [CC]; also `prompt_id`, `effort` (captured) | `stop_hook_active`, `last_assistant_message` (captured) |
| Post-shell changed files | `tool_response.bashEditDiff.changedFiles` (v2.1.269+, best effort, public beta, "not to enforce a policy") [CC] | Not documented |

`bashEditDiff` is a useful hint for `post_edit` but the docs say not to rely on it for policy, so the hash comparison stays the primary check.

## Responses

### Deny an edit

Both agents accept [CC][CX]:

```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"src/auth.ts is claimed by priya's agent for task 12. Wait, work on other files, or run: crewbo claim request-handoff src/auth.ts"}}
```

- CC: `deny` takes precedence over other hooks' decisions; the reason is shown to Claude. Exit 0 with no output is "no decision", not approval.
- CC: exit 2 also blocks, reason on stderr, and overrides any JSON.
- CX: exit 2 also blocks, reason on stderr. Plain stdout is ignored for `PreToolUse`.
- CX: emitting `permissionDecision: "ask"`, `continue: false`, `stopReason` or `suppressOutput` marks the hook failed and lets the tool run. The Codex encoder must never emit these.

Crewbo uses the JSON form (exit 0) for both, so the reason format is identical.

### Inject context

- CC: `hookSpecificOutput.additionalContext` on `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`. Capped at 10,000 characters; longer output is saved to a file and replaced with a preview.
- CX: `additionalContext` on `SessionStart`, `UserPromptSubmit`, `SubagentStart`, `PreToolUse`, `PostToolUse`. Default cap about 2,500 tokens, configurable per handler with `additionalContextLimit`.
- Crewbo keeps digests under 2,000 characters for both.
- CC: write context as plain facts; "text framed as out-of-band system commands can trigger Claude's prompt-injection defenses".

### Stop

Crewbo never blocks stopping. `stop` returns no output.

## Registration

### Claude Code

User-level `~/.claude/settings.json`, exec form (`args` present means no shell) [CC]:

```json
{"hooks":{
  "SessionStart":[{"hooks":[{"type":"command","command":"/abs/path/crewbo","args":["hook","claude-code","session_start"]}]}],
  "UserPromptSubmit":[{"hooks":[{"type":"command","command":"/abs/path/crewbo","args":["hook","claude-code","prompt"]}]}],
  "PreToolUse":[
    {"matcher":"Edit|Write|NotebookEdit","hooks":[{"type":"command","command":"/abs/path/crewbo","args":["hook","claude-code","pre_edit"]}]},
    {"matcher":"Bash|PowerShell","hooks":[{"type":"command","command":"/abs/path/crewbo","args":["hook","claude-code","pre_shell"]}]}],
  "PostToolUse":[{"matcher":"Edit|Write|NotebookEdit|Bash|PowerShell","hooks":[{"type":"command","command":"/abs/path/crewbo","args":["hook","claude-code","post_edit"]}]}],
  "Stop":[{"hooks":[{"type":"command","command":"/abs/path/crewbo","args":["hook","claude-code","stop"]}]}]}}
```

- Hook entries merge across settings levels and hot-reload; an identical handler in several files runs once [CC].
- On Windows, exec form requires a real `.exe` [CC]; `crewbo.exe` satisfies this.
- Exec form (`args`) exists since Claude Code 2.1.139 ([CHANGELOG](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)). The installer refuses older versions.
- Timeouts: default 600 s, 30 s for `UserPromptSubmit`. A timed-out or failed hook lets the call through unless `onFailure: "block"` (v2.1.295+) [CC]. Crewbo does not set `onFailure` in v1 (fail open, ADR 0004).
- A `PreToolUse` deny is honoured under `bypassPermissions` (confirmed on Linux in Phase 0).

### Codex

User-level `~/.codex/hooks.json` or `[hooks]` in `~/.codex/config.toml` [CX]. Fields: `type: "command"`, `command`, optional `commandWindows` / `command_windows`, `timeout` (seconds, default 600) [CX].

- Non-managed hooks run only after the user trusts them in `/hooks`; trust is recorded against "the hook's current hash" and changed hooks are skipped until re-trusted [CX]. Phase 0 on Windows (0.162.1) showed the hash covers the hook entry, not the executable: after rebuilding the binary, `/hooks` still showed `Trusted` and the new binary ran (U-02 resolved). The entry is kept byte-for-byte stable across upgrades, so upgrades need no re-trust.
- `--dangerously-bypass-hook-trust` exists on `codex exec` (confirmed in `codex exec --help`, 0.161.0). Crewbo does not use it in v1.
- How Codex spawns the command per OS is not on the docs page. Phase 0 observed `/usr/bin/bash -c <command>` on Linux and `/bin/zsh -c <command>` on macOS (unchanged with `SHELL=/bin/bash`), so Codex uses the account's login shell with `-c`, not `$SHELL` and not a login shell flag (U-19 resolved). zsh still reads `~/.zshenv` (U-25). Windows remains open (U-03, U-21).
- `permission_mode` arrives as `bypassPermissions` for `codex exec --sandbox workspace-write`; it does not describe the sandbox.
- On Windows, Codex names its shell tool `Bash` but runs PowerShell (Phase 0), so the adapter treats Codex shell commands as PowerShell on Windows (`platform.AgentShellIsPowerShell`, #37).

## Headless launch flags (confirmed in local `--help`)

| | Claude Code 2.1.294 | codex-cli 0.161.0 |
| --- | --- | --- |
| Non-interactive | `-p` / `--print` | `codex exec` |
| Working dir | No flag; spawn with cwd set to the worktree | `-C` / `--cd <DIR>` |
| Permissions | `--permission-mode` choices: `acceptEdits`, `auto`, `bypassPermissions`, `manual`, `dontAsk`, `plan` | `-s` / `--sandbox`: `read-only`, `workspace-write`, `danger-full-access` |
| Output | `--output-format stream-json --verbose` | `--json`, `-o` / `--output-last-message <FILE>` |
| Session | `--session-id <uuid>`, `-n` / `--name` | `codex exec resume` |
| Settings override | `--settings <file-or-json>` | n/a |
| Avoid | `--bare` (skips hooks) | `--dangerously-bypass-approvals-and-sandbox` |

Note: the research report mentions a `default` permission mode; the installed CLI lists `manual` instead. Crewbo always passes the mode explicitly. v1 uses `acceptEdits` for Claude Code and `workspace-write` for Codex.

## MCP registration (confirmed in local `--help`)

- `claude mcp add --scope user --transport stdio crewbo <abs>/crewbo mcp` (usage: `claude mcp add [options] <name> <commandOrUrl> [args...]`)
- `codex mcp add crewbo -- <abs>/crewbo mcp`
