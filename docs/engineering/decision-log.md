# Decision log

One line per decision, newest last. Format: `YYYY-MM-DD | area | decision | why | link`. Costly-to-reverse decisions also get an ADR.

2026-10-11 | process | Seed commit pushed directly to main (LICENSE, README, PRD, research) | Empty repo had no PR base; approved by Ronel | cf7f495
2026-10-11 | process | Repo stays private until ADR 0011 (sync server licence) is decided | Avoid outside contributions under terms that block relicensing; Ronel | ADR 0011
2026-10-11 | schedule | Timeline shifted 1 day earlier: Phase A on Oct 11, Phase 0 Oct 12 to 17, gate Oct 17, week 8 review Dec 5 | Ronel wants to start today | implementation-plan.md
2026-10-11 | docs | Docs live under docs/ (product, research, architecture, adr, engineering) | Matches handoff layout; Ronel moved them | ADR 0010
2026-10-11 | tooling | Dev tools come from Arch official repos: go 1.27.0, buf 1.72.0, golangci-lint 2.13.2, gofumpt 0.10.0, goreleaser 2.14.0, go-task 3.53.1 | Signed by Arch packagers, upstream URLs checked | this PR
2026-10-11 | tooling | Go 1.27.2 pinned via toolchain directive (go 1.27.0) | Latest stable on go.dev/dl at 2026-10-11 | go.mod
2026-10-11 | tooling | Task (Taskfile.yml) instead of Make | Native on Windows | ADR 0010
2026-10-11 | ci | GitHub Actions pinned to commit SHAs; Dependabot updates them | Supply-chain hygiene | .github/workflows
2026-10-11 | ci | gitleaks v8.30.1 (MIT) via go install instead of gitleaks-action | Action needs a licence key for org repos | ci.yml
2026-10-11 | ci | CodeQL runs only when the repo is public | Code scanning on private repos needs a paid plan | codeql.yml
2026-10-11 | ci | Go CI steps skipped until .go files exist | Skeleton must pass with no packages | ci.yml
2026-10-11 | build | CGO_ENABLED=0 for all builds | Simple cross-compilation; forces pure-Go SQLite | ADR 0002, ADR 0008
2026-10-11 | deps | Candidate: modernc.org/sqlite v1.60.1 (BSD-3-Clause) | Pure Go, works with CGO off | ADR 0008, confirm in M1
2026-10-11 | deps | Candidate: github.com/modelcontextprotocol/go-sdk v1.8.0 (Apache-2.0 / MIT) | Official SDK, PRD choice | ADR 0007, confirm in M2
2026-10-11 | deps | Candidate: github.com/zalando/go-keyring v0.2.8 (MIT) | PRD choice; maintained (last push 2026-07-24) | ADR 0006, confirm in M3
2026-10-11 | deps | Candidate: github.com/coder/websocket v1.8.15 (ISC) | Maintained, context-aware, stdlib-style API | ADR 0008, confirm in M3
2026-10-11 | deps | Candidate: mvdan.cc/sh/v3 v3.14.1 (BSD-3-Clause) | POSIX shell parser named in research | components.md, confirm in M2
2026-10-11 | security | Task signatures cover exact serialized bytes, never a re-encoding | Protobuf serialization is not canonical | protocol.md
2026-10-11 | hooks | Use JSON deny (exit 0) for both agents, not exit 2 | Same reason format for both; documented by both | hooks.md
2026-10-11 | hooks | Drop MultiEdit from Claude matchers unless Phase 0 sees it | Not in current hooks or tools reference | hooks.md
2026-10-11 | dispatch | Claude dispatch uses --permission-mode acceptEdits; Codex uses --sandbox workspace-write | Explicit, non-bypass modes verified in local --help | hooks.md
2026-10-11 | protocol | Keepalive ping 20 s, close after 60 s silence; reconnect backoff 1 s to 30 s with jitter | Initial values, tune with Phase 0 and dogfood data | protocol.md
2026-10-11 | data | Invite default expiry 24 h | Hackathon length | data-model.md
2026-10-11 | data | Glob overlap check is conservative prefix comparison in v1 | Never misses a conflict; false positives acceptable | data-model.md
2026-10-11 | product | Long-term IDE recorded as ADR 0012 Proposed; PRD non-goal unchanged | PRD change needs Ronel's decision | ADR 0012
2026-10-11 | ci | Skip CI for docs-only changes; macOS only on PRs, weekly and manual runs | Save Actions minutes while private; Ronel has Windows, a friend has a Mac for manual checks | repo-settings.md
2026-10-11 | process | Repo settings are applied by Ronel with documented gh commands | Claude Code permission rules block settings changes | repo-settings.md
2026-10-11 | community | CoC contact will be an address on crewbo.dev once the domain is set up | Ronel | U-17
2026-10-11 | keys | Wrap every keyring call in a 3 s timeout, then fall back to the 0600 file | go-keyring blocked 60 s+ without a secret service (S7) | phase0-findings.md
2026-10-11 | hooks | Codex adapter accepts absolute and relative paths in apply_patch headers | 0.161.0 sends absolute paths (S2) | phase0-findings.md
2026-10-11 | hooks | Shell checks: mvdan/sh parser plus literal-name heuristic plus post-run hash check | 71% / 85% coverage, 0 false positives (S3) | phase0-findings.md
2026-10-11 | dispatch | Codex: hooks only in v1; App Server turn/steer deferred | Timeboxed; fallback from U-07 | phase0-findings.md
2026-10-11 | dispatch | Use the Claude messaging socket for urgent pushes to dispatched -p sessions started with crossSessionInbound accept | Delivery confirmed on Linux (S4) | phase0-findings.md
2026-10-11 | test | Golden hook fixtures stored per agent/version/os, scrubbed of paths and ids | Captured in Phase 0 | test/fixtures/hooks
2026-10-11 | hooks | Installer refuses a binary path containing spaces until U-22 shows quoting works | Unquoted spaced path made Codex skip hooks silently (Windows) | phase0-findings.md
2026-10-11 | hooks | Hook strips a leading UTF-8 BOM from stdin; undecodable input is logged and warned, never a silent allow | BOM made the probe allow everything (Windows harness) | phase0-findings.md
2026-10-11 | doctor | Detect "hooks configured but never firing" from the absence of any hook event, not only session_start | Codex SessionStart did not fire under exec on Windows; spaced path failed silently | failure-modes.md F9
2026-10-11 | hooks | No re-trust prompt handling needed on upgrade for Codex | Trust hash covers the hook entry, not the binary (U-02) | phase0-findings.md
2026-10-11 | testing | macOS checks by the teammate are Codex-only or plain Terminal; Claude Code on macOS waits for a Mac user with a Claude Code plan | Teammate has a Codex plan only | U-23
2026-10-11 | doctor | `crewbo doctor` checks the login shell's startup files for stdout output | Codex runs hooks with the login shell (`zsh -c` reads ~/.zshenv) | U-25
2026-10-11 | gate | Gate B: conditional go. Conditions: 6 more independent interviews and same-repo confirmation by end of M1; re-check before M2 | Hooks block on 2 of 3 OSes for both agents' coverage; 3 of 4 interviewed teams mix vendors, small sample | phase0-findings.md
2026-10-11 | schedule | M1 starts Oct 11 instead of Oct 18; later milestone dates unchanged and act as buffer | Phase 0 finished six days early; Ronel | implementation-plan.md
2026-10-11 | process | M1 tracked as GitHub milestone "M1" with issues #10 to #19 and label `task` | Gate C approval; Ronel | GitHub
2026-10-11 | cli | Subcommand routing with the standard library, no CLI framework | Few commands in v1; no dependency needed | #10
2026-10-11 | ci | golangci-lint pinned to v2.14.0 (was v2.13.2) | v2.13.2 cannot read export data from Go 1.27.2 (typecheck errors); v2.14.0 checked locally | #10
2026-10-11 | claims | Claim paths compare case-insensitively on every OS | Claims cross machines; a case-only difference is the same file on Windows and macOS. Resolves a wording difference between data-model.md and cross-platform.md | #13
2026-10-11 | claims | internal/claims normalises with the path package, not path/filepath | Paths from any OS must normalise identically on every OS | #13
2026-10-11 | claims | Set.Add rejects claims whose lease is not active at the given time | An already-expired claim would be accepted and silently ignored; found by a lease test | #13
2026-10-11 | platform | Path rules split per OS (paths_linux.go, paths_darwin.go, paths_windows.go) with injected Env; each OS's tests run on that OS in CI | Matches the platform rule in CLAUDE.md; no global state | #11
2026-10-11 | platform | macOS keeps config and data in the same Application Support folder; Linux runtime fallback honours $TMPDIR before /tmp | Apple convention; respects a user's chosen temp dir | #11
2026-10-11 | deps | google.golang.org/protobuf v1.36.12 (BSD-3-Clause) for generated types and protojson | Required by ADR 0007; maintained by the Go team | #12
2026-10-11 | proto | protoc-gen-go runs via `go run` in buf.gen.yaml, so generator and runtime share the go.mod version | No separately installed plugin to drift | #12
2026-10-11 | proto | Wire JSON uses default lowerCamelCase names; receivers decode with DiscardUnknown | Standard protojson; unknown-field tolerance is the compatibility rule in protocol.md | #12
2026-10-11 | proto | Times are int64 Unix milliseconds, not google.protobuf.Timestamp | Plain JSON, matches the protocol draft | #12
2026-10-11 | ci | buf breaking runs only when main already has .proto files; compares against a locally fetched main | First proto PR has nothing to compare; avoids authenticating buf to the private repo over HTTPS | #12
2026-10-11 | ci | buf CLI pinned to 1.73.0 in CI (buf-action `version`) | Action defaulted to latest; local Arch package is 1.72.0, both pass | #12
2026-10-11 | deps | modernc.org/sqlite v1.60.1 confirmed (BSD-3-Clause; repo active 2026-10-11). Links 8 modules, all MIT or BSD licensed: go-humanize, google/uuid, bigfft, x/sys, modernc libc, mathutil, memory, sqlite | Pure Go, required by CGO_ENABLED=0 | #17
2026-10-11 | store | One connection per database (SetMaxOpenConns(1)); pragmas busy_timeout(5000), journal_mode(WAL), foreign_keys(1) set in the DSN | Single writer (daemon owner goroutine); avoids SQLITE_BUSY between our own connections | #17
2026-10-11 | store | Migrations are ordered SQL strings tracked by PRAGMA user_version, each applied with its version bump in one transaction; a newer schema is refused | No migration library needed; safe against crashes and downgrades | #17
2026-10-11 | store | Corruption detected by quick_check and driver result codes SQLITE_NOTADB and SQLITE_CORRUPT (primary code of extended codes) | Typed *sqlite.Error.Code() instead of message matching | #17
2026-10-11 | ci | CI also builds with CGO_ENABLED=0 | Release builds use it; tests keep cgo because -race needs it | #17
2026-10-11 | gate | Gate B condition 1 met: 10 teams interviewed, 9 mix vendors. Condition 2 (same-repository use) still open | Interviews from Ronel | phase0-findings.md
2026-10-11 | gate | Gate B conditions met; conditional go becomes go | 10 interviews; Ronel confirmed same-repository use for all teams | phase0-findings.md
2026-10-11 | adapters | Antigravity adapter judged feasible from docs; no scope change until a live spike answers U-27 and Ronel decides U-26 | Docs-only timebox | antigravity-hooks.md
2026-10-11 | adapters | Antigravity adapter prints nothing for unblocked calls, never `{}` and never `allow` | S9: `{}` blocks the call; `allow` would bypass user permissions | antigravity-hooks.md
2026-10-11 | product | ADR 0013 accepted: Antigravity adapter is a v1 stretch goal (M5b); PRD v1 non-goals and roadmap updated; Antigravity dispatch stays in v2 | Ronel | ADR 0013
2026-10-11 | gitx | Pass `-c core.longpaths=true` on every OS and run git with GIT_TERMINAL_PROMPT=0 | Git for Windows reads it, others ignore it, so no OS branch in gitx; unattended git must never wait for credentials | #18
2026-10-11 | gitx | Use `merge-tree --write-tree -z --name-only --no-messages` (all available in git 2.38 docs) and treat exit 1 as a conflict only when a tree OID was printed | git 2.55.0 exits 1 for an unknown ref with empty stdout | #18
2026-10-11 | security | gitx rejects refs that are empty or start with `-` | Prevents git option injection from refs received from other machines | #18
2026-10-11 | platform | Local IPC is AF_UNIX on every OS using Go's net package; no named-pipe dependency | Go supports AF_UNIX on Windows 10 build 17063+; one implementation, stdlib only; resolves U-16 | #15
2026-10-11 | platform | Windows minimum becomes Windows 10 version 1803 (build 17134), the first release at or above build 17063; PRD NFR says "Windows 10+", flagged to Ronel | AF_UNIX support | #15
2026-10-11 | product | PRD platform target changed from "Windows 10+" to "Windows 10 version 1803 or later and Windows 11" | AF_UNIX needs build 17063+; earlier versions are out of Microsoft support; Ronel asked for a recommendation | PRD
2026-10-11 | security | Local daemon connections require a per-start random token on every OS (first line of each connection) | Closes U-29 by design instead of a manual two-account Windows test; same approach as Claude Code on Windows | #16
2026-10-11 | daemon | Local API is newline-delimited JSON (Hello with token, then Request and Response), plain Go structs | Small, debuggable; protobuf is reserved for daemon to server | #16
2026-10-11 | daemon | Token is written only after Listen succeeds, compared in constant time, removed on shutdown | A refused second daemon must never replace the live token | #16
2026-10-11 | lint | gosec excluded for _test.go files | Tests build binaries and read temp files on purpose; production code stays covered | #16
2026-10-11 | ci | Coverage gate is scripts/coverage-gate.sh (85% for claims, shellparse, hooks, dispatch; missing packages skipped), run on the Linux CI job and via `task cover:gate` | One script for CI and local use; coverage logic is OS-independent | #19
2026-10-11 | daemon | `decide` also registers the session and renews its leases | One round trip per edit on the hook path | #43
2026-10-11 | hooks | `crewbo hook` with an unknown agent or event exits 0 with no output and logs to hook.log | A misconfigured hook must never block the agent | #35
2026-10-11 | hooks | Repository root found by walking up to `.git` (dir or file), no git process per hook | Keeps the hook fast; handles worktrees | #35
2026-10-11 | hooks | Shell write detection is injected (Deps.ShellTargets) until the parser lands | Lets M2-01 merge before M2-04 | #35
2026-10-11 | claudecode | Claude Code adapter never prints on Stop; allow prints nothing; warnings and context go in additionalContext | additionalContext on Stop continues the conversation (hooks reference); silence means no decision | #36
2026-10-11 | codex | Codex adapter treats "Bash" commands as PowerShell on Windows | Phase 0: Codex 0.154.0 on Windows ran Add-Content under tool name Bash | #37
2026-10-11 | codex | Codex adapter output is limited to hookSpecificOutput deny and additionalContext; Stop and allow print nothing | Unsupported fields fail the hook open (Codex docs) | #37
2026-10-11 | daemon | Claims are kept per repository (map keyed by RepoKey: symlinks resolved, slashes, case folded) | A claim must not block the same path in other repositories; hook and terminal must agree on the key | #44
2026-10-11 | cli | Terminal claims use session "terminal", lease 30 min by default (`--for`, at most 8 h), and block every agent including the user's own | No hook event renews them; teammates and rooms refine ownership in M3 | #44
2026-10-11 | daemon | claim_add is all or nothing; a conflict rolls back the request's claims and returns the blocking claim | Partial claims surprise users | #44
2026-10-11 | deps | mvdan.cc/sh/v3 v3.14.1 (BSD-3-Clause, active) for POSIX shell parsing | Named in research; S3 proved it | #38
2026-10-11 | shellparse | Literal-mention heuristic runs in the daemon (claims.Set.MentionedBy), only for commands the parser marks opaque, only for non-glob claims | Only the daemon knows claimed names; S3 showed 0 false positives when limited to opaque commands | #38
2026-10-11 | shellparse | Computed arguments make a command opaque only when a writing command would use them as a target | `echo $f >> log` must not be opaque | #38
2026-10-11 | shellparse | PowerShell is analysed by an in-repo tokenizer with a cmdlet, alias and parameter table, not a parser library | No maintained PowerShell parser exists for Go; the write surface agents use is small | #39
2026-10-11 | hooks | Post-shell hash check runs for every allowed shell command, paired by tool_use_id; literal claims only, 200 files and 10 MiB per file max | Unknown programs (go generate, black) are not opaque yet can write; glob expansion per command would walk the repo | #40
2026-10-11 | install | hooks install edits JSON via generic maps (keys re-sorted, formatting normalised) and keeps a .crewbo.bak backup | No dependency for format-preserving JSON; the backup lets users diff | #41
2026-10-11 | install | Codex install refuses binary paths with spaces or '&'; Claude Code (exec form) allows them | Phase 0: Codex skipped an unquoted spaced path silently (U-22) | #41
2026-10-11 | deps | github.com/modelcontextprotocol/go-sdk v1.8.0 (Apache-2.0, moving from MIT; active). Brings google/jsonschema-go, segmentio/encoding and asm (MIT), yosida95/uritemplate, x/oauth2, x/time (BSD) | Official SDK named in ADR 0007 | #42
2026-10-11 | mcp | MCP tools read claims through claim_list and match locally; they never call decide | decide registers a session and renews leases, which a query must not do | #42
2026-10-11 | cli | `crewbo claim add` parses flags anywhere among the paths | The standard flag package stopped at the first path and stored trailing flags as claims (found in a live MCP test) | #42
2026-10-12 | deps | github.com/coder/websocket v1.8.15 (ISC; release 2026-06-15; no dependencies of its own). API checked at that tag: all Conn methods are concurrency-safe except Read; default read limit 32 KiB, raised to the protocol's 64 KiB | Stdlib has no WebSocket server; candidate named in the Gate C list | #59
2026-10-12 | server | `crewbo serve` listens on 127.0.0.1:7420 by default; plain HTTP is refused off loopback unless `--insecure-lan` | A bare command must never expose session tokens in clear text on a network | #59
2026-10-12 | server | Oversized frames close with WebSocket status 1009 (message too big), the library's built-in behaviour; malformed or binary frames close with 1008 and 1003 | Precise codes help daemon logs; no custom framing needed | #59
2026-10-12 | store | `store.Open` takes a schema: `store.Daemon` and `store.Server` are separate migration lists for separate files | The server and daemon share no tables; one list would create unused tables in each | #60
2026-10-12 | server | Event seq is bumped and the event inserted in one transaction (`UPDATE rooms ... RETURNING`), and the room goroutine registers a subscriber in the same step as its replay read | A failed append leaves no gap, and a subscriber gets replay then live events with no gap or duplicate | #60
2026-10-12 | server | A subscriber that falls 64 events behind is dropped and resyncs from its cursor; a cursor ahead of the log receives nothing until the log passes it | The room goroutine never blocks on one slow connection | #60
2026-10-12 | server | The members table lands with M3-06 (rooms, invites, lead), not M3-02 | Its rules (10-member cap, one lead) belong with the code that enforces them | #60
2026-10-12 | auth | The Crewbo OAuth app's client ID (`Ov23liMSnQmA2q3VqyiK`, crewbo org, device flow on, no client secret) is a constant in `internal/auth`; self-hosters override it | Client IDs are public; the device flow needs no secret (GitHub docs) | #61
2026-10-12 | auth | The device flow requests no scope; the server identifies the member with `GET /user` (public profile) and API version `2026-03-10` | ADR 0009: public profile only; the version is the one the REST docs list on 2026-10-12 | #61
2026-10-12 | auth | OAuth error bodies are read whatever the HTTP status, and a poll that outlives `expires_in` reports the code as expired | The docs do not say which status carries device-flow errors | #61
2026-10-12 | server | Crewbo session tokens are 32 random bytes (`cbs_` + base64url), valid 7 days, stored only as SHA-256 hashes; `Hello` without a valid one gets `Error(UNAUTHENTICATED)` | ADR 0009 asks for short-lived server tokens; 7 days until M3-04 lets a device key renew them without a browser | #61
2026-10-12 | cli | `crewbo login` saves one session to `<config>/session.json` (0600; Windows relies on the per-user config directory's ACL) and refuses plain http except on loopback or with `--insecure-lan` | One server per member in v1; the GitHub token must never cross a network in clear text by accident | #61
2026-10-12 | deps | github.com/zalando/go-keyring v0.2.8 (MIT; release 2026-03-23, active). Brings danieljoos/wincred v1.2.3 (MIT) and godbus/dbus/v5 v5.2.2 (BSD-2-Clause); builds with CGO_ENABLED=0 on Linux, Windows and macOS (macOS shells out to /usr/bin/security) | Named in ADR 0006; the stdlib has no OS keychain access | #62
2026-10-12 | keys | Device key id is `ed25519:` plus the first 16 bytes of SHA-256(public key) in hex | Short, unambiguous about the algorithm, collision-resistant for a few devices per member | #62
2026-10-12 | keys | Every keychain call is bounded to 3 s; on error or timeout the seed goes to `<data>/device-key` (0600), published with a hard link so racing processes agree, and that file wins on every later load; a damaged keychain entry or file is an error, never replaced | U-18 (60 s+ block in Phase 0 S7); replacing a key silently would orphan the one teammates trust | #62
2026-10-12 | server | `POST /v1/devices` registers a device public key for the session's member only with an Ed25519 signature over `crewbo-register-device-v1\n` + that session token; the key id is derived on the server; a key owned by another member is refused (409); re-registering is accepted | Proof of possession stops a member claiming someone else's public key; binding to the session stops replay | #62
2026-10-12 | cli | `crewbo login` loads or creates the device key and registers it after the session exchange, and saves its id in session.json | flows.md has the device key step right after login; one command for the member | #62
2026-10-12 | server | Rooms over HTTP JSON: `POST /v1/rooms` (creator is lead), `GET /v1/rooms`, `POST /v1/rooms/{id}/invites` (lead only), `POST /v1/rooms/join`, `POST /v1/rooms/{id}/leave`; a room the caller is not in answers 404 | Request and answer fit HTTP; the WebSocket stays for the event stream; 404 stops probing room ids | #64
2026-10-12 | server | Invite codes are `cbi_` + 128 random bits, stored as SHA-256 hashes, default 9 uses (room size minus the lead) and 24 h, at most 7 days; rejoining a room uses nothing | Threat model (128 bits, hashed, expiring, use-limited); one code for a hackathon team | #64
2026-10-12 | server | The lead can leave only as the last member; lead transfer is not in M3 | PRD: one lead per room, transfer is explicit; no transfer flow exists yet | #64
2026-10-12 | server | The hub starts one Room goroutine per room on first subscription and keeps it until the server stops; a Hello cursor for a room the member is not in gets `Error(NOT_MEMBER)` and a close; a connection whose subscription ends (fell 64 events behind, or the server is stopping) is closed with 1013 (try again later) | The daemon reconnects and replays from its cursor, so no event is missed silently | #63
