import AppKit
import SwiftUI
import UniformTypeIdentifiers

// MARK: - config.json (read_config + config_set, one key at a time; follows config-changed)

@MainActor
final class ConfigStore: ObservableObject {
    static let shared = ConfigStore()
    @Published var values: JSON = [:]
    private weak var b: Burrow?

    func attach(_ b: Burrow) async {
        if self.b == nil {
            b.subscribe("config-changed") { [weak self] _, p in
                guard let p = p as? JSON, let key = p["key"] as? String else { return }
                self?.values[key] = p["value"]
            }
        }
        self.b = b
        await reload()
    }

    func reload() async {
        guard let s = try? await b?.call("read_config") as? String,
              let o = (try? JSONSerialization.jsonObject(with: Data(s.utf8))) as? JSON else { return }
        values = o
    }

    var ui: JSON { values["uiPrefs"] as? JSON ?? [:] }

    /// One uiPrefs field. Re-reads config first so a stale copy can't revert another client's change
    /// (same reason ConfigSet exists); the key written is still the whole `uiPrefs` object, as setConfig does.
    func setUI(_ field: String, _ value: Any) {
        Task {
            await reload()
            var prefs = ui
            prefs[field] = value
            values["uiPrefs"] = prefs
            do { try await b?.call("config_set", ["key": "uiPrefs", "value": prefs]) }
            catch { b?.status = "Settings: \(error.localizedDescription)" }
        }
    }

    /// A whole top-level config key (config_set merges one key; `config-changed` brings it back to everyone).
    func set(_ key: String, _ value: Any) {
        values[key] = value
        Task {
            do { try await b?.call("config_set", ["key": key, "value": value]) }
            catch { b?.status = "Settings: \(error.localizedDescription)" }
        }
    }

    func bool(_ k: String, _ d: Bool) -> Binding<Bool> {
        Binding(get: { self.ui[k] as? Bool ?? d }, set: { self.setUI(k, $0) })
    }
    func int(_ k: String, _ d: Int) -> Binding<Int> {
        Binding(get: { (self.ui[k] as? NSNumber)?.intValue ?? d }, set: { self.setUI(k, $0) })
    }
    func string(_ k: String, _ d: String) -> Binding<String> {
        Binding(get: { self.ui[k] as? String ?? d }, set: { self.setUI(k, $0) })
    }
}

// MARK: - Settings scene (⌘,)

struct SettingsView: View {
    @EnvironmentObject var b: Burrow
    @StateObject private var cfg = ConfigStore.shared
    @AppStorage("theme") private var theme = "plum"
    @AppStorage("settings.tab") private var tab = "general"

    var body: some View {
        TabView(selection: $tab) {
            GeneralSettings().tabItem { Label("General", systemImage: "gearshape") }.tag("general")
            ProvidersSettings().tabItem { Label("Providers", systemImage: "cpu") }.tag("providers")
            SubagentProfilesSettings().tabItem { Label("Sub-agents", systemImage: "person.2") }.tag("subagents")
            PermissionsSettings().tabItem { Label("Permissions", systemImage: "lock.shield") }.tag("permissions")
            NotificationsSettings().tabItem { Label("Notifications", systemImage: "bell") }.tag("notifications")
            ShortcutsSettings().tabItem { Label("Shortcuts", systemImage: "keyboard") }.tag("shortcuts")
            WorkspacesSettings().tabItem { Label("Workspaces", systemImage: "folder") }.tag("workspaces")
            RemoteSettings().tabItem { Label("Remote", systemImage: "antenna.radiowaves.left.and.right") }.tag("remote")
            AppearanceSettings().tabItem { Label("Appearance", systemImage: "paintpalette") }.tag("appearance")
            AboutSettings().tabItem { Label("About", systemImage: "info.circle") }.tag("about")
        }
        .environmentObject(cfg)
        .frame(width: 720, height: 600)
        .task { await cfg.attach(b) }
        .tint(Theme.accent)
        .preferredColorScheme(Theme.isDark ? .dark : .light)
        .id(theme)
    }
}

