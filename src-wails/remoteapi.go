package main

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// remoteScope is the capability a command needs. Scopes are enforced per
// command, not per connection: holding a ticket is not authorization to call
// everything (t3code's environment-auth profile, same reasoning).
type remoteScope string

const (
	scopeOrchRead    remoteScope = "orchestration:read"
	scopeOrchOperate remoteScope = "orchestration:operate"
	scopeTerminal    remoteScope = "terminal:operate"
	scopeAccessRead  remoteScope = "access:read"
	scopeAccessWrite remoteScope = "access:write"
)

// remoteCmd is one exposed App method.
//
// Args names the wire keys POSITIONALLY, because parameter names do not exist
// at runtime: Go's reflect does not carry them and Wails generates arg1..argN.
// This table is therefore the only place that knows an argument's name — which
// is fine, because it is also the only place that decides what is reachable at
// all.
type remoteCmd struct {
	Method string
	Args   []string
	Scope  remoteScope
}

// callApp invokes an allowed method with JSON arguments. It does marshalling
// and nothing else: no defaulting beyond the zero value, no name guessing, no
// fallback to a method the table did not name.
//
// recv is `any` rather than *App so the marshalling can be unit-tested against
// a fake carrying every return shape — several real App methods dereference
// a.daemon with no nil guard and would panic on a zero value. Real callers
// pass the *App; that the Method strings name something real is what
// TestRemoteAllowedArityMatchesMethods checks.
func callApp(recv any, c remoteCmd, args map[string]json.RawMessage) (any, error) {
	m := reflect.ValueOf(recv).MethodByName(c.Method)
	if !m.IsValid() {
		return nil, fmt.Errorf("no such method %q", c.Method)
	}
	mt := m.Type()
	if mt.NumIn() != len(c.Args) {
		return nil, fmt.Errorf("%s takes %d args, table names %d", c.Method, mt.NumIn(), len(c.Args))
	}

	in := make([]reflect.Value, mt.NumIn())
	for i, name := range c.Args {
		pv := reflect.New(mt.In(i))
		if rawArg, ok := args[name]; ok && len(rawArg) > 0 && string(rawArg) != "null" {
			if err := unmarshalArg(rawArg, pv); err != nil {
				return nil, fmt.Errorf("arg %q: %w", name, err)
			}
		}
		// An absent or null argument stays the zero value — core.ts's
		// `args.cwd ?? ""` behaviour, preserved.
		in[i] = pv.Elem()
	}

	out := m.Call(in)
	return splitResult(out)
}

// unmarshalArg decodes one argument, with one deliberate coercion: a JSON
// number into a string parameter. The frontend's pty id is its own numeric
// counter while every Go PTY method takes it as an opaque string key, and
// core.ts used to bridge that with String(args.id). Without this, no PTY call
// works at all.
func unmarshalArg(rawArg json.RawMessage, pv reflect.Value) error {
	if pv.Elem().Kind() == reflect.String {
		var n json.Number
		if json.Unmarshal(rawArg, &n) == nil && len(rawArg) > 0 && rawArg[0] != '"' {
			pv.Elem().SetString(n.String())
			return nil
		}
	}
	return json.Unmarshal(rawArg, pv.Interface())
}

// splitResult turns a method's return values into (result, error). Go methods
// here come in four shapes: (), (error), (T), (T, error).
func splitResult(out []reflect.Value) (any, error) {
	errType := reflect.TypeOf((*error)(nil)).Elem()
	var err error
	if n := len(out); n > 0 && out[n-1].Type().Implements(errType) {
		if e := out[n-1].Interface(); e != nil {
			err = e.(error)
		}
		out = out[:n-1]
	}
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out[0].Interface(), nil
}

