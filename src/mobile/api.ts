// Transport client for the axum /ws JSON-RPC-ish endpoint (src-tauri/src/http_server/).
//
// Wire format (read from websocket.rs / dispatch.rs, not guessed):
//   call:      client -> {id, command, args}      server -> {id, result} | {id, error}
//   subscribe: client -> {subscribe: "<event>"}    server -> {event, payload} once matching events fire
//   there is no unsubscribe message server-side (websocket.rs only ever inserts into the
//   subscribed set) — unsubscribe() here is client-side only: it stops routing that event
//   locally, the server keeps sending it into the void.
//
// pty-hook-{id} caveat: the "object" branch (error/session states, pid) uses app.emit()
// (Tauri-only), NOT emit_all() — it never reaches WS clients. Only the plain state STRING
// (running/waiting/permission/done) is broadcast over WS. So mobile can't see error/session.

export type CallResult = any;

interface PendingCall {
  resolve: (v: CallResult) => void;
  reject: (e: Error) => void;
}

export class BurrowWsClient {
  private ws: WebSocket | null = null;
  private nextId = 1;
  private pending = new Map<number, PendingCall>();
  private handlers = new Map<string, (payload: any) => void>();
  public onClose: (() => void) | null = null;

  connect(baseUrl: string, token: string): Promise<void> {
    const wsUrl = baseUrl.replace(/^http/, "ws").replace(/\/$/, "") + "/ws?token=" + encodeURIComponent(token);
    return new Promise((resolve, reject) => {
      let settled = false;
      const socket = new WebSocket(wsUrl);
      this.ws = socket;

      socket.onopen = () => {
        settled = true;
        resolve();
      };
      socket.onerror = () => {
        if (!settled) {
          settled = true;
          reject(new Error("WebSocket connection failed"));
        }
      };
      socket.onclose = () => {
        if (!settled) {
          settled = true;
          reject(new Error("WebSocket closed before opening (check host/token)"));
        }
        for (const p of this.pending.values()) p.reject(new Error("disconnected"));
        this.pending.clear();
        this.onClose?.();
      };
      socket.onmessage = (ev) => this.handleMessage(ev.data);
    });
  }

  private handleMessage(raw: string) {
    let msg: any;
    try {
      msg = JSON.parse(raw);
    } catch {
      return;
    }
    if (msg && typeof msg === "object" && "id" in msg) {
      const pending = this.pending.get(msg.id);
      if (!pending) return;
      this.pending.delete(msg.id);
      if ("error" in msg) pending.reject(new Error(typeof msg.error === "string" ? msg.error : JSON.stringify(msg.error)));
      else pending.resolve(msg.result);
      return;
    }
    if (msg && typeof msg === "object" && "event" in msg) {
      const handler = this.handlers.get(msg.event);
      handler?.(msg.payload);
    }
  }

  call(command: string, args: any = {}): Promise<CallResult> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error("not connected"));
    }
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws!.send(JSON.stringify({ id, command, args }));
    });
  }

  subscribe(event: string, handler: (payload: any) => void) {
    this.handlers.set(event, handler);
    this.ws?.send(JSON.stringify({ subscribe: event }));
  }

  unsubscribe(event: string) {
    this.handlers.delete(event);
  }

  close() {
    this.ws?.close();
    this.ws = null;
  }

  static async healthCheck(baseUrl: string): Promise<boolean> {
    const res = await fetch(baseUrl.replace(/\/$/, "") + "/healthz", { signal: AbortSignal.timeout(5000) });
    return res.ok;
  }
}
