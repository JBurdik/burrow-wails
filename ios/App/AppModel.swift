import ActivityKit
import Foundation
import Observation
import UIKit
import WidgetKit

struct Workspace: Identifiable, Hashable {
    var id: Int
    var name: String
    var parentId: Int?
    var path: String = ""
    /// Project icon as the desktop stores it: an image data URL (set_workspace_icon).
    var icon: String?
    var repoId: Int { parentId ?? id }
}

/// Decoded project icons, keyed by the data URL itself (it changes when the icon does).
enum IconCache {
    private static let cache = NSCache<NSString, UIImage>()
    static func image(_ dataURL: String?) -> UIImage? {
        guard let dataURL, let comma = dataURL.firstIndex(of: ",") else { return nil }
        if let hit = cache.object(forKey: dataURL as NSString) { return hit }
        guard let data = Data(base64Encoded: String(dataURL[dataURL.index(after: comma)...]), options: .ignoreUnknownCharacters),
              let img = UIImage(data: data) else { return nil }
        cache.setObject(img, forKey: dataURL as NSString)
        return img
    }
}

struct TurnAsk: Hashable {
    var question: String
    var options: [String]
    var details: [String]
    var suggested: Int
    var multi: Bool
}

struct TurnNeeds: Hashable {
    var needsYou: Bool
    var asks: [TurnAsk]
    var next: [String]

    init?(_ raw: String) {
        guard !raw.isEmpty, let d = try? JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [String: Any],
              let needs = d["needs"] as? String, needs == "you" || needs == "nothing" else { return nil }
        needsYou = needs == "you"
        asks = (d["asks"] as? [[String: Any]] ?? []).map {
            TurnAsk(question: $0["question"] as? String ?? "", options: $0["options"] as? [String] ?? [],
                    details: $0["details"] as? [String] ?? [], suggested: int($0["suggested"], -1), multi: $0["multi"] as? Bool ?? false)
        }
        next = d["next"] as? [String] ?? []
    }
}

/// One row of Go's `chats` table (list_chats), only the columns the app reads.
struct Chat: Identifiable, Hashable {
    var id: Int
    var workspaceId: Int
    var title: String
    var agentKind: String
    var model: String
    var effort: String
    var permissionMode: String
    var archived: Bool
    var lastActivityAt: Double
    var parentChatId: Int
    var kind: String
    var snoozedUntil: Double
    var settledOverride: String
    var recap: String
    var slept: Bool
    var turnNeeds: TurnNeeds?

    init(_ d: [String: Any]) {
        id = int(d["id"]); workspaceId = int(d["workspace_id"])
        title = d["title"] as? String ?? ""
        agentKind = d["agent_kind"] as? String ?? ""
        model = d["model"] as? String ?? ""
        effort = d["effort"] as? String ?? ""
        permissionMode = d["permission_mode"] as? String ?? ""
        archived = int(d["archived_at"]) > 0
        lastActivityAt = dbl(d["last_activity_at"])
        parentChatId = int(d["parent_chat_id"])
        kind = d["kind"] as? String ?? ""
        snoozedUntil = dbl(d["snoozed_until"])
        settledOverride = d["settled_override"] as? String ?? ""
        recap = d["recap"] as? String ?? ""
        slept = int(d["slept_at"]) > 0
        turnNeeds = TurnNeeds(d["turn_needs"] as? String ?? "")
    }

    var isCodex: Bool { agentKind == "codex" }
    var displayTitle: String { title.isEmpty ? "Chat \(id)" : title }
}

struct Phase: Hashable {
    var state = "idle"
    var detail = ""
    var turnEndedAt: Double = 0
    var updatedAt: Double = 0
    var inFlight: Bool { ["running", "waiting_input", "waiting_approval"].contains(state) }

    init() {}
    init(_ d: [String: Any]) {
        state = d["state"] as? String ?? "idle"
        detail = d["detail"] as? String ?? ""
        turnEndedAt = dbl(d["turn_ended_at"])
        updatedAt = dbl(d["updated_at"])
    }
}

/// src/runtime/displayStatus.ts with watching = false: the phase is Go's, the
/// read receipt (shared across devices via seen_receipts) decides unread.
enum Status: Hashable {
    case permission, waiting, error, running, unread, idle
    var needsYou: Bool { self == .permission || self == .waiting || self == .error }
}

func int(_ v: Any?, _ fallback: Int = 0) -> Int { (v as? NSNumber)?.intValue ?? fallback }
func dbl(_ v: Any?) -> Double { (v as? NSNumber)?.doubleValue ?? 0 }

