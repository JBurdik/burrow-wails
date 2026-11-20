import Foundation
import Security
import SwiftUI

// MARK: - Endpoint choice (local core vs a paired Burrow)

/// A Burrow this client paired with (src/runtime/remoteEndpoint.ts's catalog). The device token lives in the
/// Keychain, never in UserDefaults.
struct RemoteEnv: Codable, Identifiable, Hashable {
    var id: String          // environment_id from /v2/pair (falls back to baseUrl)
    var name: String
    var baseUrl: String
    var addedAt: Double
}

enum Remote {
    private static let envsKey = "remote.environments"
    static let activeKey = "remote.active"   // "" = local core
    private static let service = "BurrowSwift.remote"

    static var envs: [RemoteEnv] {
        get { UserDefaults.standard.data(forKey: envsKey).flatMap { try? JSONDecoder().decode([RemoteEnv].self, from: $0) } ?? [] }
        set { UserDefaults.standard.set(try? JSONEncoder().encode(newValue), forKey: envsKey) }
    }
    static var active: RemoteEnv? {
        let id = UserDefaults.standard.string(forKey: activeKey) ?? ""
        return envs.first { $0.id == id }
    }

    static func normalize(_ url: String) -> String {
        var s = url.trimmingCharacters(in: .whitespacesAndNewlines)
        while s.hasSuffix("/") { s.removeLast() }
        if !s.contains("://") { s = "http://" + s }
        return s
    }

    /// The `/v2/ws?ticket=…` URL for the next connection attempt. Tickets are single-use, so this runs on every attempt.
    static func ticketURL() async throws -> URL {
        if let env = active {
            guard let token = keychainGet(env.id) else { throw Burrow.CallError(message: "No device token for \(env.name) — pair again") }
            var req = URLRequest(url: URL(string: env.baseUrl + "/v2/ws-ticket")!)
            req.httpMethod = "POST"
            req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
            let (data, resp) = try await URLSession.shared.data(for: req)
            let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
            // A 401 means this machine revoked us: forget only it and fall back to the local core (remoteEndpoint.ts).
            if code == 401 { forget(env.id); throw Burrow.CallError(message: "\(env.name) revoked this device") }
            guard code == 200, let ticket = ((try? JSONSerialization.jsonObject(with: data)) as? JSON)?.s("ticket"), !ticket.isEmpty
            else { throw Burrow.CallError(message: "Could not get a ticket from \(env.name) (\(code))") }
            var comps = URLComponents(string: env.baseUrl.replacingOccurrences(of: "http", with: "ws", options: .anchored) + "/v2/ws")!
            comps.queryItems = [URLQueryItem(name: "ticket", value: ticket)]
            return comps.url!
        }
        let home = ProcessInfo.processInfo.environment["BURROW_HOME_DIR"]
            ?? NSHomeDirectory() + "/Library/Application Support/burrow-wails"
        let port = try String(contentsOfFile: home + "/hook.port", encoding: .utf8)
            .split(whereSeparator: \.isNewline).first.map(String.init) ?? ""
        let token = try String(contentsOfFile: home + "/control.token", encoding: .utf8)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        var req = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/v2/local-ticket")!)
        req.httpMethod = "POST"
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (data, _) = try await URLSession.shared.data(for: req)
        guard let info = try JSONSerialization.jsonObject(with: data) as? JSON,
              var comps = URLComponents(string: info.s("ws_url")) else { throw Burrow.CallError(message: "bad ticket reply") }
        comps.queryItems = [URLQueryItem(name: "ticket", value: info.s("ticket"))]
        return comps.url!
    }

    /// POST /v2/pair {code, name, kind} → device token (Keychain) + a catalog row. Re-pairing a machine refreshes its row.
    static func pair(baseUrl raw: String, code: String) async throws -> RemoteEnv {
        let base = normalize(raw)
        guard let url = URL(string: base + "/v2/pair") else { throw Burrow.CallError(message: "Bad URL") }
        var req = URLRequest(url: url, timeoutInterval: 10)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let name = Host.current().localizedName ?? "Mac"
        req.httpBody = try JSONSerialization.data(withJSONObject: ["code": code, "name": name + " (Swift)", "kind": "desktop"])
        let data: Data, resp: URLResponse
        do { (data, resp) = try await URLSession.shared.data(for: req) }
        catch { throw Burrow.CallError(message: "Cannot reach \(base): \(error.localizedDescription)") }
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if status == 401 { throw Burrow.CallError(message: "Wrong or expired code. Check the code in that Burrow's Settings, or generate a new one.") }
        guard status == 200, let body = (try? JSONSerialization.jsonObject(with: data)) as? JSON, !body.s("device_token").isEmpty
        else { throw Burrow.CallError(message: "Pairing failed (\(status))") }
        let id = body.s("environment_id").isEmpty ? base : body.s("environment_id")
        let env = RemoteEnv(id: id, name: URL(string: base)?.host ?? base, baseUrl: base, addedAt: Date().timeIntervalSince1970)
        guard keychainSet(id, body.s("device_token")) else { throw Burrow.CallError(message: "Could not store the device token in the Keychain") }
        envs = envs.filter { $0.id != id } + [env]
        return env
    }

    static func forget(_ id: String) {
        keychainDelete(id)
        envs = envs.filter { $0.id != id }
        if UserDefaults.standard.string(forKey: activeKey) == id { UserDefaults.standard.set("", forKey: activeKey) }
    }

    // MARK: Keychain (generic password, one per environment)

