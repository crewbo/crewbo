import { strict as assert } from "node:assert";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, test } from "node:test";
import { Client } from "../src/client";

// A fake `crewbo` whose `api` behaves as FAKE_MODE says. A script with a
// shebang runs on Linux and macOS only; Windows needs a .cmd wrapper.
const dir = mkdtempSync(join(tmpdir(), "crewbo-ext-"));
const fake = join(dir, "crewbo");
writeFileSync(
  fake,
  `#!${process.execPath}
const mode = process.env.FAKE_MODE;
if (process.argv[2] !== "api") process.exit(2);
if (mode === "down") { process.stderr.write("crewbo: the daemon is not running; start it with \`crewbo daemon\`\\n"); process.exit(1); }
const held = [];
require("node:readline").createInterface({ input: process.stdin }).on("line", (line) => {
  const req = JSON.parse(line);
  if (mode === "silent") return;
  const resp = req.method === "refuse" ? { id: req.id, error: "no" } : { id: req.id, note: req.method + " " + (req.repo || "") };
  if (mode === "reorder") { held.push(resp); if (held.length === 2) held.reverse().forEach((r) => process.stdout.write(JSON.stringify(r) + "\\n")); return; }
  process.stdout.write(JSON.stringify(resp) + "\\n");
});
`,
);
chmodSync(fake, 0o755);
after(() => rmSync(dir, { recursive: true, force: true }));

const skip = process.platform === "win32" ? "shebang scripts do not run on Windows" : false;

test("matches responses to requests by id", { skip }, async () => {
  process.env.FAKE_MODE = "reorder";
  const c = new Client(fake);
  try {
    const [a, b] = await Promise.all([c.call({ method: "status" }), c.call({ method: "claim_list", repo: "/r" })]);
    assert.equal(a.note, "status ");
    assert.equal(b.note, "claim_list /r");
  } finally {
    c.stop();
  }
});

test("a daemon refusal resolves with its error", { skip }, async () => {
  process.env.FAKE_MODE = "echo";
  const c = new Client(fake);
  try {
    assert.equal((await c.call({ method: "refuse" })).error, "no");
    assert.equal((await c.call({ method: "status" })).note, "status ");
  } finally {
    c.stop();
  }
});

test("reports why crewbo api exited, then starts it again", { skip }, async () => {
  process.env.FAKE_MODE = "down";
  const c = new Client(fake);
  await assert.rejects(c.call({ method: "status" }), /^Error: the daemon is not running; start it with `crewbo daemon`$/);
  process.env.FAKE_MODE = "echo";
  try {
    assert.equal((await c.call({ method: "status" })).note, "status ");
  } finally {
    c.stop();
  }
});

test("a missing binary names the setting", async () => {
  const c = new Client(join(dir, "nope"));
  await assert.rejects(c.call({ method: "status" }), /set crewbo\.path in your user settings/);
});

test("a call with no answer times out and restarts the process", { skip }, async () => {
  process.env.FAKE_MODE = "silent";
  const c = new Client(fake, 200);
  await assert.rejects(c.call({ method: "status" }), /did not answer status in time/);
  process.env.FAKE_MODE = "echo";
  try {
    assert.equal((await c.call({ method: "status" })).note, "status ");
  } finally {
    c.stop();
  }
});
