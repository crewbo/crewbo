# Worklog

## 2026-10-11

Done:
- Seed commit on `main` (cf7f495): PRD, research, README, LICENSE.
- Created private repo `crewbo/spikes`.
- Branch `docs/initial-architecture`: Claude Code attribution settings, scaffolding, CI skeleton, ADRs 0001 to 0012, architecture package, Lucid diagrams (folder "Crewbo architecture"), engineering docs, CLAUDE.md.
- Re-verified hook contracts against official docs and installed CLIs.

In progress:
- Phase A PR review (Gate A).

Blockers:
- Dev toolchain not installed (sudo needed): `sudo pacman -S --needed go buf golangci-lint gofumpt goreleaser go-task`. Config files are unvalidated until then.

Next:
- After Gate A approval: Phase 0 spikes S1, S3, S5, S7 on Omarchy; prepare Windows and macOS checklists for S1 and S2.

## 2026-10-11 (afternoon)

Done:
- PR #1 merged (Gate A approved by Ronel).
- PR #2: CI minutes policy (docs-only skip, macOS on PRs and weekly) and gh commands for repo settings. Claude Code permission rules blocked Claude from applying settings directly.
- Phase 0 on Omarchy in `crewbo/spikes` (68e385b): S1, S2, S3, S4, S5, S6 (single machine), S7, latency baseline. Findings in `docs/research/phase0-findings.md`; fixtures in `test/fixtures/hooks/`.

In progress:
- Gate B evidence: Windows (Ronel), macOS (friend), interviews (Ronel), Codex trust check (U-02).

Blockers:
- Repo settings need Ronel to run the commands in `docs/engineering/repo-settings.md`.
- Friend needs access to `crewbo/spikes` (collaborator invite or zip).

Next:
- Collect Windows and macOS results, add their fixtures, finish phase0-findings, then Gate B.
