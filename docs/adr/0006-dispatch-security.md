# 0006. Dispatch security: verbatim approval and Ed25519-signed tasks

- Status: Accepted (PRD 2026-10-10)
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

A lead dispatches a task that runs a coding agent on a teammate's machine. That agent has private data (the teammate's code and credentials), reads untrusted content (the task text, repository files) and can communicate externally. This is the "lethal trifecta": no reliable prompt-injection defence exists, so mitigation must be structural ([Simon Willison](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/)). The sync server relays tasks and must not be able to forge them.

## Decision

- Each device generates an Ed25519 key pair. The public key is registered with the server against the member's GitHub identity. The private seed stays in the OS keychain (`github.com/zalando/go-keyring`), with a fail-fast fallback to a 0600 file when no secret service exists.
- The lead's daemon serializes the task body once (id, room, author device key id, target member, text, owned paths, base commit, created time, expiry) and signs those exact bytes. Verifiers check the signature over the received bytes before decoding.
- The teammate's daemon verifies the signature against the lead's registered key and the room's current lead before showing anything. Unknown, revoked or non-lead keys are rejected.
- The task text is shown verbatim, in full, before approval. Nothing runs without an explicit approve action by the teammate.
- Approved tasks run in a fresh worktree, launched through the teammate's own agent CLI and login, with an explicit non-bypass permission mode. Never `bypassPermissions` or `danger-full-access`.
- Task text is data, never instructions to the daemon.

## Alternatives considered

- Auto-execute dispatched tasks: unacceptable injection risk.
- Server-side signing: a compromised server could forge tasks.
- TLS only: protects transport, not against the server itself.

## Consequences

- Key loss means re-registering the device. Key rotation and revocation are needed in v1 (runbook in `docs/runbooks/`).
- A teammate who approves without reading can still run malicious text. This residual risk is accepted and documented in the threat model.

## Links

- `docs/architecture/threat-model.md`
- `docs/research/hook-and-dispatch-internals.md`, key storage section