@Observable
final class AppModel {
    var creds: Credentials? = CredentialStore.load()
    var connected = false
    var workspaces: [Workspace] = []
    var chats: [Chat] = []
    var phases: [Int: Phase] = [:]     // by chat id
    var seen: [Int: Double] = [:]      // chat id → seen_at (Go clock, ms)
    var error: String?
    var openChat: Int?
    /// Messages typed while offline (Offline.swift), per paired machine.
    var outbox: [OutboxItem] = []
    @ObservationIgnored var flushing = false
    @ObservationIgnored var flushAgain = false
    /// list_chats as received, for the launch cache.
    @ObservationIgnored var rawChats: [[String: Any]] = []
    @ObservationIgnored var pageCache: [Int: [String: Any]] = [:]
    @ObservationIgnored var dirtyPages: Set<Int> = []
    /// Chat on screen: its finished turns are read as they land.
    var viewing: Int?
    /// APNs device token (hex), from PushDelegate; sent on every socket open.
    @ObservationIgnored var apnsToken: String? { didSet { if apnsToken != oldValue { sendPushToken() } } }

    @ObservationIgnored private(set) var client: BurrowClient?
    @ObservationIgnored var chatListeners: [Int: () -> Void] = [:]
    /// Every wire event, for feature modules (requests, queue, terminals…):
    /// `let token = model.subscribe { name, payload in … }`, `unsubscribe(token)`.
    @ObservationIgnored private var subscribers: [UUID: (String, Any?) -> Void] = [:]

    @discardableResult
    func subscribe(_ h: @escaping (String, Any?) -> Void) -> UUID {
        let id = UUID(); subscribers[id] = h; return id
    }
    func unsubscribe(_ id: UUID?) { if let id { subscribers[id] = nil } }

    init() { if creds != nil { restoreLocal(); connect() } }

    func paired(_ c: Credentials) {
        CredentialStore.save(c)
        creds = c
        restoreLocal()
        connect()
    }

    func unpair() {
        // Best effort: a revoke already dropped the token server-side.
        if let old = client, let t = apnsToken {
            Task { _ = try? await old.call("push_unsubscribe_apns", ["token": t]); old.stop() }
        } else { client?.stop() }
        client = nil
        connected = false // the stopped client's "close" is dropped (BurrowClient.emit)
        clearCache()
        CredentialStore.clear()
        creds = nil
        chats = []; workspaces = []; phases = [:]; seen = [:]
    }

    func connect() {
        guard let creds else { return }
        client?.stop()
        connected = false // the stopped client's "close" is dropped; the new one's "open" sets it
        let c = BurrowClient(creds)
        c.onEvent = { [weak self] name, payload in self?.event(name, payload) }
        c.onRevoked = { [weak self] in self?.unpair(); self?.error = BurrowError.revoked.localizedDescription }
        client = c
        c.start()
        PushDelegate.register()
    }

    private func sendPushToken() {
        guard let client, client.isOpen, let t = apnsToken else { return }
        Task { _ = try? await client.call("push_subscribe_apns", ["token": t, "sandbox": PushDelegate.sandbox]) }
    }

    /// Back from the background: the socket may be dead without knowing it.
    func foreground() {
        guard creds != nil else { return }
        guard let client, client.isOpen else { connect(); return }
        Task { if await !client.ping() { client.reconnect() } }
    }

    private func event(_ name: String, _ payload: Any?) {
        for h in subscribers.values { h(name, payload) }
        switch name {
        case "close":
            connected = false
        case "open":
            connected = true
            sendPushToken()
            // The flush needs nothing from the snapshot (chat_queue_add by id).
            Task { await flushOutbox() }
            Task { await loadSnapshot() }
        case "resync":
            Task { await loadSnapshot() }
        case "chats-changed":
            Task { await loadChats() }
        case "seen-changed":
            if let d = payload as? [String: Any], let key = d["key"] as? String, key.hasPrefix("chat:"), let id = Int(key.dropFirst(5)) {
                let at = dbl(d["at"])
                seen[id] = at == 0 ? 0 : max(seen[id] ?? 0, at)
                phasesChanged(changed: nil)
            }
        default:
            if name.hasPrefix("phase-chat:"), let id = Int(name.dropFirst(11)), let d = payload as? [String: Any] {
                phases[id] = Phase(d)
                if viewing == id, !phases[id]!.inFlight { markSeen(id) }
                phasesChanged(changed: id)
            } else if name.hasPrefix("chat-event-"), let id = Int(name.dropFirst(11)) {
                chatListeners[id]?()
            }
        }
    }

