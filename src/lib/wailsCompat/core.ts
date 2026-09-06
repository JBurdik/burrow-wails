// Shim for "@tauri-apps/api/core"'s invoke(), backed by the /v2/ws transport.
//
// This used to be a switch over ~130 commands calling Wails-generated
// bindings. It is now a thin pass-through: the command table lives in Go
// (src-wails/remoteapi.go), which is also what decides what a remote client
// may reach. One dispatch, one place to audit — remote access stops being a
// second API surface that can drift.
//
// What stays here is the handful of cases that are client-side decisions
// rather than backend calls, plus the desktop-only Wails-event protocol the
// wire deliberately does not carry (see CLIENT_SIDE_COMMANDS below).
import {
  LocalEndpoint,
  NotifyFloatGrid,
  OpenGitPanelWindow,
  SendFloatSnapshot,
} from "../../../src-wails/frontend/wailsjs/go/main/App";
import { createTransport, type Transport } from "@/runtime/transport";

type Args = Record<string, any>;

/**
 * Wire names that never travel over /v2/ws — answered here, or handed to a
 * Wails binding because the reply comes back on a desktop-only Wails event
 * (see the cases below). Go's `remoteAllowed` therefore neither has nor needs
 * an entry for them.
 *
 * Exported because commandSurface.test.ts checks every `invoke("...")` call
 * site in src/ against that table, and this is the list of legitimate
 * absences — anything else missing is a real gap.
 */
export const CLIENT_SIDE_COMMANDS: ReadonlySet<string> = new Set([
  "detach_pty",
  "send_float_snapshot",
  "notify_float_grid",
  "open_git_panel_window",
]);

let transport: Transport | null = null;

/** The desktop's authorization is that it is in-process: it asks the binding
 *  for a fresh single-use ticket, including on every reconnect. */
export function desktopTransport(): Transport {
  if (!transport) {
    transport = createTransport(async () => {
      const info = await LocalEndpoint();
      return { wsUrl: info.ws_url, ticket: info.ticket };
    });
  }
  return transport;
}

export async function invoke<T = unknown>(cmd: string, args: Args = {}): Promise<T> {
  switch (cmd) {
    // Nothing to detach. The Go daemon broadcasts frames to every attached
    // client and a closed XTerm simply stops listening, while the PTY keeps
    // running for the next reattach. Deliberately NOT left to fall through:
    // XTerm.onBeforeUnmount awaits this before disposing, so a throw here
    // skipped renderAddon.dispose() + term.dispose() and leaked an xterm
    // instance (with its WebGL context) on every closed terminal.
    case "detach_pty":
      return undefined as T;

    // The daemon binding exposes live ids (string[]), while the legacy UI
    // contract expects session records. Normalize here so restored terminal
    // threads reattach to their existing PTY instead of allocating a new one
    // and consequently missing its status hooks. (`?? []`: a nil Go slice
    // arrives as JSON null, and a reply frame with no result at all arrives
    // as undefined — neither is a list to map over.)
    case "list_pty_sessions": {
      const ids = await desktopTransport().invoke<string[] | null>("list_pty_sessions");
      return (ids ?? [])
        .map((id) => Number(id))
        .filter((pty_id) => Number.isFinite(pty_id))
        .map((pty_id) => ({ pty_id, cwd: "", title: "", alive: true })) as T;
    }

    // AcpStart takes ONE Go struct (AcpStartOpts), so the wire carries the
    // whole options object under a single key — remoteapi.go's `acp_start`
    // names exactly one argument, `opts`, because one Go parameter cannot be
    // spread across several table entries. Call sites pass the fields flat
    // (AgentChat.acpStartPayload), which is the shape the old switch
    // assembled the struct from, so that assembly lives here now.
    //
    // `id` must be stringified on this side: callApp's number->string
    // coercion only looks at the TOP-LEVEL args the table names — here that
    // is `opts` itself — and never reaches inside the object, so a bare
    // numeric id would fail to unmarshal into AcpStartOpts.ID.
    case "acp_start":
      return desktopTransport().invoke<T>("acp_start", { opts: { ...args, id: String(args.id) } });

    // foldedOrd -1 is the "don't know" sentinel, which leaves the existing
    // fold mark alone (chatstore.go writes it only when >= 0). An absent
    // argument is the zero value 0 on the Go side — a real ordinal — and one
    // call site (AgentChat's localStorage migration) omits it, so the
    // sentinel has to be supplied here, exactly as the old switch did.
    case "save_chat_messages":
      return desktopTransport().invoke<T>("save_chat_messages", {
        ...args,
        foldedOrd: args.foldedOrd ?? -1,
      });

    // The desktop-only snapshot/window protocol stays on the Wails bindings —
    // the only calls that still do, apart from LocalEndpoint above, which is
    // the socket's own bootstrap rather than an app action.
    //
    // These three are in remoteapi.go's `remoteDenied`, with a reason that is
    // still true: they answer over runtime.EventsEmit (stubs.go), which only
    // the native window receives, so a client reaching them over the wire
    // could never see the reply. event.ts keeps the matching `float-*`
    // listeners on the Wails runtime for the same reason — the emitters and
    // the listeners of that channel move together, or it half-works.
    //
    // Go builds an event topic out of the pty id ("float-grid-"+ptyID), so
    // these take it as a STRING; callers pass the numeric leaf id, which the
    // typed bridge rejected outright with `json: cannot unmarshal number into
    // Go value of type string` on every terminal resize.
    case "send_float_snapshot":
      return SendFloatSnapshot(
        String(args.ptyId ?? args.pty_id),
        args.data,
        args.cols,
        args.rows,
      ) as Promise<T>;
    case "notify_float_grid":
      return NotifyFloatGrid(String(args.ptyId ?? args.pty_id), args.cols, args.rows) as Promise<T>;
    case "open_git_panel_window":
      return OpenGitPanelWindow() as Promise<T>;
  }

  return desktopTransport().invoke<T>(cmd, args);
}
