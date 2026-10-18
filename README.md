<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/assets/svg/logo/lockup-dark.svg">
    <img src="docs/brand/assets/svg/logo/lockup-light.svg" alt="Crewbo" width="280">
  </picture>
</p>

<p align="center"><strong>Every agent, one crew.</strong></p>

Crewbo lets every teammate run their own AI coding agent on a shared repository at the same time, on their own machine and operating system. Each agent knows which files the others are working on, is stopped before it edits one of them, and conflicts between branches show up before anyone merges.

**Status:** pre-release (v0.x), being tested with real teams. Expect rough edges and breaking changes. Built and tested on Linux; the main flows have also run on macOS; Windows is partly tested.

## How it works

- **Claims.** A file or glob is claimed by the agent working on it, or by you with `crewbo claim add`. Claims are leases: an agent's claims lapse a few minutes after it stops.
- **Hooks block edits.** Crewbo installs a hook in each agent. Before the agent edits a file, or runs a shell command that writes one, the hook asks the local Crewbo daemon; an edit to a file a teammate has claimed is refused with a message naming the file and the person.
- **A small sync server shares metadata.** Everyone in a room sees everyone's claims within seconds. Code moves only through your git remote.
- **Handoffs, not fights.** Ask the holder for a file (`crewbo claim request`), they hand it off or say not yet, and the room lead can step in.
- **Tasks and merging.** Each piece of work is a task on its own branch and worktree. Crewbo checkpoints it, warns when two tasks change the same lines, and merges a task into main only through that check. A lead can send signed tasks for a teammate's agent to run, after the teammate reads and approves them.
- **See it all** in a VS Code view, a local dashboard (`crewbo dashboard`) or the CLI.

## Supported agents

| Agent | Claims enforced | Context and MCP tools | Runs dispatched tasks |
| --- | --- | --- | --- |
| Claude Code | Yes | Yes | Yes |
| Codex | Yes | Yes | Yes |
| Antigravity | Yes | Yes | Not yet |

## What leaves your machine

The sync server receives file paths and globs, task text and status, branch and commit names, handoff reasons, your GitHub login, and your computer's name and OS. File contents never leave your machine, and Crewbo sends no telemetry. You can run the sync server yourself (`crewbo serve`). Details: [what Crewbo shares and stores](docs/user/privacy.md).

## Quick start

There is no release yet: build from source. You need Go (the version in `go.mod`), git 2.38 or later and [Task](https://taskfile.dev).

```sh
task build                                   # builds ./bin/crewbo
```

One teammate runs the sync server where everyone can reach it (behind a tunnel or with TLS; see [running the sync server](docs/user/getting-started.md#running-the-sync-server)):

```sh
crewbo serve
```

Everyone, on their own machine, inside their clone of the repository:

```sh
crewbo daemon                                # keep this running
crewbo login --server https://your-server    # sign in with GitHub
crewbo room create                           # the lead, once; then: crewbo room invite
crewbo room join CODE                        # everyone else
crewbo hooks install                         # Claude Code, Codex and Antigravity; your own hooks are kept
```

Run `crewbo doctor` to check the setup: it prints a fix for anything missing. Then work as usual with your agent. Try `crewbo claim add src/auth.go`, `crewbo claim list` and `crewbo task start "Build the login page"`; `crewbo help` lists everything.

Notes:

- Codex runs new hooks only after you trust them: open Codex, type `/hooks` and trust the crewbo hooks.
- Keep the `crewbo` binary in a folder without spaces: Codex and Antigravity run hooks as shell commands.
- Agents can look up claims through Crewbo's MCP tools: `claude mcp add --scope user --transport stdio crewbo /absolute/path/to/crewbo mcp` and `codex mcp add crewbo -- /absolute/path/to/crewbo mcp`. For Antigravity, `crewbo hooks install` registers them.
- If you run other hooks that can deny a tool call, the agent may show their message instead of Crewbo's when both deny the same edit. The edit is still blocked.
- The VS Code extension is in [`extension/`](extension/README.md).

## Documentation

- [Getting started](docs/user/getting-started.md), [troubleshooting](docs/user/troubleshooting.md) and [what Crewbo shares](docs/user/privacy.md)
- [Product requirements](docs/product/PRD.md) and [architecture](docs/architecture/overview.md)
- [Decisions](docs/adr/) and the [engineering docs](docs/engineering/)
- [Brand](docs/brand/README.md)
- [Contributing](CONTRIBUTING.md), [security](SECURITY.md), [code of conduct](CODE_OF_CONDUCT.md)

## License

- The sync server, `internal/server` (`crewbo serve`), is licensed under the [GNU Affero General Public License v3.0](internal/server/LICENSE) (ADR 0011).
- Everything else, including the daemon, hooks, CLI, protocol, VS Code extension and dashboard, is licensed under the [Apache License 2.0](LICENSE).
- `crewbo` is a single binary that includes the sync server, so each released binary is covered by the AGPL-3.0 as a whole. The Apache-2.0 parts stay reusable on their own.

See [NOTICE](NOTICE).
