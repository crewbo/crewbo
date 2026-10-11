# 05 · Interface

**Status: Approved** for everything shown on the `Brand system` and `Bo in the editor` boards (`reference/canvas/brand-system.html`, `reference/canvas/bo-in-editor.html`). Items marked **Specified** are rules without a drawing.

## 1. Foundations

| Rule | Value |
|---|---|
| Grid | 8 px. 4 px only inside dense components (pills, tags, explorer rows) |
| Borders | 2 px on light UI (`#E5E5E5`, or `#0A0A0A` for secondary buttons). 1 px hairlines (`#262626`) on dark editor surfaces |
| Radii | 12 px controls · 20 px cards · 999 px pills. Dense exceptions: 10 px compact buttons, 14 px crew rows and swatches, 6 px explorer row, 16 px window and toast |
| Shadows | None. Ever. Separate with fill, border or space |
| Gradients | None |
| Section rhythm (marketing) | 96 to 128 px between sections; 56 px between blocks inside a page; 72 px page padding at 1440 wide |
| Icons | Prefer words. If an icon is needed, use 2 px stroke, round caps, monochrome |

## 2. Components (light)

**Primary button:** height 48, padding 0 22, fill `#0A0A0A`, text white 16/600, radius 12, no border.
**Secondary button:** same size, fill white, 2 px `#0A0A0A` border, ink text.
**Status pill:** height 32, padding 0 14, 2 px `#E5E5E5` border, radius 999, 14/500 text, an 8 px signal dot with an 8 px gap before the word. Labels: `claimed`, `synced`, `handoff requested`, `conflict`.
**Card:** fill `#F5F5F5` or 2 px `#E5E5E5` border, radius 20, padding 24 to 32.
**Swatch:** radius 14, label 16/600 sans with the hex in 13 px mono beneath.

## 3. Editor integration (dark)

This is how Crewbo appears inside the IDE. Reference: `reference/screenshots/bo-in-editor.png`.

### 3.1 Explorer badges

- Each file claimed by a teammate's agent shows a **16 px badge Bo** at the right end of its explorer row (`badgeSVG` in `bo.js`).
- Two or more agents on one file: badges overlap by 5 px (`margin-left: -5px`), like stacked avatars, newest on the right. The 8-unit ring in the row colour (`#1A1A1A` selected, panel colour otherwise) separates them.
- Badges are **static**. They never animate, blink or change state. The explorer is not a place for motion.
- Show at most 3 badges, then `+N` in 12 px mono `#A3A3A3`. **Specified.**
- Folders do not get badges. **Specified.**

### 3.2 Claim tag

- Sits on the line that starts the region an agent is editing (the function signature), 20 px after the code.
- Height 22, padding 0 10, 1 px border in teammate `base`, radius 999, text 13 px Instrument Sans in teammate `tint`.
- Content: `name · Model`, for example `ronel · Opus 4.6`. If the model is unknown, `name · Agent` (`riya · Codex`). Never guess the model.
- Implement as an editor decoration (VS Code `after` decoration), never as an overlay that covers code.

### 3.3 Line highlight

- The line an agent is currently writing gets a full-width wash in the teammate `highlight` colour (16 to 18% alpha).
- It moves with the agent, one line at a time.

### 3.4 Caret rider

- An **18 px Bo** in the teammate colour sits 2 px right of the agent's insertion point, vertically centred on the line, eyes glancing right (toward where text is going) and blinking.
- It moves word by word as text arrives and hops to the next line. Full rules: `07-bo-motion.md` §6.
- Only for *other* agents and your own agent's edits. Never on the human's own caret.

### 3.5 Crew panel

Right-hand side panel, 320 px wide in the mockup (respect the host's panel width in practice), padding 20, rows separated by 14 px.

- Header: `CREW · <room-name>` in 12 px mono `#737373`.
- Row: fill `#171717`, radius 14, padding 10, gap 14. Contents left to right:
  1. **Scene Bo at 64 px** (or face Bo for event states, see §4).
  2. Text stack (gap 4): name 15/600 white with `(you)` for the local user; status 13 px `#A3A3A3` (`Writing auth.go`, `Running tests`, `Idle`, `Offline since 9:40 pm`); agent and model 12 px mono in teammate `tint` (`Claude Code · Opus 4.6`). Offline rows use `#737373` and say `last used <model>`.
- Order: you first, then by most recent activity. **Specified.**

### 3.6 Handoff toast

- Docked at the bottom of the crew panel (not a modal). Fill `#0A0A0A`, 1 px `#262626` border, radius 16, padding 16, gap 12.
- Top row: the two people's **32 px face Bos** side by side, facing each other (requester `look-r`, owner `look-l`), then the message 14/600 white: `arjun wants token.go next`.
- Buttons: `Hand off` (white fill, ink text, radius 10, height 34, padding 0 14) and `Not yet` (transparent, 1 px `#404040` border, white text).

### 3.7 Status bar

- Left: 18 px white mark plus `crewbo` in 13 px mono white.
- Right: a plain count in 13 px mono `#A3A3A3`: `3 online · 2 writing · 1 handoff · 0 conflicts`. Always show conflicts, even when 0, so the absence is visible.

## 4. What the crew panel shows (precedence)

**Specified.** The panel shows one Bo per member. Event states replace the scene while they are active. Implemented in `panelView()` in `bo.js`.

| Priority | Condition (from daemon state) | Panel shows |
|---|---|---|
| 1 | Member offline (no heartbeat) | Scene: **offline** (asleep, grey) |
| 2 | Member is in an unresolved conflict | Face: **conflict** (wide eyes, one shake, then hold) |
| 3 | A handoff involving the member is pending | Face: **handoff** (look toward the other person, soft bob) |
| 4 | Member's branch merged cleanly in the last 3.2 s | Face: **merged** (happy squint, two bobs), then back |
| 5 | Agent is editing files | Scene: **writing** (glasses + keyboard) |
| 6 | Agent is running tests | Scene: **testing** (flask) |
| 7 | Online, no agent activity | Scene: **reading** (idle) |

Activity mapping for the adapters (**Specified**, confirm against hook data in Phase 0):

- *editing*: the most recent tool event is a file write or edit (Claude Code `PreToolUse` or `PostToolUse` for `Edit`, `Write`, `MultiEdit`; Codex equivalent).
- *testing*: the most recent tool event is a shell command matching the project's test command (configurable, defaults such as `go test`, `npm test`, `pytest`, `cargo test`).
- *none*: no tool event for 60 s. **Specified**, tune after dogfooding.

## 5. Light editor themes

**Open** (see README). Specified fallback: same layout; Bos unchanged; claim tag text and border in `base`; panel row `#F5F5F5`; toast fill `#FFFFFF` with 2 px `#E5E5E5` border and ink primary button.
