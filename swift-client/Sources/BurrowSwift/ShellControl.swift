import AppKit
import SwiftUI

// MARK: - control:action bridge (port of src/lib/controlBridge.ts)
//
// Verbs only a UI can perform (open a tab, focus a workspace, run a server in the RP…) reach the UI as a
// `control:action {id, action, args}` event; the Go verb blocks in UIBridge.Do until `ack_control_action(id)`.
//
// Who acks: Go has no acker election — the event goes to every /v2/ws client, the FIRST ack wins and later
// ones are dropped (controlapi.go AckControlAction). So if the Vue window and this app both performed a
// `new_tab`, the agent would get one id but the user two tabs. Hence `control.bridge`:
//   auto   (default) — act only when no Burrow desktop window process is running (a regular-policy app with a
//                      com.agenticide.wails* bundle id; the --core process is an accessory app and doesn't count),
//   always — always act (use when this is your only UI and the detection misfires),
//   off    — never act.
// Only the local core: a paired-remote ticket has no `ui:ack` scope anyway.
// When this app is connected the core sees a desktop UI (local-ticket = desktopUIDevice) and won't launch the
// Vue window for a verb (corerole.go ensureUIOpen), so in auto mode a Swift-only setup is always served.

@MainActor
final class ShellControl {
    static let shared = ShellControl()
    private weak var b: Burrow?
    /// serve() dedup: "ws\0cwd\0cmd" → pty id (RightPanel.vue findServed).
    private var served: [String: String] = [:]

    func install(_ b: Burrow) {
        guard self.b == nil else { return }
        self.b = b
        b.subscribe("control:action") { [weak self] _, payload in
            guard let self, let p = payload as? JSON, self.shouldHandle else { return }
            Task { await self.run(p) }
        }
    }

    static let modeKey = "control.bridge"
    var shouldHandle: Bool {
        guard Remote.active == nil else { return false }
        switch UserDefaults.standard.string(forKey: Self.modeKey) ?? "auto" {
        case "always": return true
        case "off": return false
        default: return !Self.desktopWindowRunning
        }
    }

    static var desktopWindowRunning: Bool {
        NSWorkspace.shared.runningApplications.contains {
            ($0.bundleIdentifier ?? "").hasPrefix("com.agenticide.wails") && $0.activationPolicy == .regular
        }
    }

    private func run(_ p: JSON) async {
        guard let b else { return }
        let id = p.s("id")
        do {
            let result = try await perform(p.s("action"), p["args"] as? JSON ?? [:])
            let data = try JSONSerialization.data(withJSONObject: result ?? [String: Any](), options: [.fragmentsAllowed])
            try await b.call("ack_control_action", ["id": id, "resultJson": String(decoding: data, as: UTF8.self), "errMsg": ""])
        } catch {
            _ = try? await b.call("ack_control_action", ["id": id, "resultJson": "", "errMsg": error.localizedDescription])
        }
    }

    struct Failure: LocalizedError {
        let msg: String
        var errorDescription: String? { msg }
    }

    func perform(_ action: String, _ a: JSON) async throws -> Any? {
        switch action {
        case "focus_workspace": return try await focusWorkspace(a.i("workspaceId"))
        case "focus_tab": return try await focusTab(a.i("ptyId"))
        case "new_tab": return try await newTab(a.i("workspaceId"), cmd: a.s("cmd"), cwd: "", focus: true)
        case "serve": return try await serve(a.i("workspaceId"), cmd: a.s("cmd"), cwd: a.s("cwd"), name: a.s("name"))
        case "worktree_setup": return try await worktreeSetup(a)
        case "tab_rename": return try await renameTab(a.i("ptyId"), a.s("title"))
        case "tab_close": return try await closeTab(a.i("ptyId"))
        case "workspace_create": return try await createWorkspace(a.s("path"), a.s("name"))
        case "workspaces_reload": try await b?.refresh(); return ["ok": true]
        case "spawn": return try await spawn(a)
        case "chat_send":
            guard a.i("chatId") > 0 else { throw Failure(msg: "chat_send needs a chat_id") }
            guard !a.s("text").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { throw Failure(msg: "chat_send needs text") }
            try await b?.call("chat_send", ["chatId": a.i("chatId"), "text": a.s("text"), "images": [String]()])
            return ["chat_id": a.i("chatId"), "sent": true]
        case "list_subagent_profiles": return await SubagentProfiles.load(b).map(\.json)
        case "list_agents": return await providers().filter { $0["enabled"] as? Bool != false }
            .map { ["id": $0.s("id"), "name": $0.s("name"), "kind": $0.s("kind")] }
        case "agent_status": return agentStatus(a)
        case "tab_output": return try tabOutput(a.i("ptyId"), lines: Int(a.i("lines")) > 0 ? Int(a.i("lines")) : 80)
        case "diagram": try await diagram(a.s("content")); return ["ok": true]
        default: throw Failure(msg: "the app has no handler for \"\(action)\"")
        }
    }

