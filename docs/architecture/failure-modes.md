# Failure modes

Expands the PRD table. "User sees" is the exact intent of the message; final wording is set in implementation.

| # | Situation | Detection | Behaviour | User sees |
| --- | --- | --- | --- | --- |
| F1 | Daemon crashes or laptop sleeps holding claims | Server: no renewal before `lease_expires_at` | Lease expires after 5 min; claim released; event logged | Others: "Claim on src/auth.ts expired (holder offline)". Holder on wake: "Your claims expired while asleep; re-claimed 3, 1 taken by priya" |
| F2 | Sync server down | WebSocket close or failed reconnect | Daemon enforces last known claims, queues requests locally, reconnects with backoff | Status line: "Offline since 14:02; enforcing last known claims; 4 changes queued" |
| F3 | Network split | Each side's daemons lose the server or each other | Each side keeps its claims; queued requests re-validated at resync; conflicts surface at resync and at the merge-tree check | On resync: "While offline, src/api.go was also claimed by arjun; your claim request was refused" |
| F4 | Stale claim on a needed file | Deny in `pre_edit` | Escalation ladder: handoff request, owner decision, lead override with reason | Agent gets the deny reason with options; owner gets a handoff prompt |
| F5 | Two people acting as lead | Server enforces one `lead_member_id` per room | Non-lead dispatch refused; lead transfer is an explicit event | "Only the room lead (ronel) can dispatch tasks" |
| F6 | Force-pushed task branch | Merge queue sees a checkpoint commit that is not a descendant of the last known one | Re-run merge-tree against the new head before merging | Lead: "Task 12 branch was rewritten; conflict check re-run" |
| F7 | Hook fails or times out | Hook catches its own error or deadline | Allow the edit; log locally | Agent context: "Crewbo could not check claims for this edit (daemon not responding). Edit allowed." |
| F8 | Daemon unreachable from hook | IPC dial fails | Same as F7 | Same as F7 |
| F9 | Codex hook untrusted, misconfigured or not firing | No hook event of any kind within 10 s of a Codex launch the daemon started (not only `session_start`, which did not fire under `codex exec` on Windows, U-21); `crewbo doctor` inspects config and the install path | Daemon cannot enforce for that session | "Codex is not running Crewbo hooks. Open /hooks in Codex and trust the crewbo hook." |
| F10 | Signature invalid or key revoked | Daemon verification | Task rejected, never shown as runnable; rejection reported to room | "A task claiming to be from ronel failed verification and was discarded" |
| F11 | Worktree creation fails | `git worktree add` non-zero exit | Release claims for the task, mark task Failed | git's error text plus a hint (long paths on Windows, dirty path) |
| F12 | Agent CLI missing, logged out, or wrong version | Preflight: `claude --version`, `claude auth status`; `codex --version` | Refuse to launch; task stays Approved | "Claude Code is not logged in. Run claude and log in, then retry." |
| F13 | `ANTHROPIC_API_KEY` present | Env check before launch | Strip from child env unless the member opted in | Once: "ANTHROPIC_API_KEY removed for dispatched runs to keep subscription billing. Opt in with crewbo config set dispatch.allow_api_key true" |
| F14 | Custom ref push rejected | `git push` non-zero exit | Fall back to `wip/<member>/<task>` branch (U-06) | Logged; no prompt |
| F15 | GitHub rate limiting on pushes | Push error mentioning rate limit | Back off; checkpoints are only at agent stop, so retries are rare | "Checkpoint push delayed by GitHub rate limiting" |
| F16 | Keychain unavailable (no secret service) | `go-keyring` error at first key save | Fail fast, write 0600 key file, never retry keychain silently | Once: "No system keychain found; key stored in ~/.local/share/crewbo/device.key (0600)" |
| F17 | Clock skew between machines | Server time in every Event vs local time | Expiry decided by server time only; warn if skew over 30 s | "Your clock is 2 min off; lease times may look wrong" |
| F18 | git older than 2.38 | `git --version` at startup | Daemon refuses to start | "Crewbo needs git 2.38 or later (found 2.34)" |
| F19 | Daemon local queue corrupt | SQLite open or integrity check fails | Move the file aside, start empty, resync from server | "Local queue was damaged and reset; 2 unsent changes lost" |
| F20 | Merge-tree reports conflict at merge time | Exit status 1 in the serial merge queue | Merge refused; task back to Checkpointed | Lead: list of conflicting paths |
