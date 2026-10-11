# 0001. Record architecture decisions

- Status: Accepted
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

Crewbo is built by a very small team with AI coding agents doing much of the work. Decisions made in chat are lost between sessions. Future contributors and agent sessions need the reason behind each costly decision, not only the result.

## Decision

Record every decision that is costly to reverse as a MADR file in `docs/adr/`. Record every other decision as one line in `docs/engineering/decision-log.md`. ADRs are immutable once accepted; changes happen through a superseding ADR.

## Alternatives considered

- Decisions only in the PRD: the PRD is a product document and changes as a whole; it does not keep history per decision.
- Wiki or chat: not versioned with the code, not reviewable in pull requests.

## Consequences

- Every architectural pull request either links an existing ADR or adds one.
- Small overhead per decision. Accepted.

## Links

- [MADR](https://adr.github.io/madr/)
