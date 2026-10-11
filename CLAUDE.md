# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Crewbo is a coordination layer that lets every teammate run their own AI coding agent (Claude Code and Codex in v1) on a shared codebase at the same time, across machines and operating systems. A local Go daemon enforces leased file claims through each agent's own hooks, a small sync server shares metadata (never file contents), and the daemon warns about cross-machine merge conflicts before merge. Code moves only through the team's git remote.

- Product: [docs/product/PRD.md](docs/product/PRD.md) (repo snapshot; the canonical living copy is the Crewbo PRD doc on claude.ai; if they disagree, ask Ronel)
- Architecture: [docs/architecture/overview.md](docs/architecture/overview.md)
- Decisions: [docs/adr/](docs/adr/)
- Engineering: [docs/engineering/](docs/engineering/) (plan, unknowns, decision log, testing, release, worklog)

Source precedence: Ronel's explicit instructions > PRD > accepted ADRs > `docs/research/` > own judgement. The research reports use the old name **Sangam** (`sangam hook` = `crewbo hook`) and predate the PRD.

## Current phase

- Gate A approved 2026-10-11 (PR #1).
- Current: M0, Phase 0 spikes in the private repo `crewbo/spikes`. Linux done; Windows, macOS and interviews pending. Findings: `docs/research/phase0-findings.md`.
- Next gate: **Gate B** (go / no-go), target Oct 17. No product code before it.

## Commands

Task runner is [Task](https://taskfile.dev) (`Taskfile.yml`). On Arch Linux the binary is `go-task`.

| Purpose | Command |
| --- | --- |
| Build | `task build` (outputs `bin/crewbo`) |
| Unit and integration tests | `task test` (`go test -race ./...`) |
| Single test | `go test -race -run TestName ./internal/claims/` |
| End-to-end tests | `task test:e2e` |
| Coverage | `task cover` |
| Lint | `task lint` (golangci-lint, buf lint) |
| Format | `task fmt` (gofumpt via golangci-lint) |
| Generate Protobuf | `task generate` (`buf generate`) |
| Run daemon | `task daemon` |
| Run sync server locally | `task serve` |
| Extension dev | Not set up yet (M5) |

None of these work until Go code exists (M1). Toolchain: Go 1.27.2 (go.mod toolchain directive), buf, golangci-lint v2, gofumpt, goreleaser, Task.

## Repo map

```
cmd/crewbo/          main package; wires subcommands only
internal/claims/     claim and lease logic (pure, no I/O)
internal/hooks/      normalised hook events; claudecode/ and codex/ adapters
internal/shellparse/ shell command write-target detection
internal/gitx/       system git wrapper (worktrees, merge-tree, refs)
internal/daemon/     daemon lifecycle, local API, sync client, queue
internal/server/     sync server (WebSocket hub, rooms, event log)
internal/dispatch/   task signing, approval, headless agent launch
internal/keys/       Ed25519 keys, keychain with 0600 file fallback
internal/auth/       GitHub device flow
internal/mcpserver/  MCP tools (Go structs)
internal/store/      SQLite persistence and migrations
internal/protocol/   generated Protobuf code (never edit)
internal/platform/   OS-specific code behind interfaces
proto/crewbo/v1/     .proto source of truth
extension/ web/      VS Code-family extension, daemon-served dashboard
test/e2e/ test/sim/ test/fixtures/hooks/   e2e, simulator, captured hook payloads
docs/                product, research, architecture, adr, engineering, runbooks, user
```

## Non-negotiable rules

1. Never invent an API, CLI flag, config key, path, library, version or behaviour. Verify against official docs (fetched, not a search snippet), the tool's `--help`, or the library source at the pinned version. Cite the URL in a code comment next to non-obvious external contracts.
2. If something cannot be verified, mark it UNVERIFIED, add it to `docs/engineering/unknowns.md` with a fallback, and continue with the fallback or ask Ronel.
3. Never claim something works unless it was run in this session. Say what ran and on which OS. Write "untested on <OS>" otherwise.
4. Never silently change a PRD or ADR decision. Write a superseding ADR and flag it to Ronel.
5. Timebox any blocking unknown to 90 minutes, then take the fallback and log it.
6. Stop and wait for Ronel at: Gate A (after planning), Gate B (after Phase 0), Gate C (before each milestone), and before changing a PRD decision, picking the sync server licence, adding a paid or non-permissive dependency, rewriting git history, changing repo settings, or anything irreversible.
7. No AI or tool attribution in commits or PRs: no `Co-Authored-By`, no "Generated with", no emoji. `.claude/settings.json` sets `attribution` to empty strings; check every message anyway.
8. No em dashes in any written copy (docs, commits, PRs, comments). No emoji, no marketing adjectives.
9. Never edit generated code (`internal/protocol/`). Regenerate with `task generate`.
10. Never use go-git for merges; shell out to system git (2.38 or later).
11. Hooks are the compiled `crewbo` binary, never shell scripts.
12. Never send file contents to the sync server. Only paths, globs, task text, branch names, vendor and presence leave a machine. No telemetry.
13. Cross-platform by default: every feature designed for Windows, macOS and Linux. OS calls only in `internal/platform`.
14. v1 supports only Claude Code and Codex. Do not add features outside the current milestone's issues.

## Coding conventions

- Formatting gofumpt; linting golangci-lint with the committed `.golangci.yml`.
- Logging `log/slog`, structured. No `fmt.Println` in library code. Never log task text, tokens or keys.
- Wrap errors with `%w` and context. No panics outside `main`. Typed sentinel errors where callers branch.
- Every goroutine has an owner and a `context.Context` cancellation path. Tests run with `-race`.
- No global mutable state; inject dependencies through constructors.
- `path/filepath` for paths, never string concatenation. Pass `-c core.longpaths=true` to git on Windows.
- Document every exported identifier.
- Prefer the standard library. Every new dependency gets a decision-log entry (licence, maintenance, why not stdlib) and is pinned.
- Coverage at least 85% for `internal/claims`, `internal/shellparse`, `internal/hooks/...`, `internal/dispatch`. Hook adapters test against golden fixtures.

## Git workflow

- Trunk-based. Branch per issue: `<type>/<short-kebab-description>`. Open a PR with the template. Ronel reviews and merges.
- Never merge own PRs, push to `main`, force-push shared branches, or rewrite published history unless Ronel says so for that case.
- Conventional Commits 1.0.0: `<type>(<scope>): <imperative lower-case summary, max 72 chars>`, body explains why, footer `Refs: #N` or ADR links.
- Types: feat, fix, docs, refactor, perf, test, build, ci, chore, revert.
- Scopes: cli, daemon, claims, hooks, claudecode, codex, shellparse, gitx, mcp, dispatch, keys, auth, server, store, proto, extension, web, docs, ci, release, deps.
- One commit per logical change that builds and passes tests. Refactors separate from behaviour changes. Generated code in its own `build(proto)` commit. Dependency bumps in their own `build(deps)` commit.
- PRs under about 400 changed lines excluding generated code. Squash merge uses the PR title (also Conventional Commits).
- Update `CHANGELOG.md` under Unreleased for user-facing changes; update this file when a command, convention or gotcha changes.

## Where to record things

- Costly-to-reverse decisions: new ADR in `docs/adr/`.
- Every other decision: one line in `docs/engineering/decision-log.md`.
- Unknowns and fallbacks: `docs/engineering/unknowns.md`.
- End of every session: append to `docs/engineering/worklog.md` (done, in progress, blockers, next step).

## Known gotchas

- Windows Claude Code hooks: register in exec form (`command` plus `args`), which spawns a real `.exe` with no shell. Shell form goes through Git Bash and mangles backslash paths. `file_path` arrives with backslashes on Windows; normalise before comparing. ([hooks reference](https://code.claude.com/docs/en/hooks))
- Codex hook trust: non-managed hooks are skipped until trusted in `/hooks`, and trust is pinned to the hook's hash. Keep the hook entry byte-for-byte stable. Emitting `permissionDecision: "ask"`, `continue`, `stopReason` or `suppressOutput` fails the hook open. ([Codex hooks](https://learn.chatgpt.com/docs/hooks))
- `ANTHROPIC_API_KEY` in the environment silently switches `claude -p` to API billing. The daemon strips it from dispatched runs unless the member opts in. ([authentication precedence](https://code.claude.com/docs/en/authentication#authentication-precedence): "In non-interactive mode (`-p`), the key is always used when present.")
- Never launch `claude -p` with `--bare`: it skips hooks and MCP. Always pass `--permission-mode` explicitly; the installed CLI (2.1.294) offers `acceptEdits`, `auto`, `bypassPermissions`, `manual`, `dontAsk`, `plan`.
- `zalando/go-keyring` can block for over 60 s when D-Bus cannot activate a secret service; wrap every call in a timeout (Phase 0 S7).
- Codex `apply_patch` headers carry absolute paths in 0.161.0; Claude sends absolute `file_path` (backslashes on Windows). Normalise both to repo-relative before comparing claims.
- GitHub recommends at most 6 pushes per minute per repository, so checkpoint refs are pushed at agent stop, not on a timer. (research: `docs/research/hook-and-dispatch-internals.md`)
- The `crewbo` org is on GitHub Free: branch protection is not enforced on the private repo, and macOS Actions minutes cost about 10 times Linux minutes. See `docs/engineering/repo-settings.md`.