struct GeneralSettings: View {
    @EnvironmentObject var b: Burrow
    @EnvironmentObject var cfg: ConfigStore
    @State private var preview: [JSON]?
    @State private var confirmRemove = false

    var body: some View {
        Form {
            Section("Chats") {
                Stepper(value: cfg.int("chatAutoSleepMinutes", 60), in: 0...1440, step: 15) {
                    let m = cfg.int("chatAutoSleepMinutes", 60).wrappedValue
                    Text("Sleep idle chats after: \(m == 0 ? "off" : "\(m) min")")
                }
                Toggle("Deliver sub-agent results to the parent automatically", isOn: cfg.bool("subagentAutoDeliver", true))
                Toggle("Thread recaps", isOn: cfg.bool("chatRecaps", true))
                Toggle("Float popped-out windows (tiling WMs)", isOn: cfg.bool("floatPopouts", false))
                Picker("Spawn sub-agents as", selection: cfg.string("spawnMode", "terminal")) {
                    Text("Terminal tab").tag("terminal")
                    Text("Chat").tag("chat")
                }
            }
            Section("Agent actions (burrow spawn / serve / focus_tab …)") {
                ControlBridgePicker()
            }
            Section("Text generation (commit messages, PRs, branch names, titles)") {
                TextGenModelPicker()
                Picker("Policy", selection: cfg.string("textGenerationPolicy", "default")) {
                    Text("Default").tag("default")
                    Text("Conventional commits").tag("conventional_commits")
                    Text("Repo conventions").tag("repo_conventions")
                }
                TextField("House rules (commit + PR prompts)", text: cfg.string("textGenerationRules", ""), axis: .vertical)
                    .lineLimit(2...5)
            }
            Section("Worktree cleanup (hourly)") {
                Toggle("Remove worktrees whose branch is merged", isOn: cfg.bool("worktreeCleanupMerged", false))
                Toggle("Remove inactive worktrees", isOn: cfg.bool("worktreeCleanupInactive", false))
                Stepper(value: cfg.int("worktreeCleanupInactiveDays", 14), in: 1...365) {
                    Text("Inactive after \(cfg.int("worktreeCleanupInactiveDays", 14).wrappedValue) days")
                }
                HStack {
                    Button("Preview") { Task { preview = try? await b.call("worktree_cleanup_preview") as? [JSON] ?? [] } }
                    if let p = preview, !p.isEmpty {
                        Button("Remove \(p.count)", role: .destructive) { confirmRemove = true }
                    }
                }
                if let p = preview {
                    if p.isEmpty { Text("Nothing to clean up").foregroundStyle(Theme.muted) }
                    ForEach(p.indices, id: \.self) { i in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(p[i].s("branch").isEmpty ? p[i].s("name") : p[i].s("branch")).font(.brand(-1))
                            Text("\(p[i].s("rule")) · \(p[i].s("reason"))").font(.ui(-3)).foregroundStyle(Theme.muted)
                        }
                    }
                }
            }
        }
        .formStyle(.grouped)
        .confirmationDialog("Remove \(preview?.count ?? 0) worktrees?", isPresented: $confirmRemove) {
            Button("Remove", role: .destructive) {
                let ids = (preview ?? []).map { $0.i("id") }
                Task {
                    _ = try? await b.call("worktree_cleanup_now", ["ids": ids])
                    preview = try? await b.call("worktree_cleanup_preview") as? [JSON]
                }
            }
        } message: { Text("Each one is re-checked first; dirty trees and live chats are kept.") }
    }
}

struct ProvidersSettings: View {
    @EnvironmentObject var cfg: ConfigStore

