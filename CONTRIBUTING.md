# Contributing

Crewbo is pre-release (v0.x). Issues and pull requests are welcome; read this first.

## Licences and sign-off

- Code in `internal/server/` (the sync server) is licensed under the AGPL-3.0; everything else under the Apache-2.0 (see `NOTICE`). Your contribution is licensed under the licence of the files it changes.
- Every commit must carry a `Signed-off-by:` line certifying the [Developer Certificate of Origin 1.1](https://developercertificate.org/). `git commit -s` adds it, using your configured name and email.
- A pull request that changes `internal/server/` also needs a signed Contributor License Agreement, which lets the maintainer offer the server under another licence in future (ADR 0011). A bot asks for it on the pull request. Until the CLA is set up, server changes from outside contributors wait.

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
