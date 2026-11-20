import Foundation

// ponytail: loose [String: Any] JSON instead of Codable models — experiment,
// tolerates server-side field additions. Add Codable when the shape settles.
typealias JSON = [String: Any]

extension Dictionary where Key == String, Value == Any {
    func i(_ k: String) -> Int64 { (self[k] as? NSNumber)?.int64Value ?? 0 }
    func s(_ k: String) -> String { self[k] as? String ?? "" }
}

/// Mirrors `settledFor` in src/stores/claudeChats.ts.
let autoSettleMs: Int64 = 3 * 24 * 3600 * 1000

func nowMs() -> Int64 { Int64(Date().timeIntervalSince1970 * 1000) }

@MainActor
final class Burrow: ObservableObject {
    @Published var status = "Connecting…"
    @Published var connected = false
    @Published var workspaces: [Int64: JSON] = [:]
    @Published var chats: [JSON] = []
    /// Every row incl. sub-agents / side chats (the RP's Agents tab reads it).
    @Published var allChats: [JSON] = []
    @Published var phases: [String: JSON] = [:]
    @Published var seen: [String: Int64] = [:]
    @Published var messages: [JSON] = []
    @Published var requests: [PendingRequest] = []
    /// Last shell_snapshot as received (tabs per workspace, phases, …) for features that need more than the fields above.
    @Published var snapshot: JSON = [:]
    /// A terminal tab (pty id) shown in the detail pane instead of a chat. Selecting one clears the other.
    @Published var selectedTab: Int64? {
        didSet { if selectedTab != nil, selectedChat != nil { selectedChat = nil } }
    }
    @Published var selectedChat: Int64? {
        didSet {
            if selectedChat != nil, selectedTab != nil { selectedTab = nil }
            guard selectedChat != oldValue else { return }
            messages = []; requests = []
            Task { await loadMessages(); await loadRequests() }
            markSeen()
        }
    }
    /// Transcript index of messages[0] (load_chat_messages_page's `start`).
    var pageStart = 0 // internal: ThreadActions.loadOlder prepends older pages

    private var ws: URLSessionWebSocketTask?
    private var subscribers: [(prefix: String, handler: (String, Any?) -> Void)] = []

    /// Every event (ringed shell events and the unnumbered pty/chat channels) whose name starts with `prefix`.
    /// Feature files use this instead of editing onEvent. ponytail: no unsubscribe; subscribe once per app lifetime
    /// (a view that needs per-id events should filter by name inside one long-lived subscription).
    func subscribe(_ prefix: String, _ handler: @escaping (String, Any?) -> Void) {
        subscribers.append((prefix, handler))
    }
    private var nextID: Int64 = 1
    private var pending: [Int64: CheckedContinuation<Any?, Error>] = [:]

    struct CallError: Error, LocalizedError {
        let message: String
        var errorDescription: String? { message }
    }

    // MARK: transport (/v2/ws)

    func connect() async {
        do {
            let url = try await Remote.ticketURL() // local core, or a paired Burrow (Remote.swift)
            let task = URLSession.shared.webSocketTask(with: url)
            task.maximumMessageSize = 64 << 20
            ws = task
            task.resume()
            Task { await readLoop(task) }
            try await refresh()
            connected = true
            status = "Connected"
        } catch {
            connected = false
            status = error.localizedDescription
            // ponytail: fixed 3 s retry, no backoff / resume-from-seq; a fresh snapshot covers the gap.
            try? await Task.sleep(for: .seconds(3))
            if !stopped { await connect() }
        }
    }

    /// Drop the socket; readLoop reconnects (Remote.swift switches environments this way).
    func reconnect() { ws?.cancel(with: .goingAway, reason: nil) }
    /// Close for good (a pop-out window closing, Window.swift).
    private var stopped = false
    func stop() { stopped = true; reconnect() }

    @discardableResult
    func call(_ cmd: String, _ args: JSON = [:]) async throws -> Any? {
        guard let ws else { throw CallError(message: "not connected") }
        let id = nextID; nextID += 1
        let frame: JSON = ["t": "call", "id": id, "cmd": cmd, "args": args]
        let text = String(data: try JSONSerialization.data(withJSONObject: frame), encoding: .utf8)!
        return try await withCheckedThrowingContinuation { cont in
            pending[id] = cont
            ws.send(.string(text)) { err in
                if let err { Task { @MainActor in self.pending.removeValue(forKey: id)?.resume(throwing: err) } }
            }
        }
    }