    // MARK: workspaces / tabs

    private func workspace(_ id: Int64) async throws -> JSON {
        guard let b else { throw Failure(msg: "not connected") }
        if b.workspaces[id] == nil { try? await b.refresh() }
        guard let w = b.workspaces[id] else { throw Failure(msg: "no workspace with id \(id)") }
        return w
    }

    private func focusWindow() {
        NSApp.activate(ignoringOtherApps: true)
        NSApp.windows.first { $0.isVisible && $0.canBecomeMain }?.makeKeyAndOrderFront(nil)
    }

    private func focusWorkspace(_ id: Int64) async throws -> JSON {
        let w = try await workspace(id)
        b?.openProject(b?.rootWorkspace(id) ?? w)
        focusWindow()
        return ["workspace_id": id, "name": w.s("name"), "path": w.s("path")]
    }

    private func focusTab(_ pty: Int64) async throws -> JSON {
        guard let tab = TerminalStore.shared.tab(pty: pty) else { throw Failure(msg: "no open tab with pty id \(pty)") }
        b?.selectedTab = pty
        focusWindow()
        return ["pty_id": pty, "workspace_id": tab.i("workspace_id")]
    }

    /// A terminal_tabs row + a PTY started now, with no view (an agent's tab must run while nobody looks at it).
    /// The view attaches later via TerminalStore.session (create_pty on a live id = reattach).
    private func newTab(_ wsID: Int64, cmd: String, cwd: String, focus: Bool, name: String = "", token: String = "") async throws -> JSON {
        guard let b else { throw Failure(msg: "not connected") }
        let target = wsID > 0 ? wsID : (b.chat(b.selectedChat)?.i("workspace_id") ?? b.orderedProjects.first?.i("id") ?? 0)
        let w = try await workspace(target)
        let dir = cwd.isEmpty ? w.s("path") : cwd
        let id = await TerminalStore.shared.nextPtyID()
        let title = !name.isEmpty ? name : cmd.isEmpty ? "Terminal" : String(cmd.split(separator: " ").first ?? "Terminal")
        var rows = try await b.call("list_terminal_tabs", ["workspaceId": target]) as? [JSON] ?? []
        rows.append(["title": title, "default_title": title, "initial_cmd": cmd.isEmpty ? NSNull() : cmd, "pty_id": Int64(id)!, "cwd": dir])
        let keys = ["title", "default_title", "initial_cmd", "pty_id", "cwd", "session_id", "branch"]
        try await b.call("save_terminal_tabs", ["workspaceId": target, "tabs": rows.map { r in keys.reduce(into: JSON()) { $0[$1] = r[$1] ?? NSNull() } }])
        try await startHeadless(id, cwd: dir, cmd: try await withResultToken(cmd, pty: id, token: token))
        try? await b.refresh()
        if focus { b.selectedTab = Int64(id) }
        return ["pty_id": Int64(id)!, "workspace_id": target]
    }

