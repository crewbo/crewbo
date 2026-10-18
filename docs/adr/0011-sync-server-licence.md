# 0011. Sync server licence

- Status: Accepted (Ronel, 2026-10-18): AGPL-3.0
- Date: 2026-10-11
- Decision owner: Ronel Abraham Mathew

## Context

The PRD plans Apache-2.0 for adapters, protocol and extension, AGPL or FSL for the sync server, and a commercial licence for enterprise features. The repository is under Apache-2.0 as a placeholder and stays private until this ADR is decided (Ronel, 2026-10-11), so no outside contribution arrives under terms that would block relicensing.

## Options

### AGPL-3.0

- OSI-approved open source; accepted by Linux distributions and most companies' open-source policies for self-hosting.
- Network clause: anyone offering a modified server as a service must publish their changes.
- Deters some companies whose policies ban AGPL.
- Does not stop a competitor from hosting the unmodified server commercially.

### FSL (Functional Source License, Apache-2.0 or MIT future licence)

- Source-available, not OSI open source. Each release converts to Apache-2.0 or MIT after 2 years.
- Forbids competing commercial use during those 2 years; allows internal use and self-hosting.
- Some developers and distributions avoid non-OSI licences; may slow community adoption and AUR or Homebrew packaging.

## Cross-cutting

- Either option needs the server code in a clearly separated directory with its own `LICENSE` file.
- Accepting outside contributions to the server later needs a CLA or DCO policy that permits the commercial licence.

## Decision

Accepted by Ronel on 2026-10-18, before the repository goes public for the M6 dogfood: the sync server (`internal/server`, run as `crewbo serve`) is licensed under **AGPL-3.0**; everything else stays Apache-2.0.

## Consequences

- `internal/server` gets its own `LICENSE` with the AGPL-3.0 text, and the README states which licence covers which code (M6-01).
- Crewbo ships as one binary (ADR 0002). Apache-2.0 code may be combined with AGPL-3.0 code, so each released `crewbo` binary is covered by AGPL-3.0 as a whole; the Apache-2.0 parts stay reusable on their own. The release notes and README say so.
- Outside contributions to the server need a contribution policy that keeps a future commercial licence possible (a DCO or a CLA); decided in M6-01 before the repository goes public.
- Choosing AGPL does not stop a competitor from hosting the unmodified server; the v3 paid tier competes on the hosted service.
