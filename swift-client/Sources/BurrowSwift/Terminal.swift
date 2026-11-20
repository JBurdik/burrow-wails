import AppKit
import Combine
import SwiftUI

// MARK: - Store

/// Every terminal session this window has opened, the event routing into them, and the
/// terminal_tabs / Right Panel bookkeeping. Sessions outlive their views (scrollback stays).
@MainActor
final class TerminalStore: ObservableObject {
    static let shared = TerminalStore()

    private(set) var sessions: [String: TerminalSession] = [:]
    private weak var b: Burrow?
    var burrow: Burrow? { b }
    private var bag: Set<AnyCancellable> = []

    /// Right Panel shells per workspace (src/lib/rpTerminals.ts), persisted like `burrow.rpTerms`.
    @Published var rp: [String: RpTerms] = [:] {
        didSet { if let d = try? JSONEncoder().encode(rp) { UserDefaults.standard.set(d, forKey: "rp.terms") } }
    }

    struct RpTerms: Codable { var ids: [String]; var active: String? }

    func install(_ b: Burrow) {
        guard self.b == nil else { return }
        self.b = b
        if let d = UserDefaults.standard.data(forKey: "rp.terms"), let v = try? JSONDecoder().decode([String: RpTerms].self, from: d) {
            rp = v
        }
        b.subscribe("pty-data-") { [weak self] name, payload in
            guard let s = self?.sessions[String(name.dropFirst("pty-data-".count))], let c = PtyChunk(payload) else { return }
            s.stream.push(c)
        }
        // A new PTY under this id numbers its output from 0 again.
        b.subscribe("pty-exit-") { [weak self] name, _ in
            self?.sessions[String(name.dropFirst("pty-exit-".count))]?.stream.reset()
        }
        b.subscribe("pty-resync-") { [weak self] name, _ in
            self?.sessions[String(name.dropFirst("pty-resync-".count))]?.onResync()
        }
        // `burrow log` lines for a tab (Terminal.vue log strip).
        b.subscribe("pty-log-") { name, p in
            guard let id = Int64(name.dropFirst("pty-log-".count)), let p = p as? JSON else { return }
            TerminalLayout.shared.log(id, p)
        }
        installKeys()
        #if DEBUG
        SplitNode.selfCheck()
        TitleState.selfCheck()
        #endif
        // Agent presets + terminal font live in config.json.
        Task { if ConfigStore.shared.values.isEmpty { await ConfigStore.shared.attach(b) } }
        // Foreground poll → tab auto-title, every 2 s like XTerm.vue.
        Task { [weak self] in
            while let self, self.b != nil {
                await self.pollTitles()
                try? await Task.sleep(for: .seconds(2))
            }
        }
        // The socket came back: fetch what streamed while it was down.
        b.$connected.removeDuplicates().dropFirst().filter { $0 }.sink { [weak self] _ in
            guard let self else { return }
            for s in self.sessions.values { Task { await s.stream.catchUp() } }
        }.store(in: &bag)
    }

    /// The session for a PTY, created (and create_pty'd once its view has a size) on first use.
    func session(_ id: String, cwd: String, initialCmd: String? = nil) -> TerminalSession? {
        if let s = sessions[id] { return s }
        guard let b, let s = TerminalSession(id: id, burrow: b) else { return nil }
        sessions[id] = s
        s.startWhenSized(cwd: cwd, initialCmd: initialCmd)
        return s
    }

    func kill(_ id: String) async {
        await sessions[id]?.kill()
        if sessions[id] == nil { _ = try? await b?.call("kill_pty", ["id": id]) }
        sessions[id] = nil
    }

