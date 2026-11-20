import Observation
import SwiftUI
import UIKit

/// Thread actions (archive, settle, snooze, pin, rename) — used in the chat's ⋯ menu
/// and the thread list's context menu. Mirrors claudeChats.ts archive/settle/snooze
/// and pinnedChats.ts.
struct ThreadMenuItems: View {
    @Environment(AppModel.self) private var model
    let chat: Chat

    var body: some View {
        let now = Date().timeIntervalSince1970 * 1000
        Button { model.togglePin(chat) } label: {
            model.isPinned(chat) ? Label("Unpin", systemImage: "pin.slash") : Label("Pin", systemImage: "pin")
        }
        Button { rename() } label: { Label("Rename…", systemImage: "pencil") }
        if chat.snoozedUntil > now {
            Button { model.act { try await model.snooze(chat.id, until: 0) } } label: {
                Label("Unsnooze (wakes \(wakeLabel(chat.snoozedUntil)))", systemImage: "moon.zzz")
            }
        } else if !chat.archived {
            Menu {
                ForEach(snoozePresets(), id: \.label) { p in
                    Button(p.label) { model.act { try await model.snooze(chat.id, until: p.until) } }
                }
            } label: { Label("Snooze", systemImage: "moon.zzz") }
        }
        if !chat.archived {
            if model.settled(chat) {
                Button { model.act { try await model.unsettle(chat) } } label: { Label("Move back to active", systemImage: "arrow.uturn.up") }
            } else {
                Button { model.act { try await model.settle(chat) } } label: { Label("Settle", systemImage: "checkmark.circle") }
            }
        }
        Divider()
        if chat.archived {
            Button { model.act { try await model.unarchive(chat.id) } } label: { Label("Bring back", systemImage: "tray.and.arrow.up") }
        } else {
            Button(role: .destructive) { model.act { try await model.archive(chat.id) } } label: { Label("Archive", systemImage: "archivebox") }
        }
    }

    private func rename() {
        let alert = UIAlertController(title: "Rename thread", message: nil, preferredStyle: .alert)
        alert.addTextField { $0.text = chat.title; $0.clearButtonMode = .whileEditing; $0.autocapitalizationType = .sentences }
        alert.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        alert.addAction(UIAlertAction(title: "Rename", style: .default) { [model, chat] _ in
            let t = alert.textFields?.first?.text?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            if !t.isEmpty, t != chat.title { model.act { try await model.rename(chat.id, t) } }
        })
        presentOnTop(alert)
    }
}

// MARK: snooze presets (src/lib/snoozePresets.ts)

struct SnoozePreset { var label: String; var until: Double }

func snoozePresets(now: Date = .now, calendar cal: Calendar = .current) -> [SnoozePreset] {
    func at(_ dayOffset: Int, _ hour: Int) -> Date {
        let day = cal.date(byAdding: .day, value: dayOffset, to: cal.startOfDay(for: now))!
        return cal.date(bySettingHour: hour, minute: 0, second: 0, of: day)!
    }
    var out = [SnoozePreset(label: "1 hour", until: now.addingTimeInterval(3600).ms)]
    let evening = at(0, 18)
    if evening > now { out.append(.init(label: "This evening", until: evening.ms)) }
    out.append(.init(label: "Tomorrow 9:00", until: at(1, 9).ms))
    // weekday: 1 = Sunday … 2 = Monday; a Monday never picks itself.
    let toMonday = (9 - cal.component(.weekday, from: now)) % 7
    out.append(.init(label: "Next Monday 9:00", until: at(toMonday == 0 ? 7 : toMonday, 9).ms))
    return out
}

/// formatWakeTime: "18:00" today, "Tue 9:00" within a week, else "3 Nov 9:00".
func wakeLabel(_ until: Double, now: Date = .now) -> String {
    let d = Date(timeIntervalSince1970: until / 1000)
    if Calendar.current.isDate(d, inSameDayAs: now) { return d.formatted(date: .omitted, time: .shortened) }
    if d.timeIntervalSince(now) < 6 * 86400 { return d.formatted(.dateTime.weekday(.abbreviated).hour().minute()) }
    return d.formatted(.dateTime.day().month(.abbreviated).hour().minute())
}

private extension Date { var ms: Double { (timeIntervalSince1970 * 1000).rounded() } }

// MARK: pins (config.json `sidebarPinnedChats`, shared with the desktop)

@Observable
final class PinStore {
    static let shared = PinStore()
    var ids: [Int] = []
    @ObservationIgnored var started = false
}

func presentOnTop(_ vc: UIViewController) {
    let scene = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first { $0.activationState == .foregroundActive }
        ?? UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first
    var top = scene?.keyWindow?.rootViewController
    while let p = top?.presentedViewController { top = p }
    top?.present(vc, animated: true)
}

