// Client for the Crewbo daemon through `crewbo api`, which relays one JSON
// request per line to the daemon's local API and writes one JSON response
// per line back. This file has no VS Code imports so it can be unit tested
// with plain Node.

import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createInterface } from "node:readline";

/** A request to the daemon's local API (internal/daemon/api.go). */
export interface Request {
  method: string;
  repo?: string;
  paths?: string[];
  ids?: string[];
  task?: string;
  text?: string;
  approve?: boolean;
}

/** A daemon response: `error` is set when the daemon refused the request. */
export interface Response {
  id: number;
  error?: string;
  note?: string;
  status?: import("./model").Status;
  claims?: import("./model").Claim[];
  tasks?: import("./model").Task[];
  handoffs?: import("./model").Handoff[];
  crew?: import("./model").CrewMember[];
  room_name?: string;
}

interface Waiter {
  resolve: (r: Response) => void;
  reject: (e: Error) => void;
  timer: NodeJS.Timeout;
  proc: ChildProcessWithoutNullStreams; // the process the request went to
}

/**
 * Client keeps one `crewbo api` process and matches responses to requests
 * by id. When the process exits (no daemon, daemon restarted) pending calls
 * fail with its error output and the next call starts a new process.
 */
export class Client {
  private proc?: ChildProcessWithoutNullStreams;
  private next = 0;
  private readonly waiting = new Map<number, Waiter>();

  constructor(
    private readonly bin: string,
    private readonly timeoutMs = 35_000,
  ) {}

  /** call sends req and resolves with the daemon's response. */
  call(req: Request): Promise<Response> {
    const proc = this.start();
    const id = ++this.next;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waiting.delete(id);
        reject(new Error(`the Crewbo daemon did not answer ${req.method} in time`));
        this.stop(); // the stream may be out of step now
      }, this.timeoutMs);
      this.waiting.set(id, { resolve, reject, timer, proc });
      proc.stdin.write(JSON.stringify({ ...req, id }) + "\n");
    });
  }

  /** stop ends the `crewbo api` process; pending calls fail. */
  stop(): void {
    this.proc?.kill();
    this.proc = undefined;
  }

  private start(): ChildProcessWithoutNullStreams {
    if (this.proc) {
      return this.proc;
    }
    const proc = spawn(this.bin, ["api"], { windowsHide: true });
    this.proc = proc;
    let stderr = "";
    proc.stderr.on("data", (b: Buffer) => {
      stderr = (stderr + b.toString()).slice(-2000);
    });
    createInterface({ input: proc.stdout }).on("line", (line) => {
      let resp: Response;
      try {
        resp = JSON.parse(line) as Response;
      } catch {
        return;
      }
      const w = this.waiting.get(resp.id);
      if (w) {
        clearTimeout(w.timer);
        this.waiting.delete(resp.id);
        w.resolve(resp);
      }
    });
    const fail = (msg: string) => {
      if (this.proc === proc) {
        this.proc = undefined;
      }
      for (const [id, w] of this.waiting) {
        if (w.proc !== proc) {
          continue; // sent to a newer process
        }
        clearTimeout(w.timer);
        this.waiting.delete(id);
        w.reject(new Error(msg));
      }
    };
    proc.on("error", (err: NodeJS.ErrnoException) => {
      fail(
        err.code === "ENOENT"
          ? `crewbo was not found at "${this.bin}"; set crewbo.path in your user settings`
          : `could not start crewbo: ${err.message}`,
      );
    });
    proc.on("close", () => {
      fail(stderr.trim().replace(/^crewbo: /, "") || "crewbo api exited");
    });
    proc.stdin.on("error", () => {}); // reported through "close"
    return proc;
  }
}
