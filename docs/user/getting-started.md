# Getting started with Crewbo

Crewbo lets everyone on your team run their own coding agent (Claude Code, Codex or Antigravity) on the same repository without the agents overwriting each other's work. This guide takes you from nothing to a working room in about 15 minutes. What Crewbo shares is on the [privacy page](privacy.md); if something goes wrong, see [troubleshooting](troubleshooting.md).

## 1. What you need

- git 2.38 or later, and a clone of your team's repository
- At least one agent: Claude Code, Codex or Antigravity, signed in
- A GitHub account
- The address of your team's sync server (your lead has it; to run one yourself, see [Running the sync server](#running-the-sync-server))
- Until the first release, Go (the version in Crewbo's `go.mod`) and [Task](https://taskfile.dev) to build Crewbo

## 2. Install Crewbo

```sh
git clone https://github.com/crewbo/crewbo
cd crewbo
task build            # creates bin/crewbo (bin\crewbo.exe on Windows)
```

Put `bin/crewbo` in a folder **without spaces** in its path, and add that folder to your `PATH`. Codex and Antigravity run hooks as shell commands, and a space in the path breaks them without any error.

## 3. Start the daemon

The daemon is the part of Crewbo that runs on your computer: your agents' hooks ask it before every edit. Start it in its own terminal and leave it running:

```sh
crewbo daemon
```

## 4. Sign in

```sh
crewbo login --server https://your-team-server
```

Crewbo shows a code: open https://github.com/login/device, enter it and approve. Crewbo asks GitHub for no permissions beyond your public profile.

## 5. Join your team's room

A room is one repository shared by one team. Run these inside your clone of the repository.

- **The lead, once:** `crewbo room create`, then `crewbo room invite` prints a join command; send it to your teammates privately.
- **Everyone else:** `crewbo room join CODE`.

`crewbo room status` shows the room, its lead and who may merge.

## 6. Connect your agents

```sh
crewbo hooks install
```

This adds Crewbo's hooks to every agent it finds (Claude Code, Codex, Antigravity), keeps your own hooks and settings, and writes a backup next to each file it changes. For **Codex**, open Codex, type `/hooks` and trust the crewbo hooks; Codex skips hooks you have not trusted.

Optionally, let your agent look up claims while it plans (Antigravity gets this from `crewbo hooks install`):

```sh
claude mcp add --scope user --transport stdio crewbo /absolute/path/to/crewbo mcp
codex mcp add crewbo -- /absolute/path/to/crewbo mcp
```

## 7. Check your setup

```sh
crewbo doctor
```

It checks the daemon, your sign-in, the server, the room, your git name and email, the hooks, and your shell, and prints a fix under every problem. A `WARN` for an agent's hooks means either that agent has not run since the daemon started (start it, send a prompt and run `crewbo doctor` again) or that it is installed without Crewbo hooks while another agent has them (fine if you do not use it in this repository).

## Working with Crewbo

### Claims: who is working on what

Your agent is stopped before it edits a file a teammate has claimed, with a message naming the file and the person. To claim files yourself:

```sh
crewbo claim add src/auth/**            # 30 minutes by default; --for 2h for longer (at most 8h)
crewbo claim list
crewbo claim release ID
```

### Asking for a file

When you need a file someone holds:

```sh
crewbo claim request src/auth/login.go --reason "fixing the login bug"
crewbo claim requests                    # what is open, answered or overridden
```

The holder answers with `crewbo claim handoff ID approve` (the claim is released and you can take it) or `decline --reason "..."`. The room lead can release any claim with `crewbo claim override PATH --reason "..."`.

### Tasks: a branch for each piece of work

```sh
crewbo task start --path src/auth/** "Build the login page"
```

This gives you a new branch and worktree with those paths claimed for you. Work there with any agent. Crewbo checkpoints your work when your agent finishes a reply (at most every 5 minutes; `crewbo task checkpoint ID` does it now), pushes the checkpoint to your remote, and warns the room when two tasks change the same lines. `crewbo task start --branch NAME` makes an existing branch a task instead.

When the task is done:

```sh
crewbo task merge ID
```

This merges it into the remote's main branch, but only if it does not conflict with main or with another task's checkpoint. If it conflicts with a teammate's task, talk to them first; then `crewbo task merge ID --despite OTHER-ID` merges anyway and they update their task. `crewbo task cancel ID` stops a task and keeps its work.

### Tasks your lead sends you

The lead can send a task for your agent to run: `crewbo task create --to LOGIN --path PATH "TEXT"`, or in VS Code with the send button at the top of the Crewbo view (**Send a task**: pick the teammate, the files, and what to do). You see it with `crewbo task list` and `crewbo task show ID`. Nothing runs until you approve it **yourself, in a terminal**:

```sh
crewbo task approve ID                   # Claude Code; --agent codex for Codex
crewbo task decline ID --reason "..."
```

Approving shows the whole task again and waits for you to type `yes`; then your agent runs it in a new worktree with your sign-in.

### Seeing it all

- **VS Code:** install `crewbo-<version>.vsix` (Extensions view, `...` menu, "Install from VSIX..."). The Crewbo view in the Explorer lists claims, tasks and handoff requests; claimed files show the holder's initials. See [`extension/README.md`](../../extension/README.md).
- **Dashboard:** `crewbo dashboard` prints an address for a read-only page on this computer.
- **CLI:** `crewbo crew` (who is online and what their agents are doing), `crewbo claim list`, `crewbo task list`, `crewbo claim requests`, `crewbo status`.

## Running the sync server

One person runs the server; everyone else only needs its address.

```sh
crewbo serve
```

It listens on `127.0.0.1:7420`, so only this computer can reach it, and keeps its data in `server.db` in Crewbo's data folder. To let teammates reach it:

- a tunnel (Cloudflare Tunnel, Tailscale) pointed at `http://127.0.0.1:7420`, which gives everyone an HTTPS address; or
- HTTPS directly: `crewbo serve --addr 0.0.0.0:7420 --tls-cert FILE --tls-key FILE`; or
- on a network you fully trust: `crewbo serve --addr 0.0.0.0:7420 --insecure-lan` (plain HTTP: anyone on that network can read session tokens).
 If the server's address changes (a restarted tunnel), everyone runs `crewbo login --server NEW-ADDRESS`; their rooms move along without signing in again.

## When you are done

```sh
crewbo report             # counts to paste into the feedback survey; sends nothing
crewbo room leave
crewbo hooks uninstall    # removes only Crewbo's entries
```
