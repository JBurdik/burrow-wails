# 006 — Chat runtime in Go, chat session with a closed interface

Source: architecture review 2026-10-15, candidates #1 + #2. Vocabulary: `CONTEXT.md`
(**Chat runtime**, **Launch settings**, **Chat session**, **Pending request**).

## Problem

A chat's agent process lifecycle lives in `src/components/AgentChat.vue`:
`restartClaude` (~2227), `clearChat` (~2290), `ensureRuntime` (~2490) each build their own
`claude_start` payload and have already drifted (only `clearChat` falls back to the project's
`claude_config_dir` and drops `resumeSessionId`; `restartClaude` swallows errors; `ensureRuntime`
surfaces them). `src/mobile/store.ts:684` and `src-wails/remote.go` `RemoteCreateChat` have further
copies. Because the process starts from a mounted view, `spawn` (chat target goes through
`c.ui(ctx, "spawn", …)` in `internal/control/verbs_delegate.go`) and `chat_send` need a live desktop;
`SubAgentHost.vue` mounts hidden `AgentChat`s just to start processes; `claudeChats.ts` carries
`pendingSubagentPrompts` / `locallyCreatedSubagentIds` for the handover race.

`src/lib/chatSession.ts` exposes its state as writable Refs, so `restartClaude` and
`controlBridge.appendSubagentMessage` bypass `respond()` and can break the idle invariant.

## Decisions (settled)

1. **Chat runtime lives in Go**, keyed by chat id: ensure-started, send, restart, clear, interrupt.
   Works with no client attached.
2. **Launch settings live on the chat row** (SQLite `chats`): add `effort`, `permission_mode`,
   `profile_id`, `append_system_prompt` (`model` exists). "Last used" defaults for a *new* chat stay
   per device (out of scope here).
3. **One door for all transports now; migrate `claude-cli` first**, Codex + generic ACP in a second step.
4. **Profile resolution in Go**: row holds `profile_id`; Go resolves it against the providers config
   (`config.json`, providers store key) at every start, falling back to the project's
   `claude_config_dir`. Same order for every start.
5. **`ChatSend` ensures the process itself** (lazy start with resume from `claude_session_id`); no
   separate ensure step for callers. Fixes send-after-`ReapIdle`.
6. **Process stop/restart/death → neutral event from Go** (`session.exited` / `session.reset`);
   the `chatSession` reducer drops every pending request, closes the trailing `partial`, clears busy.
7. **`chatSession` interface**: read-only state + `respond(PendingAnswer)`, `send`, `interrupt`,
   `restart`, `clear` (the last four forward to Go). No writable Refs; only reducers mutate `messages`.
8. **Sub-agent row in the parent transcript** is written by Go into the parent's `chat_stream` as a new
   line kind at `spawn`; projection renders it. `appendSubagentMessage` goes away.
9. **Backfill**: one-time at startup, copy `chatModelByChat`, `chatEffortByChat`,
   `chatPermissionMode.byChat` and the per-chat profile id from `config.json` into rows. Read legacy keys
   one more release, then delete.
10. **`spawn` target chat runs fully in Go** via a chat-runtime capability on control `Deps`: create
    row, resolve launch settings (saved sub-agent profile / agent / model, else parent's), `ChatSend(task)`.
    UI learns via `chats-changed`. Tab spawn stays UI.
11. **`chat_set_launch_settings(id, patch)`**: Go writes the row and applies live where the provider can
    (Claude control set_model / set_permission_mode, ACP `AcpSetConfig` / `AcpSetMode`), else on next start.
12. **`ChatClear` in Go**: wipe `chat_messages`, `chat_stream`, `claude_session_id`, stop process, emit reset.
    Client drops only local draft/images/queue.
13. **Wire**: `chat_send`, `chat_restart`, `chat_clear`, `chat_interrupt`, `chat_set_launch_settings` in
    `remoteAllowed` (operate scope). After the Claude migration `claude_start` / `claude_send` move to
    `remoteDenied`. `remote_create_chat` becomes create + launch settings; start is `chat_send`'s job.
14. **Test seam**: the Go chat runtime takes a process-starter interface — two adapters,
    `agentproc.Manager` (real) and a fake (tests). Tests through the runtime's interface: lazy start,
    resume, start after reap, restart emits reset, profile resolution + `claude_config_dir` fallback.

## Delivery

| PR | Content | Behaviour change |
|----|---------|------------------|
| PR1 | Launch-settings columns, backfill, Go profile resolution (+ unit tests) | none |
| PR2 | Go chat runtime for `claude-cli`; `chat_*` commands; reset reducer; AgentChat / controlBridge / mobile / `spawn` rewired; delete `SubAgentHost` + prompt handover | yes |
| PR3 | `chatSession` read-only interface (#2) + Go-written sub-agent row (decision 8) | internal |
| PR4 | Codex + generic ACP into the runtime; `RemoteCreateChat` without its own `CodexStart` | yes |

Each PR targets `beta`, mergeable on its own. Keep `CLAUDE.md` / `docs/architecture.md` in sync.
