// Crewbo · Bo reference component (framework-free ES module)
//
// This file is the geometric source of truth for Bo. Every SVG in
// docs/brand/assets/svg was generated from it (see build-assets.mjs).
// Port it to the product's UI stack (VS Code webview, web dashboard)
// without changing any number. Spec: docs/brand/06-bo-character.md.
//
// All functions return an SVG string on a 100 x 100 viewBox.
// Pair with bo.css for motion.

/** Brand ink and paper. */
export const INK = '#0A0A0A';
export const PAPER = '#FFFFFF';

/**
 * Teammate colours, assigned in this order as people join a room.
 * base: Bo's body. tint: text/outline on dark editor themes.
 * White eyes on every base are >= 4.9:1 (WCAG AA for graphics and text).
 */
export const TEAM_COLORS = [
  { name: 'Cobalt', base: '#1D4ED8', tint: '#93C5FD' },
  { name: 'Teal',   base: '#0F766E', tint: '#5EEAD4' },
  { name: 'Moss',   base: '#15803D', tint: '#86EFAC' },
  { name: 'Ember',  base: '#C2410C', tint: '#FDBA74' },
  { name: 'Ochre',  base: '#A16207', tint: '#FDE047' },
  { name: 'Rose',   base: '#BE185D', tint: '#F9A8D4' },
  { name: 'Violet', base: '#7E22CE', tint: '#D8B4FE' },
  { name: 'Indigo', base: '#4338CA', tint: '#A5B4FC' },
];

/** Deterministic colour for the nth member to join (0-based). Wraps after 8. */
export function teamColor(joinIndex) {
  return TEAM_COLORS[((joinIndex % TEAM_COLORS.length) + TEAM_COLORS.length) % TEAM_COLORS.length];
}

const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

function open(size, title, extraClass = '') {
  const cls = ['bo', extraClass].filter(Boolean).join(' ');
  const a11y = title ? `role="img" aria-label="${esc(title)}"` : 'aria-hidden="true"';
  return `<svg xmlns="http://www.w3.org/2000/svg" class="${cls}" viewBox="0 0 100 100" width="${size}" height="${size}" ${a11y}>`;
}

// ---------------------------------------------------------------------------
// 1. The mark (logo). Static. Bo mid-glance to the right.
//    Body r40 at (50,50). Eyes 8x18, rx1.5, at x50 and x66, y30.
// ---------------------------------------------------------------------------
/**
 * @param {{size?:number, body?:string, eyes?:string, title?:string}} o
 */
export function markSVG({ size = 100, body = INK, eyes = PAPER, title = 'Crewbo' } = {}) {
  return open(size, title).replace(' class="bo"', '') +
    `<circle cx="50" cy="50" r="40" fill="${body}"/>` +
    `<rect x="50" y="30" width="8" height="18" rx="1.5" fill="${eyes}"/>` +
    `<rect x="66" y="30" width="8" height="18" rx="1.5" fill="${eyes}"/>` +
    `</svg>`;
}

// ---------------------------------------------------------------------------
// 2. Face Bo: the live-state avatar (>= 24 px).
//    Body r40 at (50,50). Resting eyes 8x18, rx1.5, at x38 and x54, y36.
// ---------------------------------------------------------------------------
const EYES = (fill = PAPER) =>
  `<rect x="38" y="36" width="8" height="18" rx="1.5" fill="${fill}"/>` +
  `<rect x="54" y="36" width="8" height="18" rx="1.5" fill="${fill}"/>`;

/**
 * @param {{state?:'idle'|'working'|'nearby'|'handoff-right'|'handoff-left'|'conflict'|'merged'|'offline',
 *          color?:string, size?:number, title?:string, theme?:'light'|'dark', static?:boolean}} o
 * static:true renders the key pose with no animation classes (for PNG export, emails, etc.).
 */
