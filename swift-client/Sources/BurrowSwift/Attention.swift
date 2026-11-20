import AppKit
import Combine
import SwiftUI
import UserNotifications

/// State for everything that asks the user for something outside one chat's transcript:
/// secret requests, PTY permission prompts, "Always allow" rules (chatAttention.ts + claudeChats.ts rules).
@MainActor
final class AttentionStore: ObservableObject {
    static let shared = AttentionStore()

    /// list_secret_requests / secret-requests — name, reason, hasSaved; never a value.
    @Published var secrets: [JSON] = []
    /// pty id (string, as Go sends it) → that tab's pending permission requests, oldest first.
    @Published var ptyPerms: [String: [JSON]] = [:]
    /// config.json `chatPermissionRules` — tool names / `Bash:<cmd>` allowed without asking.
    @Published var rules: [String] = []
    /// config.json `uiPrefs` (sounds, ntfy, toastPosition).
    @Published var prefs: JSON = [:]
    /// "<chatID>:<requestID>" → when it was answered. Cards hide at once (also in the RP sub-agent detail, whose
    /// ChatFeed only reloads on the next phase event); expires so a reused rpc id can show again later.
    @Published var answered: [String: Date] = [:]
    /// Last notify per "<key>|<state>" — ≤ 1 per 30 s, like Go's pushGate.
    private var lastNotified: [String: Date] = [:]

    private weak var b: Burrow?
    private let rulesKey = "chatPermissionRules"

    func secretRequests(owner: String) -> [PendingRequest] {
        secrets.filter { $0.s("owner") == owner }.map(PendingRequest.init(secret:))
    }

    func start(_ b: Burrow) {
        guard self.b == nil else { return }
        self.b = b
        b.subscribe("secret-requests") { [weak self] _, p in
            self?.secrets = (p as? JSON)?["requests"] as? [JSON] ?? []
            Task { await b.loadRequests() } // chats show theirs in b.requests
        }
        b.subscribe("pty-permissions") { [weak self] _, p in
            guard let p = p as? JSON else { return }
            self?.ptyPerms["\(p["pty_id"] ?? "")"] = p["requests"] as? [JSON] ?? []
        }
        b.subscribe("config-changed") { [weak self] _, p in
            guard let self, let p = p as? JSON else { return }
            switch p.s("key") {
            case "*": Task { await self.reload() }
            case self.rulesKey: self.rules = p["value"] as? [String] ?? []
            case "uiPrefs": self.prefs = p["value"] as? JSON ?? [:]
            default: break
            }
        }
        // Subscribers run before Burrow stores the new phase, so b.phases[key] is still the previous one here.
        b.subscribe("phase-") { [weak self] name, p in
            guard let self, let next = p as? JSON else { return }
            let key = String(name.dropFirst("phase-".count))
            let prev = b.phases[key]?.s("state") ?? ""
            guard next.s("state") != prev else { return }
            Task { await self.transition(key, next) }
        }
        b.subscribe("chat-snooze-woke") { [weak self] _, p in
            guard let self, let id = (p as? JSON)?.i("chat_id"), let c = b.chat(id) else { return }
            let title = c.s("title").isEmpty ? "Chat" : c.s("title")
            ToastCenter.shared.push("Snooze over", body: title, kind: .info, target: "chat:\(id)")
            AttentionSound.play(.done, self.prefs)
            self.ntfy("done", title)
        }
        Notifier.shared.setUp(b)
    }

    /// Re-read every list on (re)connect — events missed while the socket was down aren't replayed.
    func reload() async {
        guard let b else { return }
        if let list = try? await b.call("list_secret_requests") as? [JSON] { secrets = list }
        if let list = try? await b.call("list_pty_permissions") as? [JSON] {
            ptyPerms = Dictionary(grouping: list) { $0.s("pty_id") }
        }
        if let raw = try? await b.call("read_config") as? String,
           let cfg = (try? JSONSerialization.jsonObject(with: Data(raw.utf8))) as? JSON {
            rules = cfg[rulesKey] as? [String] ?? []
            prefs = cfg["uiPrefs"] as? JSON ?? [:]
        }
        await b.loadRequests()
    }

