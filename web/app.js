// Crewbo dashboard v0 (M5-06): a read-only view of the local daemon's
// state. The token comes in the address fragment, which the browser never
// sends to a server, and goes to the daemon only in the Authorization header.
"use strict";

const token = new URLSearchParams(location.hash.slice(1)).get("token") || "";
// Drop the token from the address bar and history.
history.replaceState(null, "", location.pathname);

// visible escapes control and bidirectional formatting characters in text
// other members wrote, as `crewbo task show` does.
const hidden = /[\p{Cc}\u2028\u2029\u200e\u200f\u202a-\u202e\u2066-\u2069\u061c]/gu;
function visible(s) {
  return String(s ?? "").replace(hidden, (ch) => {
    if (ch === "\n" || ch === "\t") return ch;
    const code = ch.codePointAt(0);
    return code < 0x80 ? "\\x" + code.toString(16).padStart(2, "0") : "\\u" + code.toString(16).padStart(4, "0");
  });
}

function el(tag, text, cls) {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = visible(text);
  if (cls) e.className = cls;
  return e;
}

function row(cells) {
  const tr = document.createElement("tr");
  for (const c of cells) tr.append(c instanceof Node ? c : el("td", c));
  return tr;
}

function fill(id, rows, empty) {
  const body = document.getElementById(id);
  const none = el("td", empty, "muted");
  none.colSpan = body.closest("table").tHead.rows[0].cells.length;
  body.replaceChildren(...(rows.length ? rows : [row([none])]));
}

function pill(signal, text) {
  const p = el("span", undefined, "pill");
  p.append(el("span", undefined, "dot " + signal), el("span", text));
  return p;
}

function holder(c) {
  const who = c.Holder.MemberID || "another agent session";
  const terminal = c.Holder.SessionID === "terminal" || c.Holder.SessionID.endsWith("/terminal");
  return terminal ? who + ", from a terminal" : who + "'s agent";
}

function time(iso) {
  return new Date(iso).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function render(s) {
  const tasks = s.tasks || [];
  const text = new Map(tasks.filter((t) => t.id).map((t) => [t.id, t.text]));
  const claims = s.repos.flatMap((r) => r.claims);
  const handoffs = s.repos.flatMap((r) => r.handoffs);
  const open = handoffs.filter((h) => h.kind === "request" && h.open).length;
  const pairs = new Set();
  for (const t of tasks) for (const c of t.conflicts || []) if (t.id && c.paths.length) pairs.add([t.id, c.task].sort().join(" "));

  document.getElementById("pills").replaceChildren(
    pill("claimed", claims.length + " claimed"),
    pill("handoff", open + " handoff"),
    pill("conflict", pairs.size + (pairs.size === 1 ? " conflict" : " conflicts")),
    // Blue "synced" only while connected; anything else is plain grey.
    pill((s.status.sync || "").startsWith("connected") ? "synced" : "", s.status.sync || "not in a room"),
  );
  fill(
    "claims",
    claims.map((c) => {
      const task = c.TaskID ? c.TaskID + (text.get(c.TaskID) ? ": " + text.get(c.TaskID) : "") : "";
      return row([el("td", c.Pattern, "mono"), holder(c), task, el("td", time(c.LeaseExpires), "mono")]);
    }),
    "No claims.",
  );
  fill(
    "tasks",
    tasks
      .filter((t) => t.id)
      .map((t) => {
        const conflicts = (t.conflicts || []).filter((c) => c.paths.length).map((c) => c.task + ": " + c.paths.join(", "));
        const task = el("td");
        task.append(el("div", t.id, "mono"), el("div", t.text || "", "muted"));
        return row([task, t.state, t.sent ? "sent to " + t.assignee : "from " + t.author, conflicts.join("; ") || "none"]);
      }),
    "No tasks.",
  );
  fill(
    "handoffs",
    handoffs.map((h) => {
      const what = h.kind === "override" ? h.actor + " released " + h.holder + "'s claim on " + h.path : h.actor + " wants " + h.path + " next";
      const state = h.kind === "override" ? "overridden by the lead" : h.open ? "waiting for " + h.holder : h.answer ? h.answer + " by " + h.holder : "closed";
      return row([what, state, [h.reason, h.answer_reason].filter(Boolean).join(" / ")]);
    }),
    "No handoff requests.",
  );
}

function say(text) {
  const m = document.getElementById("message");
  m.hidden = !text;
  m.textContent = text || "";
}

async function poll() {
  try {
    const r = await fetch("/api/state", { headers: { Authorization: "Bearer " + token }, cache: "no-store" });
    if (r.status === 401) {
      say("This address has no valid token. Open the address `crewbo dashboard` prints.");
      return; // stop polling: a new token needs a new address
    }
    if (!r.ok) throw new Error(await r.text());
    render(await r.json());
    say("");
  } catch (err) {
    say("Crewbo cannot reach its daemon. Start it with `crewbo daemon`, then run `crewbo dashboard` again.");
  }
  setTimeout(poll, document.hidden ? 10000 : 2000);
}
poll();
