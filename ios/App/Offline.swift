import CryptoKit
import SwiftUI

// Offline support: a per-machine folder in Application Support holding the
// outbox (messages typed while the socket was down) and a cache of the last
// snapshot + recent transcript pages, so launch paints before the socket opens.
// No credentials or tokens ever go here (those stay in the Keychain).

/// `<Application Support>/burrow/<sha256(baseURL) prefix>/`: another paired Mac
/// gets its own folder, so its outbox and cache never mix with this one's.
enum LocalStore {
    static func dir(_ creds: Credentials?) -> URL? {
        guard let creds else { return nil }
        let key = SHA256.hash(data: Data(creds.baseURL.utf8)).prefix(8).map { String(format: "%02x", $0) }.joined()
        let url = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("burrow/\(key)", isDirectory: true)
        try? FileManager.default.createDirectory(at: url.appendingPathComponent("chats"), withIntermediateDirectories: true)
        return url
    }
    static func read(_ url: URL) -> Any? { (try? Data(contentsOf: url)).flatMap { try? JSONSerialization.jsonObject(with: $0) } }
    static func write(_ obj: Any, to url: URL) {
        if let d = try? JSONSerialization.data(withJSONObject: obj) { try? d.write(to: url, options: .atomic) }
    }
}

/// A message typed while offline; delivered via chat_queue_add on the next open.
struct OutboxItem: Codable, Identifiable, Hashable {
    var id = UUID()
    var chatId: Int
    var text: String
    var images: [String]
    /// Last server refusal (e.g. the chat is gone); kept for the user to delete.
    var error: String?
}

extension Workspace {
    init(_ d: [String: Any]) {
        self.init(id: int(d["id"]), name: d["name"] as? String ?? "", parentId: (d["parent_id"] as? NSNumber)?.intValue,
                  path: d["path"] as? String ?? "", icon: d["icon"] as? String)
    }
    var dict: [String: Any] {
        var d: [String: Any] = ["id": id, "name": name, "path": path]
        if let parentId { d["parent_id"] = parentId }
        if let icon { d["icon"] = icon }
        return d
    }
}

extension Phase {
    var dict: [String: Any] { ["state": state, "detail": detail, "turn_ended_at": turnEndedAt, "updated_at": updatedAt] }
}

extension AppModel {
    static let cachedChats = 20, cachedMessages = 150

    var online: Bool { client?.isOpen == true }

    /// Launch / pair: outbox + last snapshot of this machine, before the socket opens.
    func restoreLocal() {
        guard let dir = LocalStore.dir(creds) else { return }
        outbox = (try? Data(contentsOf: dir.appendingPathComponent("outbox.json")))
            .flatMap { try? JSONDecoder().decode([OutboxItem].self, from: $0) } ?? []
        guard let d = LocalStore.read(dir.appendingPathComponent("snapshot.json")) as? [String: Any] else { return }
        workspaces = (d["workspaces"] as? [[String: Any]] ?? []).map(Workspace.init)
        phases = Self.parsePhases(d["phases"])
        seen = Dictionary(uniqueKeysWithValues: (d["seen"] as? [String: Any] ?? [:]).compactMap { k, v in Int(k).map { ($0, dbl(v)) } })
        rawChats = d["chats"] as? [[String: Any]] ?? []
        chats = rawChats.map(Chat.init)
    }

    static func parsePhases(_ raw: Any?) -> [Int: Phase] {
        var ph: [Int: Phase] = [:]
        for (k, v) in raw as? [String: Any] ?? [:] where k.hasPrefix("chat:") {
            if let id = Int(k.dropFirst(5)), let d = v as? [String: Any] { ph[id] = Phase(d) }
        }
        return ph
    }

