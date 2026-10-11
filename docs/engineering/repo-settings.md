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

## Apply with the GitHub CLI

Claude's permission rules do not allow it to change repository settings, so run these yourself (in Claude Code, prefix each with `!`). Each was written against the GitHub REST API; check the output for errors.

```sh
R=crewbo/crewbo

# Squash merge only, PR title and body as the commit message, delete head branches
gh api -X PATCH repos/$R \
  -F allow_squash_merge=true -F allow_merge_commit=false -F allow_rebase_merge=false \
  -F delete_branch_on_merge=true \
  -f squash_merge_commit_title=PR_TITLE -f squash_merge_commit_message=PR_BODY

# Dependabot alerts and security updates
gh api -X PUT repos/$R/vulnerability-alerts
gh api -X PUT repos/$R/automated-security-fixes

# Read-only default token for workflows
gh api -X PUT repos/$R/actions/permissions/workflow \
  -f default_workflow_permissions=read -F can_approve_pull_request_reviews=false

# Private vulnerability reporting (expected to fail while the repo is private)
gh api -X PUT repos/$R/private-vulnerability-reporting
```

Branch protection is not available for private repositories on the Free plan, so it is not listed. Apply it when the repository goes public.

## CI minutes policy (2026-10-11)

- Docs-only changes (`docs/**`, `*.md`) do not trigger CI.
- macOS runs on pull requests, a weekly run (Monday 03:00 UTC) and manual runs; pushes to `main` run Linux and Windows only.
- Real-machine checks: Ronel on Windows (dual boot) and Omarchy; a friend with a Mac runs the macOS checklists in Phase 0 and before releases.
- Caveat: once branch protection with required checks is enabled, `paths-ignore` means docs-only PRs never report the required checks and cannot merge. At that point, replace `paths-ignore` with an in-job skip.
