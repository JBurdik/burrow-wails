import Foundation
import Security

/// Shared by the app and both extensions through one Keychain access group.
/// Not an App Group: the build signs with the team's wildcard profile (no
/// Xcode account needed), and a wildcard profile can't carry App Groups but
/// does allow `<team>.*` keychain groups.
enum SharedStore {
    static let group = "9QY36KZ8JP.com.agenticide.burrow.shared"

    private static func query(_ key: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrAccount as String: key,
         kSecAttrAccessGroup as String: group]
    }

    static func data(_ key: String) -> Data? {
        var q = query(key)
        q[kSecReturnData as String] = true
        var out: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess else { return nil }
        return out as? Data
    }

    static func set(_ key: String, _ data: Data?) {
        SecItemDelete(query(key) as CFDictionary)
        guard let data else { return }
        var q = query(key)
        q[kSecValueData as String] = data
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(q as CFDictionary, nil)
    }

    static func get<T: Decodable>(_ key: String, _: T.Type) -> T? {
        data(key).flatMap { try? JSONDecoder().decode(T.self, from: $0) }
    }
    static func put<T: Encodable>(_ key: String, _ value: T) { set(key, try? JSONEncoder().encode(value)) }
}

struct Credentials: Codable {
    var baseURL: String
    var token: String
}

enum CredentialStore {
    static func load() -> Credentials? { SharedStore.get("device", Credentials.self) }
    static func save(_ c: Credentials) { SharedStore.put("device", c) }
    static func clear() { SharedStore.set("device", nil) }
}

enum BurrowError: LocalizedError {
    case revoked, badURL, server(String), closed
    var errorDescription: String? {
        switch self {
        case .revoked: "This device was unpaired."
        case .badURL: "Invalid address."
        case .server(let m): m
        case .closed: "Not connected."
        }
    }
}

/// Normalises what the user typed or pasted: a bare host, a URL, or the
/// pairing link from Settings (`https://mac.ts.net:8443/#pair=123456`).
func parsePairInput(_ raw: String) -> (base: String, code: String?)? {
    var s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
    if !s.contains("://") { s = "https://" + s }
    guard var comps = URLComponents(string: s), comps.host != nil else { return nil }
    var code: String?
    if let f = comps.fragment, f.hasPrefix("pair=") { code = String(f.dropFirst(5)) }
    // Keep the path: `tailscale serve` may mount the core under one (/burrow).
    comps.fragment = nil; comps.query = nil
    while comps.path.hasSuffix("/") { comps.path.removeLast() }
    guard let base = comps.string else { return nil }
    return (base, code)
}