    /// XTerm.vue's result capture for `burrow collect <token>`: a per-launch Stop hook + the token sidecar file.
    private func withResultToken(_ cmd: String, pty: String, token: String) async throws -> String {
        guard !token.isEmpty, cmd.split(separator: " ").first == "claude", let b else { return cmd }
        let hooks = "/tmp/agentic-ide-hooks-\(pty).json"
        try await b.call("write_text_file", ["path": hooks, "content": #"{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"burrow capture \#(token)"}]}]}}"#])
        try await b.call("write_text_file", ["path": "/tmp/agentic-ide-token-\(pty)", "content": token])
        return "claude --settings \(hooks)" + cmd.dropFirst("claude".count)
    }

    private func startHeadless(_ id: String, cwd: String, cmd: String) async throws {
        guard let b else { return }
        try await b.call("create_pty", ["id": id, "cwd": cwd, "cols": 120, "rows": 32])
        guard !cmd.isEmpty else { return }
        // Wait for the prompt to print and fall quiet (XTerm.vue trySend), max 5 s.
        var last: Int64 = -1, quietSince = Date()
        let start = Date()
        while Date().timeIntervalSince(start) < 5 {
            try? await Task.sleep(for: .milliseconds(150))
            let c = PtyChunk(try? await b.call("pty_since", ["id": id, "since": 0]))
            let end = (c?.off ?? 0) + Int64(c?.data.count ?? 0)
            if end != last { last = end; quietSince = Date() }
            else if end > 0, Date().timeIntervalSince(quietSince) >= 0.3 { break }
        }
        try await b.call("write_pty", ["id": id, "data": Array((cmd + "\n").utf8).map(Int.init)])
    }

    private func renameTab(_ pty: Int64, _ title: String) async throws -> JSON {
        guard TerminalStore.shared.tab(pty: pty) != nil else { throw Failure(msg: "no open tab with pty id \(pty)") }
        await TerminalStore.shared.rename(pty: pty, to: title)
        return ["pty_id": pty, "title": title]
    }

    private func closeTab(_ pty: Int64) async throws -> JSON {
        let store = TerminalStore.shared
        if store.tab(pty: pty) != nil {
            await store.close(pty: pty)
        } else if let ws = store.rp.first(where: { $0.value.ids.contains(String(pty)) })?.key {
            await store.closeRp(Int64(ws) ?? 0, String(pty))
            served = served.filter { $0.value != String(pty) }
        } else {
            throw Failure(msg: "no open tab with pty id \(pty)")
        }
        return ["pty_id": pty, "closed": true]
    }

    private func createWorkspace(_ path: String, _ name: String) async throws -> JSON {
        guard !path.isEmpty, let b else { throw Failure(msg: "workspace_create needs a path") }
        let n = name.isEmpty ? (path as NSString).lastPathComponent : name
        let row = try await b.call("create_workspace", ["name": n, "path": path]) as? JSON ?? [:]
        try? await b.refresh()
        if let w = b.workspaces[row.i("id")] { b.openProject(w) }
        return ["workspace_id": row.i("id"), "name": row.s("name"), "path": row.s("path")]
    }

    // MARK: serve (RP terminal)

    /// Long-lived process in a Right Panel shell of that workspace; asking again for the same thing reuses it
    /// (and restarts it when only the shell is left in the foreground).
    private func serve(_ wsID: Int64, cmd: String, cwd: String, name: String) async throws -> JSON {
        guard let b else { throw Failure(msg: "not connected") }
        let w = try await workspace(wsID)
        let dir = cwd.isEmpty ? w.s("path") : cwd
        let key = "\(wsID)\0\(dir)\0\(cmd.split(whereSeparator: \.isWhitespace).joined(separator: " "))"
        let store = TerminalStore.shared
        store.install(b)
        if let hit = served[key], store.rp["\(wsID)"]?.ids.contains(hit) == true {
            let fg = (try? await b.call("get_pty_foreground", ["id": hit])) as? String ?? "?"
            if fg.isEmpty {
                await store.closeRp(wsID, hit)
            } else {
                store.rp["\(wsID)"]?.active = hit
                openRightPanelSurface("terminal")
                let restart = ["zsh", "bash", "sh", "fish", "-zsh", "-bash", "login"].contains((fg as NSString).lastPathComponent)
                if restart { try await b.call("write_pty", ["id": hit, "data": Array(("\u{15}" + cmd + "\n").utf8).map(Int.init)]) }
                return ["pty_id": Int64(hit)!, "workspace_id": wsID, "reused": true, "restarted": restart]
            }
        }
        let id = await store.nextPtyID()
        try await startHeadless(id, cwd: dir, cmd: cmd)
        var t = store.rp["\(wsID)"] ?? TerminalStore.RpTerms(ids: [], active: nil)
        t.ids.append(id); t.active = id
        store.rp["\(wsID)"] = t
        served[key] = id
        openRightPanelSurface("terminal")
        return ["pty_id": Int64(id)!, "workspace_id": wsID, "reused": false]
    }

    /// Go created a worktree: run the repo's "run when a worktree is created" scripts in one RP shell.
    private func worktreeSetup(_ a: JSON) async throws -> JSON {
        if !a.s("warning").isEmpty { b?.status = "Worktree: \(a.s("warning"))" }
        let scripts = await ProjectScripts.load(b, path: a.s("repoPath")).scripts.filter(\.runOnWorktreeCreate)
        let lines = scripts.map(\.commandLine).filter { !$0.isEmpty }
        guard !lines.isEmpty else { return ["ran": 0] }
        let cmd = "(\(lines.joined(separator: ") && ("))) && echo \"Burrow: worktree setup done\" || echo \"Burrow: worktree setup failed\""
        let r = try await serve(a.i("workspaceId"), cmd: cmd, cwd: a.s("path"), name: "Setup: " + scripts.map(\.name).joined(separator: ", "))
        return ["ran": lines.count, "pty_id": r["pty_id"] ?? 0]
    }

    // MARK: spawn

    private func providers() async -> [JSON] {
        if ConfigStore.shared.values.isEmpty, let b { await ConfigStore.shared.attach(b) }
        return ConfigStore.shared.values["providers"] as? [JSON] ?? []
    }

    private func spawn(_ a: JSON) async throws -> JSON {
        guard let b else { throw Failure(msg: "not connected") }
        let parent = a.i("parent_chat_id")
        if parent > 0 {
            let got = try await b.call("spawn_subagent_chat", [
                "parentChatId": parent, "task": a.s("task"), "profile": a.s("profile"),
                "agent": a.s("agent"), "model": a.s("model"), "cwd": a.s("cwd"),
            ]) as? JSON ?? [:]
            return ["chat_id": got.i("chat_id"), "workspace_id": got.i("workspace_id"), "parent_chat_id": parent]
        }
        let task0 = a.s("task")
        guard !task0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { throw Failure(msg: "spawn needs a task") }
        let profileID = a.s("profile").lowercased()
        let profile = profileID.isEmpty ? nil : await SubagentProfiles.load(b).first { $0.id == profileID }
        if !profileID.isEmpty, profile == nil { throw Failure(msg: "Unknown sub-agent profile \"\(profileID)\". Use scout, worker, or reviewer.") }
        let task = profile.map { "Role: \($0.name)\n\n\($0.instructions.trimmingCharacters(in: .whitespacesAndNewlines))\n\nTask:\n\(task0.trimmingCharacters(in: .whitespacesAndNewlines))" } ?? task0

        let all = await providers()
        let wanted = !a.s("agent").isEmpty ? a.s("agent") : profile?.agentId ?? ""
        let named = all.first { $0.s("id") == wanted || $0.s("name").lowercased() == wanted.lowercased() }
        if !wanted.isEmpty, named == nil { throw Failure(msg: "no agent named \"\(wanted)\" — call list_agents to see the configured ones") }
        let def = ConfigStore.shared.ui.s("defaultChatAgent")
        guard let inst = named ?? all.first(where: { $0.s("id") == def }) ?? all.first(where: { $0["enabled"] as? Bool != false })
        else { throw Failure(msg: "no agent is configured in Settings > Providers") }

        let cwd = a.s("cwd")
        let target = b.workspaces.values.first { !cwd.isEmpty && $0.s("path") == cwd }?.i("id")
            ?? b.chat(b.selectedChat)?.i("workspace_id") ?? b.orderedProjects.first?.i("id")
        guard let target else { throw Failure(msg: "no workspace to spawn into") }
        let model = !a.s("model").isEmpty ? a.s("model") : profile?.model ?? ""
        let mode = profile?.permissionMode ?? ""

        let pref = ConfigStore.shared.ui.s("spawnMode") == "chat" ? "chat" : "tab"
        let openAs = inst.s("providerId") == "openai-compatible" ? "chat" : (a.s("target").isEmpty ? pref : a.s("target"))
        if openAs == "chat" {
            if inst.s("transport") == "none" { throw Failure(msg: "This provider does not offer an embedded chat.") }
            let row = try await b.call("remote_create_chat", [
                "workspaceId": target, "agentKind": inst.s("id"), "model": model, "effort": "", "permissionMode": mode,
            ]) as? JSON ?? [:]
            let chatID = row.i("id")
            try await b.call("chat_send", ["chatId": chatID, "text": task, "images": [String]()])
            try? await b.refresh()
            return ["chat_id": chatID, "workspace_id": target]
        }
        let cmd = terminalCommand(inst, model: model, mode: mode, task: task)
        var r = try await newTab(target, cmd: cmd, cwd: cwd, focus: false, name: a.s("name").trimmingCharacters(in: .whitespaces), token: a.s("token"))
        r["agent"] = inst.s("name")
        return r
    }

    /// agentCommand.ts buildTerminalCommand (+ profileTerminalCommand's permission flags, simplified).
    // ponytail: profile env / configDir / custom-args refusals of profileTerminalCommand not ported.
    func terminalCommand(_ inst: JSON, model: String, mode: String, task: String) -> String {
        let kind = inst.s("kind")
        let bin = ["claude", "codex", "gemini"].contains(kind) ? kind : (inst.s("binary").isEmpty ? inst.s("id") : inst.s("binary"))
        var parts = [bin]
        if !model.isEmpty { parts += ["--model", Self.quote(model)] }
        if kind == "claude", !mode.isEmpty { parts += ["--permission-mode", mode] }
        parts.append(Self.quote(task))
        return parts.joined(separator: " ")
    }

    /// POSIX single-quoting (agentCommand.ts shellQuote).
    nonisolated static func quote(_ s: String) -> String { "'" + s.replacingOccurrences(of: "'", with: #"'\''"#) + "'" }

    // MARK: status / output / diagram

    private func agentStatus(_ a: JSON) -> [JSON] {
        guard let b else { return [] }
        let parent = a.i("parent_chat_id")
        let onlyChildren = a["onlyChildren"] as? Bool == true && parent > 0
        let name = { (id: Int64) in b.workspaces[id]?.s("name") ?? "\(id)" }
        var out: [JSON] = []
        if !onlyChildren {
            for (ws, tab) in b.snapshotTabs {
                let ph = b.phases["pty:\(tab.i("pty_id"))"] ?? [:]
                guard ph["is_agent"] as? Bool == true else { continue }
                out.append(["kind": "tab", "pty_id": tab.i("pty_id"), "title": tabTitle(tab), "status": ph.s("state"),
                            "workspace": ws.s("name"), "workspace_id": ws.i("id")])
            }
        }
        for c in b.allChats where !onlyChildren || c.i("parent_chat_id") == parent {
            out.append(["kind": "chat", "chat_id": c.i("id"), "title": c.s("title"), "status": b.state(c.i("id")),
                        "workspace": name(c.i("workspace_id")), "workspace_id": c.i("workspace_id"),
                        "parent_chat_id": c.i("parent_chat_id"),
                        "children": b.allChats.filter { $0.i("parent_chat_id") == c.i("id") }.map { $0.i("id") }])
        }
        return out
    }

    /// Go reads the daemon's own screen first (vt build); this fallback has no text extraction from our
    /// emulator yet. ponytail: add a bvt text dump (terminal agent) if a non-vt daemon must be supported.
    private func tabOutput(_ pty: Int64, lines: Int) throws -> JSON {
        throw Failure(msg: "pty \(pty): this Burrow client can't read terminal text (daemon built without vt)")
    }

    /// No mermaid renderer here: drop it in a markdown file and ask the RP to show it as a doc.
    private func diagram(_ content: String) async throws {
        let path = NSTemporaryDirectory() + "burrow-diagram-\(Int(Date().timeIntervalSince1970)).md"
        try ("```mermaid\n" + content + "\n```\n").write(toFile: path, atomically: true, encoding: .utf8)
        RPOpenRequests.shared.surface = "doc:" + path
        UserDefaults.standard.set(true, forKey: "rp.open")
    }
}

