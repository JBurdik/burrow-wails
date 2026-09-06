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

export function createTransport(
  getEndpoint: EndpointSource,
  opts: { WebSocketImpl?: typeof WebSocket; backoff?: Backoff } = {},
): Transport {
  const WS = opts.WebSocketImpl ?? WebSocket;
  const backoff = opts.backoff ?? createBackoff();

  let ws: WebSocket | null = null;
  let closed = false;
  let nextId = 1;
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

  async function connect() {
    if (closed) return;
    let endpoint: { wsUrl: string; ticket: string };
    try {
      // A ticket is single-use, so every attempt needs a fresh one.
      endpoint = await getEndpoint();
    } catch {
      scheduleReconnect();
      return;
    }
    if (closed) return;

    const url = `${endpoint.wsUrl}?ticket=${encodeURIComponent(endpoint.ticket)}`;
    const socket = new WS(url);
    ws = socket;

    socket.onopen = () => {
      backoff.reset();
      flush();
    };
    socket.onmessage = (ev: MessageEvent) => handleFrame(String(ev.data));
    socket.onclose = () => {
      if (ws === socket) ws = null;
      failPending("disconnected");
      scheduleReconnect();
    };
    socket.onerror = () => {
      // onclose always follows; reconnect is scheduled there so it cannot be
      // scheduled twice for one socket.
    };
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
