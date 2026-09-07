import { describe, it, expect, vi } from "vitest";
import { createTransport, MAX_CONNECT_FAILURES } from "./transport";
import { createBackoff } from "./reconnectBackoff";

/** Minimal scriptable WebSocket stand-in. */
class FakeWS {
  static instances: FakeWS[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  sent: string[] = [];
  readyState = 0;

  constructor(public url: string) {
    FakeWS.instances.push(this);
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
    this.onclose?.();
  }

  // test helpers
  open() {
    this.readyState = 1;
    this.onopen?.();
    this.deliver({ t: "welcome", environmentId: "env", scopes: [] });
  }
  deliver(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }
  lastCall() {
    return JSON.parse(this.sent[this.sent.length - 1]);
  }
}

function setup() {
  FakeWS.instances = [];
  const getEndpoint = vi.fn(async () => ({ wsUrl: "ws://x/v2/ws", ticket: "t" + FakeWS.instances.length }));
  const t = createTransport(getEndpoint, {
    WebSocketImpl: FakeWS as unknown as typeof WebSocket,
    backoff: createBackoff({ baseMs: 1, maxMs: 1, jitter: () => 0 }),
  });
  return { t, getEndpoint };
}

const tick = () => new Promise((r) => setTimeout(r, 0));

describe("createTransport", () => {
  it("puts the ticket in the url", async () => {
    setup();
    await tick();
    expect(FakeWS.instances[0].url).toContain("ticket=t0");
  });

  it("resolves invoke with the reply result", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];
    ws.open();

    const p = t.invoke<number>("answer");
    await tick();
    const call = ws.lastCall();
    expect(call.t).toBe("call");
    expect(call.cmd).toBe("answer");
    ws.deliver({ t: "reply", id: call.id, result: 42 });
    await expect(p).resolves.toBe(42);
  });

  it("queues an invoke made before the socket opens", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];

    const p = t.invoke("early");
    await tick();
    expect(ws.sent).toHaveLength(0); // nothing sent yet

