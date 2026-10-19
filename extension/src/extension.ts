// Crewbo for VS Code, v0 (M5-05): claims, the tasks they belong to,
// conflict warnings and handoff requests, read from the local daemon
// through `crewbo api`. Approving a task opens a terminal running
// `crewbo task approve`, the only approval path (ADR 0006).

import { randomBytes } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import * as vscode from "vscode";
import { Client, type Request } from "./client";
import {
  badge,
  canAnswer,
  claimsOn,
  type Claim,
  type Draft,
  claimTag,
  colorOf,
  conflictPairs,
  crewRows,
  sendTaskPicks,
  taskPaths,
  type Handoff,
  handoffLabel,
  handoffState,
  holderLabel,
  needsApproval,
  openHandoffs,
  type State,
  statusText,
  type Task,
  taskText,
  visible,
} from "./model";

// ponytail: polls the daemon every 2 s; a push subscription in the local
// API would make updates instant if polling shows up in battery or CPU.
const pollEvery = 2000;

type Node =
  | { kind: "group"; label: string; children: Node[] }
  | { kind: "claim"; claim: Claim; root: string }
  | { kind: "task"; task: Task }
  | { kind: "conflict"; label: string }
  | { kind: "handoff"; handoff: Handoff; root: string }
  | { kind: "draft"; draft: Draft };

/** What activate returns: read-only hooks for the in-editor test (test/vscode). */
export interface Api {
  state: () => State | undefined;
  statusBar: () => string;
  tree: CrewTree;
  decoration: (uri: vscode.Uri) => vscode.FileDecoration | undefined;
  crewPanelVisible: () => boolean;
}

