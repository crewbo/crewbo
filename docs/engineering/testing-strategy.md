# Testing strategy

| Layer | Scope | Tooling | Where it runs |
| --- | --- | --- | --- |
| Unit | Pure logic: `claims`, `shellparse`, `hooks/*`, `dispatch` signing, `protocol` mapping | `go test`, table-driven, `-race` | CI on Linux, macOS, Windows |
| Fuzz | `apply_patch` parser, shell parser, hook JSON decoding, path normalisation | Go native fuzzing; short runs in CI, longer locally | CI (10 s per target), local |
| Integration | Real git in temp dirs (`gitx`), SQLite in temp dirs (`store`), IPC on each OS (`platform`), daemon process lifecycle | `go test` with `t.TempDir()`; skip with a clear message if git is below 2.38 | CI on 3 OSes |
| Contract | Hook adapters against golden payloads captured from real agents in Phase 0 | `test/fixtures/hooks/<agent>/<version>/<event>.json` plus expected outputs | CI on 3 OSes |
| End-to-end | Two simulated daemons plus one server in-process or as subprocesses; flows 1 to 5 without real agents (fake agent emits fixture payloads) | `go test -tags e2e ./test/e2e/...` | CI on Linux; nightly on macOS and Windows |
| Simulation | Seeded deterministic simulator for claims, leases, disconnects, clock skew; checks invariants over thousands of steps | `test/sim`, seed printed on failure for replay | CI with fixed seeds; local random seeds |
| Manual | Real Claude Code and Codex sessions, Codex trust prompt, Windows and macOS installers, sleep and wake, the extension UI | Checklists in `docs/runbooks/manual-test.md` (written in M2) | Before each release |

## Coverage

`internal/claims`, `internal/shellparse`, `internal/hooks/...`, `internal/dispatch` must stay at or above 85% line coverage. CI reports coverage per package; a drop below the threshold fails the job once those packages exist.

## Golden fixtures

- Captured, never hand-written. Each fixture records the agent version and OS in its path and a sidecar `.meta.json` (command, date, capture method).
- When an agent release changes a payload, add a new version directory; keep old ones while that version is supported.
- Fixtures are scrubbed of usernames, home paths and session ids before commit (replace with fixed placeholders).

## Determinism rules

- No `time.Now()` inside `claims`; time is passed in.
- No real network in unit tests. e2e uses loopback only.
- Tests that need a real agent CLI are tagged `manual` and never run in CI.

## Cross-OS matrix

CI runs `ubuntu-latest`, `macos-latest`, `windows-latest`. Arch-specific behaviour (Omarchy secret service, Hyprland) is covered by manual checks on the founder's machine.