    @MainActor
    func loadSnapshot() async {
        guard let client, let snap = try? await client.call("shell_snapshot") as? [String: Any] else { return }
        client.noteSeq((snap["seq"] as? NSNumber)?.int64Value ?? 0)
        workspaces = (snap["workspaces"] as? [[String: Any]] ?? []).map(Workspace.init)
        CachedWorkspace.save(workspaces.filter { $0.parentId == nil }.map { CachedWorkspace(id: $0.id, name: $0.name) })
        phases = Self.parsePhases(snap["phases"])
        if let list = try? await client.call("seen_list") as? [[String: Any]] {
            var s: [Int: Double] = [:]
            for r in list { if let k = r["key"] as? String, k.hasPrefix("chat:"), let id = Int(k.dropFirst(5)) { s[id] = dbl(r["at"]) } }
            seen = s
        }
        await loadChats()
        saveCache()
    }

    @MainActor
    func loadChats() async {
        // list_chats, not the snapshot's slim phone shape: turn_needs, snooze,
        // settle, parent and launch settings are all on the full row.
        guard let list = try? await client?.call("list_chats") as? [[String: Any]] else { return }
        rawChats = list
        chats = list.map(Chat.init)
        phasesChanged(changed: nil)
    }

    func phase(_ id: Int) -> Phase { phases[id] ?? Phase() }
    func chat(_ id: Int) -> Chat? { chats.first { $0.id == id } }
    func workspace(_ id: Int) -> Workspace? { workspaces.first { $0.id == id } }
    func projectIcon(_ workspaceId: Int) -> UIImage? {
        guard let w = workspace(workspaceId) else { return nil }
        return IconCache.image(workspace(w.repoId)?.icon ?? w.icon)
    }

    func projectName(_ chat: Chat) -> String {
        guard let w = workspace(chat.workspaceId) else { return "" }
        return workspace(w.repoId)?.name ?? w.name
    }

    func status(_ id: Int) -> Status {
        let p = phase(id)
        let unseen = p.turnEndedAt > (seen[id] ?? 0)
        switch p.state {
        case "running": return .running
        case "waiting_input": return .waiting
        case "waiting_approval": return .permission
        case "failed": return unseen ? .error : .idle
        case "done": return unseen ? .unread : .idle
        default: return .idle
        }
    }

    /// A finished unread turn whose end_turn said nothing is left: a tick, not a dot.
    func quietDone(_ chat: Chat) -> Bool {
        status(chat.id) == .unread && chat.turnNeeds?.needsYou == false && chat.turnNeeds?.next.isEmpty == true
    }

    func needsYou(_ chat: Chat) -> Bool {
        let s = status(chat.id)
        return s.needsYou || (!s.isRunning && chat.turnNeeds?.needsYou == true)
    }

    /// settledFor in claudeChats.ts: snoozed, settled by hand, or quiet for 3 days.
    func settled(_ chat: Chat) -> Bool {
        let s = status(chat.id)
        if s == .running || s == .waiting || s == .permission { return false }
        let now = Date().timeIntervalSince1970 * 1000
        if chat.snoozedUntil > now { return true }
        if chat.settledOverride == "settled" { return true }
        if chat.settledOverride == "active" { return false }
        if chat.turnNeeds?.needsYou == true { return false }
        return now - chat.lastActivityAt >= 3 * 86_400_000
    }

    /// Top-level threads the sidebar lists (no sub-agents, side chats, archive).
    var threads: [Chat] {
        chats.filter { !$0.archived && $0.parentChatId == 0 && $0.kind != "side" }
            .sorted { max($0.lastActivityAt, phase($0.id).updatedAt) > max($1.lastActivityAt, phase($1.id).updatedAt) }
    }

    // MARK: actions

    func markSeen(_ id: Int) {
        seen[id] = max(seen[id] ?? 0, phase(id).turnEndedAt)
        phasesChanged(changed: nil)
        Task { _ = try? await client?.call("seen_mark", ["key": "chat:\(id)"]) }
    }

    func markUnread(_ id: Int) {
        seen[id] = 0
        Task { _ = try? await client?.call("seen_mark_unread", ["key": "chat:\(id)"]) }
    }

    func send(_ chat: Int, _ text: String) async throws {
        // Into a running turn chat_send would steer; queue it instead, like the desktop composer.
        let cmd = phase(chat).inFlight ? "chat_queue_add" : "chat_send"
        if try await sendOrHold(chat, cmd, text, []) { return }
        nudgeChat(chat)
        if let c = self.chat(chat) { startActivity(c) }
    }

    /// Re-read an open chat now, without waiting for its stream: the user's own
    /// message must show the moment it is sent.
    /// The composer's own send, echoed to the open ChatView before the core has
    /// it (waking a slept chat takes seconds). nil text = the send failed.
    func localSend(_ chat: Int, _ text: String?) { event("chat-local-send-\(chat)", text) }