final class BurrowClient: NSObject, @unchecked Sendable {
    static func pair(base: String, code: String, name: String) async throws -> Credentials {
        guard let url = URL(string: base + "/v2/pair") else { throw BurrowError.badURL }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: ["code": code, "name": name, "kind": "ios"])
        let (data, res) = try await URLSession.shared.data(for: req)
        guard (res as? HTTPURLResponse)?.statusCode == 200,
              let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let token = obj["device_token"] as? String
        else { throw BurrowError.server("Wrong or expired code.") }
        return Credentials(baseURL: base, token: token)
    }

    let creds: Credentials
    /// (name, payload) for every event; "resync" / "open" are synthesised.
    var onEvent: ((String, Any?) -> Void)?
    /// Called once when the device token is rejected.
    var onRevoked: (() -> Void)?

    private var task: URLSessionWebSocketTask?
    private var nextID = 0
    private var pending: [Int: CheckedContinuation<Any?, Error>] = [:]
    private var lastSeq: Int64 = 0
    private var stopped = false
    private var wake = false
    private(set) var isOpen = false
    private let lock = NSLock()

    init(_ creds: Credentials) { self.creds = creds }

    func start() { stopped = false; Task { await connectLoop() } }

    func stop() {
        stopped = true
        task?.cancel(with: .goingAway, reason: nil)
        failPending()
    }

    private func ticket() async throws -> String {
        guard let url = URL(string: creds.baseURL + "/v2/ws-ticket") else { throw BurrowError.badURL }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("Bearer " + creds.token, forHTTPHeaderField: "Authorization")
        let (data, res) = try await URLSession.shared.data(for: req)
        let code = (res as? HTTPURLResponse)?.statusCode ?? 0
        if code == 401 { throw BurrowError.revoked }
        guard code == 200, let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let t = obj["ticket"] as? String else { throw BurrowError.server("ticket: HTTP \(code)") }
        return t
    }

    private func connectLoop() async {
        var delay: UInt64 = 1
        while !stopped {
            do {
                let t = try await ticket()
                var comps = URLComponents(string: creds.baseURL)!
                comps.scheme = comps.scheme == "http" ? "ws" : "wss"
                comps.path += "/v2/ws"
                comps.queryItems = [URLQueryItem(name: "ticket", value: t)]
                // stop() while the ticket was in flight (AppModel.connect() replacing
                // this client on foreground): don't open a ghost socket next to the new one.
                if stopped { return }
                let ws = URLSession.shared.webSocketTask(with: comps.url!)
                ws.maximumMessageSize = 64 << 20
                task = ws
                if stopped { ws.cancel(with: .goingAway, reason: nil); return }
                ws.resume()
                if lastSeq > 0 { try await send(["t": "resume", "since": lastSeq]) }
                delay = 1
                try await readLoop(ws)
            } catch BurrowError.revoked {
                stopped = true
                DispatchQueue.main.async { self.onRevoked?() }
                return
            } catch {}
            if isOpen { emit("close", nil) }
            isOpen = false
            failPending()
            if stopped { return }
            // Backoff, cut short by reconnect().
            for _ in 0..<(delay * 10) where !wake && !stopped { try? await Task.sleep(nanoseconds: 100_000_000) }
            if wake { wake = false; delay = 1 } else { delay = min(delay * 2, 15) }
        }
    }

    private func readLoop(_ ws: URLSessionWebSocketTask) async throws {
        while true {
            let msg = try await ws.receive()
            let data: Data
            switch msg {
            case .string(let s): data = Data(s.utf8)
            case .data(let d): data = d
            @unknown default: continue
            }
            guard let f = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { continue }
            handle(f)
        }
    }

    private func handle(_ f: [String: Any]) {
        switch f["t"] as? String {
        case "reply":
            let id = f["id"] as? Int ?? 0
            lock.lock(); let c = pending.removeValue(forKey: id); lock.unlock()
            if let e = f["error"] as? [String: Any] {
                c?.resume(throwing: BurrowError.server(e["message"] as? String ?? "error"))
            } else {
                c?.resume(returning: f["result"])
            }
        case "welcome":
            // ponytail: no rev check; this client speaks rev 1 and the core
            // still serves minClientRev 1. Add the check at the first bump.
            isOpen = true
            if lastSeq == 0 { lastSeq = (f["seq"] as? NSNumber)?.int64Value ?? 0 }
            emit("open", nil)
        case "event":
            emit(f["name"] as? String ?? "", f["payload"])
        case "shell":
            for ev in f["events"] as? [[String: Any]] ?? [] {
                let seq = (ev["seq"] as? NSNumber)?.int64Value ?? 0
                if seq <= lastSeq { continue }
                lastSeq = seq
                emit(ev["name"] as? String ?? "", ev["payload"])
            }
        case "resync":
            lastSeq = 0
            emit("resync", nil)
        default: break
        }
    }

    private func emit(_ name: String, _ payload: Any?) {
        // A stopped client's late events (its "close" above all) must not flip the
        // model's state under the client that replaced it.
        DispatchQueue.main.async { if !self.stopped { self.onEvent?(name, payload) } }
    }

    /// A dead socket after the app was suspended often still reads `.running`;
    /// a ping with a deadline is the only honest check.
    func ping(timeout: TimeInterval = 3) async -> Bool {
        guard let task, isOpen else { return false }
        return await withCheckedContinuation { c in
            let once = OnceFlag()
            task.sendPing { err in if once.take() { c.resume(returning: err == nil) } }
            DispatchQueue.global().asyncAfter(deadline: .now() + timeout) { if once.take() { c.resume(returning: false) } }
        }
    }

    /// Drops the current socket so connectLoop reconnects (and resumes by seq).
    /// Also skips a backoff wait in progress (the banner's tap).
    func reconnect() { wake = true; task?.cancel(with: .goingAway, reason: nil) }

    func noteSeq(_ seq: Int64) { if seq > lastSeq { lastSeq = seq } }

    private func send(_ obj: [String: Any]) async throws {
        guard let task else { throw BurrowError.closed }
        let data = try JSONSerialization.data(withJSONObject: obj)
        try await task.send(.string(String(decoding: data, as: UTF8.self)))
    }

    /// One wire call. Args are by name, as in remoteapi.go's table.
    @discardableResult
    func call(_ cmd: String, _ args: [String: Any] = [:]) async throws -> Any? {
        lock.lock(); nextID += 1; let id = nextID; lock.unlock()
        return try await withCheckedThrowingContinuation { c in
            lock.lock(); pending[id] = c; lock.unlock()
            Task {
                do { try await send(["t": "call", "id": id, "cmd": cmd, "args": args]) } catch {
                    lock.lock(); let c = pending.removeValue(forKey: id); lock.unlock()
                    c?.resume(throwing: error)
                }
            }
        }
    }

    /// Waits for the socket to be up, for one-shot users (share extension).
    func ready(timeout: TimeInterval = 10) async throws {
        let end = Date().addingTimeInterval(timeout)
        while !isOpen {
            if Date() > end { throw BurrowError.closed }
            try await Task.sleep(nanoseconds: 100_000_000)
        }
    }

    private func failPending() {
        lock.lock(); let all = pending; pending = [:]; lock.unlock()
        all.values.forEach { $0.resume(throwing: BurrowError.closed) }
    }
}

