# Brand decision log

Each entry records what was decided, what was rejected, and why. Rejected options are listed so they are not reintroduced. To change a decision, add a new entry that supersedes the old one; do not edit history.

Format: `BDR-NNN · title · date · status`.

---

## BDR-001 · Monochrome brand · 2026-10-11 · Accepted

**Decision:** The brand is black and white in the spirit of Vercel and GitHub. Colour exists only for teammate identity and product signals.
**Rejected:** Round 1 colour directions on the canvas (`Split ring`, `Lanes`, `Buddy`).
**Why:** The product lives beside code editors with their own syntax colours. A monochrome brand never clashes with a theme and leaves colour free to mean "who" and "what state".

## BDR-002 · Logo: the Glance mark · 2026-10-11 · Accepted

**Decision:** A solid circle with two caret eyes shifted up and right (`02-logo.md`).
**Rejected:** The other two mono options on the `Mono agent marks` board; the earlier two-halves mark used on the old `Landing` board.
**Why:** Caret eyes say "agent that writes code"; the glance says "aware of teammates". It works at 16 px and as a GitHub avatar.
**Check done:** Informal existence check found no identical mark. This is not a legal clearance. Before trademark filing, run an image search on the WIPO Global Brand Database, IP India, and Google Lens / TinEye.

## BDR-003 · Eyes high and off-centre · 2026-10-11 · Accepted

**Decision:** Eyes are short (18 units), flat-ended, set high, and in the logo, off-centre. Below 16 px, no Bo; use a colour dot. The 16 px badge uses 9 × 19 eyes and a ring.
**Rejected:** Tall centred bars.
**Why:** At 16 px in the file explorer, two tall bars in a circle read as a pause button.

## BDR-004 · Mascot name: Bo · 2026-10-11 · Accepted

**Decision:** The mascot is Bo, from crew**bo**. Each teammate's agent is shown as their own Bo in their colour.
**Why:** Short, friendly, pronounceable in any language, and tied to the product name.

## BDR-005 · Eight teammate colours · 2026-10-11 · Accepted

**Decision:** Cobalt, Teal, Moss, Ember, Ochre, Rose, Violet, Indigo, assigned in join order, each holding white eyes at 4.5:1 or better.
**Why:** Distinct hues at similar weight; all pass contrast with white eyes; the brand Bo stays ink.

## BDR-006 · State lives in the eyes · 2026-10-11 · Accepted

**Decision:** Seven face states (idle, working, nearby, handoff, conflict, merged, offline) shown only through the eyes, plus a body bob or shake where noted. Offline is the only state that changes body colour (grey).
**Why:** Works in greyscale, works at small sizes, keeps the teammate colour stable.

## BDR-007 · In the editor, Bo rides the caret · 2026-10-11 · Accepted

**Decision:** In code, an 18 px Bo moves word by word with an agent's caret and hops line by line, with a line highlight. Scenes stay in the crew panel.
**Rejected:** Scene animations inside the editor.
**Why:** The code is the hero. The caret is the natural place for an agent's presence.

## BDR-008 · No limbs · 2026-10-11 · Accepted

**Decision:** Bo never has hands or arms. Props are worn, rest on a surface, or float at Bo's side.
**Rejected:** Several versions with hands on a keyboard and arms holding objects.
**Why:** The hands never looked genuine and made Bo worse. A limbless Bo is simpler, more iconic and easier to keep consistent.

## BDR-009 · Writing scene: rectangle glasses and keyboard · 2026-10-11 · Accepted

**Decision:** Bo wears black rectangle glasses and faces a keyboard in perspective with the spacebar row on Bo's side. Only the keys move.
**Rejected:**
- Laptop facing the viewer; laptop seen from the back of the lid; laptop in side view (three attempts, none read correctly)
- Floating code lines; a checklist
- An oversized keyboard
- Keyboard with the spacebar toward the viewer (wrong: Bo is the one typing)
- Grey glasses in the same shape as the eyes
- Six other black frame styles: round, browline, square-top, aviator, hexagon, half-moon
**Why:** Glasses mean "focused at work" without limbs. Rectangles, wider than tall, never echo the caret eyes. A keyboard oriented toward Bo is physically correct.

## BDR-010 · Testing scene: flask held high · 2026-10-11 · Accepted

**Decision:** A small Erlenmeyer flask held up at Bo's eye level, no ground line, swirling; liquid turns green and a vapour puff rises on pass. Bo is larger (r32) and sits low-left.
**Rejected:** A floating barbell and a gym or workout scene for "working"; an oversized flask; a flask on the ground with a ground line.
**Why:** A flask reads as testing. Holding it high mirrors how people hold a flask to the light. Real proportions keep Bo the subject.

