# 07 · Bo motion

**Status: Approved** for keyframes and loop timings (copied from the approved canvas). Product behaviour rules (play once vs loop, triggers) are **Specified**. Implementation: `reference/component/bo.css`.

## 1. Motion principles

1. **Still by default.** In a quiet room, the only motion on screen is the slow ambient loop of each crew panel scene and occasional blinks.
2. **One moving thing per Bo.** Never combine two motions on the same Bo (for example, a bob while the eyes scan).
3. **Motion means an event.** State changes are the reason to move. Reactions stay short: UI transitions 200 ms, never more than 300 ms.
4. **CSS only.** All loops are CSS keyframes on SVG elements. No JavaScript animation loops, no canvas, no Lottie. The only JS-driven motion is the caret rider's position.
5. **Reduced motion is a first-class mode**, not an afterthought (§7).

## 2. Transform setup (required)

Every animated element must use its own box as the transform reference, or rotations and scales pivot around the SVG origin and fly off.

```css
.bo g, .bo rect, .bo path, .bo circle, .bo polygon, .bo text {
  transform-box: fill-box;
  transform-origin: center;
}
```

Three overrides matter. Use selectors at least as specific as `.bo .x`, or the generic rule above wins (this bug happened on the canvas):

| Class | transform-box | transform-origin | Why |
|---|---|---|---|
| `.bo-swirl` | `view-box` | `82px 34px` | Flask pivots at its neck, in viewBox units |
| `.bo-slosh` | `fill-box` | `50% 100%` | Liquid skews from the bottom of the bowl |
| `.bo-page` | `fill-box` | `0% 50%` | Page hinges on its left edge, which is the spine |

## 3. Face state animations

| Class | Keyframes | Duration · easing · repeat | Used by |
|---|---|---|---|
| `bo-blink` | scaleY 1 → 0.08 at 48% → 1 (closed 44% to 52%) | 4.2 s · linear · infinite | idle, nearby, handoff, caret, scenes |
| `bo-idle` | x 0 (0–30%) → +9 (36–56%) → −9 (62–78%) → 0 (84–100%) | **10 s** product (6 s on canvas) · ease-in-out · infinite | idle |
| `bo-read` | translate(−7, 8) ↔ (7, 8) | 2.4 s · ease-in-out · infinite | working |
| `bo-look-r` / `bo-look-l` | static translateX(±9) | none | nearby, handoff, caret |
| `bo-bob` | translateY 0 → −3 → 0 | 1.6 s · ease-in-out · infinite while pending | handoff |
| `bo-bob-twice` | same | 1.6 s × **2**, then return to idle | merged |
| `bo-wide` | scaleY 1 → 1.4 | **300 ms ease-out, then hold** (canvas loops 3 s) | conflict |
| `bo-shake-once` | x 0, −3, +3, −3, +2, 0 | **540 ms, once** (canvas repeats every 3 s) | conflict |
| `bo-breathe` | scale 1 → 1.03 → 1 | 3.6 s · ease-in-out · infinite | offline |
| `bo-z1` / `bo-z2` | from (0, 6) opacity 0 → 30% opacity 1 → (6, −12) opacity 0 | 2.4 s · ease-out · infinite, z2 delayed 1.2 s | offline |

**Canvas versus product.** The approved boards loop every state so reviewers can see it. The product does not loop event states:

- conflict: shake once, eyes widen and stay wide until resolved
- merged: two bobs, then idle
- idle glance: every 10 s, not 6 s

## 4. Scene animations

