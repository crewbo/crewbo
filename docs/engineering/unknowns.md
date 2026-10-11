# Unknowns register

Rule: 90-minute timebox per unknown. Unresolved by then, take the fallback and log it in the decision log.

| ID | Unknown | Owner | Resolution path | Fallback | Status |
| --- | --- | --- | --- | --- | --- |
| U-01 | Message format and auth on Claude's messaging socket; whether an external daemon's messages are held | Claude (agent) | Phase 0 spike S4 on Linux, then Windows | Hook-injected context only; socket as wake-up | Open |
| U-02 | Codex re-asks for trust when only the hook binary updates (does the hash cover the binary?) | Claude | Phase 0 spike S2: trust, replace binary, relaunch | Installer tells the user to re-trust after upgrades | Open |
| U-03 | How Codex spawns hooks per OS (`$SHELL -lc`, `cmd.exe /C`); quoting and profile noise | Claude (Linux), Ronel (Windows) | Phase 0 S2 with zsh, fish, bash; Windows path with spaces | Install to a space-free path; hook prints only JSON | Open |
| U-04 | Minimum Claude Code version for exec-form hooks (`args`) | Claude | Release notes search; test on 2.1.294 | Require the installed version found working; refuse install below it | Open |
| U-05 | Share of shell edits caught by parser plus hash check | Claude | Phase 0 S3 test corpus | Alert-and-record only; documented limitation | Open |
| U-06 | GitHub accepts pushes to `refs/crewbo/...`; real throttling | Claude | Phase 0 S5 against `crewbo/spikes` | `wip/<member>/<task>` branches | Open |
| U-07 | Codex App Server `turn/steer` on daemon-owned threads; `codex exec` exit code on failure | Claude | Phase 0 S2b | Hooks only for Codex; parse JSONL for failure | Open |
| U-08 | Whether Omarchy runs a secret service by default | Claude | Phase 0 S7 on this machine | 0600 key file | Open |
| U-09 | Lease behaviour when a laptop sleeps mid-claim | Claude + Ronel | Phase 0 S6 with suspend | Expire and warn on wake | Open |
| U-10 | Whether teams actually mix agent vendors | Ronel | Interview 10 hackathon teams | Narrow positioning to cross-machine, single-vendor teams | Open |
| U-11 | Installed Claude Code (2.1.294) predates features the research cites (`onFailure` 2.1.295, observed 2.1.296 binary) | Claude | Check release notes; decide minimum supported version | Avoid post-2.1.294 features in v1 | Open, new 2026-10-11 |
| U-12 | `NotebookEdit` input field name for the target path | Claude | Capture in Phase 0 | Parse both `notebook_path` and `file_path` | Open, new |
| U-13 | Codex `UserPromptSubmit` and `Stop` input field names | Claude | Capture in Phase 0 | Treat unknown fields as optional | Open, new |
| U-14 | `PreToolUse` deny honoured under `bypassPermissions` (Claude) and `danger-full-access` (Codex) | Claude | Phase 0 S1, S2 | Document that bypass modes may skip enforcement | Open, new |
| U-15 | Hook round-trip latency budget (target 300 ms) on each OS | Claude + Ronel | Measure in Phase 0 S1 | Raise deadline; never above 1 s | Open, new |
| U-16 | Windows named pipe library for Go (stdlib has none) | Claude | Evaluate in M1 (licence, maintenance) | Loopback TCP with a token file (0600 or user ACL) | Open, new |
| U-17 | Code of conduct enforcement contact | Ronel | Pick an address (for example on crewbo.dev) | Keep placeholder while the repo is private | Open, new |
