# 0003. Leased advisory claims plus append-only event log

- Status: Accepted (PRD 2026-10-10)
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

Agents on different machines must not edit the same file unknowingly. Holders crash, laptops sleep, and networks split. A lock that never expires blocks the team; awareness without enforcement is ignored.

## Decision

- A claim covers a file path or a glob, relative to the repository root, and is held by one agent session on one device for one member.
- Each claim has a lease of 5 minutes, renewed on every hook event from the holder's session. A lease that is not renewed expires and the claim is released.
- Claims are advisory at the git level (nothing stops a raw `git commit`) and enforced at the agent level by hooks (ADR 0004).
- Every claim change is an event in an append-only, per-room log with a server-assigned sequence number. Current claim state is a projection of the log.
- Conflicts go through the PRD escalation ladder: inform, agent chooses wait or work elsewhere or request handoff, owner approves, lead overrides with a reason.

## Alternatives considered

- Hard locks (Git LFS style): need force-unlock and are skipped silently when the client is not configured.
- Awareness only: rules files are followed only 55 to 82% of the time (arXiv 2605.10039).

## Consequences

- A crashed holder blocks others for at most one lease period.
- Glob overlap checks are needed: two globs conflict if any path could match both. v1 checks pairwise overlap conservatively (see `docs/architecture/data-model.md`).
- Lease timing depends on wall clocks across machines. The server is the time authority for expiry; daemons use local monotonic time only for renewal scheduling.

## Links

- [Gray and Cheriton, Leases](https://blog.acolyer.org/2014/10/31/leases-an-efficient-fault-tolerant-mechanism-for-distributed-file-cache-consistency/)
- [arXiv 2606.19616](https://arxiv.org/abs/2606.19616): locks plus shared state cut duplicate work to 0 in simulation.