export function boSVG({ state = 'idle', color = INK, size = 64, title, theme = 'light', static: still = false } = {}) {
  const c = (name) => (still ? '' : name);
  const cls = (name) => (c(name) ? ` class="${c(name)}"` : '');
  const label = title ?? ({
    idle: 'Bo idle', working: 'Bo working', nearby: 'Bo noticing a teammate',
    'handoff-right': 'Bo handing off', 'handoff-left': 'Bo receiving a handoff',
    conflict: 'Bo seeing a conflict', merged: 'Bo after a clean merge', offline: 'Bo asleep, offline',
  })[state];
  let inner;
  switch (state) {
    case 'idle':
      inner = `<g><circle cx="50" cy="50" r="40" fill="${color}"/><g${cls('bo-idle')}><g${cls('bo-blink')}>${EYES()}</g></g></g>`;
      break;
    case 'working':
      // Static key pose = eyes dropped 8 units, centred.
      inner = still
        ? `<g><circle cx="50" cy="50" r="40" fill="${color}"/><g transform="translate(0 8)">${EYES()}</g></g>`
        : `<g><circle cx="50" cy="50" r="40" fill="${color}"/><g class="bo-read">${EYES()}</g></g>`;
      break;
    case 'nearby':
    case 'handoff-right':
    case 'handoff-left': {
      const dx = state === 'handoff-left' ? -9 : 9;
      const look = still ? ` transform="translate(${dx} 0)"` : ` class="${dx > 0 ? 'bo-look-r' : 'bo-look-l'}"`;
      const outer = state === 'nearby' ? '<g>' : `<g${cls('bo-bob')}>`;
      inner = `${outer}<circle cx="50" cy="50" r="40" fill="${color}"/><g${look}><g${cls('bo-blink')}>${EYES()}</g></g></g>`;
      break;
    }
    case 'conflict':
      inner = still
        ? `<g><circle cx="50" cy="50" r="40" fill="${color}"/><g transform="translate(0 45) scale(1 1.4) translate(0 -45)">${EYES()}</g></g>`
        : `<g class="bo-shake-once"><circle cx="50" cy="50" r="40" fill="${color}"/><g class="bo-wide">${EYES()}</g></g>`;
      break;
    case 'merged':
      // Happy squint: two chevrons, stroke 6, round caps. Bobs twice then returns to idle.
      inner = `<g${cls('bo-bob-twice')}><circle cx="50" cy="50" r="40" fill="${color}"/>` +
        `<path d="M36 56L41 46L46 56" fill="none" stroke="${PAPER}" stroke-width="6" stroke-linecap="round" stroke-linejoin="round"/>` +
        `<path d="M54 56L59 46L64 56" fill="none" stroke="${PAPER}" stroke-width="6" stroke-linecap="round" stroke-linejoin="round"/></g>`;
      break;
    case 'offline': {
      // Offline ignores the teammate colour: Bo goes grey. Body shifts left/down to make room for z's.
      const bodyFill = theme === 'dark' ? '#525252' : '#737373';
      const zFill = theme === 'dark' ? '#A3A3A3' : '#525252';
      inner = `<g><g${cls('bo-breathe')}><circle cx="46" cy="54" r="36" fill="${bodyFill}"/>` +
        `<path d="M30 54Q36 61 42 54" fill="none" stroke="${PAPER}" stroke-width="4.5" stroke-linecap="round"/>` +
        `<path d="M50 54Q56 61 62 54" fill="none" stroke="${PAPER}" stroke-width="4.5" stroke-linecap="round"/></g>` +
        `<text${cls('bo-z1')} x="78" y="26" font-family="Instrument Sans, sans-serif" font-size="18" font-weight="700" fill="${zFill}">z</text>` +
        `<text${cls('bo-z2')} x="88" y="14" font-family="Instrument Sans, sans-serif" font-size="13" font-weight="700" fill="${zFill}">z</text></g>`;
      break;
    }
    default:
      throw new Error(`Unknown Bo state: ${state}`);
  }
  return open(size, label) + inner + '</svg>';
}

// ---------------------------------------------------------------------------
// 3. Badge Bo: 16 px file marker in the explorer. Never animated.
//    Uses the logo pose (glance right) with a ring for stacking.
//    Body r44 with an 8-unit stroke in the explorer row background colour.
//    Eyes 9x19 (one unit heavier than the mark so they survive at 16 px).
// ---------------------------------------------------------------------------
/** @param {{color:string, ring?:string|null, size?:number}} o */
export function badgeSVG({ color, ring = '#1A1A1A', size = 16 } = {}) {
  const stroke = ring ? ` stroke="${ring}" stroke-width="8"` : '';
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100" width="${size}" height="${size}" aria-hidden="true">` +
    `<circle cx="50" cy="50" r="44" fill="${color}"${stroke}/>` +
    `<rect x="50" y="30" width="9" height="19" rx="1.5" fill="${PAPER}"/>` +
    `<rect x="66" y="30" width="9" height="19" rx="1.5" fill="${PAPER}"/></svg>`;
}

/** Below 16 px Bo is never drawn: use this 8 px colour dot. */
export function dotSVG({ color, size = 8 } = {}) {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8 8" width="${size}" height="${size}" aria-hidden="true"><circle cx="4" cy="4" r="4" fill="${color}"/></svg>`;
}

// ---------------------------------------------------------------------------
// 4. Caret Bo: 18 px Bo that rides the caret as an agent writes. look-r + blink.
// ---------------------------------------------------------------------------
export function caretSVG({ color, size = 18 } = {}) {
  return open(size, null) +
    `<g><circle cx="50" cy="50" r="40" fill="${color}"/><g class="bo-look-r"><g class="bo-blink">${EYES()}</g></g></g></svg>`;
}

