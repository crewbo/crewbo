# M5 two-machine check

Run from 2026-10-18 21:04 to 2026-10-19 17:10 (with a break, finding 1) by Ronel as room lead (Omarchy, Claude Code 2.1.294, codex-cli 0.161.0, git 2.55.0) and gavriel953 as teammate (macOS 27.0.1 arm64, Claude Code 2.1.287, codex-cli 0.163.0, git 2.54.0), against `crewbo/crewbo` at `656fd12` (#172), following `crewbo/spikes` `agent-prompts/m5-two-machine.md`. The sync server ran on Ronel's laptop behind a Cloudflare quick tunnel; the shared repository was the throwaway `crewbo/m5-check` (deleted afterwards). Both machines built crewbo from source. Full reports: `crewbo/spikes` `results/m5-two-machine/ronel.md` (PR #14, with the server's room log and dashboard screenshots) and `macos.md` (PR #15).

## Results

| Check | Result |
| --- | --- |
| C1 checkpoints and conflict warning | Both members started a task changing line 1 of `README.md`. The Mac's checkpoint was automatic; Ronel's was run with `crewbo task checkpoint` because Claude Code had hit its usage limit. Both refs reached GitHub under `refs/crewbo/<login>/<task id>`; the conflict warning followed 2 s after the second checkpoint and both members saw it on `README.md` |
| C2 merge over a conflict (ADR 0015) | Ronel's merge was refused, naming the other task, the path and the `--despite` command; with `--despite` it merged and pushed to `main`. gavriel953's merge was then refused as conflicting with `main` |
| C3 cancel (M5-09) | gavriel953 cancelled his task with a reason; the worktree was kept and the room recorded it. Ronel's `crewbo task list` did not show it (finding 2) |
| C4 lead-only merging (ADR 0014) | In lead-only mode the member's merge was refused naming the lead; the lead merged the member's checkpointed task; mode switched back |
| C5 handoff ladder (M5-03) | Request, decline with a reason (the claim stayed refused to the requester), second request approved (the claim moved), and a lead override, all seen on both machines |
| C6 dashboard (M5-06) | Showed claims, tasks, the conflict and the handoff steps on both machines; without the token the page said it has no valid token |
| C7 new server address (M5-08) | Partly verified. After an unplanned restart, Ronel's `crewbo login --server <new address>` moved his sign-in and room with no new sign-in and reconnected in 3 s; gavriel953's moved too but asked for a GitHub sign-in first (finding 7); C2 to C6 ran across the move. The planned restart failed on Cloudflare's side (finding 3), so the after-move claim check was not run |
| Extension (M5-05) | Installed from the `.vsix` on Omarchy (VS Code 1.140.0): the view, the status bar and the initials badge followed the daemon; a lease expiry cleared them. Skipped on the Mac (no `code` command) |
| Cleanup | Hooks uninstalled and task worktrees and branches removed on both machines; `crewbo room leave` skipped (the server address was gone) |

## Findings and what happened to them

1. The server, tunnel and daemon ran as background tasks of the Claude Code session on Ronel's laptop and stopped when that session hit its usage limit. Nothing was lost. For the hackathon, the server runs outside any agent session (M6-07).
2. A teammate's self-started tasks were invisible in `crewbo task list`. Fixed in #181: the first status carries the start of the task's text, and every member lists teammates' tasks.
3. Three new quick tunnels registered but their hostnames never resolved (Cloudflare and Google DNS, 3 to 8 minutes), with no Cloudflare incident listed; probably a limit on quick tunnels per address. The hackathon uses a named tunnel on crewbo.dev (M6-07).
4. The extension's Tasks group listed every task the daemon knew, by id. Fixed in #178.
5. `crewbo doctor` failed for installed agents the member does not use. Fixed in #179 (a warning when another agent has hooks).
6. A merged task kept showing a conflict after the other task was cancelled. Fixed in #180.
7. A sign-in made by a crewbo older than #136 has no saved server id, so moving it to a new address asks for a new sign-in (gavriel953 signed in with his old binary after a failed build). Expected once everyone runs a release; not changed.
8. gavriel953's first `go build` failed with `x509: certificate signed by unknown authority` while downloading a module, inside Codex's sandbox and once more; a later build succeeded. A release binary avoids it (M6-06).

C7 is to be re-run with the after-move claim check when the named tunnel is set up (M6-07).
