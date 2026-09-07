/**
 * The client half of the /v2/ws protocol (src-wails/remoteproto.go).
 *
 * This is the ONLY place the desktop and the remote client differ: both use
 * this transport, and what varies is the EndpointSource they hand it. Remote
 * access is therefore not a feature — it is a different URL.
 *
 * Must not import from src/components, src/views, src/stores, src/mobile or
 * xterm (src/runtime/boundary.test.ts enforces it).
 */
import { createBackoff, type Backoff } from "./reconnectBackoff";

export interface EndpointSource {
  (): Promise<{ wsUrl: string; ticket: string }>;
}

export interface Transport {
  invoke<T = unknown>(cmd: string, args?: Record<string, unknown>): Promise<T>;
  /** Returns an unlisten function. Survives reconnects. */
  listen<T = unknown>(event: string, handler: (payload: T) => void): () => void;
  /**
   * Called when the server says the client's position has fallen out of the
   * replay ring (or the process restarted and the numbering began again).
   * The only way back is a fresh `shell_snapshot`; the transport cannot take
   * one itself, since it does not own the read model. Returns an unlisten.
   */
  onResync(handler: () => void): () => void;
  /**
   * Tell the transport how far the caller's state now reaches — the `seq` of
   * a snapshot it just applied. Only the caller knows this: a snapshot is one
   * RPC among many from here, and its result is opaque. Without it a resync
   * would be followed by a resume from a position the server already said was
   * gone, i.e. another resync.
   */
  noteSeq(seq: number): void;
  close(): void;
}

interface Pending {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
}

/**
 * Consecutive failed connection attempts before every pending call is rejected
 * with `transport unreachable`.
 *
 * Seven, against the default backoff (250 · 500 · 1000 · 2000 · 4000 · 8000 ·
 * 10000, capped at 10 s), puts the give-up at roughly 15.75 s: a failure
 * happens after each wait, so N failures cost the sum of the first N-1 delays.
 *
 * The floor is the desktop's own cold start. `startup()` creates the ticket
 * store late and runs concurrently with the webview, so the frontend can call
 * before it exists — and `daemon.Ensure()` alone blocks up to 2 s spawning the
 * daemon, before the DB migration, the bin write and the agent-docs install.
 * Give up sooner than that and a slow boot rejects every onMounted call, the
 * socket then recovers, and nobody re-issues them: an empty app until reload.
 */
export const MAX_CONNECT_FAILURES = 7;

