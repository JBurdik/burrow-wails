# OpenAI-compatible coding agents

Burrow includes a coding agent that connects directly to an OpenAI-compatible
Chat Completions server. It runs inside the Go backend and does not require
Claude Code, Codex, OpenCode, an API subscription, or an agent CLI.

## Connect a server

1. Start your model server separately (llama.cpp, LM Studio, Ollama, or another
   server exposing `/v1/chat/completions`). Load a model and chat template that
   support function/tool calling.
2. In **Settings → Providers → +**, add **OpenAI Compatible**.
3. Set **Base URL**, for example `http://127.0.0.1:8080/v1`, and an API key if
   your server requires authentication.
4. Click **Test connection & load models**, then select the default model.
   You can also enter the exact model ID manually if discovery is unavailable.
5. Set **Context window** to the context size configured on your server, and
   reserve a suitable **Maximum output tokens** budget (defaults: 32768/4096).
6. Choose this provider in the chat model picker and send a task.

The endpoint is contacted from the Burrow backend, including when using a
paired remote client. `localhost` means the backend machine. Each provider
instance has its own connection settings. API keys are stored in the same
configuration file as other provider settings and are masked in the editor.
Connection changes apply when starting or restarting a chat runtime.

## Agent behavior

The agent streams text and reasoning, reads and searches workspace files,
creates files, applies exact text edits, and runs shell commands for builds,
tests, and git. Tools appear in the existing chat transcript and approval UI.
Workspace `AGENTS.md` instructions and the chat's extra system instructions
are included in its prompt. File tools reject paths and symlinks escaping the
workspace. Shell commands run on the host with the provider's environment;
they are not an operating-system sandbox.

The chat's permission selector offers:

- **Supervised** (default): reads/searches are allowed; changes and commands
  require an explicit Allow once decision.
- **Plan**: reads/searches only; file changes and commands are rejected.
- **Full access**: file changes and commands execute without approval.

Sub-agent role profiles can select this provider. Its `auto` profile mode
means Full access. Launches use embedded chats; there is no separate terminal
CLI. The agent can use its command tool to run development commands.

Conversation history, including tool calls and results, is stored in SQLite.
Restarting or interrupting a chat preserves that context. Earlier conversation
and completed tool steps are summarized by the configured model as space runs
low. Clear chat removes the saved model conversation. An interrupted or
uncertain command is never automatically replayed after restarting.

## Verification and limitations

The automated tests use an HTTP server implementing model discovery, streamed
Chat Completions, and tool calls. They cover approvals, denial, Plan mode,
actual workspace edits and commands, context compaction, restart/resume,
interruption, credential redaction, and workspace path boundaries.

Compatibility with a particular model still depends on its server and chat
template correctly implementing OpenAI tool calls. A text-only model will not
become a reliable coding agent solely by selecting this provider. External MCP
servers and the other CLIs' proprietary tools are not automatically imported
into this built-in runtime. Use the existing CLI providers when you need those
provider-specific features.
