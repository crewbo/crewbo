# Cross-platform notes

Targets: Windows 10+, macOS 13+, Linux x86_64 and arm64 (including Arch-based Omarchy). "Untested on <OS>" applies to everything here until Phase 0 and CI confirm it.

| Concern | Windows | macOS | Linux | Handling |
| --- | --- | --- | --- | --- |
| Local IPC | Named pipe `\\.\pipe\crewbo-<user-sid>`, ACL current user only | Unix socket in `$TMPDIR/crewbo-<uid>/` (0700) | Unix socket in `$XDG_RUNTIME_DIR/crewbo/` (0700), fallback `/tmp/crewbo-<uid>/` | `internal/platform` `Listener` and `Dialer`. Named pipe library choice is a decision-log item in M1 |
| Data dir | `%LOCALAPPDATA%\crewbo` | `~/Library/Application Support/crewbo` | `$XDG_DATA_HOME/crewbo` or `~/.local/share/crewbo` | `os.UserConfigDir` / `os.UserCacheDir` plus a platform helper for data |
| Binary install path | `%LOCALAPPDATA%\crewbo\bin\crewbo.exe`; warn if the path contains spaces or `&` (Codex runs hooks via cmd.exe, U-03) | `~/.local/bin` or Homebrew prefix | `~/.local/bin` or package manager | Hook entries use the absolute path with forward slashes |
| Paths from agents | Claude sends absolute backslash paths in `file_path` | POSIX | POSIX | Convert to repo-relative `/` form before any comparison |
| Case sensitivity | Case-insensitive (NTFS default) | Case-insensitive (APFS default) | Case-sensitive | Case-fold comparison on Windows and macOS; treat case-only matches as conflicts everywhere |
| Long paths | MAX_PATH 260 unless long paths enabled | n/a | n/a | Pass `-c core.longpaths=true` to git; keep worktree root short (`<repo>/.crewbo/wt/<short-id>`) |
| Line endings | CRLF checkouts possible | LF | LF | Hash comparison on raw bytes; repository `.gitattributes` is the team's choice |
| File watching | ReadDirectoryChangesW | FSEvents | inotify (watch limits) | Not used in v1 enforcement; post-shell check uses hashes of claimed files only |
| Keychain | Credential Manager | Keychain | Secret Service over D-Bus; often absent on Omarchy | `zalando/go-keyring`; fail fast to a 0600 file (on Windows: file in the user profile with an ACL limited to the user) |
| Service install | Per-user Scheduled Task at logon (no admin) | launchd LaunchAgent plist | systemd `--user` unit | `crewbo daemon install`; foreground `crewbo daemon` always works |
| Process spawning | No shell; `exec.Cmd` with explicit args; Claude hooks via exec form | Same | Same | Never build command lines as strings |
| Signals | No SIGTERM; use console control events or Job Object termination for child agents | SIGTERM, SIGINT | SIGTERM, SIGINT | `platform.Terminate(proc)`; child agents placed in a Job Object (Windows) or process group (Unix) so cancel stops the whole tree |
| Git | Git for Windows; check `git --version` >= 2.38 | Apple git may lag; check version | Distro git | Daemon refuses to start below 2.38 |
| Shell parsing | Claude routes shell through PowerShell when enabled; Bash tool may be absent [CC] | bash/zsh | bash/zsh/fish | `shellparse` handles POSIX and a PowerShell subset; match `Bash\|PowerShell` |
| Codex hook shell | `cmd.exe /C` (UNVERIFIED, U-03) | `$SHELL -lc` (UNVERIFIED) | `$SHELL -lc`; fish or zsh login shells may print to stdout (UNVERIFIED) | Hook binary prints only JSON; Phase 0 tests profile noise |

[CC]: https://code.claude.com/docs/en/hooks
