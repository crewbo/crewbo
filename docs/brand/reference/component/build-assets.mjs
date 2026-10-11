// Regenerates docs/brand/assets/svg/{states,scenes,badges,logo/mark-*} from bo.js + bo.css.
// Usage (from repo root):  node docs/brand/reference/component/build-assets.mjs
// Animated SVGs embed bo.css so they play standalone (browser, GitHub README).
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { markSVG, boSVG, badgeSVG, sceneSVG, caretSVG, TEAM_COLORS, INK, PAPER } from './bo.js';

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, '..', '..', 'assets', 'svg');
const css = readFileSync(join(here, 'bo.css'), 'utf8')
  .replace(/\/\*[\s\S]*?\*\//g, '')
  .replace(/\s+/g, ' ')
  .trim();

const withStyle = (svg) => svg.replace(/(<svg[^>]*>)/, `$1<style>${css}</style>`);
const write = (rel, data) => {
  const p = join(out, rel);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, data + '\n');
};

// Logo masters (static).
write('logo/mark-black.svg', markSVG({ size: 100, body: INK, eyes: PAPER }));
write('logo/mark-white.svg', markSVG({ size: 100, body: PAPER, eyes: INK }));

// Face states: animated (default ink) + static key poses.
const states = ['idle', 'working', 'nearby', 'handoff-right', 'handoff-left', 'conflict', 'merged', 'offline'];
for (const s of states) {
  write(`states/bo-${s}.svg`, withStyle(boSVG({ state: s, size: 100 })));
  write(`states/static/bo-${s}.svg`, boSVG({ state: s, size: 100, static: true }));
}

// Scenes: animated + static, one per default teammate colour used in the mockups.
const sceneColor = { writing: '#1D4ED8', testing: '#0F766E', reading: '#C2410C', offline: INK };
for (const [scene, color] of Object.entries(sceneColor)) {
  write(`scenes/scene-${scene}.svg`, withStyle(sceneSVG({ scene, color, size: 100 })));
  write(`scenes/static/scene-${scene}.svg`, sceneSVG({ scene, color, size: 100, static: true }));
}

// Badges + caret riders for all 8 teammate colours.
for (const c of TEAM_COLORS) {
  const n = c.name.toLowerCase();
  write(`badges/badge-${n}.svg`, badgeSVG({ color: c.base }));
  write(`badges/caret-${n}.svg`, withStyle(caretSVG({ color: c.base })));
  write(`states/teammates/bo-idle-${n}.svg`, withStyle(boSVG({ state: 'idle', color: c.base, size: 100 })));
}
console.log('assets written to', out);