    func addRule(_ key: String, _ b: Burrow) async {
        guard !key.isEmpty, !rules.contains(key) else { return }
        rules.append(key)
        _ = try? await b.call("config_set", ["key": rulesKey, "value": rules])
    }

    func removeRule(_ key: String, _ b: Burrow) async {
        rules.removeAll { $0 == key }
        _ = try? await b.call("config_set", ["key": rulesKey, "value": rules])
    }

    func markAnswered(_ chatID: Int64, _ r: PendingRequest) {
        let now = Date()
        answered = answered.filter { now.timeIntervalSince($0.value) < 15 }
        answered["\(chatID):\(r.id)"] = now
    }

    func isAnswered(_ chatID: Int64?, _ r: PendingRequest) -> Bool {
        if case let .secret(s) = r.kind, !secrets.contains(where: { $0.s("id") == s.s("id") }) { return true }
        guard let chatID, let at = answered["\(chatID):\(r.id)"] else { return false }
        return Date().timeIntervalSince(at) < 15
    }

    /// One phase transition → toast (in-app), sound, ntfy, native banner (chatHost.ts notifyDone / notifyPermission).
    private func transition(_ key: String, _ phase: JSON) async {
        let state = phase.s("state")
        guard ["waiting_approval", "waiting_input", "done", "failed"].contains(state), let b else { return }
        if state == "waiting_approval", key.hasPrefix("chat:"), let id = Int64(key.dropFirst(5)),
           await autoAllowCoveredAll(id) { return }
        guard let d = Notifier.describe(key: key, phase: phase, b) else { return }
        let gateKey = "\(key)|\(state)", now = Date()
        if let last = lastNotified[gateKey], now.timeIntervalSince(last) < 30 { return }
        lastNotified[gateKey] = now

        let watching = Notifier.windowKey && (key == "chat:\(b.selectedChat ?? -1)" || key == "pty:\(b.selectedTab ?? -1)"
            || key == "chat:\(SubagentNav.shared.open ?? -1)")
        let waiting = state.hasPrefix("waiting_")
        // A Claude tab's permission prompt has its own answerable toast (PtyPermissionToasts) — don't add a second.
        if key.hasPrefix("pty:"), state == "waiting_approval" {
            try? await Task.sleep(for: .milliseconds(500))
            if !(ptyPerms[String(key.dropFirst(4))] ?? []).isEmpty { AttentionSound.play(.waiting, prefs); return }
        }
        if !(watching && state == "done") {
            ToastCenter.shared.push(d.title, body: d.body, kind: state == "failed" ? .error : waiting ? .info : .done, target: d.target,
                                    action: watching ? nil : (label: "Open", run: { openAttentionTarget(d.target, b) }))
        }
        if waiting { AttentionSound.play(.waiting, prefs) }
        else if state == "done", !watching { AttentionSound.play(.done, prefs) }
        ntfy(state == "done" ? "done" : state == "failed" ? "error" : state == "waiting_approval" ? "permission" : "waiting",
             "\(d.title): \(d.body)")
        if state != "failed" { Notifier.shared.post(d, key: key, state: state) }
    }

