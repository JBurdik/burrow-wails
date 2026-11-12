import AppKit
import SwiftUI

// Settings tabs ported from src/components/Settings.vue, SubagentProfilesPanel.vue, WorkspaceConfig.vue,
// ProvidersPanel.vue / OpenAIProviderSettings.vue. Every write is one config key (ConfigStore.set / setUI).

// MARK: - General: control bridge + text-gen model

struct ControlBridgePicker: View {
    @AppStorage(ShellControl.modeKey) private var mode = "auto"
    var body: some View {
        Picker("This app performs them", selection: $mode) {
            Text("When no Burrow desktop window is open").tag("auto")
            Text("Always").tag("always")
            Text("Never").tag("off")
        }
        Text(ShellControl.desktopWindowRunning
             ? "A Burrow desktop window is running — it answers agent actions now."
             : "No Burrow desktop window is running — this app answers agent actions.")
            .font(.ui(-3)).foregroundStyle(Theme.muted)
    }
}

/// `uiPrefs.textGenerationModel` = "kind::provider::model::effort" (textgen.go; effort optional).
struct TextGenModelPicker: View {
    @EnvironmentObject var cfg: ConfigStore

    var parts: [String] {
        let p = cfg.ui.s("textGenerationModel").components(separatedBy: "::")
        return (0..<4).map { p.indices.contains($0) ? p[$0] : "" }
    }
    func write(provider: String, model: String, effort: String) {
        let kind = (cfg.values["providers"] as? [JSON] ?? []).first { $0.s("id") == provider }?.s("kind") ?? provider
        let v = provider.isEmpty ? "" : [kind, provider, model] .joined(separator: "::") + (effort.isEmpty ? "" : "::" + effort)
        cfg.setUI("textGenerationModel", v)
    }

    var body: some View {
        let p = parts, provider = p[1], model = p[2], effort = p[3]
        let agents = (cfg.values["providers"] as? [JSON] ?? [])
            .filter { $0["enabled"] as? Bool != false && ["claude", "codex", "gemini", "opencode"].contains($0.s("kind")) || $0.s("kind") == "custom" && $0.s("providerId") == "opencode" }
        Picker("Agent", selection: Binding(get: { provider }, set: { write(provider: $0, model: "", effort: "") })) {
            Text("Default (Claude Haiku)").tag("")
            ForEach(agents.indices, id: \.self) { Text(agents[$0].s("name")).tag(agents[$0].s("id")) }
        }
        if !provider.isEmpty {
            let models = ComposerModels.list(provider, current: model, cfg.values)
            Picker("Model", selection: Binding(get: { model }, set: { write(provider: provider, model: $0, effort: effort) })) {
                ForEach(models, id: \.0) { Text($0.1).tag($0.0) }
            }
            Picker("Effort", selection: Binding(get: { effort }, set: { write(provider: provider, model: model, effort: $0) })) {
                ForEach(Launch.efforts, id: \.0) { Text($0.0.isEmpty && p[0] == "codex" ? "Low (default)" : $0.1).tag($0.0) }
            }
        }
    }
}

/// The model list Composer shows (Claude catalog, else `chatModelsSeen[agent]`), without a ComposerStore.
enum ComposerModels {
    static func list(_ agent: String, current: String, _ config: JSON) -> [(String, String)] {
        let seen = ((config["chatModelsSeen"] as? JSON)?[agent] as? [JSON] ?? []).map { ($0.s("id"), $0.s("label").isEmpty ? $0.s("id") : $0.s("label")) }
        var out = [("", "Provider default")] + (agent == "claude" ? Launch.claudeModels.map { ($0.id, $0.label) } : seen)
        if !current.isEmpty, !out.contains(where: { $0.0 == current }) { out.append((current, current)) }
        return out
    }
}

// MARK: - Sub-agent profiles

struct SubagentProfilesSettings: View {
    @EnvironmentObject var cfg: ConfigStore

    var profiles: [SubagentProfile] { SubagentProfiles.normalize(cfg.values["subagentProfiles"]) }
    func update(_ i: Int, _ f: (inout SubagentProfile) -> Void) {
        var all = profiles
        f(&all[i])
        cfg.set("subagentProfiles", all.map(\.json))
    }

