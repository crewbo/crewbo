# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `crewbo claim add` treated flags placed after the paths (`claim add src/a.go --task 12`) as extra claims.
- `crewbo hooks uninstall` replaced the `.crewbo.bak` backup with the hooked config, so the original was lost. The backup is now written only from a config without Crewbo hooks.
- `crewbo hooks install` and `uninstall` re-sorted the keys of `settings.json` and wrote `&`, `<` and `>` as `\u0026`, `\u003c` and `\u003e`. Key order and characters are now kept, and an install and uninstall give back the same file.
- `crewbo hooks uninstall` left a Codex `hooks.json` containing only `{}` (or created one when nothing was installed). A config left empty is now removed.

### Added

- `crewbo serve [--addr HOST:PORT] [--tls-cert FILE --tls-key FILE] [--insecure-lan]` runs the sync server skeleton: `/healthz` and the daemon WebSocket at `/v1/ws` (Hello and Welcome only; rooms and claims follow). Plain HTTP is allowed only on loopback unless `--insecure-lan` is given.
- `crewbo version` prints the version and commit; `crewbo help` prints usage.
- `crewbo daemon` runs the per-user daemon in the foreground and stops cleanly on Ctrl+C (and SIGTERM on Linux and macOS).
- `crewbo status` reports whether the daemon is running, with its version, process id, uptime, active claims and agent sessions.
- `crewbo hook claude-code|codex <event>` enforces claims inside Claude Code and Codex: an edit to a file claimed by someone else is denied with the holder's name.
- `crewbo hooks install` and `crewbo hooks uninstall` add or remove Crewbo's hooks in Claude Code (`~/.claude/settings.json`, exec form) and Codex (`~/.codex/hooks.json`), keeping other hooks and writing a backup.
- `crewbo mcp` serves read-only MCP tools, `crewbo_claims` and `crewbo_who_holds`, so agents can see claims before planning edits.
- After a shell command, Crewbo checks whether it changed a file someone else claimed (for example through a script) and tells the agent; `crewbo status` counts these violations.
- `crewbo claim add [--for 30m] [--task ID] <path or glob>...`, `crewbo claim list` and `crewbo claim release <id>...` claim files in the current repository from a terminal.
