# Crewbo brand and design system

Version 0.1 · approved 11 October 2026 · owner: Ronel

This folder is the single source of truth for how Crewbo looks, moves and speaks. It covers the logo, colour, type, interface rules, and Bo, the mascot that represents each teammate's agent. If code and this folder disagree, this folder wins. Open a decision record (see `DECISIONS.md`) before changing it.

## How to use this folder (for Claude Code and humans)

1. Read `01-principles.md` first. Every other file applies those principles.
2. Never invent brand values. Every colour, size, radius, duration and coordinate you need is in `tokens/tokens.json` or `reference/component/bo.js`. If a value is missing, stop and add it to the "Open questions" list at the bottom of this file. Do not guess.
3. Implement Bo by porting `reference/component/bo.js` and `bo.css` to the product's UI stack. Do not redraw Bo from screenshots. The geometry in `bo.js` is exact and was approved shape by shape.
4. Check your work against `reference/component/demo.html` (serve this folder with any static server, for example `npx serve docs/brand`) and the screenshots in `reference/screenshots/`. If your output does not match, yours is wrong.
5. Run the checklist in `09-checklist.md` on every UI pull request.

## Status labels used in these docs

| Label | Meaning |
|---|---|
| **Approved** | Designed, reviewed and accepted. Implement exactly. |
| **Specified** | Rule written, not yet drawn in the canvas. Implement as written, flag anything that looks wrong. |
| **Open** | Not decided. Do not implement. Ask. |

## File map

| File | What it holds |
|---|---|
| `01-principles.md` | The seven design principles and what each one forbids |
| `02-logo.md` | Mark construction, lockups, clear space, minimum sizes, misuse |
| `03-color.md` | Neutrals, dark surfaces, signal colours, the eight teammate colours, contrast table |
| `04-typography.md` | Instrument Sans and JetBrains Mono, the type scale, casing rules |
| `05-interface.md` | Components, light and dark surfaces, editor integration, crew panel, toasts, status bar |
| `06-bo-character.md` | Bo's anatomy, every form (mark, face, badge, caret, scene) with exact geometry, the Bo rules |
| `07-bo-motion.md` | Every animation with keyframes and timing, product versus demo timing, caret ride, reduced motion |
| `08-voice.md` | How Crewbo writes, with examples of UI copy |
| `09-checklist.md` | Pull request checklist plus do and don't lists |
| `DECISIONS.md` | Brand decision log: what was chosen, what was rejected, and why |
| `tokens/tokens.json` | Design tokens (W3C DTCG format). Source of truth for values |
| `tokens/tokens.css` | CSS custom properties generated from `tokens.json` (`--cb-*`) |
| `reference/component/bo.js` | Reference Bo component. Geometric source of truth |
| `reference/component/bo.css` | Bo motion, product timings, reduced motion |
| `reference/component/demo.html` | Live render of every Bo form, crew panel and caret ride |
| `reference/component/build-assets.mjs` | Regenerates `assets/svg` from `bo.js` |
| `reference/canvas/*.html` | Standalone copies of the approved design boards (demo timings) |
| `reference/screenshots/*.png` | Still images of each board and the demo |
| `assets/svg/logo/` | Mark (black, white), outlined lockups (light, dark), outlined wordmark |
| `assets/svg/states/` | Face Bo in every state, animated; `static/` holds key poses |
| `assets/svg/scenes/` | The four crew panel scenes, animated; `static/` holds key poses |
| `assets/svg/badges/` | 16 px file badges and 18 px caret riders, one per teammate colour |
| `assets/png/` | GitHub avatar, social preview, app icons, favicons |
| `assets/fonts/` | Self-hosted WOFF2 files and their OFL licences |

## Where each asset goes

| Destination | File |
|---|---|
| GitHub organisation avatar (`github.com/crewbo`) | `assets/png/avatar-500.png` |
| GitHub repository social preview | `assets/png/social-preview-1280x640.png` |
| VS Code extension icon (`package.json` → `icon`) | `assets/png/app-icon-128.png` |
| Desktop or tray app icon | `assets/png/app-icon-1024.png` (and 512, 256) |
| Website favicon | `assets/png/favicon.ico`, `favicon-32.png`, `assets/svg/logo/mark-black.svg` |
| Apple touch icon | `assets/png/apple-touch-icon-180.png` |
| PWA manifest icons | `assets/png/favicon-192.png`, `favicon-512.png` |
| README header | `assets/svg/logo/lockup-light.svg` with `lockup-dark.svg` via `<picture>` and `prefers-color-scheme` |
| CLI banner | No logo art in the terminal. Print `crewbo` in plain text |

## Not in scope of this folder

- The landing page at crewbo.dev is not designed yet. Build it from these rules. The older `Landing` board on the design canvas uses a superseded two-halves mark and must not be used as reference.
- Marketing illustration beyond Bo is not defined.

## Open questions

Add to this list instead of guessing. Each item needs an owner decision before implementation.

1. **Light editor themes.** The editor integration was designed on a dark theme only. Specified fallback: on light themes, claim tags use the teammate `base` colour for text and border, and the line wash keeps the same alpha. Needs a visual check.
2. **More than eight teammates.** Colours wrap after eight (`teamColor(joinIndex % 8)`). Two people could share a colour in rooms larger than eight. Acceptable for v1 (rooms are small), revisit in v2.
3. **Event states inside the crew panel** (conflict, handoff, merged) are specified (face Bo replaces the scene) but not drawn as a panel row on the canvas. See `05-interface.md` §4.
4. **Model display for Claude Code.** The crew panel shows `Agent · Model`. Phase 0 (2026-10-11, Claude Code 2.1.294, Linux) captured no `model` field in Claude Code's `SessionStart`, `UserPromptSubmit`, `PreToolUse` or `Stop` payloads; the hooks reference says `SessionStart` `model` "can be omitted". Codex sends `model` on every event. Until another source is verified, Claude Code rows show the agent name only. The `Opus 4.6` examples in the boards are illustrative. See `docs/research/phase0-findings.md`.
5. **Tertiary text contrast on dark.** `color.gray.500` (`#737373`) is 3.78:1 on `surface.dark.row` (`#171717`) and 4.04:1 on `surface.dark.panel` (`#0F0F0F`), below WCAG AA 4.5:1 for 12 to 13 px text. It is used for offline crew rows and the crew panel header. The next existing grey, `gray.400` (`#A3A3A3`), passes (7.11:1) but is the same as the status line. Needs an owner decision (new token or different treatment); principle hierarchy rule 1 puts accessibility first.
6. **Reference canvases load Google Fonts.** `reference/canvas/*.html` fetch fonts from `fonts.googleapis.com`, while BDR-014 says fonts are self-hosted and `demo.html` already uses `assets/fonts/`. Reference only, but switching them to the local files keeps the folder offline and third-party free.
