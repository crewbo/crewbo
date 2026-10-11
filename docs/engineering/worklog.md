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

## 2026-10-11 (evening)

Done:
- PRs #4, #5, #6 merged (Phase 0 findings on main, brand system, brand review fixes).
- Windows Phase 0 run (spikes PR #1, 40b2686): both agents block on Windows, so the hook criterion of Gate B is met. Fixtures, findings, unknowns U-20 to U-22, threat model and failure modes updated.

In progress:
- macOS run (friend gavriel953, collaborator on crewbo/spikes) and interviews (Ronel).

Blockers:
- None on Claude's side.

Next:
- macOS results, then Gate B. Optional Windows reruns for U-20, U-21, U-22 on current Codex.

## 2026-10-11 (late)

Done:
- macOS Phase 0 (spikes PR #2, 2f6f70d, run by the teammate's Codex agent): Codex blocks on macOS; Codex hook shell resolved (login shell with -c). Claude Code on macOS untested (no plan). Fixtures and docs updated; U-23 to U-25 added.
- Windows Codex rerun prompt and script pushed to spikes (a09241d, 589773f).

Next:
- Ronel: Windows rerun (agent-prompts/windows-rerun.md), interviews.
- Teammate: Codex-only macOS rerun for S7 and latency (agent-prompts/macos-rerun.md).
- Then Gate B.

## 2026-10-11 (Gate B)

Done:
- Repo settings applied by Ronel and verified (squash only, delete branches on merge, Dependabot alerts and fixes, read-only workflow token).
- Interviews: 4 teams, 3 mix vendors. Gate B closed as conditional go.

Next:
- Gate C for M1: Ronel reviews the M1 issue list (M1-01 to M1-10).
- Ronel: 6 more independent interviews and same-repo confirmation by end of M1.

## 2026-10-11 (interviews)

Done:
- PR #25 merged (SQLite store). Interviews complete: 10 teams, 41 members; 9 teams mix vendors; v1 covers 30 of 41 members; Antigravity used by 15.

Open:
- Gate B condition 2: ask all teams whether two agents worked on the same repository in their last project.
- U-26: Antigravity adapter timing (Ronel).

Next:
- M1-09 gitx (#18).

## 2026-10-11 (Antigravity spike)

Done:
- S9 live spike with agy 1.3.3 on Omarchy: deny works (also under skip-permissions), silent hook passes through, `{}` blocks, context injection works. Fixtures added. ADR 0013 proposed (Antigravity as v1 stretch).

Next:
- Ronel: accept or reject ADR 0013. Claude: M1-09 gitx (#18).