    private func readLoop(_ task: URLSessionWebSocketTask) async {
        while true {
            guard let msg = try? await task.receive() else { break }
            let data: Data
            switch msg {
            case .string(let s): data = Data(s.utf8)
            case .data(let d): data = d
            @unknown default: continue
            }
            if let f = (try? JSONSerialization.jsonObject(with: data)) as? JSON { handle(f) }
        }
        // Dropped: fail in-flight calls (never replay them — same rule as transport.ts), reconnect.
        for (_, c) in pending { c.resume(throwing: CallError(message: "connection lost")) }
        pending.removeAll()
        ws = nil
        connected = false
        status = "Reconnecting…"
        if !stopped { await connect() }
    }

    private func handle(_ f: JSON) {
        switch f.s("t") {
        case "reply":
            guard let c = pending.removeValue(forKey: f.i("id")) else { return }
            if let e = f["error"] as? JSON { c.resume(throwing: CallError(message: e.s("message"))) }
            else { c.resume(returning: f["result"]) }
        case "shell":
            for ev in f["events"] as? [JSON] ?? [] { onEvent(ev.s("name"), ev["payload"]) }
        case "event":
            onEvent(f.s("name"), f["payload"])
        case "resync":
            Task { try? await refresh() }
        case "welcome":
            Shell.shared.incompatible = protocolMismatch(f); if let why = Shell.shared.incompatible { status = why }
        default: break
        }
    }

    private func onEvent(_ name: String, _ payload: Any?) {
        for s in subscribers where name.hasPrefix(s.prefix) { s.handler(name, payload) }
        if name.hasPrefix("phase-") {
            let key = String(name.dropFirst("phase-".count))
            phases[key] = payload as? JSON
            if key == "chat:\(selectedChat ?? -1)" {
                Task { await loadRequests() }
                markSeen()
            }
        } else if name.hasPrefix("chat-messages-changed-"), let p = payload as? JSON,
                  name == "chat-messages-changed-\(selectedChat ?? -1)" {
            splice(from: Int(p.i("fromIndex")), p["messages"] as? [JSON] ?? [])
        } else if name == "seen-changed", let p = payload as? JSON {
            seen[p.s("key")] = p.i("at")
        } else if name == "chats-changed" || name == "workspaces-changed" {
            Task { try? await refresh() }
        }
    }

    // MARK: data

    func refresh() async throws {
        async let snap = call("shell_snapshot")
        async let rows = call("list_chats")
        async let receipts = call("seen_list")
        if let snap = try await snap as? JSON {
            snapshot = snap
            var byID: [Int64: JSON] = [:]
            for w in snap["workspaces"] as? [JSON] ?? [] { byID[w.i("id")] = w }
            workspaces = byID
            phases = snap["phases"] as? [String: JSON] ?? [:]
        }
        allChats = try await rows as? [JSON] ?? []
        chats = allChats.filter {
            $0.i("archived_at") == 0 && $0.i("parent_chat_id") == 0 && $0.s("kind") != "side"
        }
        for r in try await receipts as? [JSON] ?? [] { seen[r.s("key")] = r.i("at") }
    }

    func loadMessages() async {
        guard let id = selectedChat else { return }
        let page = try? await call("load_chat_messages_page", ["chatId": id, "before": -1, "limit": 120]) as? JSON
        guard selectedChat == id, let page, let msgs = page["messages"] as? [JSON] else { return }
        pageStart = Int(page.i("start"))
        messages = msgs
    }

    /// Live transcript: Go folds the stream and sends the changed tail.
    private func splice(from: Int, _ tail: [JSON]) {
        let local = from - pageStart
        guard local >= 0, local <= messages.count else { Task { await loadMessages() }; return }
        messages = Array(messages[..<local]) + tail
    }

    // MARK: permission requests (attention agent — logic lives in Requests.swift)

    func loadRequests() async {
        guard let id = selectedChat else { return }
        let lines = (try? await call("load_pending_chat_requests", ["chatId": "\(id)"])) as? [JSON] ?? []
        guard selectedChat == id else { return }
        requests = lines.compactMap { PendingRequest(line: $0.s("line")) }
            + AttentionStore.shared.secretRequests(owner: "chat:\(id)")
    }

