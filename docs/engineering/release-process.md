# Release process

## Versioning

- Semantic Versioning. Tags `vMAJOR.MINOR.PATCH`.
- Stay on `v0.x` until the v1 gate passes. In `v0.x`, a minor bump may break compatibility; the changelog says so.
- Protocol compatibility is separate: `crewbo.v1` stays compatible within its major version (see `docs/architecture/protocol.md`).

## Artifacts (GoReleaser)

| OS | Arch | Archive |
| --- | --- | --- |
| Linux | amd64, arm64 | `.tar.gz` |
| macOS | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

Each archive holds the binary, `README.md`, `CHANGELOG.md`, `LICENSE` (Apache-2.0), `NOTICE`, `internal/server/LICENSE` (AGPL-3.0, which covers the binary as a whole) and the OFL licences of the dashboard fonts embedded in the binary (`web/fonts/LICENSE-*.txt`).

Plus the extension, `crewbo-<version>.vsix`, and `checksums.txt` (SHA-256 of every archive and the `.vsix`). Later (v2): winget, Homebrew tap, AUR. The extension's store publishing is in `docs/runbooks/publishing-extension.md`.

Try the build locally without publishing anything: `cd extension && npm ci --ignore-scripts && npm run package && cd .. && goreleaser release --snapshot --clean` (output in `dist/`).

## Signing

- Downloads are signed with GitHub artifact attestations (decision log 2026-10-19): the release workflow attests every file in `checksums.txt` with `actions/attest` (Sigstore, keyless; the attestation records the workflow, commit and repository that built the file). Anyone checks a download with `gh attestation verify FILE -R crewbo/crewbo`. Fallback if attestations are unavailable to the repository: cosign keyless signing of `checksums.txt` (U-36).
- Windows: SignPath (free for open-source projects; requires the repository to be public, so it waits for ADR 0011).
- macOS: notarization later (needs an Apple Developer account, paid; Ronel's approval required).

## Changelog flow

- Every user-facing PR adds a line under `## [Unreleased]` in `CHANGELOG.md` (Keep a Changelog sections: Added, Changed, Deprecated, Removed, Fixed, Security).
- At release, `Unreleased` becomes `## [x.y.z] - YYYY-MM-DD`.

## Cutting a release

1. Confirm `main` is green on all three OSes.
2. Open a PR `chore(release): vX.Y.Z` that moves `Unreleased` entries under the new version and sets `"version": "X.Y.Z"` in `extension/package.json` (the release workflow stops if the two differ).
3. After merge, Ronel tags: `git tag -s vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z`.
4. The `release` workflow runs GoReleaser and creates a draft GitHub release.
5. Smoke test the artifacts on Linux, macOS and Windows: download the archive for the machine, check it with `gh attestation verify`, unpack it, then `crewbo version` (shows X.Y.Z and the commit), `crewbo doctor`, and one claim refused across two daemons (the M3 check's first step). Install the `.vsix` once.
6. Publish the draft, then the extension to both stores (`docs/runbooks/publishing-extension.md`).
