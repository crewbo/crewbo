# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- The daemon took minutes to notice a silently lost network (for example Wi-Fi turned off) and kept showing "connected". It now pings the sync server and shows "offline, retrying" within about 30 seconds.
- `crewbo hooks install` could hang forever when `claude --version` stalled and left a child process holding its output (for example a version-manager shim without network access). It now gives up after the 5 s timeout plus 1 s.
- `crewbo claim add` treated flags placed after the paths (`claim add src/a.go --task 12`) as extra claims.
- `crewbo hooks uninstall` replaced the `.crewbo.bak` backup with the hooked config, so the original was lost. The backup is now written only from a config without Crewbo hooks.
- `crewbo hooks install` and `uninstall` re-sorted the keys of `settings.json` and wrote `&`, `<` and `>` as `\u0026`, `\u003c` and `\u003e`. Key order and characters are now kept, and an install and uninstall give back the same file.
- `crewbo hooks uninstall` left a Codex `hooks.json` containing only `{}` (or created one when nothing was installed). A config left empty is now removed.

### Added

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
