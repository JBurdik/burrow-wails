#!/usr/bin/env node
// Probe a running Burrow core's chat transcript wire (ADR 0001 phase B):
// connects like a local dev tool (control.token → /v2/local-ticket → /v2/ws),
// subscribes to one chat, sends ONE probe message through chat_submit and
// prints every splice / tail with its rev and timing. Read-only otherwise.
//
//   node scripts/probe-chat.mjs <chatId> [--text "probe"] [--wait 60] [--suffix dev]
//   (--suffix = BURROW_DATA_SUFFIX of the core, e.g. "dev" for wails3 dev)
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

const args = process.argv.slice(2);
const opt = (name, dflt) => {
  const i = args.indexOf(`--${name}`);
  return i >= 0 ? args[i + 1] : dflt;
};
const chatId = Number(args.find((a) => /^\d+$/.test(a)));
if (!chatId) {
  console.error("usage: probe-chat.mjs <chatId> [--text T] [--wait S] [--suffix S]");
  process.exit(2);
}
const text = opt("text", `probe ${new Date().toISOString()} — reply with one word`);
const waitS = Number(opt("wait", "60"));
const suffix = opt("suffix", process.env.BURROW_DATA_SUFFIX ?? "");
const dir = join(homedir(), "Library", "Application Support", suffix ? `burrow-wails-${suffix}` : "burrow-wails");
const port = readFileSync(join(dir, "hook.port"), "utf8").trim();
const token = readFileSync(join(dir, "control.token"), "utf8").trim();

const res = await fetch(`http://127.0.0.1:${port}/v2/local-ticket`, { method: "POST", headers: { Authorization: `Bearer ${token}` } });
if (!res.ok) throw new Error(`local-ticket: ${res.status}`);
const { ws_url, ticket } = await res.json();

const t0 = performance.now();
const ms = () => (performance.now() - t0).toFixed(0).padStart(6) + "ms";
const ws = new WebSocket(`${ws_url}?ticket=${encodeURIComponent(ticket)}`);
let nextId = 1;
const pending = new Map();
const call = (cmd, a) =>
  new Promise((resolve, reject) => {
    const id = nextId++;
    pending.set(id, { resolve, reject });
    ws.send(JSON.stringify({ t: "call", id, cmd, args: a }));
  });

let rev = null, lastTail = null, tails = 0, splices = 0, appended = 0;
const onEvent = (name, p) => {
  if (name === `chat-messages-changed-${chatId}`) {
    splices++;
    const ok = rev === null || p.baseRev === rev || p.authoritative;
    console.log(`${ms()} splice base=${p.baseRev} rev=${p.rev} from=${p.fromIndex} rows=${p.messages.length}${p.authoritative ? " AUTH" : ""}${ok ? "" : "  ← GAP (baseRev != held rev)"}`);
    rev = p.rev;
  } else if (name === `chat-tail-${chatId}`) {
    tails++;
    appended += p.append.length;
    const gap = lastTail === null ? "" : ` +${(performance.now() - lastTail).toFixed(0)}ms`;
    lastTail = performance.now();
    console.log(`${ms()} tail   rev=${p.rev} idx=${p.index} off=${p.offset} ${p.kind} +${p.append.length}ch${gap}${p.rev === rev ? "" : "  ← stale rev"}`);
  } else if (name === `phase-chat:${chatId}`) {
    console.log(`${ms()} phase  ${p.state}`);
  }
};
ws.onmessage = (m) => {
  const f = JSON.parse(m.data);
  if (f.t === "reply") {
    const p = pending.get(f.id);
    pending.delete(f.id);
    f.error ? p?.reject(new Error(f.error.message)) : p?.resolve(f.result);
  } else if (f.t === "event") onEvent(f.name, f.payload);
  else if (f.t === "shell") for (const e of f.events) onEvent(e.name, e.payload);
};
await new Promise((r, j) => { ws.onopen = r; ws.onerror = j; });

try {
  const sub = await call("chat_subscribe", { chatId, sinceRev: -1 });
  rev = sub.rev;
  console.log(`${ms()} subscribe rev=${sub.rev} splices=${sub.splices.length} inProgress=${sub.inProgress ? `#${sub.inProgress.index} ${sub.inProgress.text.length}ch` : "null"}`);
} catch (e) {
  console.log(`${ms()} chat_subscribe failed (core older than phase B?): ${e.message}`);
}
const page = await call("load_chat_messages_page", { chatId, before: -1, limit: 5 });
console.log(`${ms()} page rev=${page.rev} start=${page.start} total=${page.total}`);

const r = await call("chat_submit", { chatId, text, images: [], clientId: `probe-${randomUUID()}` });
console.log(`${ms()} submit ${r.sent ? "sent" : "queued"}`);

setTimeout(() => {
  console.log(`\n${splices} splice(s), ${tails} tail(s), ${appended} chars by tail, final rev ${rev}`);
  ws.close();
  process.exit(0);
}, waitS * 1000);