    var body: some View {
        let status = cfg.values["providerStatus"] as? [String: JSON] ?? [:]
        NavigationStack {
            List(Array((cfg.values["providers"] as? [JSON] ?? []).enumerated()), id: \.offset) { _, p in
                let st = status[p.s("id")] ?? [:]
                NavigationLink { ProviderEditor(id: p.s("id")).environmentObject(cfg) } label: {
                    HStack(alignment: .top) {
                        Circle().fill(Color.css(p.s("color").isEmpty ? "#888888" : p.s("color"))).frame(width: 9, height: 9).padding(.top, 5)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(p.s("name")).font(.ui()).foregroundStyle(p["enabled"] as? Bool == false ? Theme.muted : Theme.text)
                            Text([p.s("transport"), st.s("version"), st.s("path")].filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.brand(-3)).foregroundStyle(Theme.muted)
                            if !st.s("error").isEmpty { Text(st.s("error")).font(.ui(-3)).foregroundStyle(Theme.red) }
                        }
                        Spacer()
                        if p["enabled"] as? Bool == false { Text("disabled").font(.brand(-3)).foregroundStyle(Theme.muted) }
                    }
                }
            }
        }
    }
}

struct ShortcutsSettings: View {
    @EnvironmentObject var cfg: ConfigStore
    static let list: [(String, String)] = [
        ("⌘P", "Spotlight"), ("⌘N", "New thread"), ("⌘⇧N", "New project"), ("⌘,", "Settings"), ("⌘⌥B", "Toggle right panel"),
        ("⌘1 … ⌘9", "Jump to thread (sidebar order)"), ("⌘⌥1 … ⌘⌥9", "Jump to project"), ("⌘[ / ⌘]", "Back / forward"),
        ("⌘⌥O", "Open thread in new window"), ("⌘+ / ⌘− / ⌘0", "Text size"),
    ]

    var body: some View {
        Form {
            Section("System-wide (Burrow core)") {
                LabeledContent("Show the tray panel") {
                    HotkeyRecorder(value: Binding(get: { cfg.ui["trayHotkey"] as? String ?? "⌥Space" }, set: { cfg.setUI("trayHotkey", $0) }))
                }
                Text("Applied when the Burrow core next starts (the desktop app re-registers it live).")
                    .font(.ui(-3)).foregroundStyle(Theme.muted)
            }
            Section("This app") {
                ForEach(Self.list, id: \.0) { k, v in
                    HStack { Text(v).font(.ui()); Spacer(); Text(k).font(.brand(-1)).foregroundStyle(Theme.text2) }
                }
            }
        }
        .formStyle(.grouped)
    }
}

struct AppearanceSettings: View {
    @AppStorage("theme") private var theme = "plum"
    @AppStorage("fontSize") private var fontSize = 15.0