    /// A PTY id nobody uses: above every live daemon session, saved tab and RP shell (Terminal.vue's
    /// initPtyCounter, which also counts the daemon). ponytail: the desktop mints from an in-memory counter
    /// seeded below 1_000_000 at its start, so ours start at 1_000_000 — a desktop running alongside can't
    /// re-mint them; one restarted after we saved a tab seeds above our ids and could race us to the next one.
    func nextPtyID() async -> String {
        var top: Int64 = 1_000_000
        let live = (try? await b?.call("list_pty_sessions")) as? [String] ?? []
        for id in live { top = max(top, Int64(id) ?? 0) }
        for case let rows as [JSON] in (b?.snapshot["tabs"] as? JSON ?? [:]).values {
            for r in rows { top = max(top, r.i("pty_id")) }
        }
        for t in rp.values { for id in t.ids { top = max(top, Int64(id) ?? 0) } }
        for id in sessions.keys { top = max(top, Int64(id) ?? 0) }
        return String(top + 1)
    }

    // MARK: terminal_tabs (SQLite; desktop Terminal.vue writes the same rows)

    /// Rows of the repo and its worktrees, as of the last shell_snapshot.
    func tabs(root: Int64) -> [JSON] {
        guard let b else { return [] }
        let byWS = b.snapshot["tabs"] as? JSON ?? [:]
        let wsIDs = b.workspaces.keys.filter { b.rootWorkspace($0).i("id") == root }.sorted()
        let hidden = TerminalLayout.shared.hiddenLeaves
        return wsIDs.flatMap { byWS["\($0)"] as? [JSON] ?? [] }.filter { $0.i("pty_id") > 0 && !hidden.contains($0.i("pty_id")) }
    }

    /// Repos with at least one terminal tab, as empty chat groups — the Sidebar merges them in so a
    /// project with tabs but no active thread still gets a section.
    func tabRoots(_ b: Burrow) -> [Int64: [JSON]] {
        var out: [Int64: [JSON]] = [:]
        for case let (ws, rows as [JSON]) in b.snapshot["tabs"] as? JSON ?? [:] where rows.contains(where: { $0.i("pty_id") > 0 }) {
            out[b.rootWorkspace(Int64(ws) ?? 0).i("id")] = []
        }
        out[0] = nil
        return out
    }

    func tab(pty: Int64) -> JSON? {
        for case let rows as [JSON] in (b?.snapshot["tabs"] as? JSON ?? [:]).values {
            if let r = rows.first(where: { $0.i("pty_id") == pty }) { return r }
        }
        return nil
    }

    /// Read-modify-write one workspace's tab list. ponytail: save_terminal_tabs replaces the whole list and
    /// there is no tabs-changed event, so a desktop window open on this workspace keeps (and re-saves) its own list —
    /// the Vue app has the same whole-list write; only a Go-side per-row upsert + event would fix both.
    func mutateTabs(_ wsID: Int64, _ f: (inout [JSON]) -> Void) async {
        guard let b else { return }
        do {
            var rows = (try await b.call("list_terminal_tabs", ["workspaceId": wsID])) as? [JSON] ?? []
            f(&rows)
            let keys = ["title", "default_title", "initial_cmd", "pty_id", "cwd", "session_id", "branch"]
            let payload = rows.map { r in keys.reduce(into: JSON()) { $0[$1] = r[$1] ?? NSNull() } }
            try await b.call("save_terminal_tabs", ["workspaceId": wsID, "tabs": payload])
            try await b.refresh()
        } catch {
            b.status = "Tabs: \(error.localizedDescription)"
        }
    }

    /// New tab in a workspace: a shell, or a command (agent preset, reopened tab) typed in once the prompt is up.
    func newTab(workspaceID: Int64, cmd: String? = nil, name: String? = nil, cwd: String? = nil) async {
        guard let b else { return }
        let id = await nextPtyID()
        let cwd = cwd ?? b.workspaces[workspaceID]?.s("path") ?? NSHomeDirectory()
        let n = tabs(root: b.rootWorkspace(workspaceID).i("id")).count + 1
        let title = name ?? cmd.map { String($0.split(separator: " ").first ?? "Terminal") } ?? "Terminal \(n)"
        _ = session(id, cwd: cwd, initialCmd: cmd)
        await mutateTabs(workspaceID) {
            $0.append(["title": title, "default_title": title, "initial_cmd": cmd ?? NSNull(), "pty_id": Int64(id)!, "cwd": cwd])
        }
        b.selectedTab = Int64(id)
    }

