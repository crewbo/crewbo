# Antigravity hook check (U-26)

Docs-only timebox, 2026-10-11, about 30 of 90 minutes used, followed the same day by a live spike (S9) on Omarchy with Antigravity CLI `agy` 1.3.3. Live results are in the section below and override the docs where they differ. Sources:

- [AG-H] https://antigravity.google/docs/hooks (fetched as `hooks.md`)
- [AG-HL] https://antigravity.google/docs/cli/headless
- [AG-I] https://antigravity.google/docs/cli/install
- [AG-M] https://antigravity.google/docs/mcp

## Answer

An Antigravity adapter is feasible with the same design as the Claude Code and Codex adapters: a PreToolUse hook can deny a write with a reason, a hook can inject context before every model call, and the CLI has a headless mode with NDJSON output. One contract difference needs a live test before committing to it (no neutral PreToolUse decision, U-27).

## Mapping to Crewbo's normalised events

| Crewbo event | Antigravity | Notes |
| --- | --- | --- |
| `session_start` | `PreInvocation` with `invocationNum` 0 | No dedicated session start event [AG-H] |
| `prompt` | `PreInvocation` | Fires before every model call, more often than a prompt hook; output `injectSteps: [{"ephemeralMessage": "..."}]` [AG-H] |
| `pre_edit` | `PreToolUse`, matcher `write_to_file\|replace_file_content\|multi_replace_file_content` | Target in `toolCall.args.TargetFile` [AG-H] |
| `pre_shell` | `PreToolUse`, matcher `run_command` | `toolCall.args.CommandLine` and `toolCall.args.Cwd` [AG-H] |
| `post_edit` | `PostToolUse` | Output is `{}`; cannot change the result [AG-H] |
| `stop` | `Stop` | `fullyIdle`, `terminationReason`; returning anything but `"continue"` allows the stop [AG-H] |

Common input on every event (camelCase): `conversationId` (session id), `workspacePaths`, `transcriptPath`, `artifactDirectoryPath`, `modelName` [AG-H]. `modelName` on every event means the crew panel can show the model under BDR-017.

## Deny shape and the missing neutral decision

PreToolUse output requires `decision`, one of `allow`, `deny`, `ask`, `force_ask`, `deny_unless_prior_grant`, with an optional `reason` shown to the agent [AG-H]:

```json
{"decision": "deny", "reason": "src/auth.ts is claimed by priya's agent for task 12."}
```

Claude Code and Codex treat "no output" as "no decision" and fall through to the user's own permission rules. Antigravity documents no such option: `allow` auto-approves (Crewbo must never do that, it would bypass the user's permission settings), and `ask` "prompts you for approval, but respects Always Allow settings". What an empty object or a missing `decision` does is not documented (U-27). If it is not a pass-through, the adapter answers `ask` for paths it does not block, which may add prompts for users without Always Allow rules.

## Configuration and install

- One global file serves Antigravity 2.0, the IDE and the CLI: `~/.gemini/config/hooks.json`; workspace level `.agents/hooks.json`; the CLI also reads `~/.gemini/antigravity-cli/settings.json` [AG-H].
- The file maps named hooks (`"crewbo": {...}`) to events, with an `enabled` flag, so Crewbo can own one named entry without touching others [AG-H].
- Handlers are shell-form `command` strings with a 30 s default `timeout`; there is no exec form [AG-H]. The Codex lessons apply: install the binary under a path without spaces and print only JSON (U-22, U-25).
- No hook trust or hash-pinning step is documented, unlike Codex [AG-H]. UNVERIFIED.
- MCP over stdio, configured in `~/.gemini/config/mcp_config.json` or `.agents/mcp_config.json` [AG-M].
- The CLI (`agy`) installs on macOS, Linux and Windows (`%LOCALAPPDATA%\agy\bin`) and stores credentials in the OS keyring [AG-I].

## Headless dispatch

- `agy -p "<prompt>" --output-format stream-json` streams NDJSON; the first `init` event carries `cwd`, `tools`, `permission_mode` (default `request-review`) and `model` when set [AG-HL].
- In headless mode, file reads and writes inside the workspace are auto-allowed; shell commands are soft-denied unless granted in `permissions.allow` [AG-HL]. A dispatched task can edit files with no extra flags, and Crewbo must not use `--dangerously-skip-permissions`.
- An unauthenticated headless run exits with `authentication required` rather than hanging; an unknown `--model` exits non-zero with status `ERROR` [AG-HL].
- A `--cwd` style flag was not found in the parts read; the daemon would start `agy` with the worktree as its working directory, as for `claude -p`.

## Effort estimate

Comparable to the Codex adapter: an `internal/hooks/antigravity` decoder and encoder (camelCase input, five tool names, `decision` and `injectSteps` output), install and uninstall of one named entry in `~/.gemini/config/hooks.json`, MCP registration, and a headless launcher. About three PRs of the M2 size, plus a live spike first.

## Live spike S9 results (agy 1.3.3, Omarchy, 2026-10-11)

Run with `crewbo/spikes` `s9-antigravity/run.sh`: hooks in the throwaway repo's `.agents/hooks.json`, `agy -p ... --output-format stream-json`. `~/.gemini` was not modified. Fixtures: `test/fixtures/hooks/antigravity/1.3.3/linux/`.

