# 0016. Crewbo IDE preview at the v1 launch

- Status: Accepted (Ronel, 2026-10-19, with the launch plan, PR #184)
- Date: 2026-10-19
- Decision owner: Ronel Abraham Mathew
- Supersedes: the timeline in ADR 0012 ("revisit after the v2 exit gate"). The rest of ADR 0012 stands: the extension stays the main way to use Crewbo.

## Context

ADR 0012 put a Crewbo IDE after v2, with evidence that the extension surface blocks a needed feature. On 2026-10-19 Ronel decided to ship an IDE at the first public launch instead: teams that try a ready-made editor with Crewbo built in may adopt faster, and a launch is the moment to show the brand in full. The extension work planned before launch (the crew panel with Bo) is the IDE's core either way.

VS Code's extension API limits the brand: explorer badges can only be two characters of text, so no 16 px Bo, and no claim tags drawn the way `docs/brand/05-interface.md` specifies. An editor Crewbo builds removes those limits.

## Decision

- **Name and scope:** "Crewbo IDE", released as a **preview** at the v1 launch, next to the extension, which stays the main way to use Crewbo. The IDE is Code OSS with Crewbo's branding, the Crewbo extension built in, and Crewbo's defaults (the crew panel open, telemetry off).
- **How it is built:** from the build scripts of [VSCodium](https://github.com/VSCodium/vscodium) (MIT), which build Microsoft's MIT-licensed `vscode` repository with a custom `product.json` and pre-built extensions, rather than a fork of `vscode` itself. Only the product identity, icons, defaults and the bundled extension change. Rebuilding on a new VS Code release reruns the scripts.
- **Extensions:** the IDE uses Open VSX, like VSCodium, because the Visual Studio Marketplace terms allow its offerings only in Microsoft's products. Microsoft-only extensions (Copilot, Pylance, C/C++, Remote) are not available; the download page says so.
- **Platforms:** Linux (`tar.gz`, `deb`, AppImage), macOS (arm64 and x64), Windows (x64), from CI.
- **Signing:**
  - Windows: SignPath's free signing for open-source projects (needs the public repository, now done).
  - macOS: notarization needs the Apple Developer Program (paid, Ronel's approval, CLAUDE.md rule 6). Until then the macOS build is unsigned and the download page explains how to open it.
- **Brand:** the IDE's icons and splash come from `docs/brand/assets`; any new asset gets a `docs/brand/DECISIONS.md` entry.

## Consequences

- The launch moves from the plan's 22 to 28 November dogfood to a launch about 22 November, with the hackathon after it (`docs/engineering/implementation-plan.md`).
- New ongoing work: rebuilding the IDE for each VS Code release (monthly). If that cannot be kept up, the IDE stays at its last version with a notice, and the extension carries on.
- Building Code OSS for three OSes needs CI time (macOS minutes cost about 10 times Linux on paid plans; public repositories are free on standard runners). Not verified for this organisation (U-37).
- The PRD's "Long term, not before v3: a Crewbo IDE" becomes "Crewbo IDE preview at the v1 launch; a full IDE (claim tags, the caret rider and merge previews drawn natively) later".

## Alternatives considered

- Keep ADR 0012's timeline (Claude's recommendation on 2026-10-19): less risk to the launch date, no fork to maintain; rejected by Ronel in favour of a stronger launch.
- Fork `microsoft/vscode` directly: full control, but every upstream release must be merged by hand.
- Eclipse Theia: a framework for building IDEs, but VS Code extensions and habits carry over less directly.
