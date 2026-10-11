# 06 · Bo, the character

**Status: Approved.** Source of truth for geometry: `reference/component/bo.js`. Every number below is copied from it. Reference image: `reference/screenshots/bo-character-sheet.png`.

## 1. Who Bo is

Every teammate in a Crewbo room gets a Bo. Bo stands for that person's coding agent. Bo is a solid circle whose two eyes are text cursors. Bo has no mouth, no limbs and no other features. Everything Bo does is shown through the eyes and, in the crew panel only, through one prop.

Personality: attentive, calm, quietly friendly. Bo notices teammates, does not panic, and is pleased (not ecstatic) when work merges cleanly.

## 2. The Bo rules

These are non-negotiable. Each came from a rejected design (see `DECISIONS.md`).

1. **No limbs.** Bo never has hands, arms or legs. Props are worn (glasses), rest on a surface (keyboard, book), or float at Bo's side (flask).
2. **Still head.** One moving thing per Bo. While eyes or a prop animate, the body does not move. The only exceptions are the whole-body bob (handoff, merged), the single conflict shake, and the offline breath, which are each the *only* motion at that time.
3. **Real proportions.** Bo is the subject. Props are drawn at believable real-world size relative to Bo, never larger than Bo's head.
4. **Shapes stay distinct.** No prop or accessory reuses the tall-bar caret shape of Bo's eyes. The glasses are wider than tall for this reason.
5. **Scenes live in the panel.** In the editor, Bo only rides the caret. Scenes (keyboard, flask, book, sleep) appear only in the crew panel and in docs or marketing.
6. **Below 16 px there is no Bo.** Draw an 8 px colour dot instead.
7. **Props face Bo.** Anything Bo uses is oriented toward Bo, as it would be in real life. The keyboard's spacebar row is on Bo's side.
8. **Grounded or held, never floating oddly.** A desk line appears under objects that rest on a desk. A held object (the flask) has no ground line.

## 3. The five forms of Bo

| Form | Size | Animated? | Where | Function in `bo.js` |
|---|---|---|---|---|
| Mark | ≥ 16 px | Never | Logo, app icon, status bar, avatar | `markSVG` |
| Face | ≥ 24 px (32, 64, 96 typical) | Yes, by state | Toasts, notifications, crew panel event states, docs | `boSVG` |
| Badge | 16 px exactly | Never | Explorer file rows | `badgeSVG` |
| Caret rider | 18 px | Blink; position follows caret | Editor, next to an agent's caret | `caretSVG` |
| Scene | 64 px panel, 150 px docs | Yes, one loop | Crew panel rows | `sceneSVG` |
| Dot | 8 px | Never | Anything under 16 px (tabs, minimap, dense lists) | `dotSVG` |

All Bo drawings use a 100 × 100 viewBox.

## 4. Face geometry

| Part | Geometry | Fill |
|---|---|---|
| Body | `circle cx=50 cy=50 r=40` | teammate `base` (ink for the brand Bo) |
| Left eye | `rect x=38 y=36 w=8 h=18 rx=1.5` | `#FFFFFF` |
| Right eye | `rect x=54 y=36 w=8 h=18 rx=1.5` | `#FFFFFF` |

The face's resting eyes are centred a little high (y36 to y54 against a centre of 50). The mark uses a different, glancing pose (x50/x66, y30). Do not mix them up: the mark is a still photo of Bo glancing; the face is the live character.

### 4.1 States

| State | Eyes | Body | Meaning |
|---|---|---|---|
| **idle** | Rest, occasional glance right then left (±9), blink | Still | Online, nothing happening |
| **working** | Dropped 8 units, scanning ±7 horizontally | Still | Agent is reading or editing |
| **nearby** | Held 9 units right, blink | Still | Another agent is working in a related file |
| **handoff** | Held 9 units toward the other person (`handoff-right` or `handoff-left`), blink | Soft bob 3 units | Ownership is moving between two people. Always shown as a pair facing each other |
| **conflict** | Grow to 1.4× height (about centre) and hold | One shake (±3) | Two edits collided and need a human |
| **merged** | Replaced by two upward chevrons: `M36 56L41 46L46 56` and `M54 56L59 46L64 56`, stroke 6, round caps and joins, white | Bobs twice | A clean merge landed |
| **offline** | Closed: `M30 54Q36 61 42 54` and `M50 54Q56 61 62 54`, stroke 4.5, round caps | Grey, `circle cx=46 cy=54 r=36`, slow breath; two `z` glyphs (Instrument Sans 700, 18 and 13 units) at (78,26) and (88,14) drifting up | Daemon not connected |

Offline body: `#737373` with z in `#525252` on light; `#525252` with z in `#A3A3A3` on dark.

## 5. Badge and caret geometry

**Badge (16 px):** `circle cx=50 cy=50 r=44` in `base`, stroke 8 in the row background colour (for stacking); eyes `rect 9 × 19 rx=1.5` at x50 and x66, y30 (the mark pose, one unit heavier so it survives at 16 px). The heavier eyes and the ring are deliberate: at 16 px the face pose read as a pause button.

**Caret rider (18 px):** face geometry, eyes held 9 units right, blinking.

**Dot:** `circle r=4` in `base` on an 8 × 8 box.

## 6. Crew panel scenes

All four are drawn for the dark panel. Colours for props are in `03-color.md` §5.

### 6.1 Writing code: glasses and keyboard