    var body: some View {
        let all = profiles
        let agents = (cfg.values["providers"] as? [JSON] ?? []).filter {
            $0["enabled"] as? Bool != false && (["claude-cli", "codex-app-server"].contains($0.s("transport")) || $0.s("providerId") == "openai-compatible")
        }
        Form {
            Text("Roles for `burrow spawn --profile`. The role text is put in front of the task.")
                .font(.ui(-2)).foregroundStyle(Theme.muted)
            ForEach(all.indices, id: \.self) { i in
                Section(all[i].name) {
                    Picker("Agent", selection: Binding(get: { all[i].agentId }, set: { v in update(i) { $0.agentId = v } })) {
                        Text("Parent's / default agent").tag("")
                        ForEach(agents.indices, id: \.self) { Text(agents[$0].s("name")).tag(agents[$0].s("id")) }
                    }
                    TextField("Model (empty = agent default)", text: Binding(get: { all[i].model }, set: { v in update(i) { $0.model = v } }))
                    Picker("Permissions", selection: Binding(get: { all[i].permissionMode }, set: { v in update(i) { $0.permissionMode = v } })) {
                        ForEach(SubagentProfiles.modes, id: \.0) { Text($0.1).tag($0.0) }
                    }
                    TextField("Instructions", text: Binding(get: { all[i].instructions }, set: { v in update(i) { $0.instructions = v } }), axis: .vertical)
                        .lineLimit(3...8)
                    if all[i] != SubagentProfiles.defaults[i] {
                        Button("Reset to default") { update(i) { $0 = SubagentProfiles.defaults[i] } }
                    }
                }
            }
        }
        .formStyle(.grouped)
    }
}

// MARK: - Permissions (chat "Always" rules + macOS grants)

struct PermissionsSettings: View {
    @EnvironmentObject var b: Burrow
    @EnvironmentObject var cfg: ConfigStore
    @State private var privacy: JSON = [:]

    var body: some View {
        Form {
            Section("Always-allowed in chats") {
                AttentionRulesView()
            }
            Section("macOS") {
                ForEach([("notifications", "Notifications"), ("chats_fda", "Full Disk Access — chats (core)"),
                         ("terminals_fda", "Full Disk Access — terminals (burrow-daemon)"),
                         ("screen_recording", "Screen recording"), ("accessibility", "Accessibility")], id: \.0) { k, label in
                    HStack {
                        Text(label)
                        Spacer()
                        let v = privacy.s(k)
                        Text(v.isEmpty ? "—" : v).font(.brand(-2)).foregroundStyle(v == "granted" ? Theme.green : Theme.muted)
                    }
                }
                Button("Open Privacy & Security") {
                    NSWorkspace.shared.open(URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles")!)
                }
            }
        }
        .formStyle(.grouped)
        .task { privacy = (try? await b.call("privacy_status")) as? JSON ?? [:] }
    }
}

// MARK: - Notifications (sounds, toasts, ntfy)

struct NotificationsSettings: View {
    @EnvironmentObject var cfg: ConfigStore
    @State private var test = ""

    static let doneSounds = [("soft-1", "Soft 1"), ("soft-2", "Soft 2"), ("soft-3", "Soft 3"), ("whisper-1", "Whisper 1"),
                             ("whisper-2", "Whisper 2"), ("voice-1", "Voice 1")]
    static let waitingSounds = doneSounds + [("need-you-1", "Need You 1"), ("need-you-2", "Need You 2"), ("need-you-voice", "Need You (Voice)")]
    static let toastPositions = [("sidebar", "Sidebar (bottom)"), ("top-left", "Top left"), ("top-center", "Top center"),
                                 ("top-right", "Top right"), ("bottom-left", "Bottom left"), ("bottom-center", "Bottom center"),
                                 ("bottom-right", "Bottom right")]
    static let ntfyEvents = [("done", "Task complete"), ("waiting", "Waiting for input"), ("permission", "Permission needed"), ("error", "Turn failed (error)")]