export function activate(ctx: vscode.ExtensionContext): Api {
  // Machine scope: a workspace must not choose the binary the extension runs.
  const bin = () => vscode.workspace.getConfiguration("crewbo").get<string>("path") || "crewbo";
  let client = new Client(bin());
  let state: State | undefined;
  let down: string | undefined;
  const notified = new Set<string>();

  const changed = new vscode.EventEmitter<void>();
  const decoChanged = new vscode.EventEmitter<undefined>();
  const badge = (i: number) => vscode.Uri.joinPath(ctx.extensionUri, "media", "badges", `${i % 8}.svg`);
  const tree = new CrewTree(() => state, changed.event, badge);
  // Claim tags (05-interface.md 3.2), one decoration type per teammate
  // colour; the colours come from bo.js through the build (copy-assets.mjs).
  const colors: { base: string; tint: string }[] = JSON.parse(readFileSync(join(ctx.extensionPath, "media", "badges", "colors.json"), "utf8"));
  const tags = colors.map((c) =>
    vscode.window.createTextEditorDecorationType({
      after: { margin: "0 0 0 20px", border: `1px solid ${c.base}`, textDecoration: "none; border-radius: 999px; padding: 0 10px; font-size: 13px;" },
      dark: { after: { color: c.tint } },
      light: { after: { color: c.base } },
    }),
  );
  const view = vscode.window.createTreeView("crewbo.crew", { treeDataProvider: tree });
  const bar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 0);
  bar.command = "crewbo.crew.focus";
  bar.show();
  const texts = new TaskTexts(() => state);
  const panel = new CrewPanel(vscode.Uri.joinPath(ctx.extensionUri, "media", "crew"), () => void poll());
  ctx.subscriptions.push(
    view,
    bar,
    changed,
    decoChanged,
    vscode.workspace.registerTextDocumentContentProvider("crewbo-task", texts),
    vscode.window.registerWebviewViewProvider("crewbo.crewPanel", panel),
    vscode.window.registerFileDecorationProvider({
      onDidChangeFileDecorations: decoChanged.event,
      provideFileDecoration: (uri) => decoration(uri, state),
    }),
  );

  const repos = () => {
    const roots = (vscode.workspace.workspaceFolders ?? []).map((f) => repoRoot(f.uri.fsPath));
    return [...new Set(roots.filter((r): r is string => !!r))];
  };

  const poll = async () => {
    try {
      const st = await client.call({ method: "status" });
      const dr = await client.call({ method: "task_drafts" });
      const next: State = { status: st.status ?? { version: "" }, tasks: [], repos: [], drafts: dr.drafts ?? [] };
      for (const root of repos()) {
        // Only the tasks of this window's rooms; two folders can share a room.
        const ts = await client.call({ method: "task_list", repo: root });
        for (const t of ts.tasks ?? []) {
          if (!t.id || !next.tasks.some((x) => x.id === t.id)) {
            next.tasks.push(t);
          }
        }
        const cl = await client.call({ method: "claim_list", repo: root });
        const ho = await client.call({ method: "claim_requests", repo: root });
        // A repository outside a room has no handoff steps or crew; that is not an error.
        const cr = await client.call({ method: "crew", repo: root });
        next.repos.push({ root, claims: cl.claims ?? [], handoffs: ho.handoffs ?? [], crew: cr.crew, room: cr.room_name, crewError: cr.error });
      }
      state = next;
      down = undefined;
      notify(next);
      void vscode.commands.executeCommand("setContext", "crewbo.lead", next.repos.some((r) => r.crew?.some((m) => m.you && m.lead)));
      if (panel.visible) {
        showCrew(next);
      }
    } catch (err) {
      state = undefined;
      down = (err as Error).message;
    }
    render();
  };

  // showCrew fills the crew panel for the first folder in a room.
  const showCrew = (s: State) => {
    const reduceMotion = vscode.workspace.getConfiguration("workbench").get<string>("reduceMotion") === "on";
    const r = s.repos[0];
    if (!r) {
      panel.post({ type: "crew", reduceMotion, note: "Open a repository to see its crew." });
      return;
    }
    if (r.crewError) {
      panel.post({ type: "crew", reduceMotion, note: r.crewError.replace(/^crew: /, "") });
      return;
    }
    const time = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
    panel.post({ type: "crew", reduceMotion, room: r.room, rows: crewRows(r.crew ?? [], s.tasks, r.handoffs, time) });
  };

  // showTags puts a claim tag on the first line of each visible file that
  // another member holds: who, and which agent, without covering code.
  const showTags = () => {
    for (const editor of vscode.window.visibleTextEditors) {
      const ranges: vscode.DecorationOptions[][] = tags.map(() => []);
      const file = editor.document.uri.fsPath;
      const root = editor.document.uri.scheme === "file" ? repoRoot(dirname(file)) : undefined;
      const r = state?.repos.find((x) => x.root === root);
      if (r && root) {
        const held = claimsOn(relative(root, file).split(sep).join("/"), r.claims, new Date());
        for (const c of held) {
          const tag = claimTag(c, r.crew, state?.status.login);
          if (tag) {
            const line = editor.document.lineAt(0).range;
            ranges[tag.color % tags.length].push({
              range: new vscode.Range(line.end, line.end),
              hoverMessage: `${holderLabel(c, state?.status.login)} holds ${c.Pattern}`,
              renderOptions: { after: { contentText: tag.text } },
            });
            break;
          }
        }
      }
      tags.forEach((t, i) => editor.setDecorations(t, ranges[i]));
    }
  };

  const render = () => {
    if (!state) {
      bar.text = "crewbo  daemon not running";
      bar.tooltip = `${down ?? ""}\nStart it with: crewbo daemon`;
      view.message = `Crewbo cannot reach its daemon. ${down ?? ""} Start it with \`crewbo daemon\`.`;
    } else {
      bar.text = statusText(state);
      bar.tooltip = state.status.sync ? `Sync: ${state.status.sync}` : "Not in a room";
      view.message = undefined;
    }
    changed.fire();
    decoChanged.fire(undefined);
    showTags();
  };

  // Tell the member once about requests they can answer, tasks waiting for
  // their approval and new conflicts, without a modal (brand P2, P6).
  const notify = (s: State) => {
    const me = s.status.login;
    for (const r of s.repos) {
      for (const h of r.handoffs.filter((h) => canAnswer(h, me))) {
        once(`h:${h.request_id}`, async () => {
          const pick = await vscode.window.showInformationMessage(visible(handoffLabel(h)), "Hand off", "Not yet");
          if (pick) {
            await answer({ kind: "handoff", handoff: h, root: r.root }, pick === "Hand off");
          }
        });
      }
    }
    const fresh = (s.drafts ?? []).filter((d) => !notified.has(`d:${d.id}`));
    if (fresh.length > 0) {
      fresh.forEach((d) => notified.add(`d:${d.id}`));
      void vscode.window
        .showInformationMessage(`Your agent drafted ${fresh.length === 1 ? "a task" : `${fresh.length} tasks`} for the crew. Nothing is sent until you review and send.`, "Review")
        .then((pick) => pick && vscode.commands.executeCommand("crewbo.crew.focus"));
    }
    for (const t of s.tasks.filter(needsApproval)) {
      once(`t:${t.id}`, async () => {
        const pick = await vscode.window.showInformationMessage(`${t.author} sent you task ${t.id}`, "Review");
        if (pick) {
          await review({ kind: "task", task: t });
        }
      });
    }
    for (const t of s.tasks) {
      for (const c of t.conflicts ?? []) {
        if (t.id && c.paths.length > 0) {
          once(`c:${[t.id, c.task].sort().join(" ")}:${c.paths.join(",")}`, () => {
            void vscode.window.showWarningMessage(
              `Tasks ${t.id} and ${c.task} both changed ${visible(c.paths.slice(0, 3).join(", "))}${c.paths.length > 3 ? ` and ${c.paths.length - 3} more` : ""}. The first to merge wins; the other must update.`,
            );
          });
        }
      }
    }
  };
  const once = (key: string, show: () => unknown) => {
    if (!notified.has(key)) {
      notified.add(key);
      void show();
    }
  };

  // send calls the daemon and reports its answer or refusal.
  const send = async (req: Request, done: string): Promise<boolean> => {
    try {
      const resp = await client.call(req);
      if (resp.error) {
        void vscode.window.showWarningMessage(`Crewbo: ${resp.error}`);
        return false;
      }
      void vscode.window.showInformationMessage(resp.note ? `${done} ${resp.note}` : done);
      return true;
    } catch (err) {
      void vscode.window.showWarningMessage(`Crewbo: ${(err as Error).message}`);
      return false;
    } finally {
      void poll();
    }
  };

  const answer = async (n: Node | undefined, approve: boolean) => {
    if (n?.kind !== "handoff" || !n.handoff.request_id) {
      return;
    }
    let reason: string | undefined = "";
    if (!approve) {
      reason = await vscode.window.showInputBox({ prompt: `Why not yet? ${n.handoff.actor} sees this (optional)` });
      if (reason === undefined) {
        return;
      }
    }
    await send(
      { method: "claim_handoff", repo: n.root, ids: [n.handoff.request_id], approve, text: reason },
      approve ? `Handed off ${n.handoff.path} to ${n.handoff.actor}.` : `Kept ${n.handoff.path}.`,
    );
  };

  const review = async (n: Node | undefined) => {
    if (n?.kind !== "task" || !n.task.id) {
      return;
    }
    const t = n.task;
    const doc = await vscode.workspace.openTextDocument(vscode.Uri.parse(`crewbo-task:/${encodeURIComponent(t.id!)}.txt`));
    await vscode.window.showTextDocument(doc, { preview: true });
    const agent = await vscode.window.showQuickPick(
      [
        { label: "Claude Code", value: "claude-code" },
        { label: "Codex", value: "codex" },
      ],
      { placeHolder: `Read task ${t.id} first. Which agent should run it if you approve?` },
    );
    if (!agent) {
      return;
    }
    // A real terminal: `crewbo task approve` prints the whole task and
    // waits for the member to type yes (ADR 0006, #114).
    const term = vscode.window.createTerminal({
      name: `crewbo task approve ${t.id}`,
      shellPath: bin(),
      shellArgs: ["task", "approve", "--agent", agent.value, "--", t.id!],
      cwd: repos()[0],
    });
    term.show();
  };

  const pathOf = (arg: unknown): { root: string; path: string } | undefined => {
    const n = arg as Node | undefined;
    if (n && "kind" in n && n.kind === "claim") {
      return { root: n.root, path: n.claim.Pattern };
    }
    const uri = arg instanceof vscode.Uri ? arg : vscode.window.activeTextEditor?.document.uri;
    const root = uri && repoRoot(dirname(uri.fsPath));
    return uri && root ? { root, path: relative(root, uri.fsPath).split(sep).join("/") } : undefined;
  };

  ctx.subscriptions.push(
    vscode.commands.registerCommand("crewbo.refresh", poll),
    vscode.commands.registerCommand("crewbo.sendDraft", async (n: Node) => {
      if (n?.kind !== "draft") {
        return;
      }
      const d = n.draft;
      const text = await vscode.window.showInputBox({
        title: `Review the drafted task for ${d.assignee}`,
        prompt: `Edit it if needed. Enter sends it to ${d.assignee}${d.patterns?.length ? ` with claims on ${d.patterns.join(", ")}` : ""}; they read it and approve it themselves.`,
        value: d.text,
        validateInput: (v) => (v.trim() ? undefined : "Write what the task is"),
      });
      if (!text) {
        return;
      }
      if (await send({ method: "task_create", repo: d.repo, assignee: d.assignee, patterns: d.patterns, text }, `Sent the task to ${d.assignee}.`)) {
        await client.call({ method: "task_draft_drop", ids: [d.id] });
        void poll();
      }
    }),
    vscode.commands.registerCommand("crewbo.dropDraft", async (n: Node) => {
      if (n?.kind === "draft") {
        await client.call({ method: "task_draft_drop", ids: [n.draft.id] });
        void poll();
      }
    }),
    vscode.commands.registerCommand("crewbo.sendTask", async () => {
      const r = state?.repos.find((x) => x.crew?.some((m) => m.you && m.lead));
      if (!r?.crew) {
        void vscode.window.showWarningMessage("Crewbo: only the room's lead sends tasks, from a repository in that room.");
        return;
      }
      const title = `Send a task in ${r.room ?? "this room"}`;
      const to = await vscode.window.showQuickPick(sendTaskPicks(r.crew), { title, placeHolder: "Who should do it?" });
      if (!to) {
        return;
      }
      const paths = await vscode.window.showInputBox({
        title,
        prompt: "Files or globs the task may change, separated by commas (optional). Their agent gets claims on them.",
        placeHolder: "src/auth/**, docs/auth.md",
      });
      if (paths === undefined) {
        return;
      }
      const text = await vscode.window.showInputBox({
        title,
        prompt: `What should ${to.label}'s agent do? They read it in full and approve it in a terminal before anything runs.`,
        validateInput: (v) => (v.trim() ? undefined : "Write what the task is"),
      });
      if (!text) {
        return;
      }
      await send({ method: "task_create", repo: r.root, assignee: to.label, patterns: taskPaths(paths), text }, `Sent the task to ${to.label}.`);
    }),
    vscode.commands.registerCommand("crewbo.requestHandoff", async (arg: unknown) => {
      const p = pathOf(arg);
      if (!p) {
        return;
      }
      const reason = await vscode.window.showInputBox({ prompt: `Why do you need ${p.path}? The holder sees this (optional)` });
      if (reason !== undefined) {
        await send({ method: "claim_request", repo: p.root, paths: [p.path], text: reason }, `Asked for ${p.path}.`);
      }
    }),
    vscode.commands.registerCommand("crewbo.override", async (arg: unknown) => {
      const p = pathOf(arg);
      if (!p) {
        return;
      }
      const reason = await vscode.window.showInputBox({
        prompt: `Release the claim on ${p.path} as the room lead. The holder sees your reason.`,
        validateInput: (v) => (v.trim() ? undefined : "A reason is required"),
      });
      if (reason) {
        await send({ method: "claim_override", repo: p.root, paths: [p.path], text: reason }, `Released the claim on ${p.path}.`);
      }
    }),
    vscode.commands.registerCommand("crewbo.handOff", (n: Node) => answer(n, true)),
    vscode.commands.registerCommand("crewbo.notYet", (n: Node) => answer(n, false)),
    vscode.commands.registerCommand("crewbo.reviewTask", review),
    vscode.commands.registerCommand("crewbo.declineTask", async (n: Node) => {
      if (n?.kind !== "task" || !n.task.id) {
        return;
      }
      const reason = await vscode.window.showInputBox({ prompt: `Decline task ${n.task.id}. ${n.task.author} sees your reason (optional)` });
      if (reason !== undefined) {
        await send({ method: "task_decline", task: n.task.id, text: reason }, `Declined task ${n.task.id}.`);
      }
    }),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("crewbo.path")) {
        client.stop();
        client = new Client(bin());
        void poll();
      }
    }),
    vscode.workspace.onDidChangeWorkspaceFolders(() => void poll()),
    vscode.window.onDidChangeVisibleTextEditors(showTags),
    ...tags,
  );

  // Poll only while the window is focused or visible, so a background
  // window does not keep `crewbo api` busy.
  let timer: NodeJS.Timeout | undefined;
  const schedule = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      await poll();
      if (vscode.window.state.focused) {
        schedule();
      }
    }, pollEvery);
  };
  ctx.subscriptions.push(
    vscode.window.onDidChangeWindowState((w) => w.focused && schedule()),
    { dispose: () => (clearTimeout(timer), client.stop()) },
  );
  void poll().then(schedule);
  return { state: () => state, statusBar: () => bar.text, tree, decoration: (uri) => decoration(uri, state), crewPanelVisible: () => panel.visible };
}

