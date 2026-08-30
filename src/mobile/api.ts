// Transport client for the /ws endpoint (src-wails/httpserver.go).
//
// Wire format:
//   call:      client -> {id, command, args}      server -> {id, result} | {id, error}
//   subscribe: client -> {subscribe: "<event>"}    server -> {event, payload}
//   The server keeps no subscription state: it broadcasts every event to every client
//   and we route by the handler map below. So subscribe() is really "start listening"
//   and unsubscribe() is client-side only — the server keeps sending into the void.
//
// The command allow-list lives in HTTPServer.dispatch and is terminal-only for now
// (list_workspaces, list_terminal_tabs, write_pty, resize_pty). The chat commands
// this file also calls are not served yet — RemoteListChats/RemoteCreateChat are
// still stubs in src-wails/stubs.go, so ChatsView does not work remotely.

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

  // Trades the six-digit pairing code shown in desktop Settings for the
  // bearer token, so the token never has to be typed on a phone. The code is
  // single-use server-side; five wrong ones lock pairing until it is
  // regenerated on the desktop.
  static async pair(baseUrl: string, code: string): Promise<string> {
    const res = await fetch(baseUrl.replace(/\/$/, "") + "/pair", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code }),
      signal: AbortSignal.timeout(10000),
    });
    if (res.status === 401) throw new Error("Neplatný kód");
    if (res.status === 429) throw new Error("Párování zamčené — vygeneruj nový kód v Nastavení");
    if (!res.ok) throw new Error(`Párování selhalo (${res.status})`);
    const { token } = await res.json();
    if (!token) throw new Error("Server nevrátil token");
    return token;
  }

  static async healthCheck(baseUrl: string): Promise<boolean> {
    const res = await fetch(baseUrl.replace(/\/$/, "") + "/healthz", { signal: AbortSignal.timeout(5000) });
    return res.ok;
  }
}
