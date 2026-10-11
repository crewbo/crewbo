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

Plus `checksums.txt` (SHA-256). Later (v2): winget, Homebrew tap, AUR, Open VSX for the extension.

## Signing

- Checksums file signing: decide in M6 (candidates: cosign keyless, minisign). Decision log entry required.
- Windows: SignPath (free for open-source projects; requires the repository to be public, so it waits for ADR 0011).
- macOS: notarization later (needs an Apple Developer account, paid; Ronel's approval required).

## Changelog flow

- Every user-facing PR adds a line under `## [Unreleased]` in `CHANGELOG.md` (Keep a Changelog sections: Added, Changed, Deprecated, Removed, Fixed, Security).
- At release, `Unreleased` becomes `## [x.y.z] - YYYY-MM-DD`.

## Cutting a release

1. Confirm `main` is green on all three OSes.
2. Open a PR `chore(release): vX.Y.Z` that moves `Unreleased` entries under the new version.
3. After merge, Ronel tags: `git tag -s vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z`.
4. The `release` workflow runs GoReleaser and creates a draft GitHub release.
5. Smoke test the artifacts on Linux, macOS and Windows, then publish the draft.