    func rename(pty: Int64, to title: String) async {
        guard let wsID = tab(pty: pty)?.i("workspace_id") else { return }
        await mutateTabs(wsID) { rows in
            for i in rows.indices where rows[i].i("pty_id") == pty { rows[i]["title"] = title }
        }
    }

    /// Spotlight's agent launch: an agent CLI by name, titled after it.
    func newTab(workspaceID: Int64, agent: String) async {
        let w = agent.split(separator: " ").first.map(String.init) ?? agent
        await newTab(workspaceID: workspaceID, cmd: agent, name: w.prefix(1).uppercased() + w.dropFirst())
    }

    /// Close one pane (a tab's last pane closes the tab). ⌘⇧T can bring it back as a new tab.
    func close(pty: Int64) async {
        let L = TerminalLayout.shared
        let key = L.tabKey(of: pty)
        let row = tab(pty: pty)
        if let row { remember(row) }
        let newKey = L.remove(pty)
        if b?.selectedTab == key { b?.selectedTab = newKey }
        await kill(String(pty))
        guard let wsID = row?.i("workspace_id") else { return }
        await mutateTabs(wsID) { $0.removeAll { $0.i("pty_id") == pty } }
    }

    /// Close every pane of a tab.
    func closeTab(_ key: Int64) async {
        for leaf in TerminalLayout.shared.tree(key).leaves.reversed() { await close(pty: leaf) }
    }

    // MARK: Right Panel shells

    /// Drop shells the daemon no longer has (rpTerminals.ts keepLive), so a restart doesn't list dead ones.
    func pruneRp(_ ws: Int64) async {
        guard let live = (try? await b?.call("list_pty_sessions")) as? [String], var t = rp["\(ws)"] else { return }
        t.ids = t.ids.filter { live.contains($0) || sessions[$0] != nil }
        if !(t.active.map(t.ids.contains) ?? false) { t.active = t.ids.first }
        rp["\(ws)"] = t
    }

    func addRp(_ ws: Int64) async {
        let id = await nextPtyID()
        var t = rp["\(ws)"] ?? RpTerms(ids: [], active: nil)
        t.ids.append(id)
        t.active = id
        rp["\(ws)"] = t
    }

    func closeRp(_ ws: Int64, _ id: String) async {
        guard var t = rp["\(ws)"], let i = t.ids.firstIndex(of: id) else { return }
        t.ids.remove(at: i)
        if t.active == id { t.active = t.ids.indices.contains(i) ? t.ids[i] : t.ids.last }
        rp["\(ws)"] = t
        await kill(id)
    }
}

// MARK: - Sidebar

/// Terminal tabs of one workspace, rendered under that project's chats in the sidebar.
struct SidebarTabsSlot: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var store = TerminalStore.shared
    let workspaceID: Int64
    @State private var renaming: JSON?
    @State private var newName = ""

    var body: some View {
        let rows = store.tabs(root: workspaceID)
        Group {
            ForEach(rows, id: \.ptyKey) { t in
                TabRow(tab: t, selected: b.selectedTab == t.i("pty_id"))
                    .contentShape(Rectangle())
                    .onTapGesture { b.selectedTab = t.i("pty_id") }
                    .contextMenu {
                        Button("Rename…") { newName = TabRow.title(t); renaming = t }
                        Button("Open in Window") { TerminalLayout.shared.popOut(t.i("pty_id"), b: b, title: TabRow.title(t)) }
                        Button("Close", role: .destructive) { Task { await store.closeTab(t.i("pty_id")) } }
                    }
            }
            NewTabMenu(workspaceID: workspaceID)
        }
        .onAppear { store.install(b) }
        .alert("Rename tab", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField("Title", text: $newName)
            Button("Rename") {
                if let t = renaming { Task { await store.rename(pty: t.i("pty_id"), to: newName) } }
            }
            Button("Cancel", role: .cancel) {}
        }
    }
}

