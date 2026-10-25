# 04 · Typography

**Status: Approved.**

## Families

| Role | Family | Weights | Licence |
|---|---|---|---|
| Display, UI and body | **Instrument Sans** | 400, 500, 600 (700 only for Bo's z glyphs) | SIL OFL 1.1 |
| Code, CLI, labels, hex values, model names | **JetBrains Mono** | 400, 500 | SIL OFL 1.1 |
| Website headlines only (BDR-022) | **Instrument Serif** | 400 (normal, italic) | SIL OFL 1.1 |

Self-host from `assets/fonts/` (WOFF2, Latin subset, from Fontsource 5.x). Do not load from Google Fonts in the product: the desktop app and extension must work offline, and Google Fonts leaks requests. The canvas reference pages load from Google Fonts only for convenience.

```css
@font-face { font-family: 'Instrument Sans'; font-weight: 400; font-display: swap; src: url(/fonts/instrument-sans-latin-400-normal.woff2) format('woff2'); }
@font-face { font-family: 'Instrument Sans'; font-weight: 500; font-display: swap; src: url(/fonts/instrument-sans-latin-500-normal.woff2) format('woff2'); }
@font-face { font-family: 'Instrument Sans'; font-weight: 600; font-display: swap; src: url(/fonts/instrument-sans-latin-600-normal.woff2) format('woff2'); }
@font-face { font-family: 'JetBrains Mono';  font-weight: 400; font-display: swap; src: url(/fonts/jetbrains-mono-latin-400-normal.woff2) format('woff2'); }
@font-face { font-family: 'JetBrains Mono';  font-weight: 500; font-display: swap; src: url(/fonts/jetbrains-mono-latin-500-normal.woff2) format('woff2'); }
```

Fallback stacks: `ui-sans-serif, system-ui, sans-serif` and `ui-monospace, SFMono-Regular, Menlo, monospace`.

Inside VS Code webviews, use the editor's font (`var(--vscode-editor-font-family)`) for anything that sits next to code, and Instrument Sans for Crewbo's own panel text.

## Scale

| Token | Family | Size | Weight | Line height | Tracking | Use |
|---|---|---|---|---|---|---|
| `type.display` | Serif | 72 | 400 | 1.0 | −1.5 px | Website hero headline (BDR-022) |
| `type.displayH2` | Serif | 52 | 400 | 1.05 | −1 px | Website section titles (BDR-022) |
| `type.h1` | Sans | 56 | 600 | 1.05 | −1.6 px | Page title |
| `type.h2` | Sans | 44 | 600 | 1.1 | −1.2 px | Section title |
| `type.h3` | Sans | 22 | 600 | 1.3 | 0 | Card title, principle name |
| `type.title` | Sans | 20 | 600 | 1.3 | 0 | State and scene names |
| `type.bodyLg` | Sans | 18 | 400 | 1.55 | 0 | Marketing body |
| `type.body` | Sans | 15 | 400 | 1.5 | 0 | Product body, captions |
| `type.small` | Sans | 13 | 400 | 1.4 | 0 | Crew row status, meta |
| `type.label` | Mono | 13 | 400 | 1.4 | 0 | Eyebrows, hex codes, model names |
| `type.code` | Mono | 15 | 400 | 30 px | 0 | Code in mockups |
| `type.wordmark` | Sans | 52 | 600 | 1.0 | −1.6 px | Use the outlined SVG instead |

Crew panel specifics: name 15/600 white, status 13/400 `#A3A3A3`, agent and model line 12 px JetBrains Mono in the teammate `tint`. The claim tag is 13 px Instrument Sans.

## Rules

- Headlines are weight 600 with negative tracking (about −3% of size). Body is 400 with no tracking.
- Section eyebrows are mono, upper case, 13 px, `#525252`, numbered: `01 · THE MARK`. The separator is a middle dot with spaces (` · `).
- Sentence case everywhere in UI ("Hand off", "Not yet", "Running tests"). Never title case and never all caps except mono eyebrows.
- The product name is lower case only in the wordmark and the CLI. In sentences it is "Crewbo".
- Numbers in tables and stats use JetBrains Mono so columns align.
- No italics in UI. No underlines except links on hover.
- Do not use em dashes in UI or marketing copy. Use a colon, comma or full stop.
