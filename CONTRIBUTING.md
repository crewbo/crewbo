# Contributing

Crewbo is pre-release (v0.x). Issues and pull requests are welcome; read this first.

## Licences and the CLA

- Code in `internal/server/` (the sync server) is licensed under the AGPL-3.0; everything else under the Apache-2.0 (see `NOTICE`). Your contribution is licensed under the licence of the files it changes.
- Before your first pull request can be merged, you sign the [Crewbo Contributor License Agreement](docs/legal/CLA.md) once: the CLA assistant bot comments on the pull request with a link, and signing takes one click with your GitHub account. The CLA lets the maintainer also offer Crewbo under other licences in future (ADR 0011); your contribution always stays available under its open source licence.

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
