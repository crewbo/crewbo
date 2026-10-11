# 0011. Sync server licence

- Status: Proposed
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

Not decided. Owner: Ronel, before the first public release.
