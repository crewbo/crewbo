# Troubleshooting

Start with `crewbo doctor`: it checks the usual causes and prints a fix for each. The problems below are the ones teams met while testing Crewbo.

## My agent is not being stopped

- **"the Crewbo daemon is not running (start it with `crewbo daemon`)"** in the agent: the daemon must run while you work. Start `crewbo daemon` and keep its terminal open. Without it, hooks let everything through so your work is never blocked by a Crewbo fault.
- **Codex ignores the hooks:** Codex runs hooks only after you trust them. Open Codex, type `/hooks` and trust the crewbo hooks. Trust is tied to the exact hook command, so moving the `crewbo` binary and reinstalling the hooks asks again.
- **Codex or Antigravity hooks never run, with no error:** the path to the `crewbo` binary contains a space. Move it to a folder without spaces and run `crewbo hooks install` again.
- **Codex hook replies look broken on macOS or Linux:** your shell prints something when it starts (for zsh, from `~/.zshenv`), and that text lands in front of the hook's reply. `crewbo doctor` shows what it prints; remove it or send it to stderr.
- **Another tool's message instead of Crewbo's:** if another hook can also refuse edits (a plugin's edit gate, for example), the agent may show its message when both refuse the same edit. The edit is still blocked.

## I cannot reach the sync server

- **The address changed:** a Cloudflare quick tunnel gets a new address every time it restarts. Everyone runs `crewbo login --server NEW-ADDRESS`; when it is the same server, your sign-in and rooms move along without signing in again.
- **The tunnel does not start or connect:** some networks (university Wi-Fi, for example) block the port Cloudflare's tunnel uses (7844). Use another network, such as a phone hotspot, or Tailscale.
- **"local only: not signed in (crewbo login)"** in `crewbo status`: sign in with `crewbo login --server ADDRESS`.
- **Signing in stops on a slow network:** Crewbo retries while the GitHub code is valid. If the code expires, run `crewbo login` again.

## Tasks

- **"approve a task from an interactive terminal, where you can read it first":** approving must be typed by you, in a terminal, so an agent can never approve a task. Open a terminal window and run `crewbo task approve ID` there.
- **"... does not have; run git fetch":** the task starts from a commit your clone does not have yet. Run `git fetch` (or `git pull`) in the repository and approve again.
- **"not signed in" when approving:** sign in to the agent first (`claude` or `codex login`).
- **Billing:** a dispatched Claude Code task uses your Claude plan, not an API key: Crewbo removes `ANTHROPIC_API_KEY` from the agent's environment (for Codex, `OPENAI_API_KEY` and `CODEX_API_KEY`). Pass `--use-api-key` to keep them.
- **Checkpoints fail with a git identity error:** checkpoints and merges commit as you. Set `git config --global user.name "Your Name"` and `git config --global user.email you@example.com`.
- **Checkpoints go to `wip/<login>/<task id>` branches:** your remote refused the `refs/crewbo/...` ref, so Crewbo used a branch instead. Nothing is lost.

## Merging

- **"... conflicts with main on FILE":** update your task first: merge main into the task's branch in its worktree, resolve the conflict, then `crewbo task checkpoint ID` and merge again.
- **Refused because of a teammate's task:** your task and theirs change the same lines. Talk to them; then `crewbo task merge ID --despite THEIR-ID` merges yours, and they update theirs.
- **Refused because only the lead merges:** the room is in lead-only mode (`crewbo room status` shows it). Ask your lead to merge, or to switch back with `crewbo room set --merge members`.

## Other

- **"a daemon is already running for this user":** one daemon runs per account. Use the one that is running, or stop it first.
- **On Linux, Crewbo is slow to sign in or start:** your keychain service did not answer. Crewbo gives up waiting after 3 seconds and keeps the device key in a file only you can read.
- **A lost or stolen computer:** sign in on another computer and revoke the old one's key: see [the device key runbook](../runbooks/device-key-rotation.md).
- **Windows:** dispatched tasks run with Claude Code only for now; Codex dispatch is not supported on Windows yet.
