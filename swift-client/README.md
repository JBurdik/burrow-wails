# BurrowSwift (experiment)

Native SwiftUI client for a running Burrow core. Same `/v2/ws` surface as the
Vue desktop and the phone — no backend changes.

Auth = the UI-process door: reads `hook.port` + `control.token` from
`$BURROW_HOME_DIR` (default `~/Library/Application Support/burrow-wails`) and
POSTs `/v2/local-ticket` for a single-use WS ticket.

```bash
cd swift-client && swift run        # dev
./make-app.sh                       # → build/BurrowSwift.app (Dock name + icon)
```

What works: threads grouped by repo (Needs you / per project / Settled), live
transcript (`chat-messages-changed-<id>` splices Go's folded tail), send /
queue / interrupt, Claude + ACP permission prompts (Allow / Deny), new thread
(⌘N, `remote_create_chat` + `chat_send`, titled like the tray panel), shared
read receipts. Not here: terminals, questions / plan answers, images,
settings, sub-agents.