    ws.open();
    await tick();
    const call = ws.lastCall();
    expect(call.cmd).toBe("early");
    ws.deliver({ t: "reply", id: call.id, result: "ok" });
    await expect(p).resolves.toBe("ok");
  });

  it("rejects invoke on an error reply", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];
    ws.open();

    const p = t.invoke("boom");
    await tick();
    ws.deliver({ t: "reply", id: ws.lastCall().id, error: { code: "call_failed", message: "no" } });
    await expect(p).rejects.toThrow("no");
  });

  it("rejects in-flight invokes when the socket closes", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];
    ws.open();

    const p = t.invoke("orphan");
    await tick();
    ws.close();
    await expect(p).rejects.toThrow(/disconnected/i);
  });

  it("routes events to listeners and honours unlisten", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];
    ws.open();

    const seen: unknown[] = [];
    const off = t.listen("phase-pty:7", (p) => seen.push(p));
    ws.deliver({ t: "event", name: "phase-pty:7", payload: { state: "running" } });
    expect(seen).toHaveLength(1);

    off();
    ws.deliver({ t: "event", name: "phase-pty:7", payload: { state: "done" } });
    expect(seen).toHaveLength(1);
  });

  it("keeps listeners registered across a reconnect", async () => {
    const { t } = setup();
    await tick();
    const first = FakeWS.instances[0];
    first.open();

    const seen: unknown[] = [];
    t.listen("pty-data-1", (p) => seen.push(p));

    first.close();
    await tick();
    await tick();
    expect(FakeWS.instances.length).toBeGreaterThan(1);

    const second = FakeWS.instances[FakeWS.instances.length - 1];
    second.open();
    second.deliver({ t: "event", name: "pty-data-1", payload: "hi" });
    expect(seen).toEqual(["hi"]);
  });

  it("asks for a fresh ticket on reconnect", async () => {
    const { t, getEndpoint } = setup();
    void t; // kept alive by the setup() closure; only getEndpoint is asserted here
    await tick();
    FakeWS.instances[0].open();
    FakeWS.instances[0].close();
    await tick();
    await tick();
    expect(getEndpoint.mock.calls.length).toBeGreaterThan(1);
  });

  it("drops a queued frame instead of resending it after its call was already rejected", async () => {
    const { t } = setup();
    await tick();
    const first = FakeWS.instances[0];
    // Socket is still CONNECTING (readyState 0), so invoke() queues into
    // outbox rather than sending immediately.
    const p = t.invoke("orphan-queued");
    await tick();
    expect(first.sent).toHaveLength(0);

    // Closing before the socket ever opened rejects the call via failPending
    // — this is the path that used to leave the frame sitting in outbox.
    first.close();
    await expect(p).rejects.toThrow(/disconnected/i);

    // Let the backoff-scheduled reconnect open a fresh socket and flush.
    await tick();
    await tick();
    const second = FakeWS.instances[FakeWS.instances.length - 1];
    second.open();
    await tick();

    const sentCmds = [...first.sent, ...second.sent].map((s) => JSON.parse(s).cmd);
    expect(sentCmds).not.toContain("orphan-queued");
  });

  // A connection that can NEVER be established used to settle nothing:
  // failPending() was reachable only from onclose and close(), so with no
  // socket ever constructed every invoke() stayed pending for the lifetime of
  // the page.
  describe("when the connection can never be established", () => {
    // The backoff is 1 ms, so a handful of macrotasks covers several attempts.
    const drain = async (n = 20) => {
      for (let i = 0; i < n; i++) await new Promise((r) => setTimeout(r, 2));
    };
    const fastBackoff = () => createBackoff({ baseMs: 1, maxMs: 1, jitter: () => 0 });

    it("retries when the socket constructor throws synchronously", async () => {
      // An empty ws_url (the backend's startup bailed before it had one) makes
      // `new WebSocket("?ticket=…")` throw a SyntaxError on the spot. That
      // throw is not an onclose, so nothing rescheduled — the app sat there
      // with no socket and no retry.
      FakeWS.instances = [];
      let attempts = 0;
      const Impl = function (url: string) {
        attempts++;
        if (attempts === 1) throw new SyntaxError("The URL's scheme must be either 'ws' or 'wss'");
        return new FakeWS(url);
      } as unknown as typeof WebSocket;

      createTransport(async () => ({ wsUrl: "ws://x/v2/ws", ticket: "t" }), {
        WebSocketImpl: Impl,
        backoff: fastBackoff(),
      });
      await drain(5);
      expect(attempts).toBeGreaterThan(1);
      expect(FakeWS.instances.length).toBeGreaterThan(0);
    });

    it("rejects an invoke made before the transport gave up", async () => {
      FakeWS.instances = [];
      const t = createTransport(async () => ({ wsUrl: "", ticket: "t" }), {
        WebSocketImpl: FakeWS as unknown as typeof WebSocket,
        backoff: fastBackoff(),
        maxConnectFailures: 3,
      });
      const p = t.invoke("hangs-forever");
      const settled = expect(p).rejects.toThrow(/unreachable/i);
      await drain();
      await settled;
      // No socket was ever constructed: this is the path with no onclose.
      expect(FakeWS.instances).toHaveLength(0);
    });

    it("rejects an invoke made after it gave up, without queueing it", async () => {
      const t = createTransport(async () => {
        throw new Error("no endpoint");
      }, {
        WebSocketImpl: FakeWS as unknown as typeof WebSocket,
        backoff: fastBackoff(),
        maxConnectFailures: 2,
      });
      await drain();
      await expect(t.invoke("late")).rejects.toThrow(/unreachable/i);
    });

    it("recovers once a socket finally opens", async () => {
      FakeWS.instances = [];
      let url = "";
      const t = createTransport(async () => ({ wsUrl: url, ticket: "t" }), {
        WebSocketImpl: FakeWS as unknown as typeof WebSocket,
        backoff: fastBackoff(),
        maxConnectFailures: 2,
      });
      await drain();
      await expect(t.invoke("while-down")).rejects.toThrow(/unreachable/i);

      url = "ws://x/v2/ws";
      await drain(5);
      const ws = FakeWS.instances[FakeWS.instances.length - 1];
      ws.open();

      const p = t.invoke("after-recovery");
      await new Promise((r) => setTimeout(r, 0));
      ws.deliver({ t: "reply", id: ws.lastCall().id, result: "ok" });
      await expect(p).resolves.toBe("ok");
    });
  });

  it("rejects invoke() called after close() instead of leaving it pending", async () => {
    const { t } = setup();
    await tick();
    FakeWS.instances[0].open();

    t.close();
    await expect(t.invoke("late")).rejects.toThrow(/closed/i);
  });

  it("gives a cold start more than the daemon's own 2s startup budget", async () => {
    // The desktop's startup() runs concurrently with the webview and has
    // daemon.Ensure() on its critical path, which alone blocks up to 2s before
    // the ticket store exists. A give-up budget under that turns a slow boot
    // into an app that rejects every onMounted call and never retries them.
    const delays: number[] = [];
    const b = createBackoff({ jitter: () => 0 });
    for (let i = 0; i < MAX_CONNECT_FAILURES; i++) delays.push(b.next());

    const budgetMs = delays.slice(0, -1).reduce((a, d) => a + d, 0);
    expect(budgetMs).toBeGreaterThan(10_000);
  });

  describe("resume", () => {
    it("does not resume on the first connection", async () => {
      // Nothing held means nothing to catch up on; the caller takes a
      // snapshot instead. Resuming from 0 would only earn a resync.
      setup();
      await tick();
      const ws = FakeWS.instances[0];
      ws.open();
      await tick();
      expect(ws.sent.map((s) => JSON.parse(s).t)).not.toContain("resume");
    });

    it("resumes from the last seq it saw after a reconnect", async () => {
      setup();
      await tick();
      const first = FakeWS.instances[0];
      first.open();
      first.deliver({ t: "shell", events: [{ seq: 12, name: "workspaces-changed" }] });

      first.close();
      await tick();
      await tick();
      const second = FakeWS.instances[FakeWS.instances.length - 1];
      second.open();
      await tick();

      const resume = second.sent.map((s) => JSON.parse(s)).find((f) => f.t === "resume");
      expect(resume?.since).toBe(12);
    });

    it("resumes from the welcome seq when it never saw an event", async () => {
      // A client that connects, never snapshots and then drops still has a
      // position: where the server was when it arrived.
      setup();
      await tick();
      const first = FakeWS.instances[0];
      first.readyState = 1;
      first.onopen?.();
      first.deliver({ t: "welcome", environmentId: "env", scopes: [], seq: 5 });

      first.close();
      await tick();
      await tick();
      const second = FakeWS.instances[FakeWS.instances.length - 1];
      second.open();
      await tick();

      const resume = second.sent.map((s) => JSON.parse(s)).find((f) => f.t === "resume");
      expect(resume?.since).toBe(5);
    });

    it("delivers shell events to the same listeners a live event uses", async () => {
      const { t } = setup();
      await tick();
      const ws = FakeWS.instances[0];
      ws.open();

      const seen: unknown[] = [];
      t.listen("phase-pty:7", (p) => seen.push(p));
      ws.deliver({ t: "shell", events: [{ seq: 3, name: "phase-pty:7", payload: { state: "done" } }] });
      expect(seen).toEqual([{ state: "done" }]);
    });

    it("drops an event it has already seen", async () => {
      // A reconnect can deliver the same event live and again in the resume's
      // deltas. Handlers fire sounds and OS notifications, so at-least-once
      // delivery is not good enough.
      const { t } = setup();
      await tick();
      const ws = FakeWS.instances[0];
      ws.open();

      const seen: unknown[] = [];
      t.listen("phase-pty:7", (p) => seen.push(p));
      ws.deliver({ t: "shell", events: [{ seq: 3, name: "phase-pty:7", payload: 1 }] });
      ws.deliver({ t: "shell", events: [{ seq: 3, name: "phase-pty:7", payload: 1 }] });
      ws.deliver({ t: "shell", events: [{ seq: 4, name: "phase-pty:7", payload: 2 }] });
      expect(seen).toEqual([1, 2]);
    });

    it("reports a resync so the caller can retake a snapshot", async () => {
      const { t } = setup();
      await tick();
      const ws = FakeWS.instances[0];
      ws.open();

      let resynced = 0;
      const off = t.onResync(() => resynced++);
      ws.deliver({ t: "resync" });
      expect(resynced).toBe(1);

      off();
      ws.deliver({ t: "resync" });
      expect(resynced).toBe(1);
    });

    it("reports going down and coming back up", async () => {
      // onState is what a UI showing "reconnecting" reads. Without it the
      // store would open a second socket just to observe the first one.
      const { t } = setup();
      const seen: boolean[] = [];
      t.onState((up) => seen.push(up));
      // Fires immediately with the current state, so a late subscriber is
      // not stuck showing "connecting" until the next transition.
      expect(seen).toEqual([false]);

      await tick();
      const first = FakeWS.instances[0];
      first.open();
      expect(seen).toEqual([false, true]);

      first.close();
      await tick();
      await tick();
      const second = FakeWS.instances[FakeWS.instances.length - 1];
      second.open();
      expect(seen).toEqual([false, true, false, true]);
    });

    it("does not repeat a state it is already in", async () => {
      const { t } = setup();
      await tick();
      const ws = FakeWS.instances[0];
      ws.open();

      const seen: boolean[] = [];
      t.onState((up) => seen.push(up));
      ws.open(); // a second onopen for the same socket must not re-notify
      expect(seen).toEqual([true]);
    });

    it("does not resume from a position the server already rejected", async () => {
      // After a resync the client holds nothing until it applies a snapshot
      // and reports its seq. Resuming from the dead position would earn
      // another resync, forever.
      const { t } = setup();
      await tick();
      const first = FakeWS.instances[0];
      first.open();
      first.deliver({ t: "shell", events: [{ seq: 12, name: "workspaces-changed" }] });
      first.deliver({ t: "resync" });

      first.close();
      await tick();
      await tick();
      let second = FakeWS.instances[FakeWS.instances.length - 1];
      second.open();
      await tick();
      expect(second.sent.map((s) => JSON.parse(s).t)).not.toContain("resume");

      // ...and once the caller has applied a snapshot, that seq is the new
      // resume point.
      t.noteSeq(40);
      second.close();
      await tick();
      await tick();
      second = FakeWS.instances[FakeWS.instances.length - 1];
      second.open();
      await tick();
      const resume = second.sent.map((s) => JSON.parse(s)).find((f) => f.t === "resume");
      expect(resume?.since).toBe(40);
    });
  });
});