export function createTransport(
  getEndpoint: EndpointSource,
  opts: {
    WebSocketImpl?: typeof WebSocket;
    backoff?: Backoff;
    maxConnectFailures?: number;
  } = {},
): Transport {
  const WS = opts.WebSocketImpl ?? WebSocket;
  const backoff = opts.backoff ?? createBackoff();
  const maxConnectFailures = opts.maxConnectFailures ?? MAX_CONNECT_FAILURES;

  let ws: WebSocket | null = null;
  let closed = false;
  let nextId = 1;
  // Consecutive attempts that never reached an open socket. Reset by onopen,
  // so a connection that worked and then dropped starts counting from zero.
  let connectFailures = 0;
  // Set once `maxConnectFailures` attempts in a row have failed: the transport
  // cannot be established, so callers are told that instead of waiting.
  // Cleared by the next socket that actually opens — reconnecting never stops.
  let unreachable = false;
  const pending = new Map<number, Pending>();
  // Frames written before the socket opened. The frontend calls invoke() from
  // onMounted while the connection is still being made, and failing those
  // would make startup order matter.
  let outbox: string[] = [];
  // Listeners are client-side: the server fans every event out to every
  // connection, so this map is the routing table and it must outlive a
  // socket — otherwise a reconnect silently stops delivering pty bytes.
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  const resyncHandlers = new Set<() => void>();
  // How far this client's view reaches. Moved forward by every numbered event
  // and by noteSeq(); never backward, so an out-of-order duplicate cannot
  // rewind it. 0 means "nothing held", which is what makes a first connection
  // skip resume and take a snapshot instead.
  let lastSeq = 0;

  function flush() {
    if (!ws || ws.readyState !== 1) return;
    for (const frame of outbox) ws.send(frame);
    outbox = [];
  }

  function failPending(reason: string) {
    for (const p of pending.values()) p.reject(new Error(reason));
    pending.clear();
    // Every queued frame belongs to one of the ids just rejected above — if it
    // stayed queued, the next successful reconnect's flush() would send it
    // verbatim for a call the caller was already told failed, and the
    // eventual reply would have nowhere to go (its id is no longer pending).
    outbox = [];
  }

  /**
   * One attempt failed to produce a working socket. Past the threshold the
   * transport is declared unreachable and every waiting call is settled: a
   * caller must never block forever on a connection that is not coming.
   */
  function noteConnectFailure(reason: string) {
    connectFailures++;
    if (unreachable || connectFailures < maxConnectFailures) return;
    unreachable = true;
    failPending(`transport unreachable: ${reason}`);
  }

  async function connect() {
    if (closed) return;
    // The socket construction is INSIDE this try, not just getEndpoint(): with
    // a malformed url (an empty ws_url from a backend that failed to finish
    // starting up gives `"?ticket=…"`, which is not a ws:// url) the
    // constructor throws SYNCHRONOUSLY. That throw used to escape connect(),
    // get swallowed by the `void connect()` below, and leave the app with no
    // socket, no error and no scheduled retry — a blank window, forever.
    try {
      // A ticket is single-use, so every attempt needs a fresh one.
      const endpoint = await getEndpoint();
      if (closed) return;
      if (!endpoint.wsUrl) throw new Error("no websocket url");

      const url = `${endpoint.wsUrl}?ticket=${encodeURIComponent(endpoint.ticket)}`;
      const socket = new WS(url);
      ws = socket;

      socket.onopen = () => {
        connectFailures = 0;
        unreachable = false;
        backoff.reset();
        // Ask for the gap BEFORE flushing queued calls, so the deltas the
        // client missed are read ahead of the replies to calls it makes now.
        // Skipped on a first connection (lastSeq 0): there is no position to
        // resume from, and the caller takes a snapshot instead.
        if (lastSeq > 0) socket.send(JSON.stringify({ t: "resume", since: lastSeq }));
        flush();
      };
      socket.onmessage = (ev: MessageEvent) => handleFrame(String(ev.data));
      socket.onclose = () => {
        if (ws === socket) ws = null;
        failPending("disconnected");
        noteConnectFailure("connection closed");
        scheduleReconnect();
      };
      socket.onerror = () => {
        // onclose always follows; reconnect is scheduled there so it cannot be
        // scheduled twice for one socket.
      };
    } catch (e) {
      noteConnectFailure(e instanceof Error ? e.message : String(e));
      scheduleReconnect();
    }
  }

  function scheduleReconnect() {
    if (closed) return;
    setTimeout(connect, backoff.next());
  }

  function handleFrame(data: string) {
    let frame: any;
    try {
      frame = JSON.parse(data);
    } catch {
      return;
    }
    if (frame?.t === "reply") {
      const p = pending.get(frame.id);
      if (!p) return;
      pending.delete(frame.id);
      if (frame.error) p.reject(new Error(frame.error.message || frame.error.code || "call failed"));
      else p.resolve(frame.result);
      return;
    }
    if (frame?.t === "event") {
      dispatch(frame.name, frame.payload);
      return;
    }
    if (frame?.t === "shell") {
      for (const ev of frame.events ?? []) {
        // A reconnect can deliver an event both live (the sink fired while
        // the resume was in flight) and again in the resume's deltas. Drop
        // anything at or behind where this client already is: handlers fire
        // notifications and sounds, so "at least once" is not good enough.
        if (typeof ev?.seq === "number" && ev.seq <= lastSeq) continue;
        if (typeof ev?.seq === "number") lastSeq = ev.seq;
        dispatch(ev.name, ev.payload);
      }
      return;
    }
    if (frame?.t === "resync") {
      // The caller retakes a snapshot and calls noteSeq with its seq. Until
      // it does, this client holds nothing resumable.
      lastSeq = 0;
      for (const h of resyncHandlers) h();
      return;
    }
    if (frame?.t === "welcome") {
      // Only as a floor, and only when this client holds nothing: a client
      // that connects, never snapshots and then drops still resumes from
      // where the server was when it arrived, rather than from zero.
      if (lastSeq === 0 && typeof frame.seq === "number") lastSeq = frame.seq;
      return;
    }
  }

  /** One dispatch for live and replayed events alike, so a replayed event and
   *  a live one cannot diverge. */
  function dispatch(name: string, payload: unknown) {
    const set = listeners.get(name);
    if (!set) return;
    for (const h of set) h(payload);
  }

  void connect();

  return {
    invoke<T>(cmd: string, args: Record<string, unknown> = {}): Promise<T> {
      if (closed) return Promise.reject(new Error("transport closed"));
      // Queueing here would be queueing onto a connection that has already
      // failed to come up `maxConnectFailures` times in a row; the reconnect
      // loop keeps running, and the first socket that opens clears this.
      if (unreachable) return Promise.reject(new Error("transport unreachable"));
      const id = nextId++;
      const frame = JSON.stringify({ t: "call", id, cmd, args });
      return new Promise<T>((resolve, reject) => {
        pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
        if (ws && ws.readyState === 1) ws.send(frame);
        else outbox.push(frame);
      });
    },

    listen<T>(event: string, handler: (payload: T) => void): () => void {
      const h = handler as (payload: unknown) => void;
      let set = listeners.get(event);
      if (!set) {
        set = new Set();
        listeners.set(event, set);
      }
      set.add(h);
      return () => {
        set!.delete(h);
        if (set!.size === 0) listeners.delete(event);
      };
    },

    onResync(handler: () => void): () => void {
      resyncHandlers.add(handler);
      return () => resyncHandlers.delete(handler);
    },

    noteSeq(seq: number) {
      if (seq > lastSeq) lastSeq = seq;
    },

    close() {
      closed = true;
      failPending("transport closed");
      ws?.close();
      ws = null;
    },
  };
}