    func nudgeChat(_ chat: Int) {
        event("chat-event-\(chat)", nil)
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) { [weak self] in self?.event("chat-event-\(chat)", nil) }
    }

    func interrupt(_ chat: Int) async { _ = try? await client?.call("chat_interrupt", ["chatId": chat]) }

    func setLaunch(_ chat: Int, model: String? = nil, effort: String? = nil, permission: String? = nil) async throws {
        var patch: [String: Any] = [:]
        if let model { patch["model"] = model }
        if let effort { patch["effort"] = effort }
        if let permission { patch["permission_mode"] = permission }
        try await client?.call("chat_set_launch_settings", ["chatId": chat, "patch": patch])
        await loadChats()
    }

    func createChat(workspace: Int, agent: String, model: String, effort: String, permission: String, text: String) async throws -> Int {
        let row = try await client?.call("remote_create_chat", ["workspaceId": workspace, "agentKind": agent,
                                                               "model": model, "effort": effort, "permissionMode": permission]) as? [String: Any]
        let id = int(row?["id"])
        guard id > 0 else { throw BurrowError.server("Could not create the thread.") }
        if let client {
            let current = row?["title"] as? String ?? ""
            let cwd = self.workspace(workspace)?.path ?? ""
            Task { await client.nameThread(chatId: id, current: current, cwd: cwd, text: text) }
        }
        await loadChats()
        try await send(id, text)
        return id
    }

    // MARK: widget + Live Activity

    private func phasesChanged(changed: Int?) {
        let live = threads
        let waiting = live.filter { needsYou($0) }
        let working = live.filter { status($0.id) == .running }
        WidgetSnapshot(waiting: waiting.count, working: working.count,
                       top: (waiting + working).prefix(3).map(\.displayTitle), at: .now).save()
        WidgetCenter.shared.reloadAllTimelines()
        if let changed { updateActivities(only: changed) }
    }

    func startActivity(_ chat: Chat) {
        guard ActivityAuthorizationInfo().areActivitiesEnabled,
              !Activity<ChatActivityAttributes>.activities.contains(where: { $0.attributes.chatId == chat.id }) else { return }
        let state = ChatActivityAttributes.ContentState(state: "running", detail: "", startedAt: .now)
        _ = try? Activity.request(attributes: ChatActivityAttributes(chatId: chat.id, title: chat.displayTitle, project: projectName(chat)),
                                  content: .init(state: state, staleDate: nil))
    }

    // ponytail: updates only while the app holds the socket (foreground + a short
    // background grace). Updating a locked phone needs APNs push-to-start/update
    // from the core — add when the lock-screen view going stale actually hurts.
    private func updateActivities(only: Int) {
        for act in Activity<ChatActivityAttributes>.activities where act.attributes.chatId == only {
            let p = phase(only)
            let st = ChatActivityAttributes.ContentState(state: p.state, detail: p.detail, startedAt: act.content.state.startedAt)
            Task {
                if p.inFlight {
                    await act.update(.init(state: st, staleDate: nil))
                } else {
                    await act.end(.init(state: st, staleDate: nil), dismissalPolicy: .after(.now.addingTimeInterval(15 * 60)))
                }
            }
        }
    }
}

extension Status {
    var isRunning: Bool { self == .running }
}

/// Launch-setting choices. ponytail: the Claude list mirrors MODELS_BY_AGENT in
/// src/lib/chatModels.ts by hand; fetch it from the core when it drifts.
enum Catalog {
    static let claudeModels: [(id: String, label: String)] = [
        ("", "Default"), ("claude-opus-5-5", "Opus 5.5"), ("claude-fable-5-1", "Fable 5.1"), ("claude-opus-5", "Opus 5"),
        ("claude-sonnet-5-5", "Sonnet 5.5"), ("claude-sonnet-5", "Sonnet 5"), ("claude-haiku-5-5", "Haiku 5.5"),
    ]
    static let claudeEfforts = ["", "low", "medium", "high", "xhigh", "max"]
    static let codexEfforts = ["", "minimal", "low", "medium", "high"]
    static let permissions: [(id: String, label: String, icon: String)] = [
        ("default", "Ask", "hand.raised"), ("auto", "Auto", "sparkles"), ("acceptEdits", "Accept edits", "pencil"),
        ("plan", "Plan", "list.bullet.clipboard"), ("bypassPermissions", "Bypass", "exclamationmark.shield"),
    ]

    static func modelLabel(_ id: String) -> String { claudeModels.first { $0.id == id }?.label ?? (id.isEmpty ? "Default" : id) }
    static func effortLabel(_ e: String) -> String {
        ["": "Auto", "xhigh": "Extra high"][e] ?? e.prefix(1).uppercased() + e.dropFirst()
    }
    static func permission(_ id: String) -> (id: String, label: String, icon: String) {
        permissions.first { $0.id == id } ?? permissions[0]
    }
}