export function deactivate(): void {}

/** repoRoot walks up to the nearest directory holding .git, like hooks.RepoRoot. */
function repoRoot(dir: string): string | undefined {
  for (let d = dir; ; d = dirname(d)) {
    if (existsSync(join(d, ".git"))) {
      return d;
    }
    if (dirname(d) === d) {
      return undefined;
    }
  }
}

function decoration(uri: vscode.Uri, s: State | undefined): vscode.FileDecoration | undefined {
  if (!s || uri.scheme !== "file") {
    return undefined;
  }
  for (const r of s.repos) {
    const rel = relative(r.root, uri.fsPath);
    if (rel.startsWith("..") || rel === "") {
      continue;
    }
    const cs = claimsOn(rel.split(sep).join("/"), r.claims, new Date());
    if (cs.length > 0) {
      const tip = cs.map((c) => `claimed by ${holderLabel(c, s.status.login)}`).join("; ");
      return new vscode.FileDecoration(badge(cs), visible(tip));
    }
  }
  return undefined;
}

/** CrewTree is the Crewbo view: claims, tasks and handoff requests. */
class CrewTree implements vscode.TreeDataProvider<Node> {
  constructor(
    private readonly state: () => State | undefined,
    readonly onDidChangeTreeData: vscode.Event<void>,
    private readonly badge: (color: number) => vscode.Uri,
  ) {}

