import type { EngineInterface, Register } from 'claude-code'

// Burrow's in-process half of `burrow hook` (src-wails/bin/burrow), loaded into
// every Claude Code in a Burrow PTY via CLAUDE_CODE_PLUGIN_DIRS. Only events
// the built-in guard (cc-plugin-sec-default) lets a user-tier mod see: on a
// Team/Enterprise login it bypasses classic.* and prompt.compose/context, so
// turn end, permission and session metadata stay with the settings hook.

const RULE = `## Burrow IDE — sub-agent delegation rule

You are running inside **Burrow** IDE. To delegate work to sub-agents or fan out tasks in parallel, use \`burrow spawn\` via Bash — NOT the built-in \`Agent\` tool or \`fork\`. In-process agents don't appear as Burrow tabs, so the user can't watch or steer them. Type \`/burrow\` to load the full delegation guide.`

// Same tools `burrow hook` flags as "needs you".
const BLOCKING = new Set(['AskUserQuestion', 'ExitPlanMode'])

let ptyId: string | undefined
let home: string | undefined
let envPort: string | undefined
let ruleSent = false

// Fire-and-forget: a dead app must never stall the agent. hook.port is
// authoritative (rewritten each launch); the env port goes stale after a
// restart, so it's the fallback — same order as `burrow status`.
function report($: EngineInterface, state: string) {
  if (!ptyId) return
  const body = JSON.stringify({ ptyId: Number(ptyId), state })
  void (async () => {
    const filePort = home ? (await $.fs.read(`${home}/hook.port`).catch(() => '')).split('\n')[0]?.trim() : ''
    for (const port of [filePort, envPort]) {
      if (!port) continue
      const r = await $.http
        .fetch(`http://127.0.0.1:${port}/hook`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body })
        .catch(() => undefined)
      if (r?.ok) return
    }
  })()
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    ptyId = await $.env.get('BURROW_PTY_ID')
    home = await $.env.get('BURROW_HOME_DIR')
    envPort = await $.env.get('BURROW_HOOK_PORT')
    // Tells `burrow hook` (a child of this process) that tool events are ours.
    if (ptyId) await $.env.set('BURROW_MOD', '1')
    return next(e)
  })

  // Replaces the PreToolUse/PostToolUse settings hooks: two shell spawns and a
  // curl per tool call. Running again after the call also ends a blocking
  // tool's wait and a permission prompt, as PostToolUse did.
  on('tool.call', async ($, e, next) => {
    report($, BLOCKING.has(e.tool) ? 'waiting' : 'running')
    const r = await next(e)
    report($, 'running')
    return r
  })

  // The system prompt is guarded, so the rule rides the first prompt as context
  // the model reads and the user never sees. ponytail: once per session; a
  // compaction may summarise it away, re-send on session.compact if that bites.
  on('prompt.submit', async ($, e, next) => {
    if (!ptyId || ruleSent) return next(e)
    ruleSent = true
    return next({ ...e, context: [...(e.context ?? []), RULE] })
  })
}
