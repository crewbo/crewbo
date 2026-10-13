# v1 flows

One sequence diagram per PRD flow, including failure branches. Message names refer to [protocol.md](protocol.md).

## Flow 1: join a room

Presentation copy: [Lucid](https://lucid.app/lucidchart/3ffc1161-3058-43be-887c-666e09b4460a/view). Mermaid below is the source of truth.

```mermaid
sequenceDiagram
  autonumber
  actor M as Member
  participant CLI as crewbo CLI
  participant D as Daemon
  participant GH as GitHub
  participant S as Sync server

  M->>CLI: crewbo login
  CLI->>GH: POST /login/device/code
  GH-->>CLI: device_code, user_code, verification_uri
  CLI-->>M: show user_code and URL
  M->>GH: approve in browser
  loop poll at GitHub's interval
    CLI->>GH: POST /login/oauth/access_token
  end
  alt approved
    GH-->>CLI: access_token
  else denied or expired
    GH-->>CLI: error
    CLI-->>M: login failed, retry
  end
  CLI->>S: POST /v1/auth/session (GitHub token)
  S->>GH: GET /user
  S-->>CLI: Crewbo session token
  CLI->>D: generate or load device key
  alt keychain available
    D->>D: store seed in keychain
  else no secret service
    D->>D: write 0600 key file, warn once
  end
  CLI->>S: RegisterDevice(public key)
  M->>CLI: crewbo room join <invite>
  CLI->>S: JoinRoom(invite code)
  alt invite valid and room has fewer than 10 members
    S-->>CLI: RoomSnapshot(members, claims, seq)
    CLI->>D: start syncing room
    D->>S: Hello(room, last_seq)
  else invite invalid, expired or room full
    S-->>CLI: Error(code)
    CLI-->>M: explain and exit
  end
```

## Flow 2: lead dispatches a signed task

Presentation copy: [Lucid](https://lucid.app/lucidchart/cafe99c3-9fe7-4f91-a20a-9f335b3af3ec/view). Mermaid below is the source of truth.

```mermaid
sequenceDiagram
  autonumber
  actor L as Lead
  participant LD as Lead daemon
  participant S as Sync server
  participant TD as Teammate daemon

  L->>LD: crewbo task create --to <member> --paths <globs> --text <file>
  LD->>LD: check owned paths do not overlap active claims
  alt overlap
    LD-->>L: list conflicting claims, ask to adjust or proceed
  end
  LD->>LD: canonical encode, sign with device key
  LD->>S: DispatchTask(signed envelope)
  S->>S: check sender is room lead, size limits
  alt sender not lead
    S-->>LD: Error(not_lead)
  else
    S->>S: append TaskDispatched to event log
    alt teammate online
      S->>TD: Event(TaskDispatched)
    else offline
      S->>S: hold until teammate's next Hello
    end
    S-->>LD: Ack(seq)
  end
```

## Flow 3: teammate approves; agent claims and runs in a fresh worktree

Presentation copy: [Lucid](https://lucid.app/lucidchart/877460d5-202f-4d59-a91c-8de3023a5ebe/view). Mermaid below is the source of truth.

```mermaid
sequenceDiagram
  autonumber
  actor T as Teammate
  participant TD as Teammate daemon
  participant S as Sync server
  participant G as System git
  participant A as Agent CLI (headless)

  TD->>TD: verify signature, key registered, key holder is current lead, not expired
  alt verification fails
    TD->>S: TaskRejected(reason)
    TD-->>T: show warning, never show task as runnable
  else
    TD-->>T: notify: task waiting (CLI, extension, dashboard)
    T->>TD: open task: full text shown verbatim, owned paths, author
    alt teammate declines
      T->>TD: decline(reason)
      TD->>S: TaskDeclined
    else teammate approves
      T->>TD: approve
      TD->>S: ClaimRequest(owned paths, task id)
      alt claims granted
        S-->>TD: ClaimGranted(lease expiry)
      else some paths held by others
        S-->>TD: ClaimDenied(holders)
        TD-->>T: show holders, offer wait or proceed with remaining paths
      end
      TD->>G: fetch, worktree add -b crewbo/<task> <base commit>
      alt worktree fails (dirty path, long path, disk)
        TD->>S: release claims, TaskFailed
        TD-->>T: show git error
      else
        TD->>A: spawn in worktree (claude -p ... or codex exec ...)
        TD->>S: TaskStarted
        A-->>TD: hook events renew leases while it works
      end
    end
  end
```

## Flow 4: edit to a claimed file is blocked and the owner notified

Presentation copy: [Lucid](https://lucid.app/lucidchart/fcb2aaf3-4205-4d8b-b76d-6bb68f91aa14/view). Mermaid below is the source of truth.

```mermaid
sequenceDiagram
  autonumber
  participant A as Agent B
  participant H as crewbo hook
  participant DB as Daemon B
  participant S as Sync server
  participant DA as Daemon A (holder)
  actor O as Owner (member A)

  A->>H: PreToolUse JSON on stdin
  H->>DB: Decide(pre_edit, paths) within deadline
  alt daemon unreachable or timeout
    H-->>A: allow + warning "Crewbo daemon not reachable, claims not enforced"
  else path free or held by this session
    DB-->>H: allow (renew own lease)
    H-->>A: exit 0, no decision
  else held by another session
    DB-->>H: deny(holder, task, lease expiry)
    H-->>A: deny, reason "src/auth.ts is claimed by A's agent for task 12. Options: wait, work on other files, or run crewbo claim request-handoff src/auth.ts"
    DB->>S: ConflictObserved(path, requester)
    S->>DA: Event(ConflictObserved)
    DA-->>O: notify (extension, CLI)
    opt agent or teammate requests handoff
      DB->>S: HandoffRequest(path, reason)
      S->>DA: Event(HandoffRequested)
      alt owner approves
        O->>DA: approve handoff
        DA->>S: ClaimRelease(path) then ClaimGrant to requester
      else owner declines or no answer within lease
        O->>DA: decline
        opt lead override
          Note over S: lead sends LeadOverride(path, reason); logged in event log
        end
      end
    end
  end
```

Shell edits follow the same path through `pre_shell`. After a shell command runs, `post_edit` compares hashes of claimed files; a change to a file claimed by someone else produces `ClaimViolation` (alert and record, no revert).

## Flow 5: cross-machine merge-tree warning before merge

Presentation copy: [Lucid](https://lucid.app/lucidchart/0c8b27cd-799d-469e-8c99-8d6c9dba55f9/view). Mermaid below is the source of truth.

```mermaid
sequenceDiagram
  autonumber
  participant A as Agent
  participant H as crewbo hook
  participant D as Daemon
  participant G as System git
  participant R as Git remote
  participant S as Sync server
  actor L as Member or lead

  A->>H: Stop
  H->>D: stop(session)
  D->>G: commit checkpoint in worktree (if changes)
  D->>R: push refs/crewbo/<member>/<task>
  alt push rejected (custom ref refused, rate limited)
    D->>R: fallback push wip/<member>/<task>
  end
  D->>S: Checkpoint(task, ref, commit)
  S->>D: Event(Checkpoint) from other members
  D->>R: fetch others' checkpoint refs
  D->>G: merge-tree --write-tree <mine> <theirs>
  alt exit 0 (clean)
    D->>D: no warning
  else exit 1 (conflicts)
    D->>S: ConflictWarning(tasks, paths)
    S-->>L: notify room
  else error
    D->>D: log, retry at next checkpoint
  end
  L->>D: crewbo task merge <task> (the assignee, or only the lead in lead-only rooms)
  D->>G: re-run merge-tree against current main and the other active task branches
  alt conflict with main, or with an active branch and no lead override
    D-->>L: refuse, naming the conflicting files and tasks
  else clean
    D->>R: merge and push main
    opt push rejected because main moved
      D->>R: fetch, re-check, retry
    end
  end
```

Merging follows ADR 0014: by default the task's assignee merges their own work through this check, and the remote serialises merges; a lead-only room refuses merges from anyone but the lead. Self-started tasks (`crewbo task start`) go through the same flow.
