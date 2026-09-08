import { describe, it, expect, beforeEach, vi } from "vitest";
import {
  PairingRefusedError,
  RevokedError,
  clearRemoteCredentials,
  loadRemoteCredentials,
  pairDevice,
  remoteEndpointSource,
  saveRemoteCredentials,
  wsUrlFor,
} from "./remoteEndpoint";

// vitest runs with no DOM env here, so localStorage has to be supplied.
const store = new Map<string, string>();
(globalThis as any).localStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
  removeItem: (k: string) => void store.delete(k),
};

const json = (status: number, body: unknown) =>
  ({ ok: status >= 200 && status < 300, status, json: async () => body }) as Response;

beforeEach(() => {
  store.clear();
});

describe("wsUrlFor", () => {
  it("keeps the path prefix tailscale serve mounts us under", () => {
    // `tailscale serve` publishes /burrow, not /. Dropping the prefix
    // connects to whatever else this node serves at the root.
    expect(wsUrlFor("https://mac-mini.tailnet.ts.net/burrow")).toBe(
      "wss://mac-mini.tailnet.ts.net/burrow/v2/ws",
    );
    expect(wsUrlFor("https://host/burrow/")).toBe("wss://host/burrow/v2/ws");
    expect(wsUrlFor("http://127.0.0.1:37892")).toBe("ws://127.0.0.1:37892/v2/ws");
  });
});

describe("pairDevice", () => {
  it("stores the credentials it was given", async () => {
    const f = vi.fn(async () => json(200, { device_token: "tok", environment_id: "env" }));
    const creds = await pairDevice("https://host/burrow/", "123456", "iPhone", "phone", f as any);
    expect(creds).toEqual({ baseUrl: "https://host/burrow", deviceToken: "tok", environmentId: "env" });
    expect(loadRemoteCredentials()).toEqual(creds);
  });

  it("sends the device name so Settings can tell the rows apart", async () => {
    // A list of "Paired device" entries is not a list you can act on when
    // the phone you need to revoke is one of three.
    const f = vi.fn(async () => json(200, { device_token: "tok" }));
    await pairDevice("https://host", "123456", "iPhone", "phone", f as any);
    const body = JSON.parse((f.mock.calls[0] as any)[1].body);
    expect(body).toMatchObject({ code: "123456", name: "iPhone", kind: "phone" });
  });

  it("reports a refused code distinctly from an unreachable host", async () => {
    // Both are "it did not work" to the transport and two different things
    // for the user to fix.
    const refused = vi.fn(async () => json(401, {}));
    await expect(pairDevice("https://host", "000000", "p", "phone", refused as any)).rejects.toBeInstanceOf(
      PairingRefusedError,
    );

    const unreachable = vi.fn(async () => {
      throw new Error("network error");
    });
    const err = await pairDevice("https://host", "123456", "p", "phone", unreachable as any).catch((e) => e);
    expect(err).not.toBeInstanceOf(PairingRefusedError);
    expect(String(err)).toContain("Cannot reach");
  });

  it("refuses a success that carries no token", async () => {
    const f = vi.fn(async () => json(200, { environment_id: "env" }));
    await expect(pairDevice("https://host", "123456", "p", "phone", f as any)).rejects.toThrow(/device token/);
    expect(loadRemoteCredentials()).toBeNull();
  });
});

describe("remoteEndpointSource", () => {
  const creds = { baseUrl: "https://host/burrow", deviceToken: "tok", environmentId: "env" };

  it("asks for a fresh ticket on every attempt", async () => {
    // A ticket is single-use. A source that cached one would connect exactly
    // once and then reconnect-loop forever on a spent credential.
    let n = 0;
    const f = vi.fn(async () => json(200, { ticket: `t${++n}` }));
    const src = remoteEndpointSource(() => creds, f as any);
    expect((await src()).ticket).toBe("t1");
    expect((await src()).ticket).toBe("t2");
    expect(f).toHaveBeenCalledTimes(2);
  });

  it("never puts the device token in the url", async () => {
    // Spec §4 invariant 3 from the client side: the server refusing a token
    // in the query string is one half, not sending it is the other.
    const f = vi.fn(async () => json(200, { ticket: "t" }));
    const src = remoteEndpointSource(() => creds, f as any);
    const ep = await src();

    const [url, init] = (f.mock.calls[0] as any) as [string, RequestInit];
    expect(url).not.toContain("tok");
    expect((init.headers as any).Authorization).toBe("Bearer tok");
    expect(ep.wsUrl).not.toContain("tok");
    expect(ep.wsUrl).toBe("wss://host/burrow/v2/ws");
  });

  it("gives up and forgets the token when it has been revoked", async () => {
    // A revoked token gets 401 forever. Retrying it leaves the phone behind
    // a spinner with no route back to the pairing screen.
    saveRemoteCredentials(creds);
    const f = vi.fn(async () => json(401, {}));
    const src = remoteEndpointSource(() => loadRemoteCredentials(), f as any);
    await expect(src()).rejects.toBeInstanceOf(RevokedError);
    expect(loadRemoteCredentials()).toBeNull();
  });

  it("does not forget the token over a server error", async () => {
    // A 500 or a restart mid-request is not a revocation. Clearing here
    // would make the user re-pair over a transient failure.
    saveRemoteCredentials(creds);
    const f = vi.fn(async () => json(500, {}));
    const src = remoteEndpointSource(() => loadRemoteCredentials(), f as any);
    await expect(src()).rejects.toThrow(/ws ticket/);
    expect(loadRemoteCredentials()).not.toBeNull();
  });

  it("fails clearly when the device was never paired", async () => {
    clearRemoteCredentials();
    const src = remoteEndpointSource(() => loadRemoteCredentials());
    await expect(src()).rejects.toThrow(/not paired/);
  });
});
