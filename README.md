# Crewbo

Crewbo lets every teammate run their own AI coding agent on a shared codebase at the same time, across machines and operating systems, with each agent aware of what the others are doing.

Status: early development (M2 complete, M3 next), no release yet. See [docs/product/PRD.md](docs/product/PRD.md).

## Try it locally

Requires Go (version in `go.mod`), git 2.38+ and [Task](https://taskfile.dev).

```sh
task build                           # builds ./bin/crewbo
./bin/crewbo daemon                  # terminal 1
./bin/crewbo claim add src/auth.go   # terminal 2, inside a git repository
./bin/crewbo claim list
```

To make Claude Code and Codex respect claims, install Crewbo's hooks (your own hooks and settings are kept, with a backup):

```sh
./bin/crewbo hooks install          # then, in Codex, open /hooks and trust the crewbo hooks
./bin/crewbo hooks uninstall        # removes only Crewbo's entries
```

If you run other hooks that can deny a tool call (for example a plugin's edit gate), the agent may show their message instead of Crewbo's when both deny the same edit. The edit is still blocked.

Optionally, let agents look up claims before they plan work, through Crewbo's read-only MCP tools (`crewbo_claims`, `crewbo_who_holds`):

```sh
claude mcp add --scope user --transport stdio crewbo /absolute/path/to/crewbo mcp
codex mcp add crewbo -- /absolute/path/to/crewbo mcp
```

## License

Apache-2.0 for now. The sync server licence is undecided; see the PRD.