## BDR-011 · Idle scene: reading a book · 2026-10-11 · Accepted

**Decision:** An open book lying flat on the desk, drawn as two perspective trapezoids, with a page turning every 6 s.
**Why:** Idle should feel calm and present. The page turn gives life without moving Bo.

## BDR-012 · Offline: asleep · 2026-10-11 · Accepted

**Decision:** Grey Bo, closed curved eyes, slow breath, two z's drifting up.
**Why:** Universally readable, gentle, and clearly different from idle.

## BDR-013 · Still head, one motion · 2026-10-11 · Accepted

**Decision:** One moving thing per Bo. The head does not move while eyes or props animate.
**Rejected:** Earlier scenes where the head bobbed while typing or lifting.
**Why:** Too much head motion was distracting in a panel that is open all day.

## BDR-014 · Type: Instrument Sans and JetBrains Mono · 2026-10-11 · Accepted

**Decision:** Instrument Sans for everything except code and labels, which use JetBrains Mono. Both OFL, self-hosted.
**Why:** Instrument Sans is neutral, modern and slightly warm, close to the Vercel and GitHub feel without copying their fonts. JetBrains Mono is familiar to developers.

## BDR-015 · Interface primitives · 2026-10-11 · Accepted

**Decision:** 2 px borders, radii 12 / 20 / 999, 8 px grid, no shadows, no gradients.
**Why:** A small, strict vocabulary keeps every surface consistent and calm.

## BDR-016 · Tertiary text grey on dark: gray.450 · 2026-10-11 · Accepted

**Decision:** Add `color.gray.450` = `#858585` for tertiary text on dark surfaces: the crew panel header, offline crew rows, explorer labels and captions. `gray.500` (`#737373`) stays for the offline Bo body on light, mockup syntax comments and the book spine, and is no longer used for text on dark.
**Rejected:** Keeping `#737373` (3.78:1 on `#171717`, 4.04:1 on `#0F0F0F`, below WCAG AA 4.5:1 for 12 to 13 px text). Using `gray.400` (`#A3A3A3`, passes at 7.11:1, but is the status-line grey, so offline rows would no longer look dimmer).
**Why:** Accessibility beats everything (principles, hierarchy rule 1). `#858585` passes on every dark surface (4.86:1 on `#171717`, 4.99:1 on `#141414`, 5.19:1 on `#0F0F0F`, 5.37:1 on `#0A0A0A`) and stays visibly darker than `#A3A3A3`.

## BDR-017 · Show only verified model names · 2026-10-11 · Accepted

**Decision:** The crew panel shows a model only when Crewbo has read it. Codex: the `model` field in hook events. Claude Code sessions launched by the daemon: the `model` in the first `system/init` event of `--output-format stream-json`, shown as the raw ID (for example `claude-opus-5-5`). Claude Code sessions started by a teammate: `Claude Code` only, because hooks carry no model.
**Rejected:** Mapping model IDs to marketing names (a lookup table goes stale and would guess); inferring the model from user settings.
**Why:** P5, honest and specific. Phase 0 (`docs/research/phase0-findings.md`) captured no `model` in any Claude Code hook payload, while the `init` event of a daemon-launched run reported `claude-opus-5-5`.

## BDR-018 · Initials as the VS Code explorer badge · 2026-10-18 · Accepted

**Decision:** In VS Code's explorer, a claimed file shows the holder's first two letters as plain text, uncoloured, with the holder named in full in the tooltip (two or more holders: their count). The status bar shows the word `crewbo` without the mark.
**Rejected:** A bare coloured dot (`03-color.md`: never a bare dot that could be read either way); a custom tree view or icon font to draw the 16 px badge Bo, too much for v1.
**Why:** VS Code's file decoration API holds at most two characters of text and a theme colour, not an image, so the badge Bo in `05-interface.md` §3.1 cannot be drawn there. Initials still name the person (P5) and work without colour (P1). Ronel, M5-05.

## BDR-019 · Crew panel in VS Code · 2026-10-19 · Accepted

**Decision:** The crew panel is a webview in its own activity bar container (`Crewbo`, icon `assets/svg/logo/mark-cutout.svg`: the mark with its eyes cut out of the body, same geometry as `markSVG` in `bo.js`, because VS Code draws activity bar icons as a one-colour mask and `mark-black.svg` came out as a plain dot). It loads `bo.js`, `bo.css` and `tokens.css` themselves, copied unchanged into the extension at build time, and the self-hosted fonts. Text sizes are the ones `05-interface.md` §3.5 names, colours come from tokens, and light themes use the specified fallback (§5). Rows show no model (BDR-017) and no testing scene: the daemon does not yet tell a test run from other shell commands. VS Code's `workbench.reduceMotion` set to `on` stops Bo's motion like `prefers-reduced-motion`.
**Rejected:** Redrawing Bo in the extension's own code. The spec's right-hand placement is left to the member, who can drag the view to VS Code's secondary side bar; whether an extension can put it there by default was not checked.
**Why:** P7, built to a system: Bo comes from `bo.js` alone. M7-03.

