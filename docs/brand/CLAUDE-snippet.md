Paste the section below into the repository's `CLAUDE.md`.

---

## Brand and UI

- `docs/brand/` is the source of truth for logo, colour, type, interface and Bo (the mascot). Read `docs/brand/README.md` and `docs/brand/01-principles.md` before any UI, extension, website or asset work.
- Never invent brand values. Colours, sizes, radii and durations come from `docs/brand/tokens/tokens.json` (CSS: `--cb-*` in `tokens.css`). If a value is missing, add it to "Open questions" in `docs/brand/README.md` and ask. Do not guess.
- Bo is implemented by porting `docs/brand/reference/component/bo.js` and `bo.css` unchanged in geometry and timing. Never redraw Bo or approximate it from screenshots.
- Bo rules: no limbs; one moving thing per Bo; real proportions; no prop reuses the caret-eye shape; scenes only in the crew panel; below 16 px use a colour dot; offline Bo is grey; the brand Bo is always ink.
- Verify UI against `docs/brand/reference/component/demo.html` and `docs/brand/reference/screenshots/`, and run `docs/brand/09-checklist.md` on every UI pull request.
- Respect reduced motion (`prefers-reduced-motion` and VS Code `workbench.reduceMotion`).
- Copy: sentence case, no exclamation marks, no em dashes, name the file, person and agent, never guess a model name. See `docs/brand/08-voice.md`.
- Brand changes need a new entry in `docs/brand/DECISIONS.md`. Do not reintroduce anything listed there as rejected.
