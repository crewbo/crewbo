# 0010. Monorepo layout and toolchain

- Status: Accepted
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

The Go binary, the `.proto` contract, the VS Code-family extension and the web dashboard change together. Contributors use Windows, macOS and Linux.

## Decision

- One repository, `crewbo/crewbo`, Go module `github.com/crewbo/crewbo`, layout as in `CLAUDE.md`.
- Business logic in `internal/` packages with no OS calls. OS-specific code only in `internal/platform` behind interfaces, using `_windows.go`, `_darwin.go`, `_linux.go` files.
- Go 1.27.2 (`go 1.27.0`, `toolchain go1.27.2`), formatting with gofumpt, linting with golangci-lint v2.
- Task runner: Task (`Taskfile.yml`) rather than Make. Task is one binary on all three OSes; GNU Make is not installed by default on Windows and Makefile recipes depend on a POSIX shell.
- Releases with GoReleaser.

## Alternatives considered

- Makefile: universal on Unix, awkward on Windows.
- Separate repositories for extension and web: version skew with the daemon API.

## Consequences

- Contributors install Task. On Arch Linux the package installs the binary as `go-task`.
- The extension and web dashboard need Node tooling inside the same repository.

## Links

- [Task](https://taskfile.dev)
