import AppKit
import SwiftUI

// MARK: - Shell state (spotlight + back/forward history)

@MainActor
final class Shell: ObservableObject {
    static let shared = Shell()
    @Published var spotlight = false
    @Published var spotlightMode: SpotlightMode = .search
    /// Set from `welcome` when the core and this client can't talk (transport.ts checkProtocolRev); App shows a banner.
    @Published var incompatible: String?
    /// The main window (WindowGroup id "main") is key. ⌘1…9 menu items exist only then, so pop-outs
    /// (Git window's ⌘1/⌘2 tabs, thread windows) keep their own digit shortcuts.
    @Published var mainKey = true
    private var keyObserver: Any?
    func trackKeyWindow() {
        guard keyObserver == nil else { return }
        keyObserver = NotificationCenter.default.addObserver(forName: NSWindow.didBecomeKeyNotification, object: nil, queue: .main) { n in
            let id = (n.object as? NSWindow)?.identifier?.rawValue ?? ""
            MainActor.assumeIsolated { Shell.shared.mainKey = id.hasPrefix("main") }
        }
    }

    enum Sel: Equatable { case none, chat(Int64), tab(Int64) }
    private var back: [Sel] = []
    private var fwd: [Sel] = []
    private var navigating = false
    var canBack: Bool { !back.isEmpty }
    var canForward: Bool { !fwd.isEmpty }

    static func sel(_ b: Burrow) -> Sel {
        if let t = b.selectedTab { return .tab(t) }
        if let c = b.selectedChat { return .chat(c) }
        return .none
    }

    /// ContentView calls this on every selection change.
    func moved(from old: Sel) {
        if navigating { navigating = false; return }
        if back.last != old { back.append(old) }
        if back.count > 100 { back.removeFirst() }
        fwd.removeAll()
    }

    func go(_ b: Burrow, forward: Bool) {
        guard let target = forward ? fwd.popLast() : back.popLast() else { return }
        let cur = Self.sel(b)
        if forward { back.append(cur) } else { fwd.append(cur) }
        guard target != cur else { return }
        navigating = true
        switch target {
        case .none: b.selectedChat = nil; b.selectedTab = nil
        case .chat(let id): b.selectedChat = id
        case .tab(let id): b.selectedTab = id
        }
    }
}

/// Mirrors PROTOCOL_REV / MIN_SERVER_REV in src/runtime/transport.ts; a welcome without the fields = rev 1.
let clientProtocolRev = 1, minServerRev = 1
func protocolMismatch(_ welcome: JSON) -> String? {
    let rev = (welcome["rev"] as? NSNumber)?.intValue ?? 1
    let minClient = (welcome["minClientRev"] as? NSNumber)?.intValue ?? 1
    if minClient > clientProtocolRev { return "This client is too old for the Burrow core (needs protocol rev \(minClient)). Rebuild BurrowSwift." }
    if rev < minServerRev { return "The Burrow core is too old for this client (rev \(rev)). Quit Burrow completely and reopen it." }
    return nil
}

extension Burrow {
    /// ⌘1…9 targets, roughly in sidebar order: pinned, needs-you, working, then each project's active threads.
    // ponytail: re-derives Sidebar.swift's sections; expose the sidebar's own row order if they drift.
    var jumpThreads: [JSON] {
        let pinned = ThreadStore.shared.pinned
        let live = chats.filter { !settled($0) }
        let pin = live.filter { pinned.contains($0.i("id")) }.sorted { (pinned.firstIndex(of: $0.i("id")) ?? 0) < (pinned.firstIndex(of: $1.i("id")) ?? 0) }
        let rest = live.filter { !pinned.contains($0.i("id")) }
        let needs = rest.filter { ["waiting_input", "waiting_approval"].contains(state($0.i("id"))) }
        let working = rest.filter { state($0.i("id")) == "running" }
        let idle = rest.filter { !inFlight($0.i("id")) }
        let byRepo = Dictionary(grouping: idle) { rootWorkspace($0.i("workspace_id")).i("id") }
        let grouped = SidebarStore.shared.orderRepos(Array(byRepo.keys), self).flatMap {
            (byRepo[$0] ?? []).sorted { $0.i("last_activity_at") > $1.i("last_activity_at") }
        }
        return pin + needs + working + grouped
    }

    /// Repos in the desktop sidebar's order (sort_order); ⌘1…9 index into this.
    var orderedProjects: [JSON] {
        workspaces.values.filter { $0["parent_id"] == nil }
            .sorted { ($0.i("sort_order"), $0.i("id")) < ($1.i("sort_order"), $1.i("id")) }
    }

    /// Open a project = its most recently active thread, else the new-thread composer.
    func openProject(_ ws: JSON) {
        let id = ws.i("id")
        let recent = chats.filter { rootWorkspace($0.i("workspace_id")).i("id") == id }
            .max { $0.i("last_activity_at") < $1.i("last_activity_at") }
        if let recent { selectedChat = recent.i("id") } else { selectedChat = nil; selectedTab = nil }
    }

