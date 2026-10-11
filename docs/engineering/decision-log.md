# Decision log

One line per decision, newest last. Format: `YYYY-MM-DD | area | decision | why | link`. Costly-to-reverse decisions also get an ADR.

2026-10-11 | process | Seed commit pushed directly to main (LICENSE, README, PRD, research) | Empty repo had no PR base; approved by Ronel | cf7f495
2026-10-11 | process | Repo stays private until ADR 0011 (sync server licence) is decided | Avoid outside contributions under terms that block relicensing; Ronel | ADR 0011
2026-10-11 | schedule | Timeline shifted 1 day earlier: Phase A on Oct 11, Phase 0 Oct 12 to 17, gate Oct 17, week 8 review Dec 5 | Ronel wants to start today | implementation-plan.md
2026-10-11 | docs | Docs live under docs/ (product, research, architecture, adr, engineering) | Matches handoff layout; Ronel moved them | ADR 0010
2026-10-11 | tooling | Dev tools come from Arch official repos: go 1.27.0, buf 1.72.0, golangci-lint 2.13.2, gofumpt 0.10.0, goreleaser 2.14.0, go-task 3.53.1 | Signed by Arch packagers, upstream URLs checked | this PR
2026-10-11 | tooling | Go 1.27.2 pinned via toolchain directive (go 1.27.0) | Latest stable on go.dev/dl at 2026-10-11 | go.mod
2026-10-11 | tooling | Task (Taskfile.yml) instead of Make | Native on Windows | ADR 0010
2026-10-11 | ci | GitHub Actions pinned to commit SHAs; Dependabot updates them | Supply-chain hygiene | .github/workflows
2026-10-11 | ci | gitleaks v8.30.1 (MIT) via go install instead of gitleaks-action | Action needs a licence key for org repos | ci.yml
2026-10-11 | ci | CodeQL runs only when the repo is public | Code scanning on private repos needs a paid plan | codeql.yml
2026-10-11 | ci | Go CI steps skipped until .go files exist | Skeleton must pass with no packages | ci.yml
2026-10-11 | build | CGO_ENABLED=0 for all builds | Simple cross-compilation; forces pure-Go SQLite | ADR 0002, ADR 0008
2026-10-11 | deps | Candidate: modernc.org/sqlite v1.60.1 (BSD-3-Clause) | Pure Go, works with CGO off | ADR 0008, confirm in M1
2026-10-11 | deps | Candidate: github.com/modelcontextprotocol/go-sdk v1.8.0 (Apache-2.0 / MIT) | Official SDK, PRD choice | ADR 0007, confirm in M2
2026-10-11 | deps | Candidate: github.com/zalando/go-keyring v0.2.8 (MIT) | PRD choice; maintained (last push 2026-07-24) | ADR 0006, confirm in M3
2026-10-11 | deps | Candidate: github.com/coder/websocket v1.8.15 (ISC) | Maintained, context-aware, stdlib-style API | ADR 0008, confirm in M3
2026-10-11 | deps | Candidate: mvdan.cc/sh/v3 v3.14.1 (BSD-3-Clause) | POSIX shell parser named in research | components.md, confirm in M2
2026-10-11 | security | Task signatures cover exact serialized bytes, never a re-encoding | Protobuf serialization is not canonical | protocol.md
2026-10-11 | hooks | Use JSON deny (exit 0) for both agents, not exit 2 | Same reason format for both; documented by both | hooks.md
2026-10-11 | hooks | Drop MultiEdit from Claude matchers unless Phase 0 sees it | Not in current hooks or tools reference | hooks.md
2026-10-11 | dispatch | Claude dispatch uses --permission-mode acceptEdits; Codex uses --sandbox workspace-write | Explicit, non-bypass modes verified in local --help | hooks.md
2026-10-11 | protocol | Keepalive ping 20 s, close after 60 s silence; reconnect backoff 1 s to 30 s with jitter | Initial values, tune with Phase 0 and dogfood data | protocol.md
2026-10-11 | data | Invite default expiry 24 h | Hackathon length | data-model.md
2026-10-11 | data | Glob overlap check is conservative prefix comparison in v1 | Never misses a conflict; false positives acceptable | data-model.md
2026-10-11 | product | Long-term IDE recorded as ADR 0012 Proposed; PRD non-goal unchanged | PRD change needs Ronel's decision | ADR 0012
2026-10-11 | ci | Skip CI for docs-only changes; macOS only on PRs, weekly and manual runs | Save Actions minutes while private; Ronel has Windows, a friend has a Mac for manual checks | repo-settings.md
2026-10-11 | process | Repo settings are applied by Ronel with documented gh commands | Claude Code permission rules block settings changes | repo-settings.md
2026-10-11 | community | CoC contact will be an address on crewbo.dev once the domain is set up | Ronel | U-17
2026-10-11 | keys | Wrap every keyring call in a 3 s timeout, then fall back to the 0600 file | go-keyring blocked 60 s+ without a secret service (S7) | phase0-findings.md
2026-10-11 | hooks | Codex adapter accepts absolute and relative paths in apply_patch headers | 0.161.0 sends absolute paths (S2) | phase0-findings.md
2026-10-11 | hooks | Shell checks: mvdan/sh parser plus literal-name heuristic plus post-run hash check | 71% / 85% coverage, 0 false positives (S3) | phase0-findings.md
2026-10-11 | dispatch | Codex: hooks only in v1; App Server turn/steer deferred | Timeboxed; fallback from U-07 | phase0-findings.md
2026-10-11 | dispatch | Use the Claude messaging socket for urgent pushes to dispatched -p sessions started with crossSessionInbound accept | Delivery confirmed on Linux (S4) | phase0-findings.md
2026-10-11 | test | Golden hook fixtures stored per agent/version/os, scrubbed of paths and ids | Captured in Phase 0 | test/fixtures/hooks
