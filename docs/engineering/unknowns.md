# Unknowns register

Rule: 90-minute timebox per unknown. Unresolved by then, take the fallback and log it in the decision log.

| ID | Unknown | Owner | Resolution path | Fallback | Status |
| --- | --- | --- | --- | --- | --- |
| U-01 | Message format and auth on Claude's messaging socket; whether an external daemon's messages are held | Claude (agent) | Phase 0 spike S4 on Linux, then Windows | Hook-injected context only; socket as wake-up | Resolved on Linux for `-p` with accept (phase0-findings S4); interactive and Windows pending |
| U-02 | Codex re-asks for trust when only the hook binary updates (does the hash cover the binary?) | Claude | Phase 0 spike S2: trust, replace binary, relaunch | Installer tells the user to re-trust after upgrades | Open |
| U-03 | How Codex spawns hooks per OS (`$SHELL -lc`, `cmd.exe /C`); quoting and profile noise | Claude (Linux), Ronel (Windows) | Phase 0 S2 with zsh, fish, bash; Windows path with spaces | Install to a space-free path; hook prints only JSON | Linux: `bash -c`, no login shell. $SHELL vs passwd and Windows pending |
| U-04 | Minimum Claude Code version for exec-form hooks (`args`) | Claude | Release notes search; test on 2.1.294 | Require the installed version found working; refuse install below it | Resolved: exec form since 2.1.139 (CHANGELOG) |
| U-05 | Share of shell edits caught by parser plus hash check | Claude | Phase 0 S3 test corpus | Alert-and-record only; documented limitation | Resolved: parser 71%, plus heuristic 85%, 0 false positives (S3) |
| U-06 | GitHub accepts pushes to `refs/crewbo/...`; real throttling | Claude | Phase 0 S5 against `crewbo/spikes` | `wip/<member>/<task>` branches | Resolved: custom refs accepted, no throttling in 12 pushes (S5) |
| U-07 | Codex App Server `turn/steer` on daemon-owned threads; `codex exec` exit code on failure | Claude | Phase 0 S2b | Hooks only for Codex; parse JSONL for failure | Exit code resolved (1 on turn.failed). turn/steer not tested; fallback hooks-only taken |
| U-08 | Whether Omarchy runs a secret service by default | Claude | Phase 0 S7 on this machine | 0600 key file | Resolved: gnome-keyring present by default here. New risk U-18 |
| U-09 | Lease behaviour when a laptop sleeps mid-claim | Claude + Ronel | Phase 0 S6 with suspend | Expire and warn on wake | Open |
| U-10 | Whether teams actually mix agent vendors | Ronel | Interview 10 hackathon teams | Narrow positioning to cross-machine, single-vendor teams | Open |
| U-11 | Installed Claude Code (2.1.294) predates features the research cites (`onFailure` 2.1.295, observed 2.1.296 binary) | Claude | Check release notes; decide minimum supported version | Avoid post-2.1.294 features in v1 | Open, new 2026-10-11 |
| U-12 | `NotebookEdit` input field name for the target path | Claude | Capture in Phase 0 | Parse both `notebook_path` and `file_path` | Resolved: `notebook_path`, absolute (S1) |
| U-13 | Codex `UserPromptSubmit` and `Stop` input field names | Claude | Capture in Phase 0 | Treat unknown fields as optional | Resolved: `prompt`; Stop has `stop_hook_active`, `last_assistant_message` (S2) |
| U-14 | `PreToolUse` deny honoured under `bypassPermissions` (Claude) and `danger-full-access` (Codex) | Claude | Phase 0 S1, S2 | Document that bypass modes may skip enforcement | Claude resolved on Linux: deny holds in bypassPermissions. Codex reports bypassPermissions in exec and deny held |
| U-15 | Hook round-trip latency budget (target 300 ms) on each OS | Claude + Ronel | Measure in Phase 0 S1 | Raise deadline; never above 1 s | Spawn baseline p50 2.3 ms, p95 3.3 ms on Linux; IPC and other OSes pending |
| U-16 | Windows named pipe library for Go (stdlib has none) | Claude | Evaluate in M1 (licence, maintenance) | Loopback TCP with a token file (0600 or user ACL) | Open, new |
| U-17 | Code of conduct enforcement contact | Ronel | Address on crewbo.dev once the domain mail is set up | Keep placeholder while the repo is private | Open, planned |
| U-18 | go-keyring blocks 60 s+ when D-Bus activation of the secret service fails | Claude | Found in S7 | Hard 3 s timeout around every keyring call, then 0600 file | Mitigation planned (M3-04) |
| U-19 | Codex hook shell: follows $SHELL or the passwd login shell? | Claude + friend | macOS run (zsh default) | Hook prints only JSON; install path without spaces | Open |
