import SwiftUI

/// The phone's bottom navigation (PWA: BottomNav.vue): Threads / Project / More.
/// A plain TabView: built with the iOS 26 SDK its bar is Liquid Glass by itself.
struct MainTabs: View {
    @Environment(AppModel.self) private var model
    @State private var tab = Pane.threads
    @State private var query = ""

    enum Pane: Hashable { case threads, project, more, search }

    var body: some View {
        if #available(iOS 18, *) {
            // Search as its own tab role: on iOS 26 it is the separate glass button beside the bar.
            TabView(selection: $tab) {
                Tab("Threads", systemImage: "bubble.left.and.bubble.right", value: Pane.threads) { ThreadsView() }
                    .badge(needsCount)
                Tab("Project", systemImage: "arrow.triangle.branch", value: Pane.project) { ProjectView() }
                Tab("More", systemImage: "ellipsis", value: Pane.more) { MoreView() }
                if #available(iOS 27, *) {
                    // iOS 27 detaches only a .prominent tab beside the bar (Phone's look); the field then
                    // lives on the screen itself, since a TabView .searchable needs the .search role.
                    Tab("Search", systemImage: "magnifyingglass", value: Pane.search, role: .prominent) {
                        ChatSearchScreen(query: $query, ownField: true) { id in model.openChat = id; tab = .threads }
                    }
                } else {
                    Tab(value: Pane.search, role: .search) {
                        ChatSearchScreen(query: $query) { id in model.openChat = id; tab = .threads }
                    }
                }
            }
            .modifier(TabSearchField(query: $query))
        } else {
            TabView(selection: $tab) {
                ThreadsView()
                    .tabItem { Label("Threads", systemImage: "bubble.left.and.bubble.right") }
                    .badge(needsCount)
                    .tag(Pane.threads)
                ProjectView()
                    .tabItem { Label("Project", systemImage: "arrow.triangle.branch") }
                    .tag(Pane.project)
                MoreView()
                    .tabItem { Label("More", systemImage: "ellipsis") }
                    .tag(Pane.more)
            }
        }
    }

    private var needsCount: Int { model.threads.filter { model.needsYou($0) }.count }
}

/// The PWA's Project screen: pick a project (repos with their worktrees), then its Changes / Pull requests.
struct ProjectView: View {
    @Environment(AppModel.self) private var model
    @AppStorage("project.workspace") private var selected = 0
    @State private var rows: [Row] = []
    @State private var loaded = false

    struct Row: Identifiable, Hashable {
        var id: Int
        var name: String
        var path: String
        var depth: Int
    }

    private var current: Row? { rows.first { $0.id == selected } ?? rows.first }

    var body: some View {
        NavigationStack {
            List {
                if let cur = current {
                    Section {
                        ProjectPickerField(choices: rows.map { ProjectChoice(id: $0.id, name: $0.name, path: $0.path, depth: $0.depth) },
                                           selection: Binding(get: { cur.id }, set: { selected = $0 }))
                    }
                    Section {
                        NavigationLink { GitSurface(cwdOverride: cur.path).navigationTitle("Changes").navigationBarTitleDisplayMode(.inline) } label: {
                            Label("Changes", systemImage: "plusminus.circle")
                        }
                        NavigationLink { PullRequestsView(cwd: cur.path).navigationTitle("Pull requests").navigationBarTitleDisplayMode(.inline) } label: {
                            Label("Pull requests", systemImage: "arrow.triangle.pull")
                        }
                        NavigationLink { DevServersView(cwd: cur.path).navigationTitle("Dev servers").navigationBarTitleDisplayMode(.inline) } label: {
                            Label("Dev servers", systemImage: "server.rack")
                        }
                        NavigationLink { ProjectSubagentsView(workspaceId: cur.id).navigationTitle("Sub-agents").navigationBarTitleDisplayMode(.inline) } label: {
                            Label("Sub-agents", systemImage: "person.2")
                        }
                        .badge(model.chats.filter { $0.workspaceId == cur.id && $0.parentChatId != 0 && $0.kind != "side" && !$0.archived && model.status($0.id) == .running }.count)
                    }
                }
            }
            .overlay {
                if loaded && rows.isEmpty {
                    ContentUnavailableView("No projects", systemImage: "folder",
                                           description: Text(model.connected ? "Add one in Burrow on the Mac." : "Connecting…"))
                }
            }
            .navigationTitle("Project")
            .refreshable { await load() }
        }
        .task(id: model.connected) { await load() }
    }

    private func load() async {
        guard let list = try? await model.client?.call("list_workspaces") as? [[String: Any]] else { loaded = true; return }
        var out: [Row] = []
        let all = list.map { (id: int($0["id"]), name: $0["name"] as? String ?? "", path: $0["path"] as? String ?? "", parent: $0["parent_id"] as? NSNumber) }
        for repo in all.filter({ $0.parent == nil }) {
            out.append(Row(id: repo.id, name: repo.name, path: repo.path, depth: 0))
            for wt in all where wt.parent?.intValue == repo.id { out.append(Row(id: wt.id, name: wt.name, path: wt.path, depth: 1)) }
        }
        rows = out; loaded = true
    }
}

