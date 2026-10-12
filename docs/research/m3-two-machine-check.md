# M3 two-machine check

Run on 2026-10-12 by Ronel (Omarchy, Claude Code 2.1.294) and gavriel953 (macOS 27.0.1 arm64, Codex 0.162.1 and Claude Code 2.1.287) against `crewbo/crewbo` at `313105b` (M3-07 merged), following `crewbo/spikes` `agent-prompts/m3-two-machine.md`. The sync server ran on Ronel's laptop behind a Cloudflare quick tunnel. Full reports: `crewbo/spikes` `results/m3-two-machine/ronel.md` (PR #7) and `macos.md` with evidence (PR #8).

## Results

| Check | Result |
| --- | --- |
| Sign-in through the tunnel (GitHub device flow), device keys | Both machines; keys in the system keychain (Linux secret service, macOS login keychain, no prompt on macOS) |
| Ronel's claim blocks the Mac | Codex `apply_patch` and shell blocked; Claude Code edit and shell blocked once its hooks were installed; all name RM1338 |
| The Mac's claim blocks Ronel | Claude Code edit blocked naming gavriel953 |
| Conflict | Ronel's claim on the Mac's file refused ("already claimed by gavriel953"); granted after release |
| Claim round trip through the tunnel, confirmed by the server | Mac 74 to 90 ms; Omarchy 103 to 188 ms (target: under 2 s) |
| Offline claim | Kept locally, delivered on reconnect, seen on the other machine |
| Unplanned tunnel drop (Mac, 17:58) | The claim made at that moment was queued and delivered after a 2 s reconnect |
| Server restart | Both machines offline, then connected (Omarchy about 5 s after restart); all 12 claims survived |
| Cleanup | Hooks uninstalled on both machines; configs byte-identical to the originals |

## Findings and what happened to them

1. Claude Code on the Mac was not blocked at first: its hooks were not installed (the prompt said `--agent codex`). After `crewbo hooks install --agent claude-code` it was blocked. U-23 resolved.
2. A teammate's terminal claim was worded as their agent's: fixed in #86 (#84).
3. After Wi-Fi was turned off, the Mac's daemon took about 249 s to show offline (OS TCP timeout): fixed in #87 (#85), daemon pings every 20 s with a 10 s pong deadline.
4. The prompt had two mistakes: `--allowedTools` placed before the prompt swallowed it, and the build path differed from `~/crewbo`. To fix in the next prompt.