    var body: some View {
        let events = cfg.ui["ntfyEvents"] as? [String] ?? ["done", "permission", "error"]
        Form {
            Section("Sounds") {
                Toggle("Play sounds", isOn: cfg.bool("soundEnabled", true))
                Toggle("When a turn finishes", isOn: cfg.bool("soundDoneEnabled", true))
                Picker("Done sound", selection: cfg.string("soundDoneId", "soft-1")) {
                    ForEach(Self.doneSounds, id: \.0) { Text($0.1).tag($0.0) }
                }
                Toggle("When an agent needs you", isOn: cfg.bool("soundWaitingEnabled", true))
                Picker("Waiting sound", selection: cfg.string("soundWaitingId", "need-you-1")) {
                    ForEach(Self.waitingSounds, id: \.0) { Text($0.1).tag($0.0) }
                }
                Stepper(value: cfg.int("soundVolume", 70), in: 0...100, step: 10) {
                    Text("Volume \(cfg.int("soundVolume", 70).wrappedValue) %")
                }
            }
            Section("Toasts") {
                Picker("Position (desktop app)", selection: cfg.string("toastPosition", "sidebar")) {
                    ForEach(Self.toastPositions, id: \.0) { Text($0.1).tag($0.0) }
                }
            }
            Section("ntfy.sh push") {
                Toggle("Enable ntfy", isOn: cfg.bool("ntfyEnabled", false))
                if cfg.ui["ntfyEnabled"] as? Bool == true {
                    TextField("Server", text: cfg.string("ntfyServer", "https://ntfy.sh"))
                    TextField("Topic", text: cfg.string("ntfyTopic", ""))
                    SecureField("Access token (optional)", text: cfg.string("ntfyToken", ""))
                    ForEach(Self.ntfyEvents, id: \.0) { id, label in
                        Toggle(label, isOn: Binding(get: { events.contains(id) }, set: { on in
                            cfg.setUI("ntfyEvents", on ? events + [id] : events.filter { $0 != id })
                        }))
                    }
                    Toggle("Only when away", isOn: cfg.bool("ntfyOnlyWhenAway", true))
                    HStack {
                        Button("Send test") { Task { await sendTest() } }.disabled(cfg.ui.s("ntfyTopic").isEmpty)
                        Text(test).font(.ui(-3)).foregroundStyle(Theme.muted)
                    }
                }
            }
        }
        .formStyle(.grouped)
    }

    /// lib/ntfy.ts testNtfy: POST the message to <server>/<topic>.
    func sendTest() async {
        var server = cfg.ui.s("ntfyServer").isEmpty ? "https://ntfy.sh" : cfg.ui.s("ntfyServer")
        while server.hasSuffix("/") { server.removeLast() }
        guard let url = URL(string: server + "/" + cfg.ui.s("ntfyTopic")) else { test = "Bad server URL"; return }
        var req = URLRequest(url: url, timeoutInterval: 10)
        req.httpMethod = "POST"
        req.setValue("Burrow test", forHTTPHeaderField: "Title")
        if !cfg.ui.s("ntfyToken").isEmpty { req.setValue("Bearer " + cfg.ui.s("ntfyToken"), forHTTPHeaderField: "Authorization") }
        req.httpBody = Data("Notifications from Burrow work.".utf8)
        do {
            let (_, r) = try await URLSession.shared.data(for: req)
            let code = (r as? HTTPURLResponse)?.statusCode ?? 0
            test = (200..<300).contains(code) ? "Sent" : "Failed (\(code))"
        } catch { test = error.localizedDescription }
    }
}

// MARK: - Shortcuts (+ the core's system-wide hotkey)

struct HotkeyRecorder: View {
    @Binding var value: String
    @State private var recording = false
    @State private var monitor: Any?

    var body: some View {
        HStack {
            Button(recording ? "Press keys…" : (value.isEmpty ? "Off" : value)) { recording ? stop() : start() }
                .font(.brand(-1))
            if !value.isEmpty { Button("Clear") { value = "" } }
        }
        .onDisappear { stop() }
    }

    func start() {
        recording = true
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { e in
            if e.keyCode == 53 { stop(); return nil } // Esc cancels
            if let s = Self.spec(e) { value = s; stop() }
            return nil
        }
    }
    func stop() {
        recording = false
        if let m = monitor { NSEvent.removeMonitor(m) }
        monitor = nil
    }