private extension Dictionary where Key == String, Value == Any {
    var ptyKey: Int64 { i("pty_id") }
}

struct TabRow: View {
    @EnvironmentObject var b: Burrow
    let tab: JSON
    var selected = false

    static func title(_ t: JSON) -> String {
        [t.s("title"), t.s("default_title")].first { !$0.isEmpty } ?? "Terminal"
    }

    var body: some View {
        let id = tab.i("pty_id")
        let phase = b.phases["pty:\(id)"] ?? [:]
        let state = phase.s("state").isEmpty ? "idle" : phase.s("state")
        let seen = b.seen["pty:\(id)"] ?? 0
        HStack(spacing: 8) {
            StatusIcon(state: state, unread: state == "done" && phase.i("turn_ended_at") > seen).frame(width: 12)
            Image(systemName: tab.s("initial_cmd").isEmpty ? "terminal" : "sparkles")
                .font(.system(size: 10)).foregroundStyle(Theme.muted)
            Text(Self.title(tab)).font(.system(size: 13)).foregroundStyle(selected ? Theme.text : Theme.text.opacity(0.82)).lineLimit(1)
            Spacer(minLength: 0)
        }
        .padding(.vertical, 3).padding(.horizontal, 4)
        .background(selected ? Theme.selected : .clear, in: RoundedRectangle(cornerRadius: 6))
    }
}

/// "+ New" under a project's tabs: a shell or an agent preset (enabled provider instances from config).
struct NewTabMenu: View {
    @ObservedObject private var cfg = ConfigStore.shared
    let workspaceID: Int64
    var body: some View {
        Menu {
            Button("Terminal  ⌘T") { Task { await TerminalStore.shared.newTab(workspaceID: workspaceID) } }
            let presets = TerminalStore.shared.presets
            if !presets.isEmpty { Divider() }
            ForEach(presets, id: \.cmd) { p in
                Button(p.shortcut.isEmpty ? p.name : "\(p.name)  \(p.shortcut)") {
                    Task { await TerminalStore.shared.newTab(workspaceID: workspaceID, cmd: p.cmd, name: p.name) }
                }
            }
        } label: {
            Label("New tab", systemImage: "plus").font(.brand(-4)).foregroundStyle(Theme.muted)
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
        .padding(.leading, 4)
    }
}

// MARK: - Detail

/// Detail pane when `b.selectedTab` is set: the tab's panes (split tree), header for the focused one.
struct TerminalDetailSlot: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var store = TerminalStore.shared
    @ObservedObject private var layout = TerminalLayout.shared
    let ptyID: Int64

    var body: some View {
        let leaf = layout.focusedLeaf(ptyID)
        let tab = store.tab(pty: leaf) ?? store.tab(pty: ptyID) ?? [:]
        let phase = b.phases["pty:\(leaf)"] ?? [:]
        let state = phase.s("state").isEmpty ? "idle" : phase.s("state")
        let logs = layout.tree(ptyID).leaves.flatMap { layout.logs[$0] ?? [] }.suffix(5)
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                StatusIcon(state: state)
                Text(TabRow.title(tab)).font(.brand(-2, weight: .bold)).foregroundStyle(Theme.text).lineLimit(1)
                if !phase.s("detail").isEmpty {
                    Text(phase.s("detail")).font(.brand(-4)).foregroundStyle(Theme.status(state)).lineLimit(1)
                }
                Text(tab.s("cwd")).font(.brand(-4)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.head)
                Spacer()
                if tab["workspace_id"] != nil { NewTabMenu(workspaceID: tab.i("workspace_id")) }
                Group {
                    Button { Task { await store.split(h: true) } } label: { Image(systemName: "rectangle.split.2x1") }
                        .help("Split right (⌘D)")
                    Button { Task { await store.split(h: false) } } label: { Image(systemName: "rectangle.split.1x2") }
                        .help("Split down (⌘⇧D)")
                    Button { layout.popOut(leaf, b: b, title: TabRow.title(tab)) } label: { Image(systemName: "macwindow.on.rectangle") }
                        .help("Open pane in a separate window (⌘⌥O)")
                    Button { Task { await store.close(pty: leaf) } } label: { Image(systemName: "xmark") }
                        .help("Close pane (⌘W, kills the shell)")
                }
                .buttonStyle(.plain).foregroundStyle(Theme.muted)
            }
            .padding(.horizontal, 12).padding(.vertical, 8)
            .background(Theme.base)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
            if !logs.isEmpty {
                VStack(alignment: .leading, spacing: 1) {
                    ForEach(Array(logs.enumerated()), id: \.offset) { _, l in
                        HStack(spacing: 6) {
                            Text(l.level.uppercased()).font(.brand(-6, weight: .bold))
                                .foregroundStyle(l.level == "error" ? Theme.red : l.level == "warn" ? Theme.yellow : Theme.accent)
                            Text(l.message).font(.brand(-5)).foregroundStyle(Theme.text2).lineLimit(1)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 10).padding(.vertical, 3)
                .background(Theme.panel)
                .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
            }
            SplitTreeView(tabKey: ptyID, node: layout.tree(ptyID))
        }
        .background(Theme.terminal)
        .onAppear { store.install(b) }
        .task(id: "\(leaf)-\(state)") {
            if state == "done" { _ = try? await b.call("seen_mark", ["key": "pty:\(leaf)"]) }
        }
    }
}

