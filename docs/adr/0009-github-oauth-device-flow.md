# 0009. GitHub OAuth device flow for authentication

- Status: Accepted (PRD 2026-10-10)
- Date: 2026-10-11
- Deciders: Ronel Abraham Mathew

## Context

Users are developers who already have GitHub accounts and work in a terminal. Crewbo must not store passwords.

## Decision

- `crewbo login` uses the GitHub OAuth device flow: show a user code, the user approves at github.com/login/device, the CLI polls for a token.
- The server exchanges the GitHub identity for its own short-lived session token. The GitHub token is used only to identify the user and is not stored by the server.
- Scopes: none beyond reading the public profile.

## Alternatives considered

- Passwords: storage liability.
- GitHub App: more setup for self-hosters; reconsider in v2.

## Consequences

- Self-hosters must register their own OAuth app and set its client id.
- Device flow must be enabled on the OAuth app.

## Links

- [GitHub device flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow)
