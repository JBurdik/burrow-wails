import { describe, it, expect, vi } from "vitest";
import { createTransport } from "./transport";
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

  it("rejects invoke() called after close() instead of leaving it pending", async () => {
    const { t } = setup();
    await tick();
    FakeWS.instances[0].open();

    t.close();
    await expect(t.invoke("late")).rejects.toThrow(/closed/i);
  });
});
