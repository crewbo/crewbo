# Crewbo for VS Code (v0)

Shows the state of your Crewbo room inside VS Code, read from your local daemon:

- **Crewbo view** in the Explorer: claims in your open repositories (with the task they belong to, when you know it), your tasks and the tasks you sent (with conflict warnings), and the room's handoff requests.
- **Explorer**: a claimed file shows the holder's first two letters; hover for who holds it.
- **Status bar**: `crewbo  2 claimed · 1 handoff · 0 conflicts`. Conflicts are always shown, even at 0.
- **Actions**: right-click a claim or a file to ask for it (`crewbo claim request`); answer a request with **Hand off** or **Not yet**; the room lead can release a claim; review, approve or decline a task sent to you. Approving opens a terminal running `crewbo task approve`, which shows the whole task again and waits for you to type `yes`. That is the only way to approve (ADR 0006).

It talks to the daemon through `crewbo api`, so `crewbo` must be on your `PATH`, or set `crewbo.path` in your **user** settings (a workspace cannot set it). Start the daemon with `crewbo daemon`.

## Install

Download `crewbo-<version>.vsix` from the release (or build it with `task ext:package`), then in VS Code open the Extensions view, click the `...` menu, choose "Install from VSIX..." and pick the file. From a terminal: `code --install-extension crewbo-<version>.vsix`.

Not in v0: the Bo crew panel, claim tags and line highlights in the editor (they need presence and agent activity from other machines, which Crewbo does not share yet).

## Develop

```sh
task ext:test             # npm ci, compile, unit tests (client and model)
task ext:run -- <folder>  # open VS Code with the extension loaded from source
task ext:package          # build crewbo-<version>.vsix
```

## Manual test plan

Run on each OS before a release, with the daemon running and the folder in a room with a second member (the M5 two-machine check covers this).

1. `crewbo claim add src/a.go` on the other machine. Within a few seconds `src/a.go` shows the holder's letters in the Explorer, the Crewbo view lists it under Claims, and the status bar counts it.
2. Right-click the claim, **Ask for this file**, give a reason. The other machine's view lists the request, and its member gets a notification `<you> wants src/a.go next` with **Hand off** and **Not yet**.
3. **Not yet** with a reason: your view shows `declined by <them>` with the reason in the tooltip. Ask again, then **Hand off**: the claim disappears and you can claim the file.
4. As the room lead, right-click another member's claim, **Release this claim (room lead)**. A reason is required; the claim goes and the step is listed as overridden.
5. Send a task to this member (`crewbo task create`). A notification offers **Review**; the full text opens read-only, then the agent choice; the terminal that opens runs `crewbo task approve` and waits for `yes`. **Decline** from the view asks for an optional reason.
6. Make two task checkpoints conflict. Both members see a warning naming the tasks and files, the task lists the conflict, and the status bar counts it.
7. Stop the daemon. The status bar says `crewbo  daemon not running` and the view says how to start it. Start it again; the view recovers within a few seconds.
8. Repeat at 200% zoom and with a light theme.

### In-editor check

`test/vscode/index.ts` checks the extension inside a real VS Code against a daemon holding one terminal claim on `src/auth.go` in the opened repository:

```sh
npm run build
code --user-data-dir <empty dir> --extensionDevelopmentPath=$PWD \
  --extensionTestsPath=$PWD/out/test/vscode/index.js --wait <repository>
grep -r "crewbo extension check" <empty dir>/logs
```

The `code` launcher exits 0 even when the check fails, so read the result from the extension host log: `crewbo extension check passed` or an `AssertionError`. VS Code documents that command-line test runs need no other VS Code instance running ([testing extensions](https://code.visualstudio.com/api/working-with-extensions/testing-extension)); a separate `--user-data-dir` worked alongside a running VS Code 1.140.0 on Linux.