The agent is editing files. Bo wears black rectangle glasses and looks down at a keyboard on the desk in front of it.

| Part | Geometry |
|---|---|
| Body | `circle cx=50 cy=42 r=30`, teammate `base` |
| Eyes | `rect 6 × 8 rx=1.2` at (40,45) and (55,45), white, blink. Shorter than the face eyes so they sit inside the lenses |
| Lenses | `rect 15.5 × 12 rx=1.2` at (34.5,43) and (51,43), no fill, stroke `#0A0A0A` 2.6 |
| Bridge | `M50 46.5H51`, stroke 2.4 |
| Temples | `M34.5 46L20.5 43` and `M66.5 46L79.5 43`, stroke 2.4, round caps (they run back to the edge of the head) |
| Keyboard | `polygon 31,76 69,76 77,90 23,90`: narrow far edge at the top (toward Bo), wide near edge at the bottom. Fill `#262626`, stroke `#525252` 1 |
| Desk | `rect x=10 y=90 w=80 h=3 rx=1.5`, `#404040` |
| Key rows | Row 1 (y78, nearest Bo): small key, **spacebar** `x=39 w=22`, small key. Row 2 (y82): seven keys 5.4 wide. Row 3 (y86): seven keys 6.2 wide. All `#404040`, rx 0.8 |
| Motion | Six keys flash `#E5E5E5` in a staggered 1.4 s step loop. **Only the keys move.** |

Why: the glasses say "focused at work" without limbs. Rectangles (wider than tall) keep them from echoing the caret eyes. The spacebar sits on Bo's side because Bo is the one typing.

### 6.2 Running tests: the flask

The agent is running tests. Bo holds a small Erlenmeyer flask up at eye level and swirls it. No ground: it is held, not set down.

| Part | Geometry |
|---|---|
| Body | `circle cx=38 cy=56 r=32` (sits low and left, leaving room for the flask) |
| Eyes | `rect 6 × 12 rx=1.2` at (45,40) and (57,40), looking up-right at the flask, blink |
| Flask group | Everything below is inside `translate(0 -28)`, so the flask floats at Bo's eye level, to Bo's right |
| Neck | `rect x=79 y=60 w=6 h=7 rx=1`, fill `#262626`, stroke `#A3A3A3` 1.2 |
| Bowl | `M79 66L74.5 79.5Q73.5 84 77.5 84H86.5Q90.5 84 89.5 79.5L85 66Z`, same fill and stroke, round joins |
| Liquid | `M76.2 75H87.8L89.3 79.7Q90.2 83.2 86.6 83.2H77.4Q73.8 83.2 74.7 79.7Z`, `#2DD4BF` |
| Bubbles | Three circles r 0.9 to 1.1 at (80,79), (82.5,80.5), (84,78), `#CCFBF1`, rising 17 units |
| Vapour | Two puffs at (82,28) r2.2 and (84.5,25) r1.6, `#A3A3A3`, outside the swirl group |
| Motion | Flask swirls around the neck (pivot 82,34 in viewBox space); liquid sloshes, then turns `#4ADE80` (pass) and a puff of vapour rises. Loop 4 s. |

Why: a flask is a common symbol for "tests". It is small (about one third of Bo's height) because it is a real hand-held object. The colour change gives the pass moment.

Product note (**Specified**): if tests fail, the liquid should not turn green. Until a fail variant is designed, show the swirl without the colour change and puff, and say `Tests failed` in the status line.

### 6.3 Idle: reading a book

Online, no agent activity. Bo reads an open book lying on the desk; a page turns every few seconds.

| Part | Geometry |
|---|---|
| Body | `circle cx=50 cy=40 r=30` |
| Eyes | `rect 6 × 11 rx=1.2` at (41,36) and (53,36), drifting ±4 slowly, blink |
| Left page | `M14 90L28 74H50V90Z`, `#F5F5F5`, text lines `M30 79H43M25 84H42` stroke `#A3A3A3` 1.6 |
| Right page | `M86 90L72 74H50V90Z`, same, lines `M57 79H70M58 84H75` |
| Turning page | `M50 74H72L86 90H50Z` plus its lines, hinged on the spine (x50). Flips from scaleX 1 to −1, shading to `#C7C7C7` mid-turn |
| Spine | `M50 74V90`, `#737373` 1.5 |
| Desk | `rect x=8 y=90 w=84 h=4 rx=2`, `#404040` |

Why: idle should feel restful and present, not empty. A book lying flat in perspective (trapezoid pages) reads as a real book on a desk.

### 6.4 Offline: asleep

| Part | Geometry |
|---|---|
| Body | `circle cx=46 cy=54 r=30`, `#525252` |
| Closed eyes | `M34 54Q39 59 44 54` and `M50 54Q55 59 60 54`, stroke white 3.5, round caps |
| z glyphs | Instrument Sans 700, 16 units at (74,30) and 12 units at (84,18), `#A3A3A3`, drifting up and fading |
| Motion | Body breathes (scale 1.03) on a 3.6 s cycle |

## 7. Adding a new scene or state

Do not add one without a decision record. A proposal must:

1. Pass all eight Bo rules in §2.
2. Have exactly one moving element.
3. Use a prop that is recognisable at 64 px on `#171717`.
4. Ship as a function in `bo.js`, an entry in `bo.css`, a static key pose, and a reduced-motion pose.
