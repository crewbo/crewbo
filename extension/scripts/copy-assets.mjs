// Copies the brand files the crew panel uses into media/ (git-ignored), next
// to the panel's own script, so the webview loads Bo from the reference
// component itself, unchanged (CLAUDE.md: port bo.js, never redraw Bo).
import { copyFileSync, mkdirSync } from "node:fs";

const brand = "../docs/brand";
const out = "media/crew";
mkdirSync(`${out}/fonts`, { recursive: true });
const files = {
  [`${brand}/reference/component/bo.js`]: `${out}/bo.js`,
  [`${brand}/reference/component/bo.css`]: `${out}/bo.css`,
  [`${brand}/tokens/tokens.css`]: `${out}/tokens.css`,
  [`${brand}/assets/svg/logo/mark-cutout.svg`]: "media/mark.svg",
  [`${brand}/assets/png/app-icon-128.png`]: "media/icon.png",
  "webview/crew.js": `${out}/crew.js`,
  "webview/crew.css": `${out}/crew.css`,
  "../LICENSE": "LICENSE",
};
for (const f of ["instrument-sans-latin-400-normal.woff2", "instrument-sans-latin-600-normal.woff2", "instrument-sans-latin-700-normal.woff2",
  "jetbrains-mono-latin-400-normal.woff2", "LICENSE-InstrumentSans-OFL.txt", "LICENSE-JetBrainsMono-OFL.txt"]) {
  files[`${brand}/assets/fonts/${f}`] = `${out}/fonts/${f}`;
}
for (const [from, to] of Object.entries(files)) {
  copyFileSync(from, to);
}
