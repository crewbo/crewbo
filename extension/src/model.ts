// What the extension shows, computed from daemon responses. No VS Code
// imports, so it is unit tested with plain Node.

/** A claim as the daemon's claim_list returns it (internal/claims). */
export interface Claim {
  ID: string;
  Pattern: string;
  Holder: { SessionID: string; MemberID: string };
  TaskID?: string;
  LeaseExpires: string;
}

/** A conflict between two tasks' checkpoints (internal/daemon/api.go). */
export interface Conflict {
  task: string;
  paths: string[];
}

/** A task as task_list returns it. */
export interface Task {
  id?: string;
  room: string;
  author?: string;
  text?: string;
  owned_paths?: string[];
  state: string;
  sent?: boolean;
  assignee?: string;
  /** Another member's task, known from the room's statuses. */
  teammate?: boolean;
  conflicts?: Conflict[];
  detail?: string;
}

/** A step of the escalation ladder, as claim_requests returns it. */
export interface Handoff {
  kind: "request" | "override";
  request_id?: string;
  claim_id: string;
  path?: string;
  holder?: string;
  actor: string;
  reason?: string;
  answer?: string;
  answer_reason?: string;
  open?: boolean;
}

/** The daemon's status. */
export interface Status {
  version: string;
  sync?: string;
  login?: string;
}

/** Everything one poll of the daemon returned. */
export interface State {
  status: Status;
  tasks: Task[];
  /** Per repository root: its claims, its room's handoff steps and crew. */
  repos: { root: string; claims: Claim[]; handoffs: Handoff[]; crew?: CrewMember[]; room?: string; crewError?: string }[];
}

/**
 * overlaps is a port of claims.Overlaps (internal/claims/overlap.go), the
 * check the daemon enforces, so a file is shown as claimed exactly when an
 * agent would be blocked from it.
 */
export function overlaps(a: string, b: string): boolean {
  const as = a.split("/");
  const bs = b.split("/");
  const ap = literalPrefix(as);
  const bp = literalPrefix(bs);
  if (ap === as.length && bp === bs.length) {
    return as.length === bs.length && segmentsEqual(as, bs, as.length);
  }
  if (!segmentsEqual(as, bs, Math.min(ap, bp))) {
    return false;
  }
  if (ap === as.length) {
    return as.length > bp || hasDoubleStar(bs.slice(bp));
  }
  if (bp === bs.length) {
    return bs.length > ap || hasDoubleStar(as.slice(ap));
  }
  return true;
}

