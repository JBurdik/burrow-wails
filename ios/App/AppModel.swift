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
    var run: TurnRun?
}

/// An ask's command for the user to run (control.TurnRun): Go runs the STORED
/// command via chat_run_ask, never text from the client.
struct TurnRun: Hashable {
    var command: String
    var state: String      // "" | running | done
    var exit: Int
    var interrupted: Bool
    var reply: String      // kept on a card with other asks; goes with the answers

    init?(_ d: [String: Any]?) {
        guard let d, let cmd = d["command"] as? String, !cmd.isEmpty else { return nil }
        command = cmd
        state = d["state"] as? String ?? ""
        exit = int(d["exit"])
        interrupted = d["interrupted"] as? Bool ?? false
        reply = d["reply"] as? String ?? ""
    }
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
                    details: $0["details"] as? [String] ?? [], suggested: int($0["suggested"], -1), multi: $0["multi"] as? Bool ?? false,
                    run: TurnRun($0["run"] as? [String: Any]))
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
    var recap: String
    var slept: Bool
    var turnNeeds: TurnNeeds?
    /// Thread attention, computed by Go (src-wails/chatattention.go), shared
    /// with the desktop: settled from this unix ms on (0 = not by any rule),
    /// the last end_turn asked the user / said nothing is left.
    var settledSince: Double
    var asksYou: Bool
    var saidDone: Bool

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
        recap = d["recap"] as? String ?? ""
        slept = int(d["slept_at"]) > 0
        turnNeeds = TurnNeeds(d["turn_needs"] as? String ?? "")
        settledSince = dbl(d["settled_since"])
        asksYou = d["needs_you"] as? Bool ?? false
        saidDone = d["said_done"] as? Bool ?? false
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

/// Overlapping reads of the same thing (snapshot on every open, list_chats on every
/// chats-changed) can answer out of order: only the read that STARTED last may apply.
struct LatestWins {
    private var started = 0, applied = 0
    mutating func begin() -> Int { started += 1; return started }
    mutating func accept(_ n: Int) -> Bool { guard n > applied else { return false }; applied = n; return true }
}

