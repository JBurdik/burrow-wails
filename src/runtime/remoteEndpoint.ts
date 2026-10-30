/**
 * The remote client's half of the endpoint contract: pair once, then trade the
 * stored device token for a single-use websocket ticket on every connection
 * attempt.
 *
 * This is the ONLY thing that differs between the desktop and the phone. The
 * desktop's authorization is that it is in-process (`LocalEndpoint()`); the
 * phone's is a token it was given at pairing. Everything past this file — the
 * transport, the command table, the event names — is identical, which is the
 * whole point of the rewrite.
 *
 * Credentials live in localStorage rather than in `@/lib/config`, and that is
 * not laziness: config.ts reads through `invoke`, `invoke` needs a transport,
 * and the transport needs these credentials. Putting them in config would be
 * a cycle that deadlocks on first load.
 *
 * Must not import from src/components, src/views, src/stores, src/mobile or
 * xterm (src/runtime/boundary.test.ts enforces it).
 */
import type { EndpointSource } from "./transport";

export interface RemoteCredentials {
  /** Origin plus any path prefix, e.g. https://mac-mini.tailnet.ts.net/burrow */
  baseUrl: string;
  deviceToken: string;
  /**
   * Keyed on for client-side state (seen-at receipts, endpoint preference)
   * rather than the hostname, which changes when a tailnet IP is reassigned
   * or a machine is renamed.
   */
  environmentId: string;
}

/**
 * One paired machine. The catalog holds every machine this client was ever
 * paired to; exactly one is ACTIVE at a time — switching closes the socket and
 * reloads, there are no simultaneous connections (shell state, stores and
 * listeners all assume one backend).
 */
export interface RemoteEnvironment {
  /** From /v2/pair; the dedupe key. Falls back to baseUrl for a server that
   *  sends none, so an old build still gets exactly one row. */
  environment_id: string;
  name: string;
  baseUrl: string;
  deviceToken: string;
  addedAt: number;
  lastConnectedAt: number | null;
}

const ENVIRONMENTS_KEY = "burrow.remote.environments";
const ACTIVE_KEY = "burrow.remote.activeEnvironment";
/** The pre-catalog single slot. Read once, folded into the catalog, deleted. */
const LEGACY_KEY = "burrow.remote.credentials";

const envKey = (c: { environmentId?: string; environment_id?: string; baseUrl: string }) =>
  (c.environmentId ?? c.environment_id) || c.baseUrl;

function hostOf(baseUrl: string): string {
  try {
    return new URL(baseUrl).hostname.split(".")[0] || baseUrl;
  } catch {
    return baseUrl;
  }
}

function writeEnvironments(list: RemoteEnvironment[]): void {
  localStorage.setItem(ENVIRONMENTS_KEY, JSON.stringify(list));
}

/** Every paired machine, oldest first. Migrates the legacy slot on first read. */
export function listEnvironments(): RemoteEnvironment[] {
  if (typeof localStorage === "undefined") return [];
  const legacy = localStorage.getItem(LEGACY_KEY);
  if (legacy !== null) {
    localStorage.removeItem(LEGACY_KEY);
    try {
      const c = JSON.parse(legacy) as Partial<RemoteCredentials>;
      if (c.baseUrl && c.deviceToken) {
        upsertEnvironment({ baseUrl: c.baseUrl, deviceToken: c.deviceToken, environmentId: c.environmentId ?? "" });
      }
    } catch { /* unreadable slot: nothing to migrate */ }
  }
  try {
    const list = JSON.parse(localStorage.getItem(ENVIRONMENTS_KEY) ?? "[]");
    return Array.isArray(list) ? list.filter((e) => e?.baseUrl && e?.deviceToken) : [];
  } catch {
    return [];
  }
}

export function activeEnvironmentId(): string | null {
  if (typeof localStorage === "undefined") return null;
  listEnvironments(); // migration may set it
  return localStorage.getItem(ACTIVE_KEY);
}

/** null = none active: the phone boots into the switcher / pairing screen. */
export function setActiveEnvironment(id: string | null): void {
  if (typeof localStorage === "undefined") return;
  if (id) localStorage.setItem(ACTIVE_KEY, id);
  else localStorage.removeItem(ACTIVE_KEY);
}