  getChildren(n?: Node): Node[] {
    const s = this.state();
    if (!s) {
      return [];
    }
    if (n?.kind === "group") {
      return n.children;
    }
    if (n?.kind === "task") {
      return (n.task.conflicts ?? [])
        .filter((c) => c.paths.length > 0)
        .map((c) => ({ kind: "conflict", label: `conflicts with ${c.task}: ${c.paths.join(", ")}` }));
    }
    if (n) {
      return [];
    }
    const claims: Node[] = s.repos.flatMap((r) => r.claims.map((claim) => ({ kind: "claim" as const, claim, root: r.root })));
    const tasks: Node[] = s.tasks.filter((t) => t.id).map((task) => ({ kind: "task" as const, task }));
    const handoffs: Node[] = s.repos.flatMap((r) => r.handoffs.map((handoff) => ({ kind: "handoff" as const, handoff, root: r.root })));
    const pairs = conflictPairs(s.tasks);
    const drafts: Node[] = (s.drafts ?? []).map((draft) => ({ kind: "draft" as const, draft }));
    return [
      ...(drafts.length > 0 ? [{ kind: "group" as const, label: `Drafted tasks (${drafts.length}, not sent)`, children: drafts }] : []),
      { kind: "group", label: `Claims (${claims.length})`, children: claims },
      { kind: "group", label: `Tasks (${tasks.length}, ${pairs} ${pairs === 1 ? "conflict" : "conflicts"})`, children: tasks },
      { kind: "group", label: `Handoff requests (${openHandoffs(s).length} open)`, children: handoffs },
    ];
  }