    /// Glyphs ⌘⌥⌃⇧ then the key, as trayhotkey.go parseHotkey reads it; needs ⌘, ⌥ or ⌃.
    static func spec(_ e: NSEvent) -> String? {
        let f = e.modifierFlags
        guard f.contains(.command) || f.contains(.option) || f.contains(.control) else { return nil }
        var s = ""
        if f.contains(.command) { s += "⌘" }
        if f.contains(.option) { s += "⌥" }
        if f.contains(.control) { s += "⌃" }
        if f.contains(.shift) { s += "⇧" }
        let key: String
        switch e.keyCode {
        case 49: key = "Space"
        case 36: key = "Enter"
        case 48: key = "Tab"
        default: key = (e.charactersIgnoringModifiers ?? "").uppercased()
        }
        return key.isEmpty ? nil : s + key
    }
}

// MARK: - Providers (edit)

struct ProviderEditor: View {
    @EnvironmentObject var cfg: ConfigStore
    let id: String

    var p: JSON { (cfg.values["providers"] as? [JSON] ?? []).first { $0.s("id") == id } ?? [:] }
    func set(_ k: String, _ v: Any) {
        var all = cfg.values["providers"] as? [JSON] ?? []
        guard let i = all.firstIndex(where: { $0.s("id") == id }) else { return }
        all[i][k] = v
        cfg.set("providers", all)
    }
    func str(_ k: String) -> Binding<String> { Binding(get: { p.s(k) }, set: { set(k, $0) }) }

    var body: some View {
        let openai = p["openai"] as? JSON ?? ["baseUrl": "http://127.0.0.1:8080/v1", "apiKey": "", "model": "", "contextWindow": 32768, "maxOutputTokens": 4096]
        let oa = { (k: String) in Binding<String>(get: { (openai[k] as? NSNumber)?.stringValue ?? openai.s(k) },
                                                  set: { v in var o = openai; o[k] = ["contextWindow", "maxOutputTokens"].contains(k) ? (Int(v) ?? 0) : v; set("openai", o) }) }
        Form {
            Toggle("Enabled", isOn: Binding(get: { p["enabled"] as? Bool != false }, set: { set("enabled", $0) }))
            TextField("Name", text: str("name"))
            if p.s("providerId") == "openai-compatible" {
                TextField("Base URL", text: oa("baseUrl"))
                SecureField("API key", text: oa("apiKey"))
                TextField("Default model", text: oa("model"))
                TextField("Context window", text: oa("contextWindow"))
                TextField("Maximum output tokens", text: oa("maxOutputTokens"))
            } else {
                TextField("Binary (empty = default)", text: str("binary"))
                TextField("Terminal arguments", text: str("terminalArgs"))
                if p.s("kind") == "claude" { TextField("CLAUDE_CONFIG_DIR", text: str("configDir")) }
                TextField("Environment (KEY=value, one per line)", text: Binding(
                    get: { (p["env"] as? [String: String] ?? [:]).sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" }.joined(separator: "\n") },
                    set: { text in
                        var env: [String: String] = [:]
                        for line in text.split(separator: "\n") {
                            let kv = line.split(separator: "=", maxSplits: 1).map(String.init)
                            if kv.count == 2, !kv[0].trimmingCharacters(in: .whitespaces).isEmpty { env[kv[0].trimmingCharacters(in: .whitespaces)] = kv[1] }
                        }
                        set("env", env)
                    }), axis: .vertical).lineLimit(2...6)
            }
        }
        .formStyle(.grouped)
        .navigationTitle(p.s("name"))
    }
}

// MARK: - Per-project config (WorkspaceConfig.vue)

struct ProjectConfigSheet: View {
    @EnvironmentObject var b: Burrow
    @EnvironmentObject var cfg: ConfigStore
    @Environment(\.dismiss) private var dismiss
    let ws: JSON
    @State private var name = ""
    @State private var file = ProjectScripts()
    @State private var loaded = false
    @State private var error = ""

    var id: String { "\(ws.i("id"))" }
    var proj: JSON { (cfg.values["projectSettings"] as? JSON)?[id] as? JSON ?? [:] }
    /// lib/projectSettings.ts setProjectSettings: "" drops the field, an empty object drops the project.
    func setProj(_ k: String, _ v: String) {
        var all = cfg.values["projectSettings"] as? JSON ?? [:]
        var mine = all[id] as? JSON ?? [:]
        mine[k] = v.isEmpty ? nil : v
        all[id] = mine.isEmpty ? nil : mine
        cfg.set("projectSettings", all)
    }
    func saveFile() {
        Task {
            do { try await file.save(b, path: ws.s("path")); error = "" } catch { self.error = error.localizedDescription }
        }
    }

