// Call one /v2/ws command against the running core and print the reply.
//   node tools/probe.mjs <cmd> '<json args>'      e.g. node tools/probe.mjs list_pty_sessions '{}'
// With --listen <prefix> [seconds]: print events whose name starts with prefix instead.
import fs from "node:fs";
const home = process.env.BURROW_HOME_DIR ?? process.env.HOME + "/Library/Application Support/burrow-wails";
const port = fs.readFileSync(home + "/hook.port", "utf8").split("\n")[0].trim();
const token = fs.readFileSync(home + "/control.token", "utf8").trim();
const t = await (await fetch(`http://127.0.0.1:${port}/v2/local-ticket`, { method: "POST", headers: { Authorization: `Bearer ${token}` } })).json();
const ws = new WebSocket(`${t.ws_url}?ticket=${t.ticket}`);
const [a, b, c] = process.argv.slice(2);
if (a === "--listen") {
  setTimeout(() => process.exit(0), (Number(c) || 10) * 1000);
  ws.onmessage = (m) => {
    const f = JSON.parse(m.data);
    const evs = f.t === "shell" ? f.events : f.t === "event" ? [f] : [];
    for (const e of evs) if (e.name.startsWith(b)) console.log(e.name, JSON.stringify(e.payload).slice(0, 600));
  };
} else {
  ws.onopen = () => ws.send(JSON.stringify({ t: "call", id: 1, cmd: a, args: JSON.parse(b ?? "{}") }));
  ws.onmessage = (m) => { const f = JSON.parse(m.data); if (f.t === "reply") { console.log(JSON.stringify(f.error ?? f.result, null, 1).slice(0, 8000)); ws.close(); } };
}
