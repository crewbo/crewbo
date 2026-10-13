# M4 two-machine check

Run on 2026-10-13 by Ronel as room lead (Omarchy, Claude Code 2.1.294, codex-cli 0.161.0) and gavriel953 as teammate (macOS 27.0.1 arm64, Claude Code 2.1.287, codex-cli 0.162.1), against `crewbo/crewbo` at `e72aea8` (#111) plus the login fix later merged as #112, following `crewbo/spikes` `agent-prompts/m4-two-machine.md`. The sync server ran on Ronel's laptop behind a Cloudflare quick tunnel; the shared repository was `crewbo/spikes` at `073d211` on both machines. Full reports: `crewbo/spikes` `results/m4-two-machine/ronel.md` (PR #10, with the server's room log) and `macos.md` (PR #11).

## Results

| Check | Result |
| --- | --- |
| C1 nothing runs before approval | The task showed as `dispatched` from RM1338, `show` printed the text exactly, and no worktree existed |
| C2 Claude Code run | Approved at a terminal by typing `yes`; claim on `m4-check/claude.txt` granted 0.17 s later; Claude Code 2.1.287 wrote the file in the worktree on `crewbo/<task id>` at the base commit in about 7 s; claim released; `finished` on both machines |
| C3 Codex run | `--agent codex`; the claim was visible on the lead's machine while Codex ran (about 26 s, default model); `finished` on both machines |
| C4 decline | The lead saw `declined` with `Reason: not in this check` |
| C5 expiry | A one-minute task expired; the Mac reported EXPIRED 3 s after the deadline (5 s sweep); approving it afterwards was refused (`already expired`) |
| C6 offline delivery | Sent while the Mac's daemon was stopped; received 1.5 s after it restarted |
| C7 an agent cannot approve | A piped `yes` was refused. Codex's own run of `crewbo task approve` was not refused at once: it reached the prompt and printed `Not approved.` (finding 1) |
| C8 control characters | `ESC[2K` in the task text showed as `\x1b[2K` on both machines; the line was not erased |
| Cleanup | Hooks uninstalled, task worktrees and branches removed, room left (Mac) |

## Findings and what happened to them

1. The approve check counted `/dev/null` (a character device, where agents point stdin) as a terminal, so an agent reached the prompt. Nothing could type `yes`, so nothing was approved. Fixed in #114 (`term.IsTerminal`). An agent whose shell runs in a pseudo-terminal still reaches the prompt; that limit is in the decision log.
2. `crewbo login` gave up when one poll of GitHub timed out on a slow network: fixed in #112.
3. The Cloudflare quick tunnel could not run on a university network that blocks port 7844 over both QUIC and TCP (`--protocol http2` also failed); a phone hotspot worked. For checks, use a network that allows outbound port 7844, or Tailscale.
4. A restarted quick tunnel has a new address, and `session.json` and `rooms.json` keep the old one, so both machines changed it by hand. Open: Crewbo needs a way to move a session and its rooms to a new server address.
5. The lead's `crewbo task list` shows a sent task only a few seconds after sending, once its room event has come back and been verified. Minor; not changed.
6. A RUNNING status was recorded twice after a reconnect (at-least-once delivery); the lead's view ignored the duplicate. One claim reached the server 7.4 s after approval, cause unknown.

U-31 is resolved for macOS: Claude Code 2.1.287 ran a dispatched task with `--session-id` and the prompt on stdin. Windows is still unverified.