  getTreeItem(n: Node): vscode.TreeItem {
    const s = this.state();
    const me = s?.status.login;
    const None = vscode.TreeItemCollapsibleState.None;
    switch (n.kind) {
      case "group":
        return new vscode.TreeItem(n.label, vscode.TreeItemCollapsibleState.Expanded);
      case "claim": {
        const it = new vscode.TreeItem(n.claim.Pattern, None);
        const text = s && taskText(n.claim, s.tasks);
        it.description = `claimed · ${visible(holderLabel(n.claim, me))}`;
        it.tooltip = visible(
          [
            `${n.claim.Pattern} is claimed by ${holderLabel(n.claim, me)} until ${new Date(n.claim.LeaseExpires).toLocaleTimeString()}`,
            n.claim.TaskID ? `Task ${n.claim.TaskID}${text ? `: ${text}` : ""}` : "",
          ]
            .filter(Boolean)
            .join("\n"),
        );
        // The holder's badge Bo (05-interface.md 3.1); a lock when they are not in the crew.
        const color = colorOf(n.claim.Holder.MemberID, s?.repos.find((r) => r.root === n.root)?.crew);
        it.iconPath = color === undefined ? new vscode.ThemeIcon("lock", new vscode.ThemeColor("crewbo.claimed")) : this.badge(color);
        it.contextValue = n.claim.Holder.MemberID === me ? "claim.mine" : "claim.other";
        return it;
      }
      case "task": {
        const t = n.task;
        const it = new vscode.TreeItem(visible((t.text ?? "").split("\n")[0] || `${t.id}`), t.conflicts?.some((c) => c.paths.length) ? vscode.TreeItemCollapsibleState.Expanded : None);
        it.description = t.sent ? `${t.state} · sent to ${t.assignee}` : `${t.state} · ${t.teammate ? "by" : "from"} ${t.author}`;
        it.tooltip = visible(`${t.text ?? ""}\n\nTask ${t.id}${t.detail ? `\n\n${t.detail}` : ""}`);
        it.contextValue = needsApproval(t) ? "task.approvable" : "task";
        it.command = { command: "crewbo.reviewTask", title: "Read task", arguments: [n] };
        if (!needsApproval(t)) {
          it.command = undefined;
        }
        return it;
      }
      case "conflict": {
        const it = new vscode.TreeItem(visible(n.label), None);
        it.description = "conflict";
        it.iconPath = new vscode.ThemeIcon("warning", new vscode.ThemeColor("crewbo.conflict"));
        return it;
      }
      case "draft": {
        const d = n.draft;
        const it = new vscode.TreeItem(visible(d.text.split("\n")[0]), None);
        it.description = `to ${d.assignee}${d.patterns?.length ? ` · ${d.patterns.join(", ")}` : ""}`;
        it.tooltip = visible(`Drafted by an agent for ${d.assignee}; not sent.\n\n${d.text}`);
        it.iconPath = new vscode.ThemeIcon("edit");
        it.contextValue = "draft";
        it.command = { command: "crewbo.sendDraft", title: "Review and send", arguments: [n] };
        return it;
      }
      case "handoff": {
        const h = n.handoff;
        const it = new vscode.TreeItem(visible(handoffLabel(h)), None);
        it.description = handoffState(h);
        it.tooltip = visible([h.reason, h.answer_reason && `${h.holder}: ${h.answer_reason}`].filter(Boolean).join("\n"));
        if (h.open) {
          it.iconPath = new vscode.ThemeIcon("arrow-swap", new vscode.ThemeColor("crewbo.handoff"));
        }
        it.contextValue = canAnswer(h, me) ? "handoff.answerable" : "handoff";
        return it;
      }
    }
  }
}