    /// The one door for answering anything the agent is blocked on (chatSession.ts `respond`).
    func answer(_ r: PendingRequest, _ a: RequestAnswer, chatID: Int64? = nil) async {
        guard let id = chatID ?? selectedChat else { return }
        do {
            try await respond(chatID: id, r, a)
            AttentionStore.shared.markAnswered(id, r)
            requests.removeAll { $0.id == r.id }
        } catch {
            status = "Answer failed: \(error.localizedDescription)"
        }
    }

    // MARK: new thread

    func createThread(workspaceID: Int64, agent: String, model: String, effort: String, mode: String, prompt: String) async {
        do {
            guard let row = try await call("remote_create_chat", [
                "workspaceId": workspaceID, "agentKind": agent, "model": model, "effort": effort, "permissionMode": mode,
            ]) as? JSON else { return }
            let id = row.i("id")
            try await refresh()
            selectedChat = id
            try await call("chat_send", ["chatId": id, "text": prompt, "images": [String]()])
            await title(chatID: id, workspaceID: workspaceID, prompt: prompt)
        } catch {
            status = "Create failed: \(error.localizedDescription)"
        }
    }

    func setLaunch(_ chatID: Int64, _ key: String, _ value: String) async {
        do {
            try await call("chat_set_launch_settings", ["chatId": chatID, "patch": [key: value]])
            try await refresh()
        } catch {
            status = "Settings failed: \(error.localizedDescription)"
        }
    }

    /// remote_create_chat names it "Chat N (phone)"; same fix as TrayPanel's nameThread.
    private func title(chatID: Int64, workspaceID: Int64, prompt: String) async {
        let firstLine = prompt.split(whereSeparator: \.isNewline).first.map(String.init) ?? prompt
        await rename(chatID, String(firstLine.prefix(60)))
        let cwd = workspaces[workspaceID]?.s("path") ?? ""
        if let t = try? await call("generate_chat_title", ["cwd": cwd, "model": "", "policy": "", "text": prompt]) as? String,
           !t.isEmpty {
            await rename(chatID, t)
        }
    }

    private func rename(_ chatID: Int64, _ title: String) async {
        let rows = (try? await call("list_chats")) as? [JSON] ?? []
        guard var row = rows.first(where: { $0.i("id") == chatID }) else { return }
        row["title"] = title
        try? await call("save_chats", ["chats": [row]])
    }

    func send(_ text: String) async {
        guard let id = selectedChat else { return }
        do { try await call("chat_send", ["chatId": id, "text": text, "images": [String]()]) }
        catch { status = "Send failed: \(error.localizedDescription)" }
    }

    func interrupt() async {
        guard let id = selectedChat else { return }
        try? await call("chat_interrupt", ["chatId": id])
    }

    /// Receipt shared with every other client (seen_receipts, Go's clock).
    private func markSeen() {
        guard let id = selectedChat, state(id) == "done" else { return }
        Task { try? await call("seen_mark", ["key": "chat:\(id)"]) }
    }

    // MARK: derived

    func phase(_ id: Int64) -> JSON { phases["chat:\(id)"] ?? [:] }
    func state(_ id: Int64) -> String { phase(id)["state"] as? String ?? "idle" }

    func inFlight(_ id: Int64) -> Bool {
        ["running", "waiting_input", "waiting_approval"].contains(state(id))
    }

    func unread(_ id: Int64) -> Bool {
        let p = phase(id)
        return p.s("state") == "done" && p.i("turn_ended_at") > (seen["chat:\(id)"] ?? 0) && id != selectedChat
    }

    func settled(_ c: JSON) -> Bool {
        let id = c.i("id")
        if inFlight(id) { return false }
        if c.i("snoozed_until") > nowMs() { return true }
        switch c.s("settled_override") {
        case "settled": return true
        case "active": return false
        default: return nowMs() - c.i("last_activity_at") >= autoSettleMs
        }
    }

    /// Worktrees fold into their repo, like the desktop Sidebar.
    func rootWorkspace(_ wsID: Int64) -> JSON {
        var w = workspaces[wsID] ?? [:]
        while let p = w["parent_id"] as? NSNumber, let parent = workspaces[p.int64Value] { w = parent }
        return w
    }

    func chat(_ id: Int64?) -> JSON? { allChats.first { $0.i("id") == id } }

    /// Repos only (worktrees fold into them), most recently opened first.
    var projects: [JSON] {
        workspaces.values.filter { $0["parent_id"] == nil }
            .sorted { $0.i("last_opened") > $1.i("last_opened") }
    }
}
