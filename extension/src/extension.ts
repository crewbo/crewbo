// Crewbo for VS Code, v0 (M5-05): claims, the tasks they belong to,
// conflict warnings and handoff requests, read from the local daemon
// through `crewbo api`. Approving a task opens a terminal running
// `crewbo task approve`, the only approval path (ADR 0006).

import { existsSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import * as vscode from "vscode";
import { Client, type Request } from "./client";
import {
  badge,
  canAnswer,
  claimsOn,
  type Claim,
  conflictPairs,
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
  | { kind: "handoff"; handoff: Handoff; root: string };

/** What activate returns: read-only hooks for the in-editor test (test/vscode). */
export interface Api {
  state: () => State | undefined;
  statusBar: () => string;
  tree: CrewTree;
  decoration: (uri: vscode.Uri) => vscode.FileDecoration | undefined;
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
  const tree = new CrewTree(() => state, changed.event);
  const view = vscode.window.createTreeView("crewbo.crew", { treeDataProvider: tree });
  const bar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 0);
  bar.command = "crewbo.crew.focus";
  bar.show();
  const texts = new TaskTexts(() => state);
  ctx.subscriptions.push(
    view,
    bar,
    changed,
    decoChanged,
    vscode.workspace.registerTextDocumentContentProvider("crewbo-task", texts),
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
      const tasks = await client.call({ method: "task_list" });
      const next: State = { status: st.status ?? { version: "" }, tasks: tasks.tasks ?? [], repos: [] };
      for (const root of repos()) {
        const cl = await client.call({ method: "claim_list", repo: root });
        const ho = await client.call({ method: "claim_requests", repo: root });
        // A repository outside a room has no handoff steps; that is not an error.
        next.repos.push({ root, claims: cl.claims ?? [], handoffs: ho.handoffs ?? [] });
      }
      state = next;
      down = undefined;
      notify(next);
    } catch (err) {
      state = undefined;
      down = (err as Error).message;
    }
    render();
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
  return { state: () => state, statusBar: () => bar.text, tree, decoration: (uri) => decoration(uri, state) };
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
    return [
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
        it.iconPath = new vscode.ThemeIcon("lock", new vscode.ThemeColor("crewbo.claimed"));
        it.contextValue = n.claim.Holder.MemberID === me ? "claim.mine" : "claim.other";
        return it;
      }
      case "task": {
        const t = n.task;
        const it = new vscode.TreeItem(`${t.id}`, t.conflicts?.some((c) => c.paths.length) ? vscode.TreeItemCollapsibleState.Expanded : None);
        it.description = t.sent ? `${t.state} · sent to ${t.assignee}` : `${t.state} · from ${t.author}`;
        it.tooltip = visible(`${t.text ?? ""}${t.detail ? `\n\n${t.detail}` : ""}`);
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