    /// Live terminal tabs from the last shell_snapshot (rows without a pty are closed tabs).
    var snapshotTabs: [(ws: JSON, tab: JSON)] {
        let tabs = snapshot["tabs"] as? [String: [JSON]] ?? [:]
        return tabs.flatMap { k, list in
            list.filter { $0["pty_id"] is NSNumber }.map { (workspaces[Int64(k) ?? 0] ?? [:], $0) }
        }
    }
}

func tabTitle(_ t: JSON) -> String {
    for k in ["title", "default_title", "initial_cmd"] where !t.s(k).isEmpty { return t.s(k) }
    return "Terminal"
}

// MARK: - Spotlight (⌘P)

struct Spotlight: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openSettings) private var openSettings
    @ObservedObject private var shell = Shell.shared
    @State private var query = ""
    @State private var hits: [JSON] = []
    @State private var index = 0
    @FocusState private var focused: Bool

    struct Row: Identifiable {
        let id: String
        let section: String
        let icon: String
        let title: String
        let subtitle: String
        var keepOpen = false
        let run: () -> Void
    }

    static let settingsTabs = [("general", "General"), ("providers", "Providers"), ("subagents", "Sub-agent profiles"),
                               ("permissions", "Permissions"), ("notifications", "Notifications & sounds"), ("shortcuts", "Shortcuts"),
                               ("workspaces", "Workspaces & scripts"), ("remote", "Remote access"), ("appearance", "Appearance"), ("about", "About")]

    var rows: [Row] {
        let q = query.lowercased().trimmingCharacters(in: .whitespaces)
        func match(_ s: String) -> Bool { q.isEmpty || s.lowercased().contains(q) }
        var out: [Row] = []
        let actions: [(String, String, () -> Void)] = [
            ("New thread", "square.and.pencil", { b.selectedChat = nil; b.selectedTab = nil }),
            ("Toggle right panel", "sidebar.right", { UserDefaults.standard.set(!UserDefaults.standard.bool(forKey: "rp.open"), forKey: "rp.open") }),
            ("Open project…", "folder", { shell.spotlightMode = .openProject }),
            ("New project…", "folder.badge.plus", { shell.spotlightMode = .newProject }),
            ("Settings", "gearshape", { openSettings() }),
            ("Back", "chevron.left", { Shell.shared.go(b, forward: false) }),
            ("Forward", "chevron.right", { Shell.shared.go(b, forward: true) }),
        ]
        for (t, icon, run) in actions where match(t) {
            out.append(Row(id: "a:\(t)", section: "Actions", icon: icon, title: t, subtitle: "", keepOpen: t.hasSuffix("project…"), run: run))
        }
        // A pasted PR URL / #123: check it out into a worktree + thread (prCheckout.ts).
        if let pr = parsePrRef(query.trimmingCharacters(in: .whitespaces)) {
            let project = b.rootWorkspace(b.chat(b.selectedChat)?.i("workspace_id") ?? 0)
            let target = project.isEmpty ? (b.orderedProjects.first ?? [:]) : project
            out.insert(Row(id: "pr", section: "Pull request", icon: "arrow.triangle.pull", title: "Check out PR #\(pr.number)",
                           subtitle: target.s("name")) {
                Task { _ = await b.checkoutPR(project: target, number: pr.number, url: pr.url, agent: UserDefaults.standard.string(forKey: "newThread.agent") ?? "claude") }
            }, at: 0)
        }
        let wsForNew = b.chat(b.selectedChat)?.i("workspace_id") ?? b.orderedProjects.first?.i("id") ?? 0
        for a in (ConfigStore.shared.values["providers"] as? [JSON] ?? []) where a["enabled"] as? Bool != false {
            let name = a.s("name")
            if a.s("transport") != "none", match("new \(name) thread") {
                out.append(Row(id: "ag:c:\(a.s("id"))", section: "Agents", icon: "bubble.left.and.text.bubble.right", title: "New \(name) thread", subtitle: "") {
                    UserDefaults.standard.set(a.s("id"), forKey: "newThread.agent")
                    newThread(in: wsForNew, b)
                })
            }
            if a.s("providerId") != "openai-compatible", wsForNew > 0, match("new \(name) tab") {
                let cmd = ([a.s("binary").isEmpty ? a.s("kind") : a.s("binary")] + [a.s("terminalArgs")]).filter { !$0.isEmpty }.joined(separator: " ")
                out.append(Row(id: "ag:t:\(a.s("id"))", section: "Agents", icon: "terminal", title: "New \(name) tab", subtitle: b.workspaces[wsForNew]?.s("name") ?? "") {
                    Task { await TerminalStore.shared.newTab(workspaceID: wsForNew, agent: cmd) }
                })
            }
        }
        for (tab, label) in Self.settingsTabs where match("settings \(label)") && !q.isEmpty {
            out.append(Row(id: "s:\(tab)", section: "Settings", icon: "gearshape", title: "Settings › \(label)", subtitle: "") {
                UserDefaults.standard.set(tab, forKey: "settings.tab")
                openSettings()
            })
        }
        let threads = b.chats.filter { match($0.s("title")) }
            .sorted { $0.i("last_activity_at") > $1.i("last_activity_at") }.prefix(q.isEmpty ? 6 : 12)
        for c in threads {
            out.append(Row(id: "c:\(c.i("id"))", section: "Threads", icon: "bubble.left", title: c.s("title"),
                           subtitle: b.rootWorkspace(c.i("workspace_id")).s("name")) { b.selectedChat = c.i("id") })
        }
        for w in b.orderedProjects where match(w.s("name")) {
            out.append(Row(id: "p:\(w.i("id"))", section: "Projects", icon: "folder", title: w.s("name"), subtitle: w.s("path")) { b.openProject(w) })
        }
        for w in b.orderedProjects where !q.isEmpty && match("new thread in \(w.s("name"))") {
            out.append(Row(id: "np:\(w.i("id"))", section: "New thread in…", icon: "square.and.pencil", title: w.s("name"), subtitle: "") { newThread(in: w.i("id"), b) })
        }
        for (w, t) in b.snapshotTabs where match(tabTitle(t)) {
            let pty = t.i("pty_id")
            out.append(Row(id: "t:\(t.i("id"))", section: "Terminal tabs", icon: "terminal", title: tabTitle(t), subtitle: w.s("name")) { b.selectedTab = pty })
        }
        for h in hits {
            out.append(Row(id: "h:\(h.i("chat_id")):\(h.i("ts"))", section: "In chats", icon: "text.magnifyingglass",
                           title: h.s("title"), subtitle: h.s("snippet")) { b.selectedChat = h.i("chat_id") })
        }
        return out
    }

    var body: some View {
        Group {
            switch shell.spotlightMode {
            case .search: search
            case .openProject: ProjectOpenPicker()
            case .newProject: NewProjectPicker()
            }
        }
        .onDisappear { shell.spotlightMode = .search }
    }

    var search: some View {
        let rows = rows
        return VStack(spacing: 0) {
            TextField("Search threads, projects, tabs, actions…", text: $query)
                .textFieldStyle(.plain).font(.ui(2)).padding(14)
                .focused($focused)
                .onSubmit { pick(rows) }
            Divider().overlay(Theme.border)
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(Array(rows.enumerated()), id: \.element.id) { i, r in
                            if i == 0 || rows[i - 1].section != r.section {
                                Text(r.section.uppercased()).font(.brand(-4, weight: .bold)).foregroundStyle(Theme.muted)
                                    .padding(.horizontal, 14).padding(.top, 10).padding(.bottom, 4)
                            }
                            HStack(spacing: 10) {
                                Image(systemName: r.icon).foregroundStyle(Theme.text2).frame(width: 18)
                                Text(r.title).font(.ui()).foregroundStyle(Theme.text).lineLimit(1)
                                Text(r.subtitle).font(.ui(-3)).foregroundStyle(Theme.muted).lineLimit(1)
                                Spacer(minLength: 0)
                            }
                            .padding(.horizontal, 14).padding(.vertical, 7)
                            .background(i == index ? Theme.selected : .clear, in: RoundedRectangle(cornerRadius: 7))
                            .padding(.horizontal, 6)
                            .contentShape(Rectangle())
                            .onTapGesture { index = i; pick(rows) }
                            .id(i)
                        }
                        if rows.isEmpty { Pet(state: "idle", size: 36).frame(maxWidth: .infinity).padding(24) }
                    }
                    .padding(.bottom, 8)
                }
                .onChange(of: index) { proxy.scrollTo(index) }
            }
        }
        .frame(width: 600, height: 440)
        .background(Theme.panel)
        .onAppear { focused = true }
        .onKeyPress(.downArrow) { index = min(index + 1, max(rows.count - 1, 0)); return .handled }
        .onKeyPress(.upArrow) { index = max(index - 1, 0); return .handled }
        .onKeyPress(.escape) { dismiss(); return .handled }
        .onChange(of: query) { index = 0 }
        .task(id: query) {
            // Full-text chat hits (chat_fts), debounced.
            let q = query.trimmingCharacters(in: .whitespaces)
            guard q.count >= 2 else { hits = []; return }
            try? await Task.sleep(for: .milliseconds(200))
            guard !Task.isCancelled else { return }
            let r = (try? await b.call("search_chats", ["query": q, "limit": 8])) as? [JSON] ?? []
            if !Task.isCancelled { hits = r }
        }
    }

    func pick(_ rows: [Row]) {
        guard rows.indices.contains(index) else { return }
        let r = rows[index]
        if !r.keepOpen { dismiss() }
        r.run()
    }
}
