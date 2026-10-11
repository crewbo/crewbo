# M2 end-to-end check on Windows

Run by Ronel on 2026-10-11 against `44aa7fb` (all M2 PRs merged), following `crewbo/spikes` `agent-prompts/windows-m2.md`. Full report with exact output: `crewbo/spikes` `results/windows-m2/report.md` (PR #3).

Windows 11 Home build 26200, Claude Code 2.1.296, Codex CLI 0.162.1, git 2.51.0.windows.1.

## Results

| Feature | Result |
| --- | --- |
| Build, `crewbo daemon`, `crewbo status` | Pass |
| `crewbo hooks install` (Claude Code exec form, Codex after trusting in `/hooks`) | Pass; findings 1 to 3 |
| Terminal claim blocks Claude Code `Edit` | Pass |
| Terminal claim blocks Claude Code `PowerShell` (`Add-Content -Path src\auth.go`, `Set-Content -LiteralPath .\src\auth.go`) | Pass: the PowerShell parser works live |
| Unclaimed file edit allowed | Pass |
| Script that changes a claimed file (`.\fmt.ps1`) reported after the fact | Pass; counted as a violation in `crewbo status` |
| Codex `apply_patch` with an absolute `C:\...` header, and Codex shell `Add-Content` | Pass, both blocked |
| MCP `crewbo_claims` and `crewbo_who_holds` | Pass |
| Release, then the same edit succeeds | Pass |
| `crewbo hooks uninstall` | Pass with findings 1 to 3 |

Path matching held for `src/auth.go`, `src\auth.go`, `.\src\auth.go` and `C:\dev\cbtest\src\auth.go`.

## Findings and what happened to them

1. Uninstall overwrote `.crewbo.bak` with the hooked config. Fixed in #57.
2. Install and uninstall re-sorted keys and escaped `&`, `<`, `>`. Fixed in #57 (byte-identical round trip).
3. A Codex `hooks.json` created by install was left as `{}`. Fixed in #57. The `trusted_hash` entries Codex writes to `~/.codex/config.toml` stay; Crewbo does not edit that file.
4. Another user-level hook (the ECC plugin's GateGuard) denied first and hid Crewbo's message. Documented in the README in #57; `crewbo doctor` (later milestone) should list other PreToolUse hooks.
5. Codex 0.162.1 runs `SessionStart` under `codex exec`. U-21 resolved.
6. In `claude -p`, `acceptEdits` does not cover running a script; `--allowedTools 'PowerShell(.\fmt.ps1)'` was needed. Relevant for M4 dispatch.

## Unknowns

- U-21 resolved (finding 5).
- U-20 (Codex Windows sandbox for allowed edits) and U-22 (quoted spaced hook path) not covered by this run; still open.
