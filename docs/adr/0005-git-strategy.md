# 0005. Git strategy: task branches, refs/crewbo/ checkpoints, daemon merge queue

- Status: Accepted (PRD 2026-10-10); the merge rule is superseded by ADR 0014 (members merge their own work through the conflict check, with an optional lead-only mode)
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

Code must move only through the team's own git remote. Teams use personal and free GitHub accounts, where GitHub's merge queue is not available (organization repositories only). GitHub recommends at most 6 pushes per minute per repository.

## Decision

- One short-lived branch per task, `crewbo/<task-id>-<slug>`, in its own worktree.
- Checkpoint refs under `refs/crewbo/<member>/<task-id>` pushed when an agent stops, not on a timer.
- Conflict detection with `git merge-tree --write-tree` between task heads, run by the daemon after each checkpoint.
- Merges go through the daemon's own serial merge queue on the lead's machine.
- Always shell out to system git, version 2.38 or later (first release with `merge-tree --write-tree`). Never use go-git for merges.
- Pass `-c core.longpaths=true` on Windows.

## Alternatives considered

- One shared branch: conflicts at every push.
- Long-lived branches: conflicts surface late.
- GitHub merge queue as primary: organization repositories only.
- Pushing WIP refs every 30 s: exceeds 6 pushes per minute at 3 active users.

## Consequences

- If GitHub rejects pushes to custom refs (unknown U-06), the fallback is `wip/<member>/<task-id>` branches.
- The lead's machine must be online to merge.

## Links

- [git-merge-tree](https://git-scm.com/docs/git-merge-tree)
- `docs/research/hook-and-dispatch-internals.md`, "Custom refs work on GitHub"