## BDR-020 · Claim badges and claim tags in VS Code · 2026-10-19 · Accepted

**Decision:** Claim rows in the Crewbo view show the holder's 16 px badge Bo (`badgeSVG` in `bo.js`, generated at build time, no ring because tree rows never stack badges). A file another member holds gets the claim tag of §3.2 at the end of its first line, `riya · Codex` (or only the name for a terminal claim), as an `after` decoration in the teammate `tint` (`base` on light themes) with a 1 px `base` border, radius 999 and 13 px text.
**Rejected:** Placing the tag on a function's line: claims are per file until symbol claims (v2). Loading Instrument Sans into the editor: VS Code decorations cannot load fonts, so the tag uses the editor's font.
**Why:** §3.1 and §3.2, within what VS Code's tree and decoration APIs can draw (BDR-018 covers the explorer). M7-04.

## BDR-021 · The mark in the Crewbo IDE · 2026-10-20 · Accepted

**Decision:** The Crewbo IDE shows the mark wherever Code OSS showed its logo: the title bar and the Welcome tab use `mark-black.svg` on light themes and `mark-white.svg` on dark and high-contrast dark themes, unchanged. The empty-editor watermark (Code OSS's "letterpress", up to 256 px) is the same two files drawn faint with CSS opacity: 10% on light themes, 30% on dark and high-contrast themes, chosen from screenshots of the IDE so the mark recedes behind the shortcut list (Code OSS's grey watermark was fainter on light themes). The files stay black and white; only the watermark is drawn faint.
**Rejected:** A full-strength 256 px mark in every empty editor (too loud for a background element); no watermark at all; a grey or tinted copy of the mark (§02 allows only the black and white versions, so no new file is drawn).
**Why:** The IDE must not show VSCodium's or Microsoft's logo (ADR 0016), and a watermark only works if it recedes. Ronel, M8-03.

## BDR-022 · A display serif for the website · 2026-10-21 · Accepted

**Decision:** The website's headlines (the hero and section titles) use **Instrument Serif** 400 (`font.family.display`, `type.display` 72 px and `type.displayH2` 52 px), self-hosted from `assets/fonts/` (Fontsource 5.3.0, SIL OFL 1.1). Everything else, and all product UI (extension, IDE, dashboard, CLI), stays Instrument Sans and JetBrains Mono.
**Rejected:** A serif in the product (the code is the hero, P2); a display face from another family (Instrument Serif is the companion of Instrument Sans, so the two sit together); loading it from Google Fonts.
**Why:** Ronel wanted the site to read like the YC launches he pointed to (tryopentag.com, inkbox.ai), which set headlines in a distinctive display face. Ronel, crewbo/website#3.

## BDR-023 · The spelled Bo wordmark in the website footer · 2026-10-21 · Accepted

**Decision:** The website footer may spell `crewbo` in the brand mark (`markSVG`, ink, 32 px marks on a 24 px grid, a 5 × 7 letter grid), beside the mark at 168 px. Only on the website footer and only at 1200 px wide or more; below that, the mark alone. Hovering one mark shows a teammate's face Bo; hovering the row turns the large mark into Bo (ink) for one merged bob.
**Rejected:** Spelling it in teammate colours at rest (the brand mark is always ink); using it as a logo anywhere else.
**Why:** Ronel's idea after heyclicky.com's footer, which spells the name in its own icons; shaped with him in crewbo/website#3.

## BDR-024 · Gradient light and a keyword colour in the launch film · 2026-10-25 · Accepted

**Decision:** The launch film (and its cut-downs) may light its backgrounds with soft gradients built from Cobalt and Teal (`base` and `tint`) on paper or ink, and may set one keyword per line in Cobalt (`#1D4ED8` on light, its `tint` `#93C5FD` on dark). Everything else in the film follows the brand: the logo is the black or white mark and lockup, unchanged; Bo comes from `bo.js`; product UI is shown as it is.
**Rejected:** Gradients or an accent colour in the product UI, the website or the docs (05-interface.md stands there); a coloured, outlined or glowing logo (02-logo.md, Misuse).
**Why:** Every launch film Ronel chose as a reference (references in the film project: Numtera, LangEase, Google Vids and five more) lights its scenes with gradients and colours one word in a headline, and flat fields read as bland next to them. Ronel, for the launch film only.
