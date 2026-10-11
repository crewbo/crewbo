# GitHub settings to apply manually

Claude does not change repository or organization settings. Ronel applies these in the GitHub UI.

## Plan constraints (checked 2026-10-11)

The `crewbo` org is on GitHub Free and `crewbo/crewbo` is private. Consequences:

- Branch protection and rulesets are not enforced on private repositories on the Free plan. Until the repository is public or the org upgrades, `main` is protected by convention only (no direct pushes, PRs only).
- Actions minutes: Free orgs get 2,000 minutes per month for private repositories. Per-minute rates: Linux $0.006, Windows $0.010, macOS $0.062, so one macOS minute uses about 10 times the budget of a Linux minute. Public repositories on standard runners are free. ([GitHub Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions))
- To stay inside the budget while private, consider running the macOS and Windows jobs only on pull requests to `main` and on a nightly schedule, not on every push. This is a decision for Ronel.

## Branch protection or ruleset for `main`

Settings, Rules, Rulesets, New branch ruleset, target `main`:

- Require a pull request before merging (1 approval once there is a second maintainer; 0 for now so Ronel can merge).
- Require status checks to pass: `test (ubuntu-latest)`, `test (macos-latest)`, `test (windows-latest)`, `lint`, `secrets`.
- Require linear history.
- Block force pushes.
- Restrict deletions.

Apply these now so they take effect as soon as the repository is public or the plan changes.

## Merge settings

Settings, General, Pull Requests:

- Allow squash merging only (disable merge commits and rebase merging).
- Default squash commit message: pull request title and description.
- Automatically delete head branches.

## Security

Settings, Advanced Security (or Code security):

- Dependabot alerts: on.
- Dependabot security updates: on.
- Private vulnerability reporting: on (available for public repositories; for private repositories use a security contact until public).
- Secret scanning and push protection: on when available. On private repositories this needs a paid plan; the CI `secrets` job (gitleaks) covers the gap.
- Code scanning (CodeQL): runs from `.github/workflows/codeql.yml` once public.

## Actions

Settings, Actions, General:

- Allow actions from GitHub and verified creators, plus the pinned third-party actions used in workflows.
- Workflow permissions: read repository contents by default.
