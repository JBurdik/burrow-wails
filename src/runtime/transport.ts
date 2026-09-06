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
  close(): void;
}

interface Pending {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
}

/**
 * How many connection attempts may fail in a row before the transport declares
 * itself unreachable, settling everything waiting on it (see `unreachable`).
 *
 * With the default backoff (250 ms base, doubling) five attempts span ~7.5 s,
 * which is long enough to ride out a hook server that is still binding its
 * port at startup and short enough that a caller is never left staring at a
 * promise that will not settle.
 */
const MAX_CONNECT_FAILURES = 5;

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
      const set = listeners.get(frame.name);
      if (!set) return;
      for (const h of set) h(frame.payload);
      return;
    }
    // `welcome` needs no handling yet — phase 4 uses its seq to resume.
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

    close() {
      closed = true;
      failPending("transport closed");
      ws?.close();
      ws = null;
    },
  };
}
