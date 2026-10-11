# 03 · Colour

**Status: Approved** unless marked otherwise. Token names refer to `tokens/tokens.json`; CSS variables are in `tokens/tokens.css` as `--cb-color-…`.

## 1. Neutrals (the brand)

| Token | Hex | Use |
|---|---|---|
| `color.ink` | `#0A0A0A` | Text, logo, primary button, dark chrome |
| `color.gray.800` | `#262626` | Strong body copy on light; prop fills on dark |
| `color.gray.700` | `#404040` | Keys, desk lines, inactive controls on dark |
| `color.gray.600` | `#525252` | Secondary text on light (7.81:1 on white) |
| `color.gray.500` | `#737373` | Offline Bo on light. Not for text on dark (3.78:1 on `#171717`) |
| `color.gray.450` | `#858585` | Tertiary text on dark (4.86:1 on `#171717`, 5.19:1 on `#0F0F0F`). BDR-016 |
| `color.gray.400` | `#A3A3A3` | Secondary text on dark (7.3:1 on `#141414`) |
| `color.gray.200` | `#E5E5E5` | Borders on light |
| `color.gray.100` | `#F5F5F5` | Card and section fill on light |
| `color.paper` | `#FFFFFF` | Page background; Bo's eyes |

Pure neutral greys (no warm or cool tint). Never use `#000000`; ink is `#0A0A0A`.

## 2. Dark surfaces (editor integration)

Designed against a near-black editor. These are mockup surfaces: inside VS Code, use the theme's own colours (`var(--vscode-…)`) for backgrounds and only bring Crewbo's colours for Crewbo elements.

| Token | Hex | Use |
|---|---|---|
| `surface.dark.chrome` | `#0A0A0A` | Title bar, status bar |
| `surface.dark.editor` | `#141414` | Editor |
| `surface.dark.panel` | `#0F0F0F` | Explorer, crew panel |
| `surface.dark.row` | `#171717` | Crew panel row |
| `surface.dark.rowActive` | `#1A1A1A` | Selected explorer row, badge ring |
| `surface.dark.line` | `#262626` | 1 px dividers |

## 3. Signal colours (product only)

| Token | Hex | Meaning | Rule |
|---|---|---|---|
| `signal.claimed` | `#15803D` | A file or glob is claimed | Dot + the word "claimed" |
| `signal.synced` | `#1D4ED8` | In sync / information | Dot + word |
| `signal.handoff` | `#F59E0B` | Handoff requested / warning | Dot + word. Text on amber is ink, never white |
| `signal.conflict` | `#DC2626` | Real conflict, needs a human | Dot + word. Only for real conflicts |

Signals never appear in marketing or the logo. They are always paired with a label (pill: 8 px dot, then the word).

**Known overlap:** `signal.synced` equals teammate Cobalt and `signal.claimed` equals teammate Moss. This is acceptable because signals only appear as a labelled pill and teammate colours only appear on Bo, claim tags and line highlights. Never show a bare coloured dot that could be read either way.

## 4. Teammate colours

Every member of a room gets one colour, assigned in this order as they join. The colour is theirs for the life of the room.

| # | Name | `base` (Bo body) | White eyes on base | `tint` (text on dark) | Tint on `#141414` | `highlight` (line wash) |
|---|---|---|---|---|---|---|
| 1 | Cobalt | `#1D4ED8` | 6.70:1 | `#93C5FD` | 10.22:1 | `rgba(29,78,216,0.16)` |
| 2 | Teal | `#0F766E` | 5.47:1 | `#5EEAD4` | 12.45:1 | `rgba(15,118,110,0.18)` |
| 3 | Moss | `#15803D` | 5.02:1 | `#86EFAC` | 13.12:1 | `rgba(21,128,61,0.18)` * |
| 4 | Ember | `#C2410C` | 5.18:1 | `#FDBA74` | 10.92:1 | `rgba(194,65,12,0.16)` * |
| 5 | Ochre | `#A16207` | 4.92:1 | `#FDE047` | 13.97:1 | `rgba(161,98,7,0.18)` * |
| 6 | Rose | `#BE185D` | 6.04:1 | `#F9A8D4` | 10.16:1 | `rgba(190,24,93,0.16)` * |
| 7 | Violet | `#7E22CE` | 6.98:1 | `#D8B4FE` | 10.42:1 | `rgba(126,34,206,0.16)` * |
| 8 | Indigo | `#4338CA` | 7.90:1 | `#A5B4FC` | 9.24:1 | `rgba(67,56,202,0.16)` * |

\* Cobalt and Teal highlights were designed. The other six follow the same rule (alpha 0.16 for cooler or brighter hues, 0.18 for darker ones) and are **Specified**. Check them visually on the first real build.

All contrast ratios were computed with the WCAG 2.x relative luminance formula. Every base holds white eyes at 4.5:1 or better.

Rules:

- `base` is for Bo's body, the 16 px badge, the caret rider and the claim tag border.
- `tint` is for text on dark surfaces: the claim tag label and the agent/model line in the crew panel.
- On light editor themes (Open question 1), use `base` for tag text and border.
- Bo's body on a dark surface has low contrast for some colours (Indigo 2.43:1 on `#0F0F0F`). That is acceptable because the colour never carries meaning alone: the name is always next to it and the white eyes define the shape. Do not add outlines to fix it.
- Offline Bos ignore the teammate colour and turn grey (`#737373` on light, `#525252` on dark).
- The brand Bo (logo, docs, marketing hero) is always ink, never a teammate colour.

## 5. Scene prop colours

Used only inside crew panel scenes (`06-bo-character.md` §6).

| Token | Hex | Use |
|---|---|---|
| `prop.liquid` | `#2DD4BF` | Flask liquid while tests run |
| `prop.liquidPass` | `#4ADE80` | Flask liquid when tests pass |
| `prop.bubble` | `#CCFBF1` | Rising bubbles |
| `prop.pageShade` | `#C7C7C7` | Book page mid-turn |
| Glasses | `#0A0A0A` | Ink |
| Keyboard body, flask glass | `#262626` with `#525252` / `#A3A3A3` outline | |
| Keys, desk | `#404040`; lit key `#E5E5E5` | |
| Book pages | `#F5F5F5` with `#A3A3A3` text lines, spine `#737373` | |

## 6. Code syntax colours in mockups

The editor mockup uses: keywords `#C084FC`, comments `#737373`, default text `#D4D4D4`, line numbers `#525252`. These are for mockups and marketing screenshots only. In the product, the user's theme controls syntax colours.
