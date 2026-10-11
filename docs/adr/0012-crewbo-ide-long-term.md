# 0012. Crewbo IDE as a long-term product

- Status: Proposed
- Date: 2026-10-11
- Decision owner: Ronel Abraham Mathew

## Context

The PRD lists "forking an IDE or editor" as a permanent non-goal. On 2026-10-11 Ronel proposed building a Crewbo-focused IDE in the long term, starting with an extension. v1 already plans a VS Code-family extension.

## Proposal

- v1 and v2: extension only, as in the PRD. No change.
- Revisit an IDE only after the v2 exit gate (50 weekly active rooms), with evidence that the extension surface blocks a needed feature.
- Until then, keep "forking an IDE" listed as a non-goal through v3, rather than permanent.

## Trade-offs

- For: full control over the editing surface (claim overlays, merge previews, approval UX) and a stronger brand.
- Against: the wedge is "keep the agent and editor you already use". An own IDE competes with Cursor, VS Code and Conductor directly and asks teams to switch tools, which is the behaviour Crewbo exists to avoid. Maintaining a VS Code fork is a large ongoing cost for a team of one.

## Decision

Not decided. If accepted, the PRD's permanent non-goals change and this ADR records why.