    /// lib/ntfy.ts — JSON publish, best effort.
    func ntfy(_ event: String, _ message: String) {
        let p = prefs
        guard p["ntfyEnabled"] as? Bool == true, !p.s("ntfyTopic").isEmpty,
              (p["ntfyEvents"] as? [String] ?? ["done", "permission", "error"]).contains(event),
              !(p["ntfyOnlyWhenAway"] as? Bool ?? true && Notifier.windowKey) else { return }
        let meta: [String: (Int, String, String)] = [
            "done": (3, "white_check_mark", "Task complete"), "waiting": (4, "speech_balloon", "Waiting for input"),
            "permission": (4, "lock", "Permission needed"), "error": (5, "rotating_light", "Turn failed"),
        ]
        guard let m = meta[event] else { return }
        var server = p.s("ntfyServer").isEmpty ? "https://ntfy.sh" : p.s("ntfyServer")
        while server.hasSuffix("/") { server.removeLast() }
        guard let url = URL(string: server) else { return }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if !p.s("ntfyToken").isEmpty { req.setValue("Bearer \(p.s("ntfyToken"))", forHTTPHeaderField: "Authorization") }
        let body: JSON = ["topic": p.s("ntfyTopic"), "title": "Burrow · \(m.2)", "message": message.isEmpty ? m.2 : message,
                          "priority": m.0, "tags": [m.1]]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)
        URLSession.shared.dataTask(with: req).resume() // a failed push must never disrupt the UI
    }

    /// Answers every Claude can_use_tool request of chat `id` that an "Always" rule covers (chatSession.ts onLine).
    /// True when that left nothing for the user, so no notification.
    private func autoAllowCoveredAll(_ id: Int64) async -> Bool {
        guard let b, !rules.isEmpty else { return false }
        let lines = (try? await b.call("load_pending_chat_requests", ["chatId": "\(id)"])) as? [JSON] ?? []
        let reqs = lines.compactMap { PendingRequest(line: $0.s("line")) }
        var answered = 0
        for r in reqs {
            guard case let .permission(_, tool, input) = r.kind,
                  ruleKeys(tool, input).contains(where: rules.contains) else { continue }
            await b.answer(r, .permission(allow: true, always: false), chatID: id)
            answered += 1
        }
        return answered > 0 && answered == reqs.count
    }
}

/// Native notifications (permission / waiting / done) while the window is not key. Click opens the thread or tab.
@MainActor
final class Notifier: NSObject, UNUserNotificationCenterDelegate {
    static let shared = Notifier()
    private weak var b: Burrow?
    /// `swift run` has no bundle: UNUserNotificationCenter.current() traps there.
    private var available: Bool { Bundle.main.bundleIdentifier != nil }

    func setUp(_ b: Burrow) {
        self.b = b
        guard available else { return }
        let c = UNUserNotificationCenter.current()
        c.delegate = self
        c.requestAuthorization(options: [.alert, .sound, .badge]) { _, _ in }
    }

    static var windowKey: Bool { NSApp.isActive && NSApp.keyWindow != nil }

    struct Described { let title: String; let body: String; let target: String }

    /// Title / body / click target for a phase transition; nil = nothing to tell (archived chat, idle shell's done).
    static func describe(key: String, phase: JSON, _ b: Burrow) -> Described? {
        let state = phase.s("state")
        var title = phase.s("title")
        var target = key
        if key.hasPrefix("chat:"), let id = Int64(key.dropFirst(5)) {
            guard let c = b.chat(id), c.i("archived_at") == 0 else { return nil }
            title = c.s("title").isEmpty ? "Untitled" : c.s("title")
            if c.i("parent_chat_id") != 0 { // a sub-agent's prompt opens its parent thread
                title = "↳ " + title
                target = "chat:\(c.i("parent_chat_id"))|sub:\(id)"
            }
        } else if key.hasPrefix("pty:") {
            guard phase["is_agent"] as? Bool == true || state != "done" else { return nil }
            if title.isEmpty { title = "Terminal" }
        } else { return nil }
        let detail = phase.s("detail")
        let body: String
        switch state {
        case "waiting_approval": body = detail.isEmpty ? "Needs your permission" : detail
        case "waiting_input": body = detail.isEmpty ? "Waiting for your input" : detail
        case "failed": body = detail.isEmpty ? "Turn failed" : detail
        default: body = "Finished"
        }
        return Described(title: title, body: body, target: target)
    }

