# M7-08: prompting from the agents' VS Code extensions

Run on 2026-10-20 at about 01:06 on Omarchy (Linux) by Ronel, with Claude Code watching the daemon. Question: do Crewbo's hooks block a claimed file when a member prompts from the agents' own VS Code chat panels instead of a terminal?

## Setup

- VS Code 1.140.0 with `anthropic.claude-code` 2.1.301 and `openai.chatgpt` (Codex) 26.1007.21434; codex-cli 0.161.0.
- crewbo built from `main` after #208; the daemon running; no room (claims local).
- A throwaway repository with `locked.txt` (claimed from a terminal by ronel for 2 h) and `free.txt` (unclaimed).
- `crewbo hooks install --agent claude-code` and `--agent codex`; the Codex hooks trusted once with `/hooks` in a `codex` terminal.

## What the docs say

- Claude Code: `~/.claude/settings.json` is "shared between the extension and CLI. Use it for allowed commands, environment variables, hooks, and MCP servers." ([VS Code extension](https://code.claude.com/docs/en/vs-code))
- Codex: the hooks page names the CLI and the `hooks.json` and `config.toml` locations, and does not say whether the IDE extension runs hooks ([hooks](https://learn.chatgpt.com/docs/hooks)). Its IDE page does not mention configuration sharing ([IDE](https://learn.chatgpt.com/docs/codex/ide)).

## Results

| Prompt | Claude Code panel | Codex panel |
| --- | --- | --- |
| "Add the line hello to locked.txt" | Refused. The panel showed `PreToolUse:Bash hook error: locked.txt is claimed by ronel (from a terminal) until at least 03:01 (Crewbo)`; the agent explained the claim and offered `crewbo claim request locked.txt` | Refused: "the workspace lock blocked the write because locked.txt is claimed by ronel until 03:01" |
| "Add the line hello to free.txt" | Edited (`hello` added) | Left it unchanged: the line was already there |

`locked.txt` was unchanged afterwards. `crewbo doctor` showed `hooks: claude-code ... last event 01:09` and `hooks: codex ... last event 01:08`.

**Conclusion:** both extensions run the hooks Crewbo installs for their CLIs, and claims block their edits. For Codex, trusting the hooks once in the CLI also covered the extension in this run.

## Finding

Claude's first blocked call was a read-only `find` that only named `locked.txt`. A shell command whose writes Crewbo cannot parse is refused when it mentions a claimed file (decision log 2026-10-11, M2), so harmless searches are refused too. Safe, but stricter than needed; worth narrowing for commands known to be read-only.

## Not covered

Windows and macOS; MCP tools from the panels (`crewbo_crew`, `crewbo_draft_task`); Antigravity, which is its own editor.