    var body: some View {
        Form {
            Section("Text size") {
                Slider(value: $fontSize, in: 11...28, step: 1) { Text("\(Int(fontSize)) pt") }
            }
            Section("Theme") {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 160))], spacing: 10) {
                    ForEach(Theme.all, id: \.key) { p in
                        Button { theme = p.key } label: {
                            VStack(alignment: .leading, spacing: 6) {
                                HStack(spacing: 4) {
                                    ForEach(Array([p.accent, p.green, p.yellow, p.red].enumerated()), id: \.offset) {
                                        RoundedRectangle(cornerRadius: 3).fill($0.element).frame(width: 14, height: 14)
                                    }
                                }
                                Text(p.label).font(.brand(-2)).foregroundStyle(p.text).lineLimit(1)
                            }
                            .padding(10)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(p.base, in: RoundedRectangle(cornerRadius: 10))
                            .overlay(RoundedRectangle(cornerRadius: 10).stroke(p.key == theme ? p.accent : p.border, lineWidth: p.key == theme ? 2 : 1))
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
        .formStyle(.grouped)
    }
}

// MARK: - Workspaces

struct WorkspacesSettings: View {
    @EnvironmentObject var b: Burrow
    @State private var remove: JSON?
    @State private var config: JSON?
    @State private var secrets: [Int64: [JSON]] = [:]
    @State private var error = ""

    /// Repos in the user's order (sort_order, as the desktop sidebar), worktrees folded in.
    var ordered: [JSON] {
        b.workspaces.values.filter { $0["parent_id"] == nil }
            .sorted { ($0.i("sort_order"), $0.i("id")) < ($1.i("sort_order"), $1.i("id")) }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            List {
                ForEach(ordered.indices, id: \.self) { i in
                    let w = ordered[i]
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            ProjectIcon(ws: w, size: 22)
                            VStack(alignment: .leading) {
                                Text(w.s("name")).font(.ui())
                                Text(w.s("path")).font(.brand(-4)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.middle)
                            }
                            Spacer()
                            Button { move(i, -1) } label: { Image(systemName: "chevron.up") }.disabled(i == 0)
                            Button { move(i, 1) } label: { Image(systemName: "chevron.down") }.disabled(i == ordered.count - 1)
                            Menu {
                                Button("Project settings…") { config = w }
                                Button("Set icon…") { pickIcon(w.i("id")) }
                                Button("Clear icon") { Task { await call("set_workspace_icon", ["id": w.i("id"), "icon": ""]) } }
                                Button("Saved secrets") { Task { await loadSecrets(w.i("id")) } }
                                Divider()
                                Button("Remove from Burrow…", role: .destructive) { remove = w }
                            } label: { Image(systemName: "ellipsis") }
                                .menuStyle(.borderlessButton).fixedSize()
                        }
                        if let list = secrets[w.i("id")] {
                            if list.isEmpty { Text("No saved secrets").font(.ui(-3)).foregroundStyle(Theme.muted).padding(.leading, 30) }
                            ForEach(list.indices, id: \.self) { j in
                                HStack {
                                    Text(list[j].s("name")).font(.brand(-2))
                                    Text("updated \(ago(list[j].i("updated")))").font(.ui(-3)).foregroundStyle(Theme.muted)
                                    Spacer()
                                    Button("Delete", role: .destructive) {
                                        Task {
                                            await call("delete_saved_secret", ["repoId": w.i("id"), "name": list[j].s("name")])
                                            await loadSecrets(w.i("id"))
                                        }
                                    }
                                }
                                .padding(.leading, 30)
                            }
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
            HStack {
                if !error.isEmpty { Text(error).font(.ui(-2)).foregroundStyle(Theme.red) }
                Spacer()
                Button("Add project…") { Shell.shared.showSpotlight(.openProject) }
            }
            .padding(10)
        }
        .sheet(isPresented: Binding(get: { config != nil }, set: { if !$0 { config = nil } })) {
            if let w = config { ProjectConfigSheet(ws: w).environmentObject(b).environmentObject(ConfigStore.shared) }
        }
        .confirmationDialog("Remove \(remove?.s("name") ?? "")?", isPresented: Binding(get: { remove != nil }, set: { if !$0 { remove = nil } })) {
            Button("Remove", role: .destructive) {
                guard let id = remove?.i("id") else { return }
                Task { await call("delete_workspace", ["id": id]) }
            }
        } message: { Text("The folder on disk is not touched.") }
    }

    func call(_ cmd: String, _ args: JSON) async {
        do { try await b.call(cmd, args); error = ""; try? await b.refresh() }
        catch { self.error = error.localizedDescription }
    }

    func loadSecrets(_ repoID: Int64) async {
        secrets[repoID] = (try? await b.call("list_saved_secrets", ["repoId": repoID])) as? [JSON] ?? []
    }

    func move(_ i: Int, _ d: Int) {
        var ids = ordered.map { $0.i("id") }
        ids.swapAt(i, i + d)
        Task { await call("set_workspace_order", ["ids": ids]) }
    }

    /// workspaces.icon is a data: URL; downscale to 128 px PNG so it stays small on the wire (shell_snapshot carries it).
    func pickIcon(_ id: Int64) {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.image]
        guard panel.runModal() == .OK, let url = panel.url, let src = NSImage(contentsOf: url) else { return }
        let side: CGFloat = 128
        let img = NSImage(size: NSSize(width: side, height: side), flipped: false) { r in
            src.draw(in: r); return true
        }
        guard let tiff = img.tiffRepresentation, let png = NSBitmapImageRep(data: tiff)?.representation(using: .png, properties: [:]) else { return }
        Task { await call("set_workspace_icon", ["id": id, "icon": "data:image/png;base64," + png.base64EncodedString()]) }
    }
}
