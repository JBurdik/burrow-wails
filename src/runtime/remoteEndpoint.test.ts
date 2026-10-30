import { describe, it, expect, beforeEach, vi } from "vitest";
import {
  PairingRefusedError,
  RevokedError,
  activeEnvironmentId,
  clearRemoteCredentials,
  listEnvironments,
  removeEnvironment,
  renameEnvironment,
  loadRemoteCredentials,
  pairDevice,
  pairingUrl,
  parsePairFragment,
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

describe("pairing QR fragment", () => {
  it("round-trips a code through pairingUrl", () => {
    const url = pairingUrl("https://mac.tail1234.ts.net/burrow/", "042137");
    expect(url).toBe("https://mac.tail1234.ts.net/burrow/#pair=042137");
    expect(parsePairFragment(new URL(url).hash)).toBe("042137");
  });

  it("accepts the hash with or without its leading #", () => {
    expect(parsePairFragment("#pair=123456")).toBe("123456");
    expect(parsePairFragment("pair=123456")).toBe("123456");
    expect(parsePairFragment("#foo=1&pair=123456")).toBe("123456");
  });

  it("ignores anything that is not exactly six digits", () => {
    for (const h of ["", "#", "#/ws/3", "#pair=", "#pair=12345", "#pair=1234567", "#pair=12a456", "#pair=%20123456", "#code=123456"]) {
      expect(parsePairFragment(h)).toBeNull();
    }
  });
});

describe("environment catalog", () => {
  const ticketOk = vi.fn(async () => json(200, { ticket: "t" }));

  it("migrates the old single slot once and makes it active", () => {
    store.set("burrow.remote.credentials", JSON.stringify({ baseUrl: "https://mac.tail.ts.net/burrow", deviceToken: "tok", environmentId: "env-a" }));
    const list = listEnvironments();
    expect(list).toHaveLength(1);
    expect(list[0]).toMatchObject({ environment_id: "env-a", name: "mac", deviceToken: "tok", lastConnectedAt: null });
    expect(store.has("burrow.remote.credentials")).toBe(false);
    expect(activeEnvironmentId()).toBe("env-a");
    expect(loadRemoteCredentials()).toEqual({ baseUrl: "https://mac.tail.ts.net/burrow", deviceToken: "tok", environmentId: "env-a" });
    expect(listEnvironments()).toHaveLength(1);
  });

  it("adds a second machine instead of replacing, and dedupes re-pairing by environment_id", async () => {
    await pairDevice("https://a", "111111", "iPhone", "phone", (async () => json(200, { device_token: "a1", environment_id: "A" })) as any);
    await pairDevice("https://b", "222222", "iPhone", "phone", (async () => json(200, { device_token: "b1", environment_id: "B" })) as any);
    await pairDevice("https://a2", "333333", "iPhone", "phone", (async () => json(200, { device_token: "a2", environment_id: "A" })) as any);
    const list = listEnvironments();
    expect(list.map((e) => [e.environment_id, e.baseUrl, e.deviceToken])).toEqual([["A", "https://a2", "a2"], ["B", "https://b", "b1"]]);
    expect(activeEnvironmentId()).toBe("A");
    renameEnvironment("B", "  VPS ");
    expect(listEnvironments()[1].name).toBe("VPS");
  });

  it("a 401 forgets only the revoked machine", async () => {
    saveRemoteCredentials({ baseUrl: "https://a", deviceToken: "a", environmentId: "A" });
    saveRemoteCredentials({ baseUrl: "https://b", deviceToken: "b", environmentId: "B" });
    await expect(remoteEndpointSource(loadRemoteCredentials, (async () => json(401, {})) as any)()).rejects.toBeInstanceOf(RevokedError);
    expect(listEnvironments().map((e) => e.environment_id)).toEqual(["A"]);
    expect(activeEnvironmentId()).toBeNull();
    expect(loadRemoteCredentials()).toBeNull();
  });

  it("stamps lastConnectedAt on a granted ticket", async () => {
    saveRemoteCredentials({ baseUrl: "https://a", deviceToken: "a", environmentId: "A" });
    await remoteEndpointSource(loadRemoteCredentials, ticketOk as any)();
    expect(listEnvironments()[0].lastConnectedAt).toBeTypeOf("number");
    removeEnvironment("A");
    expect(listEnvironments()).toEqual([]);
  });
});