    private static func query(_ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: account]
    }
    static func keychainGet(_ account: String) -> String? {
        var q = query(account)
        q[kSecReturnData as String] = true
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let d = out as? Data else { return nil }
        return String(data: d, encoding: .utf8)
    }
    @discardableResult static func keychainSet(_ account: String, _ value: String) -> Bool {
        keychainDelete(account)
        var q = query(account)
        q[kSecValueData as String] = Data(value.utf8)
        return SecItemAdd(q as CFDictionary, nil) == errSecSuccess
    }
    static func keychainDelete(_ account: String) { SecItemDelete(query(account) as CFDictionary) }
}

extension Burrow {
    /// Switch between the local core ("") and a paired environment; stores, listeners and selection are per backend.
    func switchEnvironment(_ id: String) {
        UserDefaults.standard.set(id, forKey: Remote.activeKey)
        selectedChat = nil
        selectedTab = nil
        workspaces = [:]; chats = []; allChats = []; phases = [:]; seen = [:]; snapshot = [:]
        reconnect()
    }
}

// MARK: - Settings → Remote access

struct RemoteSettings: View {
    @EnvironmentObject var b: Burrow
    @AppStorage(Remote.activeKey) private var activeID = ""
    @State private var pair: JSON = [:]
    @State private var endpoint = ""
    @State private var devices: [JSON] = []
    @State private var error = ""
    @State private var revoke: JSON?
    @State private var envs = Remote.envs
    @State private var newURL = ""
    @State private var newCode = ""
    @State private var pairing = false
    @State private var pairError = ""

    var body: some View {
        Form {
            Section("This Burrow") {
                if !error.isEmpty { Text(error).foregroundStyle(Theme.red).font(.ui(-2)) }
                LabeledContent("Pairing code") {
                    HStack {
                        Text(pair.s("code").isEmpty ? (pair["locked"] as? Bool == true ? "locked" : "—") : pair.s("code"))
                            .font(.brand(4, weight: .bold)).textSelection(.enabled)
                        Button("New code") { Task { _ = try? await b.call("remote_regenerate_pair_code"); await load() } }
                    }
                }
                if pair.i("expires_at") > 0 {
                    LabeledContent("Expires", value: Date(timeIntervalSince1970: Double(pair.i("expires_at")) / 1000).formatted(date: .omitted, time: .standard))
                }
                if !endpoint.isEmpty { LabeledContent("Address", value: endpoint).textSelection(.enabled) }
            }
            Section("Paired devices") {
                if devices.isEmpty { Text("No paired devices").foregroundStyle(Theme.muted) }
                ForEach(devices.indices, id: \.self) { i in
                    let d = devices[i]
                    HStack {
                        VStack(alignment: .leading) {
                            Text(d.s("name")).font(.ui())
                            Text("\(d.s("kind")) · last seen \(ago(d.i("last_seen")))\(d["push"] as? Bool == true ? " · push" : "")")
                                .font(.ui(-3)).foregroundStyle(Theme.muted)
                        }
                        Spacer()
                        Button("Revoke", role: .destructive) { revoke = d }
                    }
                }
            }
            Section("Connect this client to another Burrow") {
                Picker("Connected to", selection: Binding(get: { activeID }, set: { b.switchEnvironment($0) })) {
                    Text("This Mac (local core)").tag("")
                    ForEach(envs) { Text("\($0.name) — \($0.baseUrl)").tag($0.id) }
                }
                ForEach(envs) { e in
                    HStack {
                        Text(e.name).font(.ui(-1))
                        Spacer()
                        Button("Forget") {
                            let wasActive = e.id == activeID
                            Remote.forget(e.id); envs = Remote.envs
                            if wasActive { b.switchEnvironment("") }
                        }
                    }
                }
                TextField("Address (e.g. https://mac.tailnet.ts.net)", text: $newURL)
                TextField("Six-digit pairing code", text: $newCode)
                HStack {
                    if !pairError.isEmpty { Text(pairError).font(.ui(-2)).foregroundStyle(Theme.red) }
                    Spacer()
                    Button(pairing ? "Pairing…" : "Pair") {
                        pairing = true; pairError = ""
                        Task {
                            do {
                                let env = try await Remote.pair(baseUrl: newURL, code: newCode.filter(\.isNumber))
                                envs = Remote.envs; newCode = ""; newURL = ""
                                b.switchEnvironment(env.id)
                            } catch { pairError = error.localizedDescription }
                            pairing = false
                        }
                    }
                    .disabled(pairing || newURL.isEmpty || newCode.filter(\.isNumber).count != 6)
                }
            }
        }
        .formStyle(.grouped)
        .task { await load() }
        .confirmationDialog("Revoke \(revoke?.s("name") ?? "")?", isPresented: Binding(get: { revoke != nil }, set: { if !$0 { revoke = nil } })) {
            Button("Revoke", role: .destructive) {
                guard let id = revoke?.s("id") else { return }
                Task { _ = try? await b.call("revoke_remote_device", ["id": id]); await load() }
            }
        } message: { Text("It is disconnected immediately and has to pair again.") }
    }

    func load() async {
        do {
            pair = try await b.call("remote_pair_status") as? JSON ?? [:]
            devices = try await b.call("remote_devices") as? [JSON] ?? []
            endpoint = ((try? await b.call("remote_pairing_endpoint")) as? JSON)?.s("http_base") ?? ""
            error = ""
        } catch {
            // A paired device never holds access:* scopes — pairing is managed on that machine itself.
            self.error = error.localizedDescription
        }
    }
}

func ago(_ ms: Int64) -> String {
    ms == 0 ? "never" : RelativeDateTimeFormatter().localizedString(for: Date(timeIntervalSince1970: Double(ms) / 1000), relativeTo: .now)
}
