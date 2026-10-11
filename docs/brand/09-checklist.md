# 09 · Checklist

Copy this into every pull request that touches UI, the extension, the website or assets.

## Pull request checklist

**Values**
- [ ] No hex codes, pixel sizes, radii or durations outside `tokens/tokens.json` (or `--cb-*` variables).
- [ ] No shadows, no gradients, no new radii.
- [ ] Borders are 2 px on light, 1 px hairline on dark.
- [ ] Spacing is on the 8 px grid.

**Bo**
- [ ] Every Bo comes from the ported `bo.js`. No hand-drawn or redrawn Bo.
- [ ] Correct form for the size: dot < 16 px, badge 16 px, caret 18 px, face ≥ 24 px, scene 64 px.
- [ ] Mark uses the glance pose (x50/x66, y30). Face uses x38/x54, y36. Not swapped.
- [ ] One moving thing per Bo. Head still.
- [ ] No limbs. Props worn, resting or floating at Bo's side.
- [ ] No prop reuses the caret-eye shape.
- [ ] Scenes only in the crew panel (or docs). In the editor, only the caret rider and claim tag.
- [ ] Event states play once (conflict shake, merged bob), not looped.
- [ ] Offline Bo is grey, ignores the teammate colour.
- [ ] Brand Bo is ink, never a teammate colour.

**Accessibility**
- [ ] Reduced motion stops every animation and shows the key pose.
- [ ] Every meaningful Bo has `role="img"` and an `aria-label` that states the person and state (`riya, running tests`). Decorative Bos are `aria-hidden="true"`.
- [ ] Colour is never the only signal: signals have words, teammates have names.
- [ ] Text contrast ≥ 4.5:1 (use the tables in `03-color.md`).
- [ ] Animations pause when the view is hidden.

**Copy**
- [ ] Sentence case, no exclamation marks, no em dashes.
- [ ] Names the file, person and agent. No guessed model names.
- [ ] A handoff is not shown as an error.

**Verification**
- [ ] Compared side by side with `reference/component/demo.html` and `reference/screenshots/`.
- [ ] Checked at 100% and 200% zoom, dark and light editor themes.

## Do

- Let the code dominate. Keep Crewbo at the edges.
- Use words next to every signal colour.
- Keep Bo still unless something happened.
- Use the outlined lockup SVG instead of live text for the logo.
- Add a line to `README.md` → Open questions when something is not specified.

## Don't

- Don't add hands, arms, legs, a mouth or eyebrows to Bo.
- Don't draw a laptop, a barbell, a gym, code lines, checklists or anything else from `DECISIONS.md` → Rejected.
- Don't centre the logo's eyes (it becomes a pause icon).
- Don't animate explorer badges.
- Don't put scenes or large Bos inside the editor.
- Don't use a teammate colour for the brand, or a signal colour for decoration.
- Don't load fonts from Google in the product.
- Don't use `#000000`. Ink is `#0A0A0A`.