/** Adds a machine, or refreshes its token if this environment_id is already
 *  in the catalog (re-pairing the same Mac must not make a second row). Makes
 *  it active and returns its id. */
export function upsertEnvironment(c: RemoteCredentials): string {
  const id = envKey(c);
  const list = listEnvironments();
  const hit = list.find((e) => e.environment_id === id);
  if (hit) Object.assign(hit, { baseUrl: c.baseUrl, deviceToken: c.deviceToken });
  else list.push({ environment_id: id, name: hostOf(c.baseUrl), baseUrl: c.baseUrl, deviceToken: c.deviceToken, addedAt: Date.now(), lastConnectedAt: null });
  writeEnvironments(list);
  setActiveEnvironment(id);
  return id;
}

export function renameEnvironment(id: string, name: string): void {
  const list = listEnvironments();
  const hit = list.find((e) => e.environment_id === id);
  if (!hit || !name.trim()) return;
  hit.name = name.trim();
  writeEnvironments(list);
}

/** Forgets one machine's credentials; clears the active pointer if it was it. */
export function removeEnvironment(id: string): void {
  writeEnvironments(listEnvironments().filter((e) => e.environment_id !== id));
  if (localStorage.getItem(ACTIVE_KEY) === id) setActiveEnvironment(null);
}

function markConnected(id: string): void {
  const list = listEnvironments();
  const hit = list.find((e) => e.environment_id === id);
  if (!hit) return;
  hit.lastConnectedAt = Date.now();
  writeEnvironments(list);
}

/** Thrown when the stored token is no longer accepted. Distinct from a
 *  network failure because the fix is different: re-pair, do not retry. */
export class RevokedError extends Error {
  constructor() {
    super("This device is no longer paired");
    this.name = "RevokedError";
  }
}

/** Thrown when pairing itself was refused — a wrong, expired or locked-out
 *  code. Distinct from an unreachable host for the same reason. */
export class PairingRefusedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "PairingRefusedError";
  }
}

/** The ACTIVE machine's credentials, or null when none is active. */
export function loadRemoteCredentials(): RemoteCredentials | null {
  const id = activeEnvironmentId();
  const e = id ? listEnvironments().find((x) => x.environment_id === id) : undefined;
  return e ? { baseUrl: e.baseUrl, deviceToken: e.deviceToken, environmentId: e.environment_id } : null;
}

export function saveRemoteCredentials(c: RemoteCredentials): void {
  if (typeof localStorage === "undefined") return;
  upsertEnvironment(c);
}

/** Forgets the ACTIVE machine only; the rest of the catalog stays. */
export function clearRemoteCredentials(): void {
  const id = activeEnvironmentId();
  if (id) removeEnvironment(id);
}

// Desktop-only: whether THIS window should drive a paired remote environment
// instead of its own in-process backend. The phone has no such switch — it is
// never anything but remote — but the desktop's whole point today is that it
// always has a local backend to fall back to, and pairing one does not mean
// abandoning that. core.ts's activeTransport() reads this to decide between
// desktopTransport() and remoteTransport() when Wails runtime is present;
// outside Wails (the phone) this is never consulted at all.
const DESKTOP_USE_REMOTE_KEY = "burrow.desktop.useRemote";

export function desktopUsesRemote(): boolean {
  return typeof localStorage !== "undefined" && localStorage.getItem(DESKTOP_USE_REMOTE_KEY) === "1";
}

export function setDesktopUsesRemote(v: boolean): void {
  if (typeof localStorage === "undefined") return;
  if (v) localStorage.setItem(DESKTOP_USE_REMOTE_KEY, "1");
  else localStorage.removeItem(DESKTOP_USE_REMOTE_KEY);
}

/** Strips a trailing slash so joining a path never produces a double one. */
export function normalizeBaseUrl(url: string): string {
  return url.trim().replace(/\/+$/, "");
}

/** http(s) → ws(s), keeping any path prefix (`tailscale serve` mounts us
 *  under /burrow, so the prefix is not optional). */