/// A snapshot's phases with the live `phase-chat` events that landed while it was in
/// flight on top: those are newer than the snapshot, which would wipe them.
func mergePhases(snapshot: [Int: Phase], live: [Int: Phase]) -> [Int: Phase] {
    snapshot.merging(live) { _, l in l }
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
    /// First prompt of a just-created thread, sent by its ChatView when it appears (createChat).
    var pendingFirst: [Int: String] = [:]
    /// Messages typed while offline (Offline.swift), per paired machine.
    var outbox: [OutboxItem] = []
    @ObservationIgnored var flushing = false
    @ObservationIgnored var flushAgain = false
    /// list_chats as received, for the launch cache.
    @ObservationIgnored var rawChats: [[String: Any]] = []
    @ObservationIgnored var snapshotReads = LatestWins()
    @ObservationIgnored var chatReads = LatestWins()
    /// Phase events seen while a snapshot is being read (nil = none in flight).
    @ObservationIgnored var phasesDuringSnapshot: [Int: Phase]?
    @ObservationIgnored var chatsReloading = false
    @ObservationIgnored var chatsReloadAgain = false
    @ObservationIgnored var lastWidget: WidgetSnapshot?
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
            reloadChatsCoalesced()
        case "seen-changed":
            if let d = payload as? [String: Any], let key = d["key"] as? String, key.hasPrefix("chat:"), let id = Int(key.dropFirst(5)) {
                let at = dbl(d["at"])
                seen[id] = at == 0 ? 0 : max(seen[id] ?? 0, at)
                phasesChanged(changed: nil)
            }
        default:
            if name.hasPrefix("phase-chat:"), let id = Int(name.dropFirst(11)), let d = payload as? [String: Any] {
                let p = Phase(d)
                phases[id] = p
                if phasesDuringSnapshot != nil { phasesDuringSnapshot?[id] = p }
                if viewing == id, !p.inFlight, p.turnEndedAt > (seen[id] ?? 0) { markSeen(id) }
                phasesChanged(changed: id)
            } else if name.hasPrefix("chat-event-"), let id = Int(name.dropFirst(11)) {
                chatListeners[id]?()
            }
        }
    }

    @MainActor
    func loadSnapshot() async {
        guard let client else { return }
        let gen = snapshotReads.begin(), chatGen = chatReads.begin()
        phasesDuringSnapshot = [:]
        // Three independent reads: one round trip, not three. `seq` comes from the
        // snapshot itself, so the order they answer in does not matter.
        async let snapR = try? client.call("shell_snapshot")
        async let seenR = try? client.call("seen_list")
        async let chatsR = try? client.call("list_chats")
        let (snapV, seenV, chatsV) = await (snapR, seenR, chatsR)
        // Not the newest read any more: a newer one owns the window and the result.
        guard snapshotReads.accept(gen) else { return }
        let live = phasesDuringSnapshot ?? [:]
        phasesDuringSnapshot = nil
        guard let snap = snapV as? [String: Any] else { return }
        client.noteSeq((snap["seq"] as? NSNumber)?.int64Value ?? 0)
        workspaces = (snap["workspaces"] as? [[String: Any]] ?? []).map(Workspace.init)
        CachedWorkspace.save(workspaces.filter { $0.parentId == nil }.map { CachedWorkspace(id: $0.id, name: $0.name) })
        phases = mergePhases(snapshot: Self.parsePhases(snap["phases"]), live: live)
        if let list = seenV as? [[String: Any]] {
            var s: [Int: Double] = [:]
            for r in list { if let k = r["key"] as? String, k.hasPrefix("chat:"), let id = Int(k.dropFirst(5)) { s[id] = dbl(r["at"]) } }
            seen = s
        }
        if let list = chatsV as? [[String: Any]], chatReads.accept(chatGen) { applyChats(list) }
        saveCache()
    }

    @MainActor
    func loadChats() async {
        // list_chats, not the snapshot's slim phone shape: turn_needs, snooze,
        // settle, parent and launch settings are all on the full row.
        let gen = chatReads.begin()
        guard let list = try? await client?.call("list_chats") as? [[String: Any]], chatReads.accept(gen) else { return }
        applyChats(list)
    }

    private func applyChats(_ list: [[String: Any]]) {
        rawChats = list
        chats = list.map(Chat.init)
        phasesChanged(changed: nil)
    }

    /// chats-changed fires on every message (last_activity): one list_chats at a
    /// time, and one more after it if events came in meanwhile.
    private func reloadChatsCoalesced() {
        if chatsReloading { chatsReloadAgain = true; return }
        chatsReloading = true
        Task { @MainActor in
            repeat { chatsReloadAgain = false; await loadChats() } while chatsReloadAgain
            chatsReloading = false
        }
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
        status(chat.id) == .unread && chat.saidDone
    }

    func needsYou(_ chat: Chat) -> Bool {
        let s = status(chat.id)
        return s.needsYou || (!s.isRunning && chat.asksYou)
    }

    /// settledFor in claudeChats.ts: the rule is Go's (settled_since); only the
    /// in-flight gate and the clock are ours.
    func settled(_ chat: Chat) -> Bool {
        let s = status(chat.id)
        if s == .running || s == .waiting || s == .permission { return false }
        return chat.settledSince > 0 && Date().timeIntervalSince1970 * 1000 >= chat.settledSince
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
        // Go decides send vs queue (chat_submit).
        if try await submitOrHold(chat, text, []) == .held { return }
        nudgeChat(chat)
        if let c = self.chat(chat) { startActivity(c) }
    }

    /// Re-read an open chat now, without waiting for its stream: the user's own
    /// message must show the moment it is sent.
    /// The composer's own send, echoed to the open ChatView before the core has
    /// it (waking a slept chat takes seconds), matched to Go's row by `clientId`.
    /// nil text = drop that echo (the send failed, or Go queued it).
    func localSend(_ chat: Int, _ text: String?, clientId: UUID) {
        var p: [String: Any] = ["clientId": clientId.uuidString]
        if let text { p["text"] = text }
        event("chat-local-send-\(chat)", p)
    }

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
        // Show the thread now and let ChatView send the first prompt (pendingFirst): it echoes the
        // message at once, and a slow agent start no longer keeps the New thread sheet open.
        chats.append(Chat(row ?? [:]))
        pendingFirst[id] = text
        Task { await loadChats() }
        return id
    }

    // MARK: widget + Live Activity

    private func phasesChanged(changed: Int?) {
        let live = threads
        let waiting = live.filter { needsYou($0) }
        let working = live.filter { status($0.id) == .running }
        let snap = WidgetSnapshot(waiting: waiting.count, working: working.count,
                                  top: (waiting + working).prefix(3).map(\.displayTitle), at: .now)
        if !(lastWidget?.sameContent(snap) ?? false) {
            lastWidget = snap
            snap.save()
            WidgetCenter.shared.reloadAllTimelines()
        }
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