// remoteAllowed is the whole remote surface. Ported one-for-one from the
// switch that used to live in src/lib/wailsCompat/core.ts, which is where the
// wire-name-to-method mapping has always been written down. Argument order is
// each Go method's real parameter order; argument names are the properties
// core.ts read off its `args` object (chosen when core.ts offered a
// camelCase/snake_case alternative pair via `??`).
//
// detach_pty is deliberately absent: core.ts's case for it never called Go
// (Promise.resolve()) — there is nothing to port, and Task 7 keeps it
// client-side.
var remoteAllowed = map[string]remoteCmd{
	// PTY — id is the frontend's own numeric counter, always coerced to a
	// string by callApp's number->string rule (core.ts used to String() it).
	"create_pty":         {Method: "CreatePty", Args: []string{"id", "cwd", "cols", "rows"}, Scope: scopeTerminal},
	"write_pty":          {Method: "WritePty", Args: []string{"id", "data"}, Scope: scopeTerminal},
	"resize_pty":         {Method: "ResizePty", Args: []string{"id", "cols", "rows"}, Scope: scopeTerminal},
	"kill_pty":           {Method: "KillPty", Args: []string{"id"}, Scope: scopeTerminal},
	"get_pty_foreground": {Method: "GetPtyForeground", Args: []string{"id"}, Scope: scopeOrchRead},
	"list_pty_sessions":  {Method: "ListPtySessions", Args: nil, Scope: scopeOrchRead},

	// Environment. environment_id is also what the WS handler test calls,
	// because it is safe on a bare &App{} — most commands are not.
	"environment_id":   {Method: "EnvironmentID", Args: nil, Scope: scopeOrchRead},
	"remote_endpoints": {Method: "RemoteEndpoints", Args: nil, Scope: scopeAccessRead},

	// Workspaces / tabs
	"list_workspaces":     {Method: "ListWorkspaces", Args: nil, Scope: scopeOrchRead},
	"create_workspace":    {Method: "CreateWorkspace", Args: []string{"name", "path"}, Scope: scopeOrchOperate},
	"delete_workspace":    {Method: "DeleteWorkspace", Args: []string{"id"}, Scope: scopeOrchOperate},
	"rename_workspace":    {Method: "RenameWorkspace", Args: []string{"id", "name"}, Scope: scopeOrchOperate},
	"touch_workspace":     {Method: "TouchWorkspace", Args: []string{"id"}, Scope: scopeOrchOperate},
	"set_workspace_icon":  {Method: "SetWorkspaceIcon", Args: []string{"id", "icon"}, Scope: scopeOrchOperate},
	"set_workspace_order": {Method: "SetWorkspaceOrder", Args: []string{"ids"}, Scope: scopeOrchOperate},
	"list_terminal_tabs":  {Method: "ListTerminalTabs", Args: []string{"workspaceId"}, Scope: scopeOrchRead},
	"save_terminal_tabs":  {Method: "SaveTerminalTabs", Args: []string{"workspaceId", "tabs"}, Scope: scopeOrchOperate},

	// Worktrees
	"create_worktree": {Method: "CreateWorktree", Args: []string{"repoPath", "name", "path", "branch", "baseRef"}, Scope: scopeOrchOperate},
	"remove_worktree": {Method: "RemoveWorktree", Args: []string{"id", "force"}, Scope: scopeOrchOperate},

	// Git / gh / text generation. RunGit/RunGh are generic passthroughs (they
	// can run a write command like `commit` or `push`), so they get the
	// mutating scope even though most calls in practice are reads. The
	// generate_* calls don't mutate anything themselves, but each one shells
	// out to a configured provider CLI with client-supplied prompt text and a
	// 180s budget (textgen.go) — spawning a host process and billing
	// inference is not what a read scope should authorize, so these are
	// operate scope despite returning only a string.
	"run_git":                 {Method: "RunGit", Args: []string{"cwd", "args"}, Scope: scopeOrchOperate},
	"run_gh":                  {Method: "RunGh", Args: []string{"cwd", "args"}, Scope: scopeOrchOperate},
	"generate_commit_message": {Method: "GenerateCommitMessage", Args: []string{"cwd", "model", "policy"}, Scope: scopeOrchOperate},
	"generate_chat_title":     {Method: "GenerateChatTitle", Args: []string{"cwd", "model", "policy", "text"}, Scope: scopeOrchOperate},
	"generate_branch_name":    {Method: "GenerateBranchName", Args: []string{"cwd", "model", "policy", "message"}, Scope: scopeOrchOperate},
	"generate_pr_content":     {Method: "GeneratePrContent", Args: []string{"cwd", "model", "policy", "baseBranch", "headBranch"}, Scope: scopeOrchOperate},

	// Checkpoints — pre-turn worktree snapshots (checkpoints.go)
	"create_checkpoint":  {Method: "CreateCheckpoint", Args: []string{"cwd", "ptyId", "label"}, Scope: scopeOrchOperate},
	"list_checkpoints":   {Method: "ListCheckpoints", Args: []string{"cwd", "limit"}, Scope: scopeOrchRead},
	"checkpoint_diff":    {Method: "CheckpointDiff", Args: []string{"cwd", "commit"}, Scope: scopeOrchRead},
	"restore_checkpoint": {Method: "RestoreCheckpoint", Args: []string{"cwd", "commit"}, Scope: scopeOrchOperate},

	// Workspace search (⌘P)
	"search_files": {Method: "SearchFiles", Args: []string{"cwd", "query", "limit"}, Scope: scopeOrchRead},

	// FS / misc
	//
	// LOAD-BEARING NOTE for whichever phase first hands scopeOrchRead/
	// scopeOrchOperate to a client that ISN'T the in-process desktop holding
	// every scope: orchestration:read grants unrestricted host file read
	// (ReadTextFile/ReadFileBase64 are a bare os.ReadFile(path), fs.go) and
	// orchestration:operate grants unrestricted host file write (WriteTextFile
	// is a bare os.WriteFile(path, ...), same file) — no root/workspace check
	// on either. So these two scopes are NOT a containment boundary; they sit
	// alongside access:read/access:write in name only. A session holding
	// orchestration:read can read <app-data>/control.token and
	// <app-data>/http.token by path, which is full authority over the
	// loopback control API and the tailnet bearer token respectively. Adding
	// a path guard belongs in fs.go, with the methods it constrains — not
	// here, and not as a side effect of this task — because it's a real
	// behaviour change to calls the desktop legitimately makes on arbitrary
	// paths (file tree, editor) and getting it wrong breaks both. This is a
	// hard prerequisite before any phase exposes a scoped-but-not-fully-
	// trusted session (e.g. a paired phone) to these two scopes over the
	// network; it is not exploitable today, where the only client is the
	// desktop itself, already holding every scope.
	"write_text_file":        {Method: "WriteTextFile", Args: []string{"path", "content"}, Scope: scopeOrchOperate},
	"read_text_file":         {Method: "ReadTextFile", Args: []string{"path"}, Scope: scopeOrchRead},
	"read_text_file_checked": {Method: "ReadTextFile", Args: []string{"path"}, Scope: scopeOrchRead},
	"read_file_base64":       {Method: "ReadFileBase64", Args: []string{"path"}, Scope: scopeOrchRead},
	"home_dir":               {Method: "HomeDir", Args: nil, Scope: scopeOrchRead},
	"config_file_path":       {Method: "ConfigFilePath", Args: nil, Scope: scopeOrchRead},
	"create_dir":             {Method: "CreateDir", Args: []string{"path"}, Scope: scopeOrchOperate},
	"read_dir_shallow":       {Method: "ReadDirShallow", Args: []string{"path"}, Scope: scopeOrchRead},
	"open_path_in":           {Method: "OpenPathIn", Args: []string{"path", "target"}, Scope: scopeOrchOperate},
	"list_open_targets":      {Method: "ListOpenTargets", Args: nil, Scope: scopeOrchRead},
	"get_app_version":        {Method: "GetAppVersion", Args: nil, Scope: scopeOrchRead},
	"set_sleep_inhibit":      {Method: "SetSleepInhibit", Args: []string{"active"}, Scope: scopeOrchOperate},
	"get_hook_server_port":   {Method: "GetHookServerPort", Args: nil, Scope: scopeOrchRead},

	// Keybindings config file — same shape as read_config/write_config below,
	// just not wired through core.ts's switch yet (no wire name existed
	// before this table). Named to match config_file_path/read_config/
	// write_config's own naming style.
	"keybindings_file_path": {Method: "KeybindingsFilePath", Args: nil, Scope: scopeOrchRead},
	"read_keybindings":      {Method: "ReadKeybindings", Args: nil, Scope: scopeOrchRead},
	"write_keybindings":     {Method: "WriteKeybindings", Args: []string{"content"}, Scope: scopeOrchOperate},

	// Claude Code — id is the frontend's chat id; the Go side emits
	// claude-data-<id> under exactly that name, so it must round-trip.
	"claude_start": {Method: "ClaudeStart", Args: []string{
		"id", "cwd", "resumeSessionId", "permissionMode", "appendSystemPrompt",
		"model", "effort", "configDir", "profileCommand", "profileArgs",
	}, Scope: scopeOrchOperate},
	"claude_send":  {Method: "ClaudeSend", Args: []string{"id", "text", "sessionId", "images"}, Scope: scopeOrchOperate},
	"claude_stop":  {Method: "ClaudeStop", Args: []string{"id"}, Scope: scopeOrchOperate},
	"claude_abort": {Method: "ClaudeAbort", Args: []string{"id"}, Scope: scopeOrchOperate},

	// ACP / Codex — the Go bridge owns the JSON-RPC handshake and emits
	// acp-data-<id> / acp-req-<id> under the frontend's chat id.
	//
	// AcpStart takes a single Go struct argument (AcpStartOpts), unlike every
	// other entry here which is scalar positional args — core.ts assembles
	// that struct client-side from a flat args object. There is no way to
	// spread a struct's fields across multiple table entries for one Go
	// parameter (the arity test requires exactly one Args name for one Go
	// param), so the wire carries the whole options object under a single
	// key. This is the one call in this table I could not port as a literal
	// 1:1 transcription of core.ts's argument list.
	//
	// NOTE for Task 3's client: callApp's number->string coercion only looks
	// at TOP-LEVEL args named in Args — here that's "opts" itself, not the
	// fields nested inside it. AcpStartOpts.ID is a string, and core.ts:160
	// does String(args.id) before nesting it into the object it passes to
	// App.AcpStart. A caller that sends {"opts":{"id":3,...}} (a bare numeric
	// id, unstringified) will fail to unmarshal, because the coercion never
	// reaches inside the object. Whoever builds the opts payload must
	// stringify id itself, same as core.ts already does.
	"acp_start":         {Method: "AcpStart", Args: []string{"opts"}, Scope: scopeOrchOperate},
	"codex_start":       {Method: "CodexStart", Args: []string{"id", "cwd", "env", "resumeSessionId"}, Scope: scopeOrchOperate},
	"acp_send":          {Method: "AcpSend", Args: []string{"id", "text", "images"}, Scope: scopeOrchOperate},
	"codex_send":        {Method: "CodexSend", Args: []string{"id", "text", "images"}, Scope: scopeOrchOperate},
	"acp_set_mode":      {Method: "AcpSetMode", Args: []string{"id", "modeId"}, Scope: scopeOrchOperate},
	"acp_set_config":    {Method: "AcpSetConfig", Args: []string{"id", "configId", "value"}, Scope: scopeOrchOperate},
	"acp_list_sessions": {Method: "AcpListSessions", Args: []string{"id", "cwd"}, Scope: scopeOrchOperate},
	"acp_stop":          {Method: "AcpStop", Args: []string{"id"}, Scope: scopeOrchOperate},
	"codex_stop":        {Method: "CodexStop", Args: []string{"id"}, Scope: scopeOrchOperate},
	// CodexListModels spawns `codex app-server` to probe the model catalog
	// (acp.go) — a host process spawn, so operate scope, not read, matching
	// the generate_* calls above for the same reason.
	"codex_list_models": {Method: "CodexListModels", Args: []string{"cwd"}, Scope: scopeOrchOperate},

	// LSP
	"lsp_start": {Method: "LspStart", Args: []string{"id", "command", "args", "cwd"}, Scope: scopeOrchOperate},
	"lsp_send":  {Method: "LspSend", Args: []string{"id", "message"}, Scope: scopeOrchOperate},
	"lsp_stop":  {Method: "LspStop", Args: []string{"id"}, Scope: scopeOrchOperate},

	// Providers. ProbeProvider passes its client-supplied `binary` straight to
	// resolveAgentBin (claudechat.go), which returns any existing absolute
	// path unchanged, then runs it (`exec.CommandContext(ctx, path,
	// "--version")`, providers.go) — arbitrary host process execution, so
	// operate scope, not read.
	"probe_provider":     {Method: "ProbeProvider", Args: []string{"binary", "cwd"}, Scope: scopeOrchOperate},
	"latest_npm_version": {Method: "LatestNpmVersion", Args: []string{"pkg"}, Scope: scopeOrchRead},

	// Skills / MCP servers
	"list_skills":       {Method: "ListSkills", Args: nil, Scope: scopeOrchRead},
	"set_skill_enabled": {Method: "SetSkillEnabled", Args: []string{"dir", "enabled"}, Scope: scopeOrchOperate},
	"delete_skill":      {Method: "DeleteSkill", Args: []string{"dir"}, Scope: scopeOrchOperate},
	"list_mcp_servers":  {Method: "ListMcpServers", Args: nil, Scope: scopeOrchRead},
	"add_mcp_server":    {Method: "AddMcpServer", Args: []string{"name", "config"}, Scope: scopeOrchOperate},
	"remove_mcp_server": {Method: "RemoveMcpServer", Args: []string{"name"}, Scope: scopeOrchOperate},

	// Local extensions (manifest registry + explicit user-run commands)
	"list_extensions":         {Method: "ListExtensions", Args: nil, Scope: scopeOrchRead},
	"set_extension_enabled":   {Method: "SetExtensionEnabled", Args: []string{"id", "enabled"}, Scope: scopeOrchOperate},
	"extensions_directory":    {Method: "ExtensionsDirectory", Args: nil, Scope: scopeOrchRead},
	"get_extension_settings":  {Method: "GetExtensionSettings", Args: []string{"extensionId"}, Scope: scopeOrchRead},
	"install_extension":       {Method: "InstallExtension", Args: []string{"source"}, Scope: scopeOrchOperate},
	"save_extension_settings": {Method: "SaveExtensionSettings", Args: []string{"extensionId", "values"}, Scope: scopeOrchOperate},
	"run_extension_command":   {Method: "RunExtensionCommand", Args: []string{"extensionId", "commandId", "cwd"}, Scope: scopeOrchOperate},

	// Claude session reading
	"list_claude_sessions":   {Method: "ListClaudeSessions", Args: []string{"cwd"}, Scope: scopeOrchRead},
	"read_claude_transcript": {Method: "ReadClaudeTranscript", Args: []string{"cwd", "sessionId"}, Scope: scopeOrchRead},
	"read_claude_activity":   {Method: "ReadClaudeActivity", Args: []string{"cwd", "sessionId"}, Scope: scopeOrchRead},

	// Control/permission responses
	"claude_respond_control": {Method: "ClaudeRespondControl", Args: []string{"id", "requestId", "response"}, Scope: scopeOrchOperate},
	"acp_respond_permission": {Method: "AcpRespondPermission", Args: []string{"id", "rpcId", "optionId"}, Scope: scopeOrchOperate},
	"acp_respond_user_input": {Method: "AcpRespondUserInput", Args: []string{"id", "rpcId", "answers"}, Scope: scopeOrchOperate},

	// Misc
	"system_stats":             {Method: "SystemStats", Args: nil, Scope: scopeOrchRead},
	"list_fonts":               {Method: "ListFonts", Args: nil, Scope: scopeOrchRead},
	"save_temp_image":          {Method: "SaveTempImage", Args: []string{"b64", "ext"}, Scope: scopeOrchOperate},
	"is_pid_alive":             {Method: "IsPidAlive", Args: []string{"pid"}, Scope: scopeOrchRead},
	"set_max_agents":           {Method: "SetMaxAgents", Args: []string{"n"}, Scope: scopeOrchOperate},
	"set_burrow_mcp_max_depth": {Method: "SetBurrowMcpMaxDepth", Args: []string{"n"}, Scope: scopeOrchOperate},

	// Chat transcripts (SQLite — they outgrew config.json)
	"load_chat_messages":     {Method: "LoadChatMessages", Args: []string{"chatId"}, Scope: scopeOrchRead},
	"save_chat_messages":     {Method: "SaveChatMessages", Args: []string{"chatId", "messages", "foldedOrd"}, Scope: scopeOrchOperate},
	"chat_folded_ord":        {Method: "ChatFoldedOrd", Args: []string{"chatId"}, Scope: scopeOrchRead},
	"load_chat_events_since": {Method: "LoadChatEventsSince", Args: []string{"chatId", "since"}, Scope: scopeOrchRead},
	"load_chat_stream_since": {Method: "LoadChatStreamSince", Args: []string{"chatId", "since"}, Scope: scopeOrchRead},
	"delete_chat_messages":   {Method: "DeleteChatMessages", Args: []string{"chatId"}, Scope: scopeOrchOperate},

	// App config file
	"read_config":  {Method: "ReadConfig", Args: nil, Scope: scopeOrchRead},
	"write_config": {Method: "WriteConfig", Args: []string{"content"}, Scope: scopeOrchOperate},

	// Claude account/usage (stubbed — "unavailable" until reverse-engineered)
	"claude_get_account": {Method: "ClaudeGetAccount", Args: []string{"cwd"}, Scope: scopeOrchRead},
	"claude_plan_usage":  {Method: "ClaudePlanUsage", Args: []string{"configDir", "force"}, Scope: scopeOrchRead},
	"claude_usage_5h":    {Method: "ClaudeUsage5h", Args: []string{"configDir"}, Scope: scopeOrchRead},

	// Remote chat sync (stubbed — no transport yet). remote_create_chat's
	// wire arg names deliberately do NOT copy core.ts's `args.cwd` here:
	// core.ts's case passes `args.cwd ?? ""` as RemoteCreateChat's second
	// argument, but that Go parameter is `agentKind` (must literally be
	// "claude" today — see remote.go) and the one real caller,
	// src/mobile/store.ts, already sends `{workspaceId, agentKind}`, never
	// `cwd`. core.ts's case is unreachable dead code (nothing calls
	// invoke("remote_create_chat", ...) — only the mobile client does, and it
	// talks to its own hand-written dispatch, not core.ts), so its `cwd` name
	// is a stale copy/paste, not a wire contract anything depends on. Naming
	// it `agentKind` here matches both the Go signature and the one real
	// caller.
	"remote_sync_chat":   {Method: "RemoteSyncChat", Args: []string{"chat"}, Scope: scopeOrchOperate},
	"remote_list_chats":  {Method: "RemoteListChats", Args: nil, Scope: scopeOrchRead},
	"remote_create_chat": {Method: "RemoteCreateChat", Args: []string{"workspaceId", "agentKind"}, Scope: scopeOrchOperate},

	// Daemon admin (stubbed)
	"daemon_stats":           {Method: "DaemonStats", Args: nil, Scope: scopeOrchRead},
	"clean_daemon":           {Method: "CleanDaemon", Args: nil, Scope: scopeOrchOperate},
	"kill_orphan_sessions":   {Method: "KillOrphanSessions", Args: []string{"keepIds"}, Scope: scopeOrchOperate},
	"restart_daemon":         {Method: "RestartDaemon", Args: nil, Scope: scopeOrchOperate},
	"control_verbs":          {Method: "ControlVerbs", Args: nil, Scope: scopeOrchRead},
	"repair_agent_status":    {Method: "RepairAgentStatus", Args: nil, Scope: scopeOrchOperate},
	"reinstall_status_hooks": {Method: "ReinstallStatusHooks", Args: nil, Scope: scopeOrchOperate},
	"remove_status_hooks":    {Method: "RemoveStatusHooks", Args: nil, Scope: scopeOrchOperate},
	"format_source":          {Method: "FormatSource", Args: []string{"path", "content", "cwd"}, Scope: scopeOrchRead},

	// Remote HTTP server / Tailscale. TailscaleServe/TailscaleServeStop are
	// the thin CLI-wrapping halves SetTailscaleServe composes (see
	// tailscale.go) — exposed too, at the same write scope, since a client
	// asking to serve one specific port is a legitimate (if lower-level) use
	// that SetTailscaleServe's combined toggle+status-refresh doesn't cover.
	//
	// get_http_server_status is access:write, not access:read, even though it
	// only returns a status struct: HttpServerStatus carries Token (the
	// long-lived http.token that authenticates every remote connection and
	// survives restarts) and PairCode (app.go) — reading it is equivalent to
	// authenticating as a fully-privileged remote client, or to pairing
	// another device. A read-scoped session must not be able to escalate to
	// that just by asking for "status".
	"get_http_server_status": {Method: "GetHttpServerStatus", Args: nil, Scope: scopeAccessWrite},
	"regenerate_pair_code":   {Method: "RegeneratePairCode", Args: nil, Scope: scopeAccessWrite},
	"get_tailscale_status":   {Method: "GetTailscaleStatus", Args: nil, Scope: scopeAccessRead},
	"set_tailscale_serve":    {Method: "SetTailscaleServe", Args: []string{"enabled", "port"}, Scope: scopeAccessWrite},
	"set_http_enabled":       {Method: "SetHttpEnabled", Args: []string{"enabled"}, Scope: scopeAccessWrite},
	"tailscale_serve":        {Method: "TailscaleServe", Args: []string{"port"}, Scope: scopeAccessWrite},
	"tailscale_serve_stop":   {Method: "TailscaleServeStop", Args: nil, Scope: scopeAccessWrite},
}

