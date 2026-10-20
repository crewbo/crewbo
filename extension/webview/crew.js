// Crew panel: draws the rows the extension posts, with Bo from the brand's
// reference component, unchanged (copied next to this file at build time).
import { boSVG, panelView, sceneSVG, teamColor } from "./bo.js";

const vscode = acquireVsCodeApi(); // may be called only once
const head = document.getElementById("head");
const list = document.getElementById("crew");
const foot = document.getElementById("foot");
let order = ""; // the rows' names in the order drawn
const drawn = new Map(); // row name to [key, element]: only changed rows redraw, so Bo's motion is not restarted every poll

function light() {
  return document.body.classList.contains("vscode-light") || document.body.classList.contains("vscode-high-contrast-light");
}

function row(r) {
  const c = teamColor(r.color);
  const v = panelView({ online: r.online, conflict: r.conflict, handoffPending: r.handoffPending, activity: r.activity });
  const el = document.createElement("div");
  el.className = "row";
  el.setAttribute("role", "listitem");
  el.innerHTML = v.kind === "scene"
    ? sceneSVG({ scene: v.scene, color: c.base, size: 64, title: r.label })
    : boSVG({ state: v.state, color: c.base, size: 64, title: r.label, theme: light() ? "light" : "dark" });
  const text = document.createElement("div");
  text.className = "text";
  for (const [cls, value] of [["name", r.name], ["status", r.status], ["agent", r.agent]]) {
    if (!value) continue;
    const s = document.createElement("span");
    s.className = cls;
    s.textContent = value;
    if (cls === "agent") s.style.color = light() ? c.base : c.tint;
    text.append(s);
  }
  el.append(text);
  return el;
}

// Buttons for sign-in and rooms: the extension runs only its own commands
// (CrewPanel.commands).
function actions(items) {
  const bar = document.createElement("div");
  bar.className = "actions";
  items.forEach((a, i) => {
    const b = document.createElement("button");
    b.className = i === 0 ? "primary" : "secondary";
    b.textContent = a.label;
    b.addEventListener("click", () => vscode.postMessage({ type: "run", command: a.command }));
    bar.append(b);
  });
  return bar;
}

window.addEventListener("message", (e) => {
  const m = e.data;
  if (m?.type !== "crew") return;
  document.body.classList.toggle("reduce-motion", !!m.reduceMotion);
  // "CREW · <room-name>": the label in capitals, the room name as it is.
  const label = document.createElement("span");
  label.textContent = "Crew";
  head.replaceChildren(label, m.room ? ` · ${m.room}` : "");
  if (m.note) {
    drawn.clear();
    order = "";
    const p = document.createElement("p");
    p.className = "note";
    p.textContent = m.note;
    list.replaceChildren(p);
    foot.replaceChildren();
    if (m.actions?.length) list.append(actions(m.actions));
    return;
  }
  const next = [];
  const seen = new Set();
  for (const r of m.rows) {
    const key = JSON.stringify([r, light()]);
    let d = drawn.get(r.name);
    if (!d || d[0] !== key) {
      d = [key, row(r)];
      drawn.set(r.name, d);
    }
    seen.add(r.name);
    next.push(d[1]);
  }
  for (const name of drawn.keys()) if (!seen.has(name)) drawn.delete(name);
  // Re-inserting an element restarts its animations, so keep the ones
  // that did not change where they are.
  const names = m.rows.map((r) => r.name).join("\n");
  if (names === order && list.children.length === next.length) {
    next.forEach((el, i) => list.children[i] !== el && list.children[i].replaceWith(el));
  } else {
    list.replaceChildren(...next);
    order = names;
  }
  // Under the rows: the empty-room line, and the lead's invite button.
  const key = JSON.stringify(m.foot ?? null);
  if (foot.dataset.key !== key) {
    foot.dataset.key = key;
    foot.replaceChildren();
    if (m.foot) {
      const p = document.createElement("p");
      p.className = "note";
      p.textContent = m.foot.note;
      foot.append(p);
      if (m.foot.actions?.length) foot.append(actions(m.foot.actions));
    }
  }
});

// Ask for the crew once this script listens; earlier messages are lost.
vscode.postMessage({ type: "ready" });