const isGlob = (s: string) => /[*?[]/.test(s);

function literalPrefix(segs: string[]): number {
  const i = segs.findIndex(isGlob);
  return i < 0 ? segs.length : i;
}

function segmentsEqual(a: string[], b: string[], n: number): boolean {
  for (let i = 0; i < n; i++) {
    // Go's strings.EqualFold folds case by Unicode simple folding.
    if (a[i].toLowerCase() !== b[i].toLowerCase()) {
      return false;
    }
  }
  return true;
}

const hasDoubleStar = (segs: string[]) => segs.some((s) => s.includes("**"));

/** claimsOn returns the active claims covering a repository-relative file. */
export function claimsOn(path: string, claims: Claim[], now: Date): Claim[] {
  return claims.filter((c) => overlaps(path, c.Pattern) && new Date(c.LeaseExpires) > now);
}

/** isTerminal mirrors daemon.IsTerminalSession. */
export const isTerminal = (session: string) => session === "terminal" || session.endsWith("/terminal");

/** holderLabel names who holds a claim, as the hook's deny message does. */
export function holderLabel(c: Claim, me?: string): string {
  const who = c.Holder.MemberID === me && me ? `${me} (you)` : c.Holder.MemberID;
  if (!who) {
    return "another agent session";
  }
  return isTerminal(c.Holder.SessionID) ? `${who}, from a terminal` : `${who}'s agent`;
}

/**
 * badge is the explorer badge for a claimed file: the holder's first two
 * letters (VS Code file decorations hold at most two characters, so the
 * brand's 16 px Bo badge cannot be shown there).
 */
export function badge(cs: Claim[]): string {
  const who = cs[0]?.Holder.MemberID ?? "";
  return cs.length > 1 ? `${cs.length}` : who.slice(0, 2) || "?";
}

/** taskText returns the text of a claim's task when this member knows it. */
export function taskText(c: Claim, tasks: Task[]): string | undefined {
  return c.TaskID ? tasks.find((t) => t.id === c.TaskID)?.text : undefined;
}

/** needsApproval reports a task sent to this member and not yet answered. */
export const needsApproval = (t: Task) => !t.sent && !t.teammate && t.state === "dispatched" && !!t.id;

/** conflictPairs counts distinct pairs of conflicting tasks. */
export function conflictPairs(tasks: Task[]): number {
  const pairs = new Set<string>();
  for (const t of tasks) {
    for (const c of t.conflicts ?? []) {
      if (t.id && c.paths.length > 0) {
        pairs.add([t.id, c.task].sort().join(" "));
      }
    }
  }
  return pairs.size;
}

/** openHandoffs returns the open requests across repositories. */
export function openHandoffs(s: State): Handoff[] {
  return s.repos.flatMap((r) => r.handoffs.filter((h) => h.kind === "request" && h.open));
}

/** canAnswer reports whether this member holds the claim a request asks for. */
export const canAnswer = (h: Handoff, me?: string) => !!me && h.open === true && h.holder === me;

/** statusText is the status bar text: plain counts, conflicts always shown. */
export function statusText(s: State): string {
  const claimed = s.repos.reduce((n, r) => n + r.claims.length, 0);
  const handoffs = openHandoffs(s).length;
  const conflicts = conflictPairs(s.tasks);
  return `crewbo  ${claimed} claimed · ${handoffs} handoff · ${conflicts} ${conflicts === 1 ? "conflict" : "conflicts"}`;
}

/** handoffLabel is the brand's handoff copy: "arjun wants token.go next". */
export function handoffLabel(h: Handoff): string {
  if (h.kind === "override") {
    return `${h.actor} released ${h.holder}'s claim on ${h.path}`;
  }
  return `${h.actor} wants ${h.path} next`;
}

/** handoffState describes where a request stands. */
export function handoffState(h: Handoff): string {
  if (h.kind === "override") {
    return "overridden by the lead";
  }
  if (h.open) {
    return `waiting for ${h.holder}`;
  }
  return h.answer ? `${h.answer} by ${h.holder}` : "closed";
}

/**
 * visible shows text another member wrote with every control character
 * except newline and tab escaped, as `crewbo task show` does.
 */
export function visible(s: string): string {
  return s.replace(/[\p{Cc}\u2028\u2029\u200e\u200f\u202a-\u202e\u2066-\u2069\u061c]/gu, (ch) => {
    if (ch === "\n" || ch === "\t") {
      return ch;
    }
    const named: Record<string, string> = { "\x07": "\\a", "\b": "\\b", "\f": "\\f", "\r": "\\r", "\v": "\\v" };
    const code = ch.codePointAt(0) ?? 0;
    // strconv.QuoteRune's forms: named escapes, \xNN below 0x80, else \uNNNN.
    return named[ch] ?? (code < 0x80 ? `\\x${code.toString(16).padStart(2, "0")}` : `\\u${code.toString(16).padStart(4, "0")}`);
  });
}

/** A member as the daemon's crew view returns it (internal/daemon/crew.go). */
export interface CrewMember {
  login: string;
  you?: boolean;
  lead?: boolean;
  color: number;
  online: boolean;
  offline_since?: string;
  agents?: { agent: string; path?: string; idle: boolean; at: string }[];
  claims?: string[];
}

/**
 * CrewRow is one crew panel row: the inputs of bo.js panelView(), which
 * picks the Bo, and the text beside it (docs/brand/05-interface.md 3.5, 4).
 */
export interface CrewRow {
  name: string;
  color: number;
  online: boolean;
  conflict: boolean;
  handoffPending: boolean;
  activity: "editing" | "none";
  status: string;
  agent: string;
  /** For screen readers: the person and the state (09-checklist.md). */
  label: string;
}

const agentNames: Record<string, string> = { "claude-code": "Claude Code", codex: "Codex", antigravity: "Antigravity" };

/** base is the last part of a slash path, as the brand's "Writing auth.go". */
const base = (p: string) => p.slice(p.lastIndexOf("/") + 1);

/**
 * crewRows maps the crew view, the tasks and the open handoff requests to
 * panel rows. A member is in a conflict when a task of theirs has one, and
 * has a pending handoff when they ask for or hold a requested file. No
 * model is shown: hooks of a member's own agent carry none (BDR-017).
 */
export function crewRows(crew: CrewMember[], tasks: Task[], handoffs: Handoff[], time: (iso: string) => string): CrewRow[] {
  const me = crew.find((m) => m.you)?.login;
  const owner = (t: Task) => (t.sent ? t.assignee : t.teammate ? t.author : me);
  return crew.map((m) => {
    const clash = tasks.find((t) => owner(t) === m.login && t.conflicts?.some((c) => c.paths.length));
    const handoff = handoffs.find((h) => h.kind === "request" && h.open && (h.actor === m.login || h.holder === m.login));
    const a = m.agents?.[0];
    const editing = !!a && !a.idle && !!a.path;
    let status = "Idle";
    if (!m.online) {
      status = m.offline_since ? `Offline since ${time(m.offline_since)}` : "Offline";
    } else if (clash) {
      status = `Conflict in ${base(clash.conflicts?.find((c) => c.paths.length)?.paths[0] ?? "")}`;
    } else if (handoff) {
      status = handoffLabel(handoff);
    } else if (editing) {
      status = `Writing ${base(a.path ?? "")}`;
    } else if (a && !a.idle) {
      status = "Working";
    }
    const name = m.you ? `${m.login} (you)` : m.login;
    return {
      name,
      color: m.color,
      online: m.online,
      conflict: m.online && !!clash,
      handoffPending: m.online && !!handoff,
      activity: m.online && editing ? "editing" : "none",
      status: visible(status),
      agent: m.online && a ? (agentNames[a.agent] ?? a.agent) : "",
      label: visible(`${m.login}, ${status.charAt(0).toLowerCase()}${status.slice(1)}`),
    };
  });
}

/** colorOf is a member's teammate colour index, if they are in the crew. */
export function colorOf(login: string, crew: CrewMember[] | undefined): number | undefined {
  return crew?.find((m) => m.login === login)?.color;
}

/**
 * claimTag is the brand's claim tag for a file another member holds
 * (05-interface.md 3.2): "riya · Codex", with the agent the member's agents
 * last used; a terminal claim names only the member. No model (BDR-017).
 */
export function claimTag(c: Claim, crew: CrewMember[] | undefined, me?: string): { text: string; color: number } | undefined {
  const who = c.Holder.MemberID;
  const color = colorOf(who, crew);
  if (!who || who === me || color === undefined) {
    return undefined;
  }
  const agent = crew?.find((m) => m.login === who)?.agents?.[0]?.agent;
  const text = !isTerminal(c.Holder.SessionID) && agent ? `${who} · ${agentNames[agent] ?? agent}` : who;
  return { text: visible(text), color };
}