/** TaskTexts shows a task's full text as a read-only document. */
class TaskTexts implements vscode.TextDocumentContentProvider {
  constructor(private readonly state: () => State | undefined) {}

  provideTextDocumentContent(uri: vscode.Uri): string {
    const id = decodeURIComponent(uri.path.replace(/^\//, "").replace(/\.txt$/, ""));
    const t = this.state()?.tasks.find((t) => t.id === id);
    if (!t) {
      return `Task ${id} is not in your task list any more.`;
    }
    return [
      `Task ${t.id} from ${t.author}`,
      `Paths: ${(t.owned_paths ?? []).join(", ") || "none"}`,
      "",
      "----- task text -----",
      visible(t.text ?? ""),
      "----- end of task text -----",
      "",
      "Approving runs an agent on this machine with your sign-in, in a new worktree.",
      "You approve in the terminal that opens next, after the task is shown again.",
    ].join("\n");
  }
}

/**
 * CrewPanel is the crew panel (docs/brand/05-interface.md 3.5): a webview
 * that draws Bo with the brand's reference component. It runs no code
 * from the network: scripts and fonts come from the extension's media.
 */
class CrewPanel implements vscode.WebviewViewProvider {
  private view?: vscode.WebviewView;
  constructor(
    private readonly media: vscode.Uri,
    private readonly shown: () => void,
  ) {}

  get visible(): boolean {
    return !!this.view?.visible;
  }

  resolveWebviewView(view: vscode.WebviewView): void {
    this.view = view;
    const w = view.webview;
    w.options = { enableScripts: true, localResourceRoots: [this.media] };
    const uri = (f: string) => w.asWebviewUri(vscode.Uri.joinPath(this.media, f)).toString();
    const nonce = randomBytes(16).toString("hex");
    w.html = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${w.cspSource}; font-src ${w.cspSource}; script-src 'nonce-${nonce}' ${w.cspSource};">
<link rel="stylesheet" href="${uri("tokens.css")}"><link rel="stylesheet" href="${uri("bo.css")}"><link rel="stylesheet" href="${uri("crew.css")}">
</head><body><p class="head" id="head"></p><div class="crew" id="crew" role="list"></div>
<script type="module" nonce="${nonce}" src="${uri("crew.js")}"></script></body></html>`;
    w.onDidReceiveMessage((m: { type?: string }) => m?.type === "ready" && this.shown());
    view.onDidChangeVisibility(() => view.visible && this.shown());
  }

  post(msg: unknown): void {
    void this.view?.webview.postMessage(msg);
  }
}
