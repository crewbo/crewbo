# Contributing

Crewbo is pre-release and the repository is private while the sync server licence is decided. External contributions are not yet accepted.

## Setup

Requirements: Go (version in `go.mod`), git 2.38 or later, [Task](https://taskfile.dev), [buf](https://buf.build), [golangci-lint](https://golangci-lint.run) v2.

```sh
task build
task test
task lint
```

## Workflow

- Branch from `main` as `<type>/<short-kebab-description>`.
- One pull request per issue. Keep it under about 400 changed lines, excluding generated code.
- Commits follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/). See `CLAUDE.md` for types and scopes.
- CI must pass on Linux, macOS and Windows.
- Record decisions in `docs/engineering/decision-log.md`; costly-to-reverse ones also get an ADR in `docs/adr/`.

## Writing style

Plain English, short sentences, no em dashes, no emoji.
