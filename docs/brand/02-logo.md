# 02 · Logo

**Status: Approved.** Direction name on the canvas: "Glance".

## The idea

The logo is Bo mid-glance: a solid circle with two short caret-shaped eyes shifted up and to the right, as if Bo just looked over at a teammate. It says three things at once: an agent (the caret eyes), awareness of others (the glance), and a friendly character (a face in two strokes).

## Construction (exact)

On a 100 × 100 viewBox:

| Element | Geometry | Fill on light | Fill on dark |
|---|---|---|---|
| Body | `circle cx=50 cy=50 r=40` | `#0A0A0A` | `#FFFFFF` |
| Left eye | `rect x=50 y=30 width=8 height=18 rx=1.5` | `#FFFFFF` | `#0A0A0A` |
| Right eye | `rect x=66 y=30 width=8 height=18 rx=1.5` | `#FFFFFF` | `#0A0A0A` |

```svg
<svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="Crewbo">
  <circle cx="50" cy="50" r="40" fill="#0A0A0A"/>
  <rect x="50" y="30" width="8" height="18" rx="1.5" fill="#FFFFFF"/>
  <rect x="66" y="30" width="8" height="18" rx="1.5" fill="#FFFFFF"/>
</svg>
```

Why these numbers:

- The eyes are **8 × 18 with a 1.5 corner radius**: flat-ended bars, read as text cursors, not pills.
- The eye gap (8) equals the eye width, so the pair reads as one glance.
- The eyes sit **high (y30) and off-centre (x50 to x74)**. A centred pair of tall bars in a circle reads as a pause icon. This placement was chosen specifically to avoid that. Never centre the logo's eyes.
- The circle fills 80% of the viewBox. The remaining 10 units on each side are built-in padding. Do not crop it.

Files: `assets/svg/logo/mark-black.svg`, `assets/svg/logo/mark-white.svg`.

## Lockup

Mark plus the wordmark `crewbo` in Instrument Sans 600, always lower case.

| Property | Value |
|---|---|
| Mark box | 64 px (the circle is 51.2 px) |
| Gap between mark box and text | 14 px |
| Wordmark | 52 px, weight 600, letter-spacing −1.6 px |
| Vertical alignment | Wordmark ink centred on the mark's centre |

Use the outlined files (`assets/svg/logo/lockup-light.svg`, `lockup-dark.svg`). They need no font. Scale them as a unit; never re-space them. The outlined wordmark alone is `wordmark-black.svg`.

## App icon

Black rounded square, white mark. Tile corner radius = 32/140 of the tile (22.86%). Mark box = 96/140 of the tile, centred. Files: `assets/png/app-icon-{128,256,512,1024}.png`.

## Clear space and minimum size

- Clear space on every side of the mark or lockup: **one eye width** (8 units at the 100-unit scale, so 5 px at 64 px).
- Minimum mark size: **16 px**. Below that, do not draw any Bo: use the teammate colour dot (see `06-bo-character.md`) or the word "crewbo".
- Minimum lockup width: **80 px**.

## Colour

Only two versions exist: black on light, white on dark. No colour versions of the logo. Teammate colours are for teammates' Bos, never for the brand mark. The brand Bo is always black (or white when reversed).

## Misuse

Do not:

- centre the eyes, or use the animated face geometry (eyes at x38/x54) as the logo
- change eye size, gap or radius
- put the logo in a teammate colour, gradient, or outline-only style
- add a drop shadow, glow, border, or a speech bubble
- add limbs, glasses or props to the logo (props exist only in crew panel scenes)
- stretch, rotate or crop the mark
- set the wordmark in capitals or title case ("Crewbo" is correct in running text; the wordmark itself is `crewbo`)
- use the superseded two-halves mark from the old `Landing` board

## In running text

Write the product name as **Crewbo**. The CLI and package name is `crewbo`. The mascot is **Bo**.