export function wsUrlFor(baseUrl: string): string {
  return normalizeBaseUrl(baseUrl).replace(/^http/, "ws") + "/v2/ws";
}

/**
 * What the Settings QR encodes: `<baseUrl>/#pair=<code>`. The code rides in the
 * fragment, which a browser never sends to the server, so it cannot land in a
 * request or proxy log. The slash before `#` matters: the bundle loads its
 * assets relatively, and from `.../burrow` they would resolve against `/`.
 */
export function pairingUrl(baseUrl: string, code: string): string {
  return `${normalizeBaseUrl(baseUrl)}/#pair=${encodeURIComponent(code)}`;
}

/** The code in a `#pair=` fragment, or null. Exactly six digits or nothing. */
export function parsePairFragment(hash: string): string | null {
  const code = new URLSearchParams(hash.replace(/^#/, "")).get("pair");
  return code && /^\d{6}$/.test(code) ? code : null;
}

/**
 * Trades a six-digit pairing code for this device's own token.
 *
 * `name` and `kind` are what desktop Settings renders in its device list, so a
 * user revoking a lost phone can tell which row is which — a list of
 * "Paired device" entries is not a list you can act on.
 */
export async function pairDevice(
  baseUrl: string,
  code: string,
  name: string,
  kind: string,
  fetchImpl: typeof fetch = fetch,
): Promise<RemoteCredentials> {
  const base = normalizeBaseUrl(baseUrl);
  let res: Response;
  try {
    res = await fetchImpl(base + "/v2/pair", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code, name, kind }),
      signal: AbortSignal.timeout(10_000),
    });
  } catch (e) {
    // Unreachable host, wrong URL, no tailnet. Not a refusal — the code may
    // be perfectly good — so it must not read as one.
    throw new Error(`Cannot reach ${base}: ${e instanceof Error ? e.message : String(e)}`);
  }
  if (res.status === 401) {
    // The server answers one status for a wrong code, an expired code and a
    // locked-out endpoint, on purpose (which of the three it was is not
    // information an unauthenticated caller has earned), so this message has
    // to cover all three.
    throw new PairingRefusedError("Wrong or expired code. Check the code in desktop Settings, or generate a new one.");
  }
  if (!res.ok) throw new Error(`Pairing failed (${res.status})`);

  const body = (await res.json()) as { device_token?: string; environment_id?: string };
  if (!body.device_token) throw new Error("The server did not return a device token");
  const creds: RemoteCredentials = {
    baseUrl: base,
    deviceToken: body.device_token,
    environmentId: body.environment_id ?? "",
  };
  saveRemoteCredentials(creds);
  return creds;
}

/**
 * An EndpointSource for a paired device.
 *
 * Called again on EVERY connection attempt, because a ticket is single-use — a
 * source that cached one would work exactly once and then reconnect-loop
 * forever. The device token travels in an Authorization header and never in
 * the URL; only the ticket does, which is safe there precisely because it dies
 * on first use and after 30 s (spec §4 invariant 3).
 *
 * A 401 forgets that one environment and throws RevokedError. Without that a
 * revoked phone retries a dead token behind a "connecting…" spinner with no
 * way back to the pairing screen.
 */
export function remoteEndpointSource(
  get: () => RemoteCredentials | null,
  fetchImpl: typeof fetch = fetch,
): EndpointSource {
  return async () => {
    const creds = get();
    if (!creds) throw new Error("this device is not paired");

    const res = await fetchImpl(normalizeBaseUrl(creds.baseUrl) + "/v2/ws-ticket", {
      method: "POST",
      headers: { Authorization: `Bearer ${creds.deviceToken}` },
      signal: AbortSignal.timeout(10_000),
    });
    if (res.status === 401) {
      // Only THIS machine is dead; the others in the catalog are untouched.
      removeEnvironment(envKey(creds));
      throw new RevokedError();
    }
    if (!res.ok) throw new Error(`could not get a ws ticket (${res.status})`);

    const body = (await res.json()) as { ticket?: string };
    if (!body.ticket) throw new Error("the server did not return a ticket");
    markConnected(envKey(creds));
    return { wsUrl: wsUrlFor(creds.baseUrl), ticket: body.ticket };
  };
}