extension AppModel {
    /// Run an action; a failure is shown as an alert (the menu that started it is gone by then).
    func act(_ f: @escaping () async throws -> Void) {
        Task { @MainActor in
            do { try await f() } catch {
                let a = UIAlertController(title: "Couldn't do that", message: error.localizedDescription, preferredStyle: .alert)
                a.addAction(UIAlertAction(title: "OK", style: .default))
                presentOnTop(a)
            }
        }
    }

    // MARK: pins

    func isPinned(_ chat: Chat) -> Bool {
        let pins = PinStore.shared
        if !pins.started { startPins() }
        return pins.ids.contains(chat.id)
    }

    func togglePin(_ chat: Chat) {
        let pins = PinStore.shared
        if !pins.started { startPins() }
        pins.ids = pins.ids.contains(chat.id) ? pins.ids.filter { $0 != chat.id } : pins.ids + [chat.id]
        let ids = pins.ids
        act { try await self.client?.call("config_set", ["key": "sidebarPinnedChats", "value": ids]) }
    }

    private func startPins() {
        let pins = PinStore.shared
        pins.started = true
        subscribe { name, payload in
            if name == "open" || name == "resync" { Task { await self.loadPins() } }
            if name == "config-changed", let d = payload as? [String: Any], d["key"] as? String == "sidebarPinnedChats" {
                pins.ids = (d["value"] as? [Any] ?? []).map { int($0) }
            }
        }
        Task { await loadPins() }
    }

    @MainActor private func loadPins() async {
        guard let raw = try? await client?.call("read_config") as? String,
              let cfg = try? JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [String: Any] else { return }
        PinStore.shared.ids = (cfg["sidebarPinnedChats"] as? [Any] ?? []).map { int($0) }
    }

    // MARK: row edits

    /// save_chats writes whole rows (title, settled_override, archived_at, …), so
    /// patch Go's current row, never a reconstruction from the app's slim `Chat`.
    @MainActor
    func patchChats(_ ids: Set<Int>, _ patch: (inout [String: Any]) -> Void) async throws {
        guard let client else { throw BurrowError.server("Not connected.") }
        let rows = try await client.call("list_chats") as? [[String: Any]] ?? []
        let changed = rows.filter { ids.contains(int($0["id"])) }.map { r -> [String: Any] in var r = r; patch(&r); return r }
        guard !changed.isEmpty else { return }
        try await client.call("save_chats", ["chats": changed])
        await loadChats()
    }

    func rename(_ id: Int, _ title: String) async throws {
        try await patchChats([id]) { $0["title"] = title; $0["pinned_title"] = true }
    }

    func snooze(_ id: Int, until: Double) async throws {
        try await client?.call("chat_snooze", ["chatId": id, "untilMs": Int64(until)])
        await loadChats()
    }

    func settle(_ chat: Chat) async throws {
        try await patchChats([chat.id]) { $0["settled_override"] = "settled" }
        // Manual settle stops the process now (Go refuses unless the phase is settled).
        _ = try? await client?.call("chat_sleep", ["chatId": chat.id])
    }

    func unsettle(_ chat: Chat) async throws {
        try await patchChats([chat.id]) { $0["settled_override"] = "active" }
        if chat.snoozedUntil > 0 { try await snooze(chat.id, until: 0) } // "Move back to active" wins over a snooze
    }

    /// Archive the thread and its sub-agents, stopping their processes (claudeChats.archive).
    func archive(_ id: Int) async throws {
        guard let client else { return }
        let rows = try await client.call("list_chats") as? [[String: Any]] ?? []
        let targets = rows.filter { int($0["id"]) == id || int($0["parent_chat_id"]) == id }
        for r in targets {
            let t = r["transport"] as? String ?? ""
            let stop = t == "codex-app-server" ? "codex_stop" : t.isEmpty || t == "claude-cli" ? "claude_stop" : "acp_stop"
            _ = try? await client.call(stop, ["id": String(int(r["id"]))])
        }
        let now = Int64(Date().timeIntervalSince1970 * 1000)
        try await patchChats(Set(targets.map { int($0["id"]) })) { if int($0["archived_at"]) == 0 { $0["archived_at"] = now } }
    }

    func unarchive(_ id: Int) async throws {
        try await patchChats([id]) { $0["archived_at"] = 0 }
    }

    func deleteChat(_ id: Int) async throws {
        try await client?.call("delete_chat", ["id": id])
        if PinStore.shared.ids.contains(id), let c = chat(id) { togglePin(c) }
        await loadChats()
    }
}
