# 0015. A member may merge over another task's conflicting checkpoint after confirming

- Status: Accepted (Ronel, 2026-10-13)
- Date: 2026-10-13
- Decision owner: Ronel Abraham Mathew
- Supersedes: in ADR 0014, "refuses a merge that would conflict ... with another member's active branch unless the lead overrides it". The rest of ADR 0014 stands.

## Context

Building `crewbo task merge` (M5-04) showed that ADR 0014's rule deadlocks: when two unmerged tasks conflict, each conflicts with the other's checkpoint, so neither can merge without the lead stepping in. One of them has to go first; once it is merged, the other conflicts with main, which is refused anyway until it is updated.

## Decision

- A conflict with main is always refused.
- A conflict with another task's latest checkpoint is refused once, naming the task and the files and showing the command to merge anyway. The member confirms by repeating the merge with `--despite <task>` for each conflicting task. The merge then goes ahead, and the other task's owner must update their task, because it now conflicts with main.
- No lead override is needed for this. The optional lead-only room mode in ADR 0014 is unchanged.

## Alternatives considered

- Keep the lead override (ADR 0014): every conflicting pair would need the lead.
- Merge without asking and only warn: the member may not have seen the other task.

## Consequences

- Whoever merges first decides; the confirmation makes that a conscious choice after talking to the other owner.