// ---------------------------------------------------------------------------
// 5. Crew-panel scenes (64 px in the panel; 150 px on the character sheet).
//    Designed for the dark panel (#171717 rows). Bo is the subject; props are
//    drawn at real proportion and never touch Bo with limbs (Bo has none).
// ---------------------------------------------------------------------------
const KEY = (x, y, w, h, k) => `<rect${k ? ` class="bo-key ${k}"` : ''} x="${x}" y="${y}" width="${w}" height="${h}" rx="0.8" fill="#404040"/>`;

function writingScene(color, still) {
  const k = (name) => (still ? null : name);
  return `<g>` +
    // Bo: r30, centred high so the keyboard sits in front of it.
    `<circle cx="50" cy="42" r="30" fill="${color}"/>` +
    // Eyes: short 6x8 carets that sit inside the glasses.
    `<g${still ? '' : ' class="bo-blink"'}><rect x="40" y="45" width="6" height="8" rx="1.2" fill="${PAPER}"/><rect x="55" y="45" width="6" height="8" rx="1.2" fill="${PAPER}"/></g>` +
    // Glasses: black rectangle pair (wider than tall: never the caret shape).
    `<rect x="34.5" y="43" width="15.5" height="12" rx="1.2" fill="none" stroke="${INK}" stroke-width="2.6"/>` +
    `<rect x="51" y="43" width="15.5" height="12" rx="1.2" fill="none" stroke="${INK}" stroke-width="2.6"/>` +
    `<path d="M50 46.5H51" stroke="${INK}" stroke-width="2.4"/>` +
    `<path d="M34.5 46L20.5 43M66.5 46L79.5 43" stroke="${INK}" stroke-width="2.4" stroke-linecap="round"/>` +
    // Keyboard in perspective: narrow far edge (y76, toward Bo) to wide near edge (y90, toward viewer).
    `<polygon points="31,76 69,76 77,90 23,90" fill="#262626" stroke="#525252" stroke-width="1"/>` +
    // Desk line.
    `<rect x="10" y="90" width="80" height="3" rx="1.5" fill="#404040"/>` +
    // Row nearest Bo holds the spacebar (Bo is the one typing).
    KEY(33, 78, 4.5, 2.6) + KEY(39, 78, 22, 2.6, k('bo-k5')) + KEY(62.5, 78, 4.5, 2.6) +
    KEY(29, 82, 5.4, 3, k('bo-k1')) + KEY(35.4, 82, 5.4, 3) + KEY(41.8, 82, 5.4, 3, k('bo-k3')) + KEY(48.2, 82, 5.4, 3) +
    KEY(54.6, 82, 5.4, 3, k('bo-k6')) + KEY(61, 82, 5.4, 3) + KEY(67.4, 82, 4, 3, k('bo-k2')) +
    KEY(26.5, 86, 6.2, 3) + KEY(33.7, 86, 6.2, 3, k('bo-k4')) + KEY(40.9, 86, 6.2, 3) + KEY(48.1, 86, 6.2, 3) +
    KEY(55.3, 86, 6.2, 3, k('bo-k1')) + KEY(62.5, 86, 6.2, 3) + KEY(69.7, 86, 4, 3) +
    `</g>`;
}

function testingScene(color, still) {
  const cl = (n) => (still ? '' : ` class="${n}"`);
  return `<g>` +
    // Bo: r32, sits low-left so the raised flask has room at eye level.
    `<circle cx="38" cy="56" r="32" fill="${color}"/>` +
    // Eyes look up-right toward the flask.
    `<g${cl('bo-blink')}><rect x="45" y="40" width="6" height="12" rx="1.2" fill="${PAPER}"/><rect x="57" y="40" width="6" height="12" rx="1.2" fill="${PAPER}"/></g>` +
    // Vapour puffs (appear when the liquid turns green = tests pass).
    (still ? '' : `<circle class="bo-p1" cx="82" cy="28" r="2.2" fill="#A3A3A3"/><circle class="bo-p2" cx="84.5" cy="25" r="1.6" fill="#A3A3A3"/>`) +
    // Erlenmeyer flask, floating at Bo's side, raised 28 units (held up to the light). No ground.
    `<g${cl('bo-swirl')}><g transform="translate(0 -28)">` +
    `<rect x="79" y="60" width="6" height="7" rx="1" fill="#262626" stroke="#A3A3A3" stroke-width="1.2"/>` +
    `<path d="M79 66L74.5 79.5Q73.5 84 77.5 84H86.5Q90.5 84 89.5 79.5L85 66Z" fill="#262626" stroke="#A3A3A3" stroke-width="1.2" stroke-linejoin="round"/>` +
    `<path${cl('bo-slosh')} d="M76.2 75H87.8L89.3 79.7Q90.2 83.2 86.6 83.2H77.4Q73.8 83.2 74.7 79.7Z" fill="#2DD4BF"/>` +
    (still ? '' :
      `<circle class="bo-fb1" cx="80" cy="79" r="1.1" fill="#CCFBF1"/><circle class="bo-fb2" cx="82.5" cy="80.5" r="0.9" fill="#CCFBF1"/><circle class="bo-fb3" cx="84" cy="78" r="1.1" fill="#CCFBF1"/>`) +
    `</g></g></g>`;
}

