# What Crewbo shares and stores

Crewbo never sends the contents of your files anywhere, and it sends no telemetry. Code moves only through your own git remote. This page lists everything that does leave your computer, and where it is kept.

## What goes to the sync server

The sync server is run by someone on your team (or by you, with `crewbo serve`). Everything below is visible to every member of your room, and to whoever runs the server.

| When | What is sent |
| --- | --- |
| You sign in (`crewbo login`) | Your GitHub user id and login (Crewbo asks GitHub for no permissions beyond your public profile), your device's public key, your computer's hostname and its OS (`linux`, `darwin` or `windows`) |
| You or your agent claim a file | The path or glob, how long the claim lasts, the task it belongs to, your login and an id for the agent session |
| You send or start a task | The task text, the paths it may change, the commit it starts from, who sent it and who it is for, signed with your device key |
| A task changes state | The state (for example running, checkpointed, merged), the checkpoint's branch or ref name and commit id, and for a failed run its error message, which can include a local folder path |
| Two tasks conflict | The two task ids and commits, and the paths that conflict |
| You ask for a file, answer, or override | The claim, and the reason you typed |
| You are connected | That you are online in the room, while your daemon is connected (not stored) |

## What the sync server stores

Like any server, it sees your IP address while you are connected; Crewbo does not store it.

The server keeps a log of every event above for each room, in one SQLite file (`server.db`). It is not deleted when you leave a room. It also keeps sign-in sessions (only a hash of each session token, valid for 7 days), registered device public keys, room members and invite codes (only a hash of each). Whoever runs the server can read all of it.

## What stays on your computer

- The daemon's database: the room events it has received, your tasks, and the agents it has seen (`crewbo report` reads this).
- Your session, your rooms and your device's private key. The private key is kept in your system keychain, or in a file only you can read when there is no keychain; it never leaves your computer.
- Task worktrees and logs of task runs.
- The local dashboard runs on `127.0.0.1` only and needs the token in the address `crewbo dashboard` prints.

## Your git remote

Checkpoints of your tasks are pushed to your repository's remote (as `refs/crewbo/<login>/<task id>`, or a `wip/` branch if the remote refuses that), and merges go to its main branch, using your own git credentials and identity.

## `crewbo report`

`crewbo report` prints counts (agents used, tasks, handoffs, conflicts) for you to copy into a survey. It holds no paths, task text or names, and it sends nothing.
