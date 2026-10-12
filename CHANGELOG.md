# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- An agent working in a git worktree (`git worktree add`) was not blocked by claims made in the main checkout or shared by teammates, because Crewbo treated each worktree as a separate repository. A linked worktree now shares its repository's claims and room.
- When the hook could not get an answer from the daemon for any reason, it always said the daemon "did not answer in time". It now says that only for real timeouts and names the actual failure otherwise.
- The daemon took minutes to notice a silently lost network (for example Wi-Fi turned off) and kept showing "connected". It now pings the sync server and shows "offline, retrying" within about 30 seconds.
- A claim a teammate made from their terminal was described as their agent's ("gavriel953's agent") in hook messages, `crewbo claim list` and the MCP tools once shared through a room. It now says "(from a terminal)" again.
- `crewbo hooks install` could hang forever when `claude --version` stalled and left a child process holding its output (for example a version-manager shim without network access). It now gives up after the 5 s timeout plus 1 s.
- `crewbo claim add` treated flags placed after the paths (`claim add src/a.go --task 12`) as extra claims.
- `crewbo hooks uninstall` replaced the `.crewbo.bak` backup with the hooked config, so the original was lost. The backup is now written only from a config without Crewbo hooks.
- `crewbo hooks install` and `uninstall` re-sorted the keys of `settings.json` and wrote `&`, `<` and `>` as `\u0026`, `\u003c` and `\u003e`. Key order and characters are now kept, and an install and uninstall give back the same file.
- `crewbo hooks uninstall` left a Codex `hooks.json` containing only `{}` (or created one when nothing was installed). A config left empty is now removed.

### Added

- `crewbo task approve ID --agent codex` runs the task with Codex (`codex exec --sandbox workspace-write`) instead of Claude Code, on Linux and macOS; Windows is refused until Codex's sandbox works there. Crewbo checks `codex login status` first and reports why a run failed. Any agent working in a task's worktree holds the task's claims.
- `crewbo task approve` now runs the task: Claude Code works on it in a new git worktree (under Crewbo's data directory, on branch `crewbo/<task id>`, starting at the lead's commit), with the task's paths claimed for it and kept claimed while it runs. Before approving, Crewbo checks that your clone has the lead's commit and that Claude Code is signed in, so you can fix either and approve again. `ANTHROPIC_API_KEY` is removed from Claude Code's environment unless you pass `--use-api-key`. The worktree and a log are kept after the run; the lead is told when it starts and if it fails.
- `crewbo task list`, `show ID`, `approve ID` and `decline ID [--reason TEXT]` for tasks sent to you. `show` prints the whole task, with any control characters (such as terminal escape codes) shown as text so nothing in it can be hidden. `approve` shows the task first and asks you to type yes, and works only in an interactive terminal; approving runs nothing yet. Declines, approvals and tasks left unanswered until they expire are reported to the room. ID may be any unique start of a task id.
- A teammate's daemon now receives tasks sent to them, including ones sent while they were offline, and checks each one against the room lead's registered signing key before keeping it. Tasks that fail the check are kept as rejected, with the reason, and none of their content is trusted. Listing, approving and running tasks follow in the next releases.
- `crewbo task create --to LOGIN [--path PATH]... [--for DURATION] (TEXT | --file FILE)` lets a room lead send a signed task to a teammate's agent. Approving and running tasks follow in the next releases.
- In a repository that is in a room, `crewbo claim add` and `crewbo claim release` go through the sync server: a teammate's claim blocks your agents' edits on their machine and yours, and you are told who holds a file. Claims made offline are kept locally, shared on reconnect, and dropped (with a note in `crewbo status`) if someone else took the file meanwhile.
- The daemon connects to the sync server for repositories in a room (after `crewbo login` and `crewbo room create` or `join`), catches up after a server restart or time offline, and keeps working locally when it cannot connect. `crewbo status` shows the sync state.
- `crewbo room create | invite | join | leave | status`, run inside a git repository, put that repository in a room on the sync server: the creator is the lead, invites are codes valid 24 h for up to 9 people by default, and a room holds at most 10 members.
- `crewbo login` also creates this device's signing key (kept in the system keychain, or in a file readable only by you when no keychain answers within 3 s) and registers its public key with the sync server.
- `crewbo login --server URL` signs in with GitHub (device flow: open github.com/login/device and enter the code shown) and saves a 7-day Crewbo session for that sync server. `crewbo serve` now issues these sessions, requires one on every daemon connection, and keeps its data in `server.db` (`--data DIR`).
- `crewbo serve [--addr HOST:PORT] [--tls-cert FILE --tls-key FILE] [--insecure-lan]` runs the sync server skeleton: `/healthz` and the daemon WebSocket at `/v1/ws` (Hello and Welcome only; rooms and claims follow). Plain HTTP is allowed only on loopback unless `--insecure-lan` is given.
- `crewbo version` prints the version and commit; `crewbo help` prints usage.
- `crewbo daemon` runs the per-user daemon in the foreground and stops cleanly on Ctrl+C (and SIGTERM on Linux and macOS).
- `crewbo status` reports whether the daemon is running, with its version, process id, uptime, active claims and agent sessions.
- `crewbo hook claude-code|codex <event>` enforces claims inside Claude Code and Codex: an edit to a file claimed by someone else is denied with the holder's name.
- `crewbo hooks install` and `crewbo hooks uninstall` add or remove Crewbo's hooks in Claude Code (`~/.claude/settings.json`, exec form) and Codex (`~/.codex/hooks.json`), keeping other hooks and writing a backup.
- `crewbo mcp` serves read-only MCP tools, `crewbo_claims` and `crewbo_who_holds`, so agents can see claims before planning edits.
- After a shell command, Crewbo checks whether it changed a file someone else claimed (for example through a script) and tells the agent; `crewbo status` counts these violations.
- `crewbo claim add [--for 30m] [--task ID] <path or glob>...`, `crewbo claim list` and `crewbo claim release <id>...` claim files in the current repository from a terminal.