/// What the widget reads: written by the app whenever phases change.
struct WidgetSnapshot: Codable {
    var waiting: Int
    var working: Int
    var top: [String]
    var at: Date

    static let key = "widget.snapshot"
    static func load() -> WidgetSnapshot? { SharedStore.get(key, WidgetSnapshot.self) }
    func save() { SharedStore.put(Self.key, self) }
}

/// Projects the share extension can target, cached by the app.
struct CachedWorkspace: Codable, Identifiable, Hashable {
    var id: Int
    var name: String
    static let key = "workspaces"
    static func load() -> [CachedWorkspace] { SharedStore.get(key, [CachedWorkspace].self) ?? [] }
    static func save(_ ws: [CachedWorkspace]) { SharedStore.put(key, ws) }
}

final class OnceFlag: @unchecked Sendable {
    private var done = false
    private let lock = NSLock()
    func take() -> Bool { lock.lock(); defer { lock.unlock() }; if done { return false }; done = true; return true }
}

// MARK: thread titles (src/lib/chatTitle.ts + TrayPanel.vue nameThread)

/// smartTitle: the prompt's first six words, filler prefix dropped.
func smartTitle(_ text: String) -> String {
    let filler = #"^(can you |please |i want (you )?to |how (do i|to) |what (is|are) (the |a )?|could you |would you |help me |i need (you )?to )"#
    var clean = text.replacingOccurrences(of: filler, with: "", options: [.regularExpression, .caseInsensitive])
    clean = clean.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    let words = clean.split(separator: " ")
    let slug = words.prefix(6).joined(separator: " ")
    let title = slug.prefix(1).uppercased() + slug.dropFirst()
    return title.count < clean.count ? title + "…" : title
}

extension BurrowClient {
    /// uiPrefs from the Mac's config.json (a JSON object or, older, a string).
    func uiPrefs() async -> [String: Any] {
        guard let raw = try? await call("read_config") as? String,
              let cfg = try? JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [String: Any] else { return [:] }
        if let p = cfg["uiPrefs"] as? [String: Any] { return p }
        if let s = cfg["uiPrefs"] as? String, let p = try? JSONSerialization.jsonObject(with: Data(s.utf8)) as? [String: Any] { return p }
        return [:]
    }

    /// remote_create_chat names the row "Chat N (phone)": give it the prompt
    /// heuristic now and, unless `heuristicOnly`, the model's title when it
    /// answers. Both writes are compare-and-set, so a rename made meanwhile wins.
    func nameThread(chatId: Int, current: String, cwd: String, text: String, heuristicOnly: Bool = false) async {
        let heuristic = smartTitle(text)
        guard !heuristic.isEmpty else { return }
        _ = try? await call("remote_set_chat_title", ["id": chatId, "title": heuristic, "expectTitle": current])
        guard !heuristicOnly else { return }
        let prefs = await uiPrefs()
        guard let title = try? await call("generate_chat_title", [
            "cwd": cwd, "model": prefs["textGenerationModel"] as? String ?? "",
            "policy": prefs["textGenerationPolicy"] as? String ?? "", "text": text,
        ]) as? String, !title.isEmpty else { return }
        _ = try? await call("remote_set_chat_title", ["id": chatId, "title": title, "expectTitle": heuristic])
    }
}
