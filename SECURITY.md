# Security policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub private vulnerability reporting:
https://github.com/crewbo/crewbo/security/advisories/new

Do not open a public issue. We aim to acknowledge reports within 3 working days.

## Scope

Crewbo runs on teammates' machines and executes tasks dispatched by a team lead, so these areas matter most:

- Task signature verification and the approval gate.
- The hook binary's handling of agent input (it parses untrusted JSON and shell commands).
- The sync server's room isolation and authentication.
- Key storage.

## Supported versions

Crewbo is pre-release (`v0.x`). Only the latest release receives fixes.
