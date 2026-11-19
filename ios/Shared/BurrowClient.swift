import Foundation
import Network
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

/// A socket write that failed (a URLError on a connection that just died) is the
/// same fact as "not connected": callers hold the message and retry on that, and
/// would otherwise surface a raw URLError as a refusal.
func wireError(_ e: Error) -> Error { e is BurrowError ? e : BurrowError.closed }

enum PathAction { case none, probe, wake }

/// What a network path update should do to the socket. Pure; see watchNetwork.
func pathAction(wasSatisfied: Bool, nowSatisfied: Bool, ifacesChanged: Bool, open: Bool) -> PathAction {
    guard nowSatisfied else { return .none }          // offline: the loops already retry
    if !open { return .wake }                         // backing off: the network is back, go now
    return !wasSatisfied || ifacesChanged ? .probe : .none
}

/// Auth / pairing HTTP hops must not hang for URLSession's 60 s default on a
/// blackholed network: connectLoop's backoff only runs after they return.
func httpRequest(_ url: URL, timeout: TimeInterval = 10) -> URLRequest {
    var r = URLRequest(url: url)
    r.timeoutInterval = timeout
    return r
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

    // Touched from the read loop, the connect loop, the heartbeat and the main
    // thread: every one of these goes through `lock`.
    private var _task: URLSessionWebSocketTask?
    private var _lastSeq: Int64 = 0
    private var _stopped = false
    private var _wake = false
    private var _isOpen = false
    private var task: URLSessionWebSocketTask? { get { locked { _task } } set { locked { _task = newValue } } }
    private var lastSeq: Int64 { get { locked { _lastSeq } } set { locked { _lastSeq = newValue } } }
    private var stopped: Bool { get { locked { _stopped } } set { locked { _stopped = newValue } } }
    private var wake: Bool { get { locked { _wake } } set { locked { _wake = newValue } } }
    private(set) var isOpen: Bool { get { locked { _isOpen } } set { locked { _isOpen = newValue } } }
    private var nextID = 0
    private var pending: [Int: CheckedContinuation<Any?, Error>] = [:]
    private let lock = NSLock()
    private var monitor: NWPathMonitor?
    private func locked<T>(_ f: () -> T) -> T { lock.lock(); defer { lock.unlock() }; return f() }
    /// Last frame read. A socket that died in the foreground (Wi-Fi → cellular,
    /// a NAT timeout) reads nothing and errors nothing: only a ping notices.
    private var lastInbound = Date()

    init(_ creds: Credentials) { self.creds = creds }

    func start() { stopped = false; watchNetwork(); Task { await connectLoop() } }

    /// Wi-Fi → cellular, or coming back online: don't wait for the 10 s heartbeat (or
    /// the backoff) to notice that the socket is gone.
    private func watchNetwork() {
        let m = NWPathMonitor()
        var last: (ok: Bool, ifaces: Set<NWInterface.InterfaceType>)?
        m.pathUpdateHandler = { [weak self] path in
            guard let self, !self.stopped else { return }
            let ifaces = Set(path.availableInterfaces.map(\.type))
            let ok = path.status == .satisfied
            defer { last = (ok, ifaces) }
            guard let prev = last else { return } // the first update is just the current state
            switch pathAction(wasSatisfied: prev.ok, nowSatisfied: ok, ifacesChanged: prev.ifaces != ifaces, open: self.isOpen) {
            case .wake: self.wake = true
            case .probe: Task { if await !self.ping() { self.reconnect() } }
            case .none: break
            }
        }
        m.start(queue: DispatchQueue(label: "burrow.netpath"))
        monitor = m
    }

    func stop() {
        monitor?.cancel(); monitor = nil
        stopped = true
        task?.cancel(with: .goingAway, reason: nil)
        failPending()
    }

    private func ticket() async throws -> String {
        guard let url = URL(string: creds.baseURL + "/v2/ws-ticket") else { throw BurrowError.badURL }
        var req = httpRequest(url)
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
                lastInbound = Date()
                if lastSeq > 0 { try await send(["t": "resume", "since": lastSeq]) }
                delay = 1
                let beat = Task { await heartbeat(ws) }
                defer { beat.cancel() }
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

    /// While the socket is quiet, ping it every 10 s; no pong in 3 s = dead,
    /// cancel it so connectLoop reconnects and resumes. Without this a silently
    /// dead socket hung every call and dropped every event until the app was
    /// backgrounded and reopened (foreground()'s ping).
    private func heartbeat(_ ws: URLSessionWebSocketTask) async {
        while !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 10_000_000_000)
            if Task.isCancelled || task !== ws { return }
            if Date().timeIntervalSince(lastInbound) < 10 { continue }
            if await !ping() { ws.cancel(with: .goingAway, reason: nil); return }
        }
    }

    private func readLoop(_ ws: URLSessionWebSocketTask) async throws {
        while true {
            let msg = try await ws.receive()
            lastInbound = Date()
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
            // ponytail: no rev check. Rev 2 only changed pty bytes to base64,
            // which ptyBytes (Terminals.swift) reads either way. Add the check
            // at the first break this client can't absorb.
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

    func noteSeq(_ seq: Int64) { locked { if seq > _lastSeq { _lastSeq = seq } } }

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
                    c?.resume(throwing: wireError(error))
                }
            }
            Task { await watch(id) }
        }
    }

    /// A call with no reply after 6 s: ping. Alive = a slow call (waking a slept
    /// chat takes seconds), keep waiting; dead = fail it as .closed (callers hold
    /// and retry — clientId makes that safe) and reconnect. Gives up at 2 min.
    private func watch(_ id: Int) async {
        for _ in 0..<20 {
            try? await Task.sleep(nanoseconds: 6_000_000_000)
            lock.lock(); let open = pending[id] != nil; lock.unlock()
            if !open { return }
            if await ping() { continue }
            break
        }
        lock.lock(); let c = pending.removeValue(forKey: id); lock.unlock()
        guard let c else { return }
        c.resume(throwing: BurrowError.closed)
        reconnect()
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

    /// Everything but the timestamp: an unchanged one needs no Keychain write / timeline reload.
    func sameContent(_ o: WidgetSnapshot) -> Bool { waiting == o.waiting && working == o.working && top == o.top }

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