| Scene | Class | Keyframes | Duration · easing |
|---|---|---|---|
| Writing | `bo-key` + `bo-k1…k6` | fill `#404040` → `#E5E5E5` at 8% → `#404040` at 16% | 1.4 s · `steps(1, end)` · infinite. Delays: k1 0, k2 −0.35, k3 −0.8, k4 −0.2, k5 −1.0, k6 −0.6 s |
| Testing | `bo-swirl` | rotate 0 → −12° (15%) → 9° (30%) → −7° (45%) → 0 (60–100%) | 4 s · ease-in-out |
| | `bo-slosh` | skewY 0 → 10 → −8 → 6 → 0 (60%); fill `#2DD4BF` to 60%, `#4ADE80` 72–92%, back to `#2DD4BF` | 4 s · ease-in-out |
| | `bo-p1`, `bo-p2` | hidden to 66%; 72% opacity 0.9; 92% translate(2, −10) scale 1.4 opacity 0 | 4 s · ease-out; p2 delayed 0.25 s |
| | `bo-fb1…3` | translate(0, 0) opacity 0 → 20% opacity 1 → (0.5, −17) opacity 0 | 2 s · ease-in; delays 0, 0.7, 1.4 s |
| Reading | `bo-look-down` | x −4 (0–40%) → +4 (50–90%) → −4 | 5 s · ease-in-out |
| | `bo-page` | scaleX 1 (0–70%) → 0.02 (80%) → −1 (90–100%) | 6 s · ease-in-out |
| | `bo-leaf` | fill `#F5F5F5` (0–72%) → `#C7C7C7` (80%) → `#F5F5F5` (88–100%) | 6 s · ease-in-out |
| Offline | `bo-breathe`, `bo-z1`, `bo-z2` | as in §3 | as in §3 |

Product rule for testing (**Specified**): the green-and-puff part of the loop represents a pass. Drive it from the real result if the adapter reports one. Until then, run the loop as designed while tests are running.

## 5. Transitions between states

- Swap the SVG with a 200 ms opacity crossfade (`bo-swap-enter` classes in `bo.css`). No morphing between shapes.
- Do not restart a loop when the state has not changed (React and Svelte keys must be stable per member and state).
- Debounce activity changes by 1 s so a fast edit, test, edit sequence does not flicker. **Specified.**

## 6. Caret ride (editor)

Bo rides an agent's caret while it writes. Approved behaviour from the `Bo in the editor` board: **Bo moves word by word along the line, then hops to the next line**, with the line highlight moving with it.

Algorithm (reference implementation in `demo.html`):

1. On each text change from a teammate's agent, find the insertion point after the change (line, column).
2. Place the 18 px caret Bo at `x = textStart + column × charWidth + 2 px`, `y = lineTop + (lineHeight − 18) / 2`. In VS Code, use a decoration anchored at the position (`after` content with an SVG data URI) rather than measuring pixels.
3. **Same line:** jump instantly (no easing). Text arrives in words or chunks, so Bo steps.
4. **New line:** move with a 120 ms ease-out (`.is-newline`).
5. Move the line highlight to the same line.
6. Coalesce updates to at most one per animation frame. If an agent writes a large block at once, place Bo at the end of the block; do not replay it word by word.
7. When the agent stops writing for 2 s, keep Bo at the last position with blink only. Remove it when the claim is released. **Specified.**

On the canvas this was faked with CSS `steps()` and `ch` units (`pen-a`, `pen-b`, `t-a12…` keyframes in `reference/canvas/bo-in-editor.html`). That is a demo technique only. The product follows real edits.

## 7. Reduced motion

When `prefers-reduced-motion: reduce` is set (or the VS Code setting `workbench.reduceMotion` is `on`, or Crewbo's own `crewbo.ui.reduceMotion` setting is on):

- All Bo animations stop: `.bo * { animation: none !important; transition: none !important; }`.
- Each Bo shows its **key pose**: idle and nearby with eyes at rest or held; working with eyes dropped 8 units and centred; conflict with eyes at 1.4× height; merged with chevrons; offline with closed eyes and both z's visible; scenes with the flask upright and teal, the page unturned and keys unlit.
- The caret rider jumps directly to the latest position. The line highlight does not slide.
- Static key poses are pre-rendered in `assets/svg/states/static/` and `assets/svg/scenes/static/`, and `bo.js` produces them with `{ static: true }`.

## 8. Performance

- Pause every Bo animation when its view is hidden (`animation-play-state: paused` when the webview is not visible or the window loses focus).
- Never animate layout properties. Only `transform`, `opacity` and SVG `fill`.
- A crew panel with 8 members must stay under 2% CPU on an idle laptop. If it does not, reduce the scene loops to the visible rows only.
