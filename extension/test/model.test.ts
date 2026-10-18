import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  badge,
  canAnswer,
  type Claim,
  claimsOn,
  conflictPairs,
  type Handoff,
  handoffLabel,
  handoffState,
  holderLabel,
  needsApproval,
  overlaps,
  type State,
  statusText,
  taskText,
  visible,
} from "../src/model";

// The table from internal/claims/overlap_test.go: the extension must mark a
// file claimed exactly when the daemon blocks it.
test("overlaps matches claims.Overlaps", () => {
  const cases: [string, string, boolean][] = [
    ["src/a.go", "src/a.go", true],
    ["src/a.go", "SRC/A.go", true],
    ["src/a.go", "src/b.go", false],
    ["src", "src/a.go", false],
    ["src/a.go", "src/*.go", true],
    ["src/a.go", "lib/*.go", false],
    ["src", "src/*.go", false],
    ["src", "src/**", true],
    ["src/deep/a.go", "src/**", true],
    ["*.go", "src/a.go", true],
    ["src/*.go", "src/*_test.go", true],
    ["src/*.go", "lib/*.go", false],
    ["src/*", "src/a/*.go", true],
    ["**", "anything/at/all", true],
    ["docs/[ab].md", "docs/a.md", true],
    ["docs/[ab].md", "web/a.md", false],
  ];
  for (const [a, b, want] of cases) {
    assert.equal(overlaps(a, b), want, `overlaps(${a}, ${b})`);
    assert.equal(overlaps(b, a), want, `overlaps(${b}, ${a})`);
  }
});

const claim = (pattern: string, member: string, session = "key/s1", task?: string, minutes = 30): Claim => ({
  ID: `c-${pattern}`,
  Pattern: pattern,
  Holder: { SessionID: session, MemberID: member },
  TaskID: task,
  LeaseExpires: new Date(Date.now() + minutes * 60_000).toISOString(),
});

test("claimsOn skips expired claims and other files", () => {
  const cs = [claim("src/**", "riya"), claim("src/a.go", "dev", "k/s", undefined, -1), claim("lib/b.go", "ronel")];
  assert.deepEqual(
    claimsOn("src/a.go", cs, new Date()).map((c) => c.Holder.MemberID),
    ["riya"],
  );
  assert.equal(claimsOn("README.md", cs, new Date()).length, 0);
});

test("holderLabel and badge name the holder", () => {
  assert.equal(holderLabel(claim("a", "riya")), "riya's agent");
  assert.equal(holderLabel(claim("a", "riya", "key/terminal")), "riya, from a terminal");
  assert.equal(holderLabel(claim("a", "riya", "terminal"), "riya"), "riya (you), from a terminal");
  assert.equal(holderLabel(claim("a", "")), "another agent session");
  assert.equal(badge([claim("a", "riya")]), "ri");
  assert.equal(badge([claim("a", "riya"), claim("b", "dev")]), "2");
  assert.equal(badge([claim("a", "")]), "?");
});

test("taskText and needsApproval", () => {
  const tasks = [
    { id: "t-1", room: "r", text: "fix login", state: "dispatched", author: "ronel" },
    { id: "t-2", room: "r", state: "dispatched", sent: true, assignee: "riya" },
    { id: "t-3", room: "r", state: "running" },
  ];
  assert.equal(taskText(claim("a", "riya", "k/s", "t-1"), tasks), "fix login");
  assert.equal(taskText(claim("a", "riya", "k/s", "t-9"), tasks), undefined);
  assert.equal(taskText(claim("a", "riya"), tasks), undefined);
  assert.deepEqual(
    tasks.filter(needsApproval).map((t) => t.id),
    ["t-1"],
  );
});

const request = (over: Partial<Handoff>): Handoff => ({
  kind: "request",
  request_id: "r-1",
  claim_id: "c-1",
  path: "token.go",
  holder: "riya",
  actor: "arjun",
  ...over,
});

test("handoff copy and who can answer", () => {
  assert.equal(handoffLabel(request({})), "arjun wants token.go next");
  assert.equal(handoffLabel(request({ kind: "override", actor: "ronel" })), "ronel released riya's claim on token.go");
  assert.equal(handoffState(request({ open: true })), "waiting for riya");
  assert.equal(handoffState(request({ answer: "declined" })), "declined by riya");
  assert.equal(handoffState(request({})), "closed");
  assert.equal(handoffState(request({ kind: "override" })), "overridden by the lead");
  assert.equal(canAnswer(request({ open: true }), "riya"), true);
  assert.equal(canAnswer(request({ open: true }), "arjun"), false);
  assert.equal(canAnswer(request({ open: false }), "riya"), false);
  assert.equal(canAnswer(request({ open: true }), undefined), false);
});

test("statusText counts claims, open requests and conflict pairs", () => {
  const s: State = {
    status: { version: "v" },
    tasks: [
      { id: "t-1", room: "r", state: "checkpointed", conflicts: [{ task: "t-2", paths: ["a.go"] }] },
      { id: "t-2", room: "r", state: "checkpointed", conflicts: [{ task: "t-1", paths: ["a.go"] }, { task: "t-3", paths: [] }] },
    ],
    repos: [
      { root: "/r", claims: [claim("a", "x"), claim("b", "y")], handoffs: [request({ open: true }), request({ request_id: "r-2" })] },
    ],
  };
  assert.equal(conflictPairs(s.tasks), 1);
  assert.equal(statusText(s), "crewbo  2 claimed · 1 handoff · 1 conflict");
  s.tasks = [];
  assert.equal(statusText(s), "crewbo  2 claimed · 1 handoff · 0 conflicts");
});

test("visible escapes control characters like crewbo task show", () => {
  assert.equal(visible("a\tb\nc"), "a\tb\nc");
  assert.equal(visible("x\x1b[2Jy"), "x\\x1b[2Jy");
  assert.equal(visible("\r\x07\b\f\v"), "\\r\\a\\b\\f\\v");
  assert.equal(visible("evil\u202Etxt.exe"), "evil\\u202etxt.exe");
  assert.equal(visible("line\u2028sep\u0085"), "line\\u2028sep\\u0085");
  assert.equal(visible("ronel · café"), "ronel · café");
});
