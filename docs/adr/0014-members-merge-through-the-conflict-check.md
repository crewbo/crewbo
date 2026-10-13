# 0014. Members merge their own work through the conflict check; every piece of work is a task

- Status: Accepted (Ronel, 2026-10-13); the rule on conflicts with another task's checkpoint is superseded by ADR 0015 (merge after confirming with `--despite`)
- Date: 2026-10-13
- Decision owner: Ronel Abraham Mathew
- Supersedes: the merge rule in ADR 0005 ("Merges go through the daemon's own serial merge queue on the lead's machine"). The rest of ADR 0005 stands.

## Context

ADR 0005 gives every task its own branch and worktree, checkpoints task branches when an agent stops, checks them against each other with `git merge-tree`, and merges through a serial queue on the lead's machine. Two problems showed up while closing M4:

1. Only dispatched tasks get a branch, checkpoints and conflict checks. A member who starts their own feature branch and works with their own agent is still bound by claims, but Crewbo does not know the branch exists, so a conflict with another member's branch is found only at merge time. In a hackathon most work is self-directed, not dispatched.
2. A lead-only merge queue makes the lead a bottleneck who must be online, which slows a hackathon team, while the safety actually comes from the conflict check, not from who presses the button.

## Decision

- **Every piece of team work is a task.** Besides tasks the lead dispatches, a member can start a task for themselves (`crewbo task start "<text>" --path <glob>...`): no lead and no approval step, otherwise the same flow: its own branch `crewbo/<task id>` and worktree, claims on its paths, checkpoints when the agent stops, and `git merge-tree` checks against the other active task branches. An existing branch can be registered the same way, so work started before Crewbo is covered.
- **Default merge mode: members merge their own work.** The task's assignee (and the lead, for any task) runs `crewbo task merge <id>`. Crewbo fetches, re-runs `git merge-tree` against the current `main` and against every other active task branch, and refuses a merge that would conflict with `main`, or with another member's active branch unless the lead overrides it. A clean merge is pushed to `main`.
- **Merges are serialised by the remote.** Pushing to `main` fails when someone else pushed first; Crewbo then fetches, re-checks and retries, so two members merging at once never both land unchecked. No merge queue on one machine is needed.
- **Optional lead-only mode.** The lead can set a room to lead-only merging; members' daemons then refuse `crewbo task merge` from anyone but the lead, and the lead merges as before.
- How a task branch lands on `main` (merge commit or squash) is decided in M5 and recorded in the decision log.

## Alternatives considered

- Keep the lead-only queue (ADR 0005): simplest and safest, but a bottleneck and needs the lead online; kept as the optional mode.
- Lead-only or members per room, chosen at room creation (option c): more to build and explain; the optional mode gives the same choice with one default.
- Track any branch automatically, without tasks: Crewbo cannot tell which branches are team work, and claims and checkpoints need a task to hang on.

## Consequences

- Lead-only mode is a Crewbo rule, not a lock: a member can still `git push` to `main` with plain git. Real enforcement needs branch protection, which GitHub Free does not enforce on private repositories (`docs/engineering/repo-settings.md`); the PRD's team audience mostly uses free accounts.
- The lead's machine no longer has to be online for work to land.
- Self-started tasks need a room event the server records without a lead's signature (the member is author and assignee); its shape is decided in M5 and does not weaken the verification of dispatched tasks.
- PRD, `docs/architecture/flows.md` (flow 5) and the M5 issue list are updated to match.

## Links

- ADR 0005 (git strategy), ADR 0006 (dispatch security)
- `docs/research/m4-two-machine-check.md`