    /// Native banner, only while the window is not key (in-app toasts cover the focused case).
    func post(_ d: Described, key: String, state: String) {
        guard available, !Self.windowKey else { return }
        let n = UNMutableNotificationContent()
        n.title = d.title
        n.body = d.body
        n.sound = nil // AttentionSound plays the user's chosen sound
        n.userInfo = ["target": d.target]
        n.threadIdentifier = key
        // Same identifier per key: a newer state replaces the older banner instead of stacking.
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: key, content: n, trigger: nil))
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                            withCompletionHandler done: @escaping () -> Void) {
        let target = response.notification.request.content.userInfo["target"] as? String ?? ""
        Task { @MainActor in
            NSApp.activate(ignoringOtherApps: true)
            if let b = self.b { openAttentionTarget(target, b) }
            done()
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                            withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        done([.banner, .sound])
    }
}

// OWNER: attention agent. Mounted once in ContentView's background: notifications, Dock badge, global listeners.
struct AttentionSlot: View {
    @EnvironmentObject var b: Burrow
    private let minute = Timer.publish(every: 60, on: .main, in: .common).autoconnect()

    var body: some View {
        Color.clear
            .onAppear { AttentionStore.shared.start(b) }
            .onReceive(b.$connected) { if $0 { Task { await AttentionStore.shared.reload() } } }
            .onReceive(b.$phases) { phases in
                let n = phases.values.filter { $0.s("state").hasPrefix("waiting_") }.count
                NSApp.dockTile.badgeLabel = n > 0 ? "\(n)" : nil
            }
            .onReceive(minute) { _ in markActive() }
            .onReceive(b.$selectedChat) { _ in DispatchQueue.main.async { markActive() } }
            .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in markActive() }
    }

    /// "Never sleep a watched chat" reaches Go this way (chatsleep.go); also gates Web Push for it.
    private func markActive() {
        guard Notifier.windowKey, let id = b.selectedChat else { return }
        Task { try? await b.call("chat_mark_active", ["chatId": id]) }
    }
}

/// lib/sounds.ts: bundled sounds (Resources/sounds) or a custom file, honouring uiPrefs enable + volume.
@MainActor
enum AttentionSound {
    enum Kind { case done, waiting }
    static let builtin = ["soft-1", "soft-2", "soft-3", "whisper-1", "whisper-2", "voice-1",
                          "need-you-1", "need-you-2", "need-you-voice"] // need-you-* are for "waiting" only
    /// One player per kind so rapid repeats restart instead of piling up.
    private static var players: [String: NSSound] = [:]

    static func play(_ kind: Kind, _ prefs: JSON, force: Bool = false) {
        let done = kind == .done
        if !force {
            guard prefs["soundEnabled"] as? Bool ?? true,
                  prefs[done ? "soundDoneEnabled" : "soundWaitingEnabled"] as? Bool ?? true else { return }
        }
        let id = prefs.s(done ? "soundDoneId" : "soundWaitingId")
        let url: URL?
        if id == "custom" {
            let path = (prefs.s(done ? "soundDoneCustomPath" : "soundWaitingCustomPath") as NSString).expandingTildeInPath
            url = path.isEmpty ? nil : URL(fileURLWithPath: path)
        } else {
            let name = builtin.contains(id) ? id : (done ? "soft-1" : "need-you-1")
            url = Bundle.module.url(forResource: name, withExtension: "wav", subdirectory: "Resources/sounds")
        }
        guard let url else { return }
        let key = url.path
        let s = players[key] ?? NSSound(contentsOf: url, byReference: true)
        guard let s else { return }
        players[key] = s
        s.stop()
        s.volume = Float(max(0, min(100, (prefs["soundVolume"] as? NSNumber)?.doubleValue ?? 70)) / 100)
        s.play()
    }
}