function readingScene(color, still) {
  const cl = (n) => (still ? '' : ` class="${n}"`);
  return `<g>` +
    `<circle cx="50" cy="40" r="30" fill="${color}"/>` +
    // Eyes 6x11, looking down at the book; drift slowly left/right.
    `<g${cl('bo-look-down')}><g${cl('bo-blink')}><rect x="41" y="36" width="6" height="11" rx="1.2" fill="${PAPER}"/><rect x="53" y="36" width="6" height="11" rx="1.2" fill="${PAPER}"/></g></g>` +
    // Open book lying flat on the desk, drawn in perspective (two trapezoid halves).
    `<path d="M14 90L28 74H50V90Z" fill="#F5F5F5"/><path d="M30 79H43M25 84H42" stroke="#A3A3A3" stroke-width="1.6"/>` +
    `<path d="M86 90L72 74H50V90Z" fill="#F5F5F5"/><path d="M57 79H70M58 84H75" stroke="#A3A3A3" stroke-width="1.6"/>` +
    // Turning page: hinged on the spine, flips right to left.
    `<g${cl('bo-page')}><path${cl('bo-leaf')} d="M50 74H72L86 90H50Z" fill="#F5F5F5"/><path d="M57 79H70M58 84H75" stroke="#A3A3A3" stroke-width="1.6"/></g>` +
    `<path d="M50 74V90" stroke="#737373" stroke-width="1.5"/>` +
    `<rect x="8" y="90" width="84" height="4" rx="2" fill="#404040"/>` +
    `</g>`;
}

function offlineScene(still) {
  const cl = (n) => (still ? '' : ` class="${n}"`);
  return `<g><g${cl('bo-breathe')}><circle cx="46" cy="54" r="30" fill="#525252"/>` +
    `<path d="M34 54Q39 59 44 54" fill="none" stroke="${PAPER}" stroke-width="3.5" stroke-linecap="round"/>` +
    `<path d="M50 54Q55 59 60 54" fill="none" stroke="${PAPER}" stroke-width="3.5" stroke-linecap="round"/></g>` +
    `<text${cl('bo-z1')} x="74" y="30" font-family="Instrument Sans, sans-serif" font-size="16" font-weight="700" fill="#A3A3A3">z</text>` +
    `<text${cl('bo-z2')} x="84" y="18" font-family="Instrument Sans, sans-serif" font-size="12" font-weight="700" fill="#A3A3A3">z</text></g>`;
}

/**
 * @param {{scene:'writing'|'testing'|'reading'|'offline', color?:string, size?:number, title?:string, static?:boolean}} o
 */
export function sceneSVG({ scene, color = INK, size = 64, title, static: still = false } = {}) {
  const label = title ?? ({
    writing: 'Bo typing on a keyboard in rectangular glasses',
    testing: 'Bo swirling a test flask',
    reading: 'Bo reading a book',
    offline: 'Bo sleeping',
  })[scene];
  const body = {
    writing: () => writingScene(color, still),
    testing: () => testingScene(color, still),
    reading: () => readingScene(color, still),
    offline: () => offlineScene(still),
  }[scene];
  if (!body) throw new Error(`Unknown scene: ${scene}`);
  return open(size, label) + body() + '</svg>';
}

/**
 * Map daemon activity to what the crew panel shows. Event states win over scenes.
 * See 05-interface.md §4 for the precedence table.
 * @param {{online:boolean, conflict?:boolean, handoffPending?:boolean, justMerged?:boolean,
 *          activity?:'editing'|'testing'|'none'}} s
 * @returns {{kind:'face', state:string} | {kind:'scene', scene:string}}
 */
export function panelView(s) {
  if (!s.online) return { kind: 'scene', scene: 'offline' };
  if (s.conflict) return { kind: 'face', state: 'conflict' };
  if (s.handoffPending) return { kind: 'face', state: 'handoff-right' };
  if (s.justMerged) return { kind: 'face', state: 'merged' };
  if (s.activity === 'editing') return { kind: 'scene', scene: 'writing' };
  if (s.activity === 'testing') return { kind: 'scene', scene: 'testing' };
  return { kind: 'scene', scene: 'reading' };
}
