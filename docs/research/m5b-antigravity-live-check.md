# M5b live check: an Antigravity agent blocked by a claim

Run on 2026-10-18 by Claude on Omarchy (Arch Linux x86_64) with Antigravity CLI `agy` 1.4.2 and `crewbo` at `0c1a46e` (#150), for M5b-03 (#148), with Ronel's permission to install into his real `~/.gemini`.

## Setup

1. `~/.gemini/config` backed up (it held only an empty `mcp_config.json`, `.migrated` and `projects/`; no `hooks.json`).
2. `./bin/crewbo hooks install --agent antigravity` on the real home:

   ```text
   Antigravity: hooks installed in /home/ronel/.gemini/config/hooks.json
   Antigravity MCP: MCP server installed in /home/ronel/.gemini/config/mcp_config.json
     backup of your config before Crewbo: /home/ronel/.gemini/config/mcp_config.json.crewbo.bak
   ```

3. A Crewbo daemon isolated from Ronel's own (its own `XDG_RUNTIME_DIR`, `XDG_DATA_HOME`, `XDG_CONFIG_HOME`), a throwaway git repository with `blocked.txt` and `free.txt`, and `crewbo claim add --for 1h blocked.txt` (a terminal claim, which blocks every agent). `agy` ran with the same three variables so its hooks reached that daemon; its own settings and sign-in stayed in `~/.gemini`.

## Results

| Check | Result |
| --- | --- |
| Edit of a claimed file | Refused. The agent reported: `tool call denied by pre-tool hook: blocked.txt is claimed by ronel (from a terminal) until at least 11:30 (Crewbo). Work on other files, or ask them to hand it over: crewbo claim request blocked.txt`; `blocked.txt` stayed `original` |
| Edit of a free file | Allowed; `free.txt` became `changed` |
| Session recorded | Daemon log: `agent session registered session=b04d8dab-... agent=antigravity` |
| Hook errors | None (`hook.log` empty) |
| MCP server | `agy` listed the `crewbo` server and `call_mcp_tool` with `ServerName: crewbo, ToolName: crewbo_claims` returned the daemon's claim on `blocked.txt` |
| Uninstall | `crewbo hooks uninstall --agent antigravity` removed both entries and the files Crewbo created; with the empty `mcp_config.json` put back from the backup, `diff -r` against the backup showed no difference |

The run used `agy -p ... --output-format stream-json --dangerously-skip-permissions` with a two-step prompt and a 150 s limit, because headless `agy` refuses file writes otherwise (U-28); the agent stayed in the repository. The run took 14 s.

## Not covered

- Windows and macOS: no `agy` run on either (U-34 for the Windows shell). Untested.
- The IDE and the Antigravity app read the same `~/.gemini/config/hooks.json` per the docs; only the CLI was run.
- `agy` keeps a cache of the MCP server's tool list in `~/.gemini/antigravity-cli/mcp/crewbo/`, which `crewbo hooks uninstall` does not remove (it is Antigravity's own state).
