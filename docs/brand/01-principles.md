# 01 · Design principles

Crewbo is a coordination layer for teammates whose AI coding agents work in the same repository at the same time. It sits beside the developer's editor and terminal, and is often open all day. The design has one job: show who is working on what, right now, without getting in the way of the code.

Seven principles follow from that. Each one has a test you can apply to a design or a pull request, and a list of what it rules out.

---

## P1 · Monochrome first, colour means something

The brand is black (`#0A0A0A`) and white. Grey does the hierarchy. Colour appears for two reasons only:

1. **Identity:** each teammate has one colour, used for their Bo, their claim tags and their line highlight.
2. **Signal:** claimed, synced, handoff and conflict, always next to a word or icon.

**Test:** remove all colour from the screen. Is it still fully usable? It must be.
**Rules out:** gradients, decorative colour, coloured section backgrounds, brand accent colours, colour as the only carrier of meaning.

## P2 · The code is the hero

The developer came to write code. Crewbo's UI lives at the edges: the explorer gutter, a side panel, a status bar, a toast. Inside the editor, the only Crewbo elements are a claim tag on the function line, a faint line highlight, and a tiny Bo riding an agent's caret.

**Test:** with Crewbo on, can you still read the file as easily as with it off?
**Rules out:** overlays on code, banners inside the editor, scenes or large Bos inside the editor, modal dialogs for routine events.

## P3 · Still by default, alive on events

Bo is still most of the time. Motion is how Crewbo tells you something changed, so motion that does not mean anything is noise. One moving thing per Bo. The head never moves while the eyes or a prop are animating.

**Test:** watch the screen for ten seconds while nothing happens. Only the slow ambient loops in the crew panel should move.
**Rules out:** bouncing idle avatars, spinners where Bo can show the state, more than one animation on the same Bo, head bobbing while typing.

## P4 · State lives in the eyes

Bo's eyes are text cursors. Everything Bo feels is in the eyes: where they look, how wide they open, whether they are closed. The body colour says *who*; the eyes say *what*.

**Test:** convert a screenshot to greyscale. Can you still tell idle from working from conflict from offline? You must.
**Rules out:** changing the body colour to show state (offline grey is the single exception), mouths, eyebrows, extra facial features.

## P5 · Honest and specific

Crewbo is a tool developers trust with their repository. It names the file, the person and the agent. It never hides a conflict and never shows a model or status it has not verified.

**Test:** could a teammate act on this message without asking a follow-up question?
**Rules out:** "Something went wrong", guessed model names, vague progress indicators, cheerful copy that hides a problem.

## P6 · Calm, never alarming

Two agents wanting the same file is the normal case, not an error. It is a handoff. Red is reserved for a real conflict that needs a human.

**Test:** does a blocked edit read as a question with a next step, or as a failure?
**Rules out:** red for handoffs, error sounds, shaking UI (except Bo's single shake on a real conflict), exclamation marks.

## P7 · Built to a system

Every value comes from tokens. Every Bo comes from `bo.js`. The 8 px grid, 2 px borders and three radii are the whole vocabulary.

**Test:** grep your change for hex codes, pixel values and durations that are not tokens. There should be none.
**Rules out:** one-off colours, shadows, new radii, hand-drawn Bo variants, "close enough" geometry.

---

## Hierarchy when principles conflict

1. Accessibility (contrast, reduced motion, colour not the only signal) beats everything.
2. P2, the code is the hero, beats Bo's personality.
3. P5, honesty, beats P6, calm. If something is broken, say so plainly.
