// Runs inside VS Code's extension host (code --extensionTestsPath), against
// a running daemon with one terminal claim on src/auth.go in the opened
// repository. Manual and local only: CI has no display (see README).

import { strict as assert } from "node:assert";
import { join } from "node:path";
import * as vscode from "vscode";
import type { Api } from "../../src/extension";

export async function run(): Promise<void> {
  const ext = vscode.extensions.getExtension<Api>("crewbo.crewbo");
  assert.ok(ext, "the extension is installed");
  const api = await ext.activate();
  const deadline = Date.now() + 15_000;
  while (!api.state() && Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 200));
  }
  assert.ok(api.state(), "read the daemon's state");
  assert.equal(api.statusBar(), "crewbo  1 claimed · 0 handoff · 0 conflicts");

  const root = vscode.workspace.workspaceFolders![0].uri.fsPath;
  const claimed = api.decoration(vscode.Uri.file(join(root, "src", "auth.go")));
  assert.ok(claimed?.badge && claimed.tooltip?.includes("claimed by"), `decoration: ${JSON.stringify(claimed)}`);
  assert.equal(api.decoration(vscode.Uri.file(join(root, "src", "token.go"))), undefined);

  const groups = api.tree.getChildren().map((n) => api.tree.getTreeItem(n).label);
  assert.deepEqual(groups, ["Claims (1)", "Tasks (0, 0 conflicts)", "Handoff requests (0 open)"]);
  const [claimNode] = api.tree.getChildren(api.tree.getChildren()[0]);
  const item = api.tree.getTreeItem(claimNode);
  assert.equal(item.label, "src/auth.go");
  assert.match(String(item.description), /^claimed · .+, from a terminal$/);
  console.log(`crewbo extension check passed: ${api.statusBar()} | ${claimed.badge} | ${claimed.tooltip} | ${item.description}`);
}