    /// After a snapshot and when the app leaves the foreground.
    func saveCache() {
        guard let dir = LocalStore.dir(creds), !rawChats.isEmpty else { return }
        let snap: [String: Any] = [
            "workspaces": workspaces.map(\.dict),
            "phases": Dictionary(uniqueKeysWithValues: phases.map { ("chat:\($0.key)", $0.value.dict) }),
            "seen": Dictionary(uniqueKeysWithValues: seen.map { (String($0.key), $0.value) }),
            "chats": rawChats,
        ]
        let pages = dirtyPages.compactMap { id in pageCache[id].map { (id, $0) } }
        dirtyPages = []
        DispatchQueue.global(qos: .utility).async {
            LocalStore.write(snap, to: dir.appendingPathComponent("snapshot.json"))
            let chatsDir = dir.appendingPathComponent("chats")
            for (id, page) in pages { LocalStore.write(page, to: chatsDir.appendingPathComponent("\(id).json")) }
            // Keep the most recently written pages only.
            let fm = FileManager.default
            let files = (try? fm.contentsOfDirectory(at: chatsDir, includingPropertiesForKeys: [.contentModificationDateKey])) ?? []
            let byAge = files.sorted {
                let a = (try? $0.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
                let b = (try? $1.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
                return a > b
            }
            for f in byAge.dropFirst(Self.cachedChats) { try? fm.removeItem(at: f) }
        }
    }

    /// Unpair: drop the cache, keep the outbox file (re-pairing this Mac delivers it).
    func clearCache() {
        if let dir = LocalStore.dir(creds) {
            try? FileManager.default.removeItem(at: dir.appendingPathComponent("snapshot.json"))
            try? FileManager.default.removeItem(at: dir.appendingPathComponent("chats"))
        }
        pageCache = [:]; dirtyPages = []; rawChats = []; outbox = []
    }

    // MARK: transcript pages

    /// The newest load_chat_messages_page result seen for a chat (memory, else disk).
    func cachedPage(_ chat: Int) -> [String: Any]? {
        if let p = pageCache[chat] { return p }
        guard let dir = LocalStore.dir(creds),
              let p = LocalStore.read(dir.appendingPathComponent("chats/\(chat).json")) as? [String: Any] else { return nil }
        pageCache[chat] = p
        return p
    }

    /// Kept in memory on every load (it runs per streamed line); written to disk by saveCache.
    func cachePage(_ chat: Int, _ page: [String: Any]) {
        var msgs = page["messages"] as? [[String: Any]] ?? []
        let drop = max(0, msgs.count - Self.cachedMessages)
        msgs = msgs.dropFirst(drop).map { m in
            guard let out = m["toolOutput"] as? String, out.count > 2000 else { return m }
            var m = m; m["toolOutput"] = String(out.prefix(2000)); return m // the row shows 1500
        }
        pageCache[chat] = ["start": int(page["start"]) + drop, "messages": msgs]
        dirtyPages.insert(chat)
    }

    // MARK: outbox

    func enqueue(_ chat: Int, _ text: String, _ images: [String] = []) {
        outbox.append(OutboxItem(chatId: chat, text: text, images: images))
        saveOutbox()
    }

    func removePending(_ id: UUID) {
        outbox.removeAll { $0.id == id }
        saveOutbox()
    }

    private func saveOutbox() {
        guard let dir = LocalStore.dir(creds), let d = try? JSONEncoder().encode(outbox) else { return }
        try? d.write(to: dir.appendingPathComponent("outbox.json"), options: .atomic)
    }

    /// On every socket open: each pending item in order through chat_queue_add (Go
    /// delivers it once the chat settles). Removed only after the call succeeded;
    /// a refusal stays with its error, a dropped socket stops the pass.
    // ponytail: a reply lost to a drop after Go queued it means a second copy on
    // the next open; an idempotency key in chat_queue_add would fix that.
    @MainActor func flushOutbox() async {
        guard !flushing else { return }
        flushing = true; defer { flushing = false }
        for id in outbox.map(\.id) {
            guard let client, client.isOpen, let item = outbox.first(where: { $0.id == id }) else { return }
            do {
                try await client.call("chat_queue_add", ["chatId": item.chatId, "text": item.text, "images": item.images])
                outbox.removeAll { $0.id == id }
                nudgeChat(item.chatId)
            } catch BurrowError.closed {
                return
            } catch {
                if let i = outbox.firstIndex(where: { $0.id == id }) { outbox[i].error = error.localizedDescription }
            }
            saveOutbox()
        }
    }

    /// Tap on the banner: reconnect now, skipping the backoff.
    func forceReconnect() {
        if let client { client.reconnect() } else { connect() }
    }
}

/// Above the threads list and the chat: nothing while connected; after 1.5 s down
/// "Reconnecting…", after 3 s "Offline". Tap = reconnect now.
struct ConnectionBanner: View {
    @Environment(AppModel.self) private var model
    @State private var shown: Kind?
    @State private var attempt = 0

    enum Kind { case reconnecting, offline }

    var body: some View {
        Group {
            if let shown, model.creds != nil {
                Button {
                    self.shown = .reconnecting; attempt += 1
                    model.forceReconnect()
                } label: {
                    HStack(spacing: 6) {
                        if shown == .reconnecting { ProgressView().controlSize(.mini) } else { Image(systemName: "wifi.slash") }
                        Text(shown == .reconnecting ? "Reconnecting…" : "Offline")
                        if !model.outbox.isEmpty { Text("· \(model.outbox.count) waiting").foregroundStyle(.secondary) }
                        if shown == .offline { Text("· Tap to retry").foregroundStyle(.secondary) }
                    }
                    .font(.caption.weight(.medium))
                    .frame(maxWidth: .infinity).padding(.vertical, 5)
                    .background(.bar)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .transition(.move(edge: .top).combined(with: .opacity))
                .accessibilityHint("Reconnects to the Mac")
            }
        }
        .animation(.default, value: shown)
        .task(id: "\(model.connected)-\(attempt)") {
            guard !model.connected else { shown = nil; return }
            do {
                try await Task.sleep(nanoseconds: 1_500_000_000)
                if shown == nil { shown = .reconnecting }
                try await Task.sleep(nanoseconds: 1_500_000_000)
                shown = .offline
            } catch {}
        }
    }
}

/// Outbox rows for one chat, inside QueuePanel.
struct PendingOutboxRows: View {
    @Environment(AppModel.self) private var model
    let chatId: Int

    var body: some View {
        ForEach(model.outbox.filter { $0.chatId == chatId }) { item in
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.text.isEmpty ? "(attachment)" : item.text).font(.callout).lineLimit(2)
                    Label(item.error ?? "Waiting for connection", systemImage: item.error == nil ? "clock" : "exclamationmark.triangle.fill")
                        .font(.caption2).foregroundStyle(item.error == nil ? Color.secondary : Color.orange)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Button(role: .destructive) { model.removePending(item.id) } label: {
                    Image(systemName: "trash").frame(width: 28, height: 28).contentShape(Rectangle())
                }
                .foregroundStyle(.secondary)
                .accessibilityLabel("Delete pending message")
            }
            .padding(8)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
            .opacity(0.8)
        }
    }
}
