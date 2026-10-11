# Crewbo

Crewbo lets every teammate run their own AI coding agent on a shared codebase at the same time, across machines and operating systems, with each agent aware of what the others are doing.

Status: early development (milestone M2), no release yet. See [docs/product/PRD.md](docs/product/PRD.md).

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

## License

Apache-2.0 for now. The sync server licence is undecided; see the PRD.
