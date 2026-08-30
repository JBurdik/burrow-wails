# 005 — Remote thread control and reliability

## Goal

Make threads reliable across restart and remote use. Remote becomes an
attention-first thread/review surface, with terminals as an explicit detail
view rather than the whole product model.

## Depends on

`001-thread-domain.md` through `004-manager-thread-orchestration.md`.

## Remote boundary

Pairing alone must not authorize all actions once a client can launch a thread,
send a prompt, approve a tool call, or edit review feedback. Introduce scoped
capabilities such as:

```text
threads:read     threads:operate
terminal:operate review:write
access:read      access:write
```

Enforce scopes for each WebSocket/RPC command.

## Implementation tasks

- [ ] Replace current remote-chat stubs with thread APIs: list/detail/timeline,
  launch/stop when allowed, approvals, and review feedback.
- [ ] Upgrade mobile to an attention-first list: Needs input, Review, Running,
  then completed threads. Terminal opens only on demand.
- [ ] On desktop restart, reconcile stored thread executions with live PTY,
  Claude, and ACP sessions. Unknown live state becomes `interrupted`, never
  falsely `done`.
- [ ] Deliver state as bounded, ordered snapshot + events. Correctness cannot
  depend only on UI polling.
- [ ] Add integration coverage for current workspace, optional worktree,
  permissions, restart during run, diff/review feedback, remote retry.
- [ ] Update `CLAUDE.md`, HTML architecture docs, and stale Tauri references to
  state that Go/Wails is current and clearly list intentional Wails-v2 gaps.

## Release gate

- [ ] `just check` passes from a clean checkout.
- [ ] Packaged build passes signing and update verification.
- [ ] Manual smoke test covers local current-workspace thread, worktree thread,
  restart/reconnect, and a paired remote review action.
- [ ] Before committing, run `gitnexus_detect_changes` and verify the affected
  execution flows match this plan.