| Question | Result |
| --- | --- |
| Do workspace hooks load without a trust step? | Yes: `.agents/hooks.json` hooks ran on the first run, no prompt |
| Deny on `replace_file_content` and `run_command` | Works; the model sees `tool call denied by pre-tool hook: <reason>` |
| Deny under `--dangerously-skip-permissions` | Still blocks (both tools) |
| U-27: no output for an unblocked call | **Passes through**: the edit was applied (under skip-permissions) |
| U-27: `{}` for an unblocked call | **Blocks**: `tool call denied by pre-tool hook:` with an empty reason. The adapter must print nothing, never `{}` |
| U-27: `{"decision":"ask"}` | Defers to the permission mode (applied under skip-permissions) |
| Context via `PreInvocation` `ephemeralMessage` | Reaches the model; it quoted the claim notice. Fires before every model call (5 times in a short run) |
| Payload fields | As documented, plus undocumented `toolAction` and `toolSummary` in `toolCall.args`; `TargetFile` is absolute |
| Headless file writes without hooks | **Denied by default** (`a tool required the "write_file" permission that headless mode cannot prompt for`), contradicting [AG-HL]'s "auto-allowed". Dispatch needs a scoped `permissions.allow` rule, never `--dangerously-skip-permissions` (new U-28) |
| Hook overhead | Probe in-process 0.4 to 2.7 ms |

## Before any commitment (original list; items 1 to 5 done in S9)

A live spike like Phase 0 S1 and S2, on Omarchy first: install `agy`, log in, run the probe from `crewbo/spikes` as a global hook, and record (1) a deny on `write_to_file` and `run_command`, (2) what `{}` and a missing `decision` do (U-27), (3) whether `ephemeralMessage` reaches the model, (4) whether hooks need any trust step, (5) captured payloads for fixtures.

## Docs re-check (2026-10-18, before Gate C for M5b)

[AG-H] fetched again; the installed CLI is now `agy` 1.3.5. Differences from the 2026-10-11 reading, all UNVERIFIED live until M5b-01 reruns the probe:

| Topic | 2026-10-11 | 2026-10-18 |
| --- | --- | --- |
| Session start | No dedicated event; first `PreInvocation` | A `SessionStart` event exists (handlers directly under the event key, like `PreInvocation`, `PostInvocation` and `Stop`) |
| After a model call | Not listed | `PostInvocation` |
| `PreToolUse` decisions | `allow`, `deny`, `ask`, `force_ask`, `deny_unless_prior_grant` | Adds `auto_approve` (Crewbo must never answer it); optional `overwrite` (merged into `toolCall.args`) and `permissionOverrides` |
| `PreInvocation` output | `injectSteps` with `ephemeralMessage` | `injectSteps` entries may also hold `systemMessage`, `userMessage` or `toolCall`; Crewbo uses `ephemeralMessage` only |
| Global hooks file | `~/.gemini/config/hooks.json`; the CLI also reads `~/.gemini/antigravity-cli/settings.json` | Same |
| Hook trust | None documented | Still none; `/hooks` in the TUI lists loaded hooks |

## Live spike S9b results (agy 1.3.5 then 1.4.2, Omarchy, 2026-10-18)

Same method as S9 (`crewbo/spikes` `s9-antigravity/run.sh`, workspace `.agents/hooks.json`, `~/.gemini` not modified), with `SessionStart` and `PostInvocation` added. `agy` updated itself from 1.3.5 to 1.4.2 between runs; every fixture in `test/fixtures/hooks/antigravity/1.4.2/linux/` comes from 1.4.2.

| Question | Result |
| --- | --- |
| Does `SessionStart` fire? | Yes, once per run, before the first `PreInvocation`; payload has the common fields only |
| Does `PostInvocation` fire? | Yes, after every model call |
| Deny on `write_to_file` and `run_command` | Still works; the edit and the shell write to `blocked.txt` were refused |
| No output for an unblocked call | Still passes through (`free.txt` changed) |
| `{}` for an unblocked call | Still blocks (`free.txt` unchanged) |
| `ephemeralMessage` claim notice | Still reaches the model: it left `blocked.txt` alone without a single denied call |
| Event name in the payload | None; the event must come from the hook's command line |
| Working directory | No `cwd` field; `run_command` carries `Cwd` in its arguments, other events only `workspacePaths` |
| Tools that write | `write_to_file` (whole-file writes now use it with `Overwrite`), `replace_file_content`, `multi_replace_file_content`; 1.4.2 also lists `sed_file` and `notebook_edit` (arguments not captured, U-33) |
| `--mode accept-edits` in headless mode | Writes are still refused when the agent first runs a shell command (`a tool required the "command" permission that headless mode cannot prompt for`); U-28 stays open |

Caution for future spikes: in one run asked to use `sed_file`, the agent, running under `--dangerously-skip-permissions`, spent five minutes exploring outside the throwaway repository with read-only commands (listing `~/Work/spikes`, reading the `agy` binary's strings and a transcript under `~/.gemini`). Nothing was written, but spikes should not ask the model for tools it may not have, and should keep runs short.