/// One PTY's terminal view; creates / reattaches the session on first appearance.
struct TerminalPane: View {
    @EnvironmentObject var b: Burrow
    let id: String
    let cwd: String
    var initialCmd: String?

    var body: some View {
        let _ = TerminalStore.shared.install(b)
        Group {
            if let s = TerminalStore.shared.session(id, cwd: cwd, initialCmd: initialCmd) {
                TerminalHostView(session: s)
            } else {
                Text("Could not start the terminal emulator").foregroundStyle(Theme.muted)
            }
        }
        .background(Theme.terminal)
    }
}

// MARK: - Right Panel

/// The Right Panel's Terminal tab: scratch shells in the thread's workspace, kept per workspace.
struct TerminalPanelSlot: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var store = TerminalStore.shared
    let chat: JSON

    var body: some View {
        let ws = chat.i("workspace_id")
        let cwd = b.workspaces[ws]?.s("path") ?? NSHomeDirectory()
        let t = store.rp["\(ws)"]
        VStack(spacing: 0) {
            HStack(spacing: 4) {
                ForEach(Array((t?.ids ?? []).enumerated()), id: \.element) { i, id in
                    HStack(spacing: 4) {
                        Text("Shell \(i + 1)").font(.brand(-4, weight: t?.active == id ? .bold : .regular))
                        Button { Task { await store.closeRp(ws, id) } } label: { Image(systemName: "xmark").font(.system(size: 8)) }
                            .buttonStyle(.plain)
                    }
                    .foregroundStyle(t?.active == id ? Theme.text : Theme.muted)
                    .padding(.horizontal, 8).padding(.vertical, 4)
                    .background(t?.active == id ? Theme.hover : .clear, in: RoundedRectangle(cornerRadius: 6))
                    .onTapGesture { store.rp["\(ws)"]?.active = id }
                }
                Button { Task { await store.addRp(ws) } } label: { Image(systemName: "plus") }
                    .buttonStyle(.plain).foregroundStyle(Theme.muted).help("New shell")
                Spacer()
            }
            .padding(6)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
            if let active = t?.active {
                PtyPermissionBanner(ptyID: Int64(active) ?? 0)
                SecretRequestBanner(ownerKey: "pty:\(active)")
                TerminalPane(id: active, cwd: cwd).id(active)
            } else {
                VStack(spacing: 10) {
                    Pet(state: "idle", size: 36)
                    Button("Open a shell") { Task { await store.addRp(ws) } }.buttonStyle(.bordered)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(Theme.terminal)
        .task(id: ws) {
            store.install(b)
            await store.pruneRp(ws)
        }
    }
}