    var body: some View {
        let agents = (cfg.values["providers"] as? [JSON] ?? []).filter { $0["enabled"] as? Bool != false && $0.s("transport") != "none" }
        VStack(spacing: 0) {
            Form {
                Section("General") {
                    TextField("Display name", text: $name).onSubmit {
                        Task { _ = try? await b.call("rename_workspace", ["id": ws.i("id"), "name": name]); try? await b.refresh() }
                    }
                    Picker("Composer agent", selection: Binding(get: { proj.s("agentId") }, set: { setProj("agentId", $0) })) {
                        Text("App default").tag("")
                        ForEach(agents.indices, id: \.self) { Text(agents[$0].s("name")).tag(agents[$0].s("id")) }
                    }
                    let agent = proj.s("agentId").isEmpty ? "claude" : proj.s("agentId")
                    Picker("Composer model", selection: Binding(get: { proj.s("modelId") }, set: { setProj("modelId", $0) })) {
                        ForEach(ComposerModels.list(agent, current: proj.s("modelId"), cfg.values), id: \.0) { Text($0.0.isEmpty ? "App default" : $0.1).tag($0.0) }
                    }
                    TextField("Worktrees folder (empty = app default)", text: Binding(get: { proj.s("worktreesDir") }, set: { setProj("worktreesDir", $0) }))
                }
                Section("Agent environment (.burrow/config.toml)") {
                    TextField("CLAUDE_CONFIG_DIR", text: Binding(get: { file.settings["claude_config_dir"] ?? "" }, set: { file.settings["claude_config_dir"] = $0 }))
                        .onSubmit(saveFile)
                    TextField(".env file", text: Binding(get: { file.settings["env_file"] ?? "" }, set: { file.settings["env_file"] = $0 }), prompt: Text(".env"))
                        .onSubmit(saveFile)
                }
                Section("Scripts") {
                    ForEach($file.scripts) { $s in
                        VStack(alignment: .leading) {
                            HStack {
                                TextField("Name", text: $s.name)
                                Button(role: .destructive) { file.scripts.removeAll { $0.id == s.id }; saveFile() } label: { Image(systemName: "trash") }
                                    .buttonStyle(.plain)
                            }
                            TextField("Steps (one per line)", text: Binding(get: { s.steps.joined(separator: "\n") },
                                                                           set: { s.steps = $0.components(separatedBy: "\n") }), axis: .vertical)
                                .font(.brand(-2)).lineLimit(1...6)
                            Toggle("Continue on error", isOn: $s.continueOnError)
                            Toggle("Run when a worktree is created", isOn: $s.runOnWorktreeCreate)
                        }
                    }
                    Button("Add script") {
                        file.scripts.append(ProjectScript(id: "script-\(UUID().uuidString.prefix(8))", name: "New Script", steps: [""],
                                                          continueOnError: false, runOnWorktreeCreate: false))
                    }
                }
            }
            .formStyle(.grouped)
            HStack {
                if !error.isEmpty { Text(error).font(.ui(-2)).foregroundStyle(Theme.red) }
                Spacer()
                Button("Save scripts & environment") { saveFile() }.disabled(!loaded)
                Button("Done") { dismiss() }.keyboardShortcut(.defaultAction)
            }
            .padding(10)
        }
        .frame(width: 560, height: 620)
        .task {
            name = ws.s("name")
            file = await ProjectScripts.load(b, path: ws.s("path"))
            loaded = true
        }
    }
}

// MARK: - About

struct AboutSettings: View {
    @EnvironmentObject var b: Burrow
    @State private var core = ""

    var body: some View {
        VStack(spacing: 14) {
            Pet(state: "idle", size: 56)
            Text("Burrow — native client").font(.brand(4, weight: .bold))
            Text("Experimental SwiftUI client for the Burrow core").font(.ui(-1)).foregroundStyle(Theme.muted)
            Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 6) {
                GridRow { Text("Core").foregroundStyle(Theme.muted); Text(core.isEmpty ? "—" : core).font(.brand(-1)) }
                GridRow { Text("Connected").foregroundStyle(Theme.muted); Text(Remote.active?.name ?? "This Mac").font(.brand(-1)) }
            }
            Button("What's new (release notes)") {
                NSWorkspace.shared.open(URL(string: "https://github.com/JBurdik/burrow-wails/releases")!)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .task { core = (try? await b.call("get_app_version")) as? String ?? "" }
    }
}
