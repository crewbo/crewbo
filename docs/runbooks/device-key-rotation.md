# Runbook: a lost, stolen or replaced computer

Each computer you sign in on with `crewbo login` gets its own device key, an Ed25519 key kept in the system keychain (or a `0600` file when there is none). As a room lead you sign tasks with it, and teammates' daemons check every task against your registered keys before they show it (ADR 0006). If a computer is lost, stolen or replaced, retire its key.

What a stolen key can do until you revoke it: send tasks that look like yours. Teammates still see the full text and must type `yes` in a terminal before anything runs. The thief also has that computer's sign-in session, which can make claims and read the room's metadata (paths, task text, branch names; never file contents).

## Steps

Do these on another computer you are signed in on. Crewbo refuses to revoke the key of the computer you run it on.

1. **Sign in on the computer you are keeping** (skip if you already are):

   ```sh
   crewbo login --server https://your-sync-server
   ```

   This registers that computer's own key.

2. **Find the old key.** The `STATE` column marks the computer you are on:

   ```sh
   crewbo device list
   ```

   ```text
   KEY                                       NAME        OS     ADDED       STATE
   ed25519:0ld0ld0ld0ld0ld0ld0ld0ld0ld0ld00  old-laptop  linux  2026-10-18  active
   ed25519:5ee5ee5ee5ee5ee5ee5ee5ee5ee5ee5   new-laptop  linux  2026-10-18  active, this computer
   ```

3. **Revoke it.** Any unique start of the key id works:

   ```sh
   crewbo device revoke ed25519:0ld
   ```

   ```text
   Revoked key ed25519:0ld0ld0ld0ld0ld0ld0ld0ld0ld0ld00 (old-laptop, linux). Teammates' daemons now reject tasks signed with it.
   You are signed out everywhere, this computer included: run crewbo login here and on each computer you still use.
   ```

   Revoking is permanent: a revoked key is never accepted again. The server also ends every sign-in session of your account and closes their live connections. A session is not tied to a key, so this is what cuts off the lost computer's session.

4. **Sign in again** on this computer and on every other computer you still use:

   ```sh
   crewbo login --server https://your-sync-server
   ```

   Until you do, `crewbo status` on those computers shows `sync: local only: the server refused this daemon: sign in again with crewbo login`. The daemon reconnects by itself once `crewbo login` saves the new session.

5. **Tell your teammates** if you are a room lead, so they decline any task they do not expect from you (see below).

## What teammates see

- **New tasks signed with the revoked key** are refused by the sync server, and a daemon that still receives one from the room log rejects it: `crewbo task list` shows it as rejected, with the reason `dispatch: signed by a revoked key`. Nothing in it is shown.
- **Tasks that were already waiting** for approval before you revoked the key stay in their list. Ask them to decline any they do not expect:

  ```sh
  crewbo task decline TASK-ID --reason "not sent by the lead"
  ```

- **Tasks already approved or running** are not stopped by revoking. If one was not yours, the assignee or you (as the lead who sent it) can stop it with `crewbo task cancel TASK-ID`.
- **Claims** made from the lost computer stay until their lease ends (5 minutes for an agent's claims, at most 8 hours for a terminal claim), because the computer can no longer renew them. Release one sooner with `crewbo claim override PATH --reason TEXT` (room lead).

## Replacing a computer you still have

Sign in on the new computer, then revoke the old one's key from the new one as above. To rotate the key of a computer you keep using, you must remove its key from the keychain by hand. Crewbo does not do this yet (decision log, 2026-10-18).

## Checked

Steps 2 to 4 were run on Omarchy (Arch Linux) against a local `crewbo serve` on 2026-10-18 (M5-10), with two computers simulated by separate config directories: the list, the revoke, and both sessions ending. Server and CLI tests cover the rest: a revoked key refused at connect and at dispatch, live connections closed, other members untouched. Signing in again (step 4) uses the normal GitHub sign-in, which was not repeated here. Untested on Windows and macOS.