/// The PWA's More screen: the connection and the unpair door.
struct MoreView: View {
    @Environment(AppModel.self) private var model
    @State private var confirmUnpair = false

    var body: some View {
        NavigationStack {
            Form {
                Section("Mac") {
                    if let host = model.creds.flatMap({ URL(string: $0.baseURL)?.host }) { LabeledContent("Host", value: host) }
                    LabeledContent("Status") {
                        Label(model.connected ? "Connected" : "Offline", systemImage: model.connected ? "checkmark.circle.fill" : "wifi.slash")
                            .foregroundStyle(model.connected ? .green : .secondary)
                    }
                }
                Section {
                    Button("Unpair this iPhone", role: .destructive) { confirmUnpair = true }
                }
            }
            .navigationTitle("More")
            .confirmationDialog("Unpair this iPhone?", isPresented: $confirmUnpair, titleVisibility: .visible) {
                Button("Unpair", role: .destructive) { model.unpair() }
            } message: { Text("You can pair again from Settings → Remote access on the Mac.") }
        }
    }
}

/// The project's listening dev servers (PWA: DevServersSurface): the same `list_dev_servers` /
/// `kill_dev_server` the thread's Processes surface uses, keyed by the folder instead of a thread.
struct DevServersView: View {
    let cwd: String
    @Environment(AppModel.self) private var model
    @State private var servers: [DevServer] = []
    @State private var loaded = false
    @State private var error: String?
    @State private var confirmKill: DevServer?

    var body: some View {
        List {
            if let error { Text(error).font(.footnote).foregroundStyle(.red) }
            ForEach(servers) { s in
                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text(":\(s.port)").font(.body.monospaced().weight(.semibold)).foregroundStyle(.tint)
                        Spacer()
                        Text("pid \(s.pid)").font(.caption).foregroundStyle(.secondary)
                    }
                    Text(s.command).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(2)
                    if s.addr.hasPrefix("127.") || s.addr.hasPrefix("[::1]") || s.addr.hasPrefix("localhost") {
                        Text("Listens on localhost only").font(.caption2).foregroundStyle(.secondary)
                    }
                }
                .swipeActions { Button("Kill", role: .destructive) { confirmKill = s } }
                .contextMenu { Button("Kill", systemImage: "xmark.octagon", role: .destructive) { confirmKill = s } }
            }
        }
        .overlay {
            if loaded && servers.isEmpty {
                ContentUnavailableView("No dev servers", systemImage: "server.rack", description: Text("Nothing is listening in this project."))
            }
        }
        .confirmationDialog("Kill this dev server?", isPresented: Binding(get: { confirmKill != nil }, set: { if !$0 { confirmKill = nil } }),
                            titleVisibility: .visible, presenting: confirmKill) { s in
            Button("Kill :\(s.port) (pid \(s.pid))", role: .destructive) { Task { await kill(s) } }
        }
        .refreshable { await refresh() }
        .task(id: cwd) {
            while !Task.isCancelled {
                await refresh()
                try? await Task.sleep(for: .seconds(3))
            }
        }
    }

    private func refresh() async {
        guard let list = try? await model.client?.call("list_dev_servers", ["workspacePath": cwd]) as? [[String: Any]] else { loaded = true; return }
        servers = list.map { DevServer(pid: int($0["pid"]), port: int($0["port"]), addr: $0["addr"] as? String ?? "", command: $0["command"] as? String ?? "") }
        loaded = true
    }

    private func kill(_ s: DevServer) async {
        do { try await model.client?.call("kill_dev_server", ["pid": s.pid]); error = nil } catch { self.error = error.localizedDescription }
        await refresh()
    }
}

/// Every thread's sub-agents in this project, newest activity first (PWA: the Sub-agents surface).
struct ProjectSubagentsView: View {
    let workspaceId: Int
    @Environment(AppModel.self) private var model

    private var subagents: [Chat] {
        model.chats.filter { $0.workspaceId == workspaceId && $0.parentChatId != 0 && $0.kind != "side" && !$0.archived }
            .sorted { $0.lastActivityAt > $1.lastActivityAt }
    }

    var body: some View {
        List(subagents) { c in
            NavigationLink { ChatView(chatId: c.id).toolbar(.hidden, for: .tabBar) } label: {
                HStack(spacing: 10) {
                    StatusGlyph(status: model.status(c.id))
                    VStack(alignment: .leading, spacing: 2) {
                        Text(c.displayTitle).lineLimit(1)
                        if let parent = model.chat(c.parentChatId) {
                            Text("↳ \(parent.displayTitle)").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        if !c.recap.isEmpty { Text(c.recap).font(.caption).foregroundStyle(.secondary).lineLimit(2) }
                    }
                }
            }
        }
        .overlay {
            if subagents.isEmpty {
                ContentUnavailableView("No sub-agents", systemImage: "person.2", description: Text("Threads in this project have not spawned any."))
            }
        }
    }
}

/// iOS 18–26: the search tab's field is the TabView's (only then iOS 26 puts it in the bottom bar).
private struct TabSearchField: ViewModifier {
    @Binding var query: String
    func body(content: Content) -> some View {
        if #available(iOS 27, *) { content } else { content.searchable(text: $query, prompt: "Search chats") }
    }
}
