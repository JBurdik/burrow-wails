import { test, expect, mock } from 'claude-code/testing'

const START = { cwd: '/w', surface: 'terminal', isInteractive: true } as never

test('reports tool phases to the hook server and attaches the rule once in a Burrow PTY', async ($, on) => {
  mock.env(on, { BURROW_PTY_ID: '7', BURROW_HOOK_PORT: '4242' })
  const posts: string[] = []
  const contexts: (readonly string[] | undefined)[] = []
  on('http.fetch', async (_$, e) => {
    expect(e.url).toBe('http://127.0.0.1:4242/hook')
    posts.push(String(e.init?.body))
    return { value: { status: 200, ok: true, headers: {}, text: '' } }
  })
  on('session.start', async (_$, e) => ({ cwd: e.cwd }) as never)
  on('env.set', async () => ({ value: undefined }) as never)
  on('tool.call', async () => ({ result: { text: 'ok' } }) as never)
  on('prompt.submit', async (_$, e) => (contexts.push(e.context), { text: e.text }))

  await $.session.start(START)
  await $.tool.call({ tool: 'AskUserQuestion', questions: [] } as never)
  await $.prompt.submit({ text: 'a' })
  await $.prompt.submit({ text: 'b' })
  await new Promise(r => setTimeout(r, 0))

  expect(posts).toEqual(['{"ptyId":7,"state":"waiting"}', '{"ptyId":7,"state":"running"}'])
  expect(contexts[0]?.[0]).toContain('burrow spawn')
  expect(contexts[1]).toBeUndefined()
})

test('does nothing outside a Burrow PTY', async ($, on) => {
  mock.env(on, { BURROW_CHAT_ID: '3' })
  let fetched = false
  let context: readonly string[] | undefined
  on('http.fetch', async () => ((fetched = true), { value: { status: 200, ok: true, headers: {}, text: '' } }))
  on('session.start', async (_$, e) => ({ cwd: e.cwd }) as never)
  on('env.set', async () => ({ value: undefined }) as never)
  on('tool.call', async () => ({ result: { text: 'ok' } }) as never)
  on('prompt.submit', async (_$, e) => ((context = e.context), { text: e.text }))

  await $.session.start(START)
  await $.tool.call({ tool: 'Bash', command: 'ls' } as never)
  await $.prompt.submit({ text: 'a' })
  expect(fetched).toBe(false)
  expect(context).toBeUndefined()
})