// remoteDenied names the App methods that are deliberately NOT reachable over
// the wire, each with the reason. An entry here is a decision, not a TODO.
var remoteDenied = map[string]string{
	// Hook/control plumbing
	"AckControlAction": "the UI's ack channel for a UI-performed verb; not a client call",

	// Window / native chrome
	"PickDirectory":      "opens a native OS file-picker dialog on the host; meaningless (and blocking) triggered from a remote client",
	"PickFile":           "opens a native OS file-picker dialog on the host; meaningless (and blocking) triggered from a remote client",
	"PickFiles":          "opens a native OS file-picker dialog on the host; meaningless (and blocking) triggered from a remote client",
	"OpenGitPanelWindow": "opens a desktop window; the float/bubble-window feature it served was removed outright (Wails v2 has no multi-window support), so this is a no-op with no meaning for a remote client",
	"RegisterTmuxWin":    "wires a desktop window id to a pty id for the same removed float-window protocol; a no-op with no meaning for a remote client",

	// Updater — swaps the running .app bundle; must not be triggerable over
	// the network. (CheckUpdate is read-only today, but it exists solely to
	// feed InstallUpdate's decision, so it stays with its two siblings rather
	// than opening a side door that tells a remote caller a build is
	// available.)
	"CheckUpdate":   "swaps the running .app bundle; must not be triggerable over the network",
	"InstallUpdate": "swaps the running .app bundle; must not be triggerable over the network",
	"RelaunchApp":   "swaps the running .app bundle; must not be triggerable over the network",

	// Float/task-live snapshot protocol (stubs.go). These call
	// runtime.EventsEmit directly rather than busEmit — events_test.go's own
	// wailsRuntimeAllowlist documents stubs.go's EventsEmit calls as "float
	// window snapshots, desktop-only" — so even if a remote client called
	// these, it could never see the reply; the reply only reaches the native
	// window.
	"RequestFloatSnapshot": "float/task-live snapshot protocol relayed via desktop-only Wails events (see wailsRuntimeAllowlist in events_test.go); a remote client would never see the reply",
	"SendFloatSnapshot":    "float/task-live snapshot protocol relayed via desktop-only Wails events (see wailsRuntimeAllowlist in events_test.go); a remote client would never see the reply",
	"NotifyFloatGrid":      "float/task-live snapshot protocol relayed via desktop-only Wails events (see wailsRuntimeAllowlist in events_test.go); a remote client would never see the reply",
}
