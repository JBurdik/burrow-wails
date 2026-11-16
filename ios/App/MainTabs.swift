import SwiftUI

/// The phone's bottom navigation (PWA: BottomNav.vue): Threads / Project / More.
/// A plain TabView: built with the iOS 26 SDK its bar is Liquid Glass by itself.
struct MainTabs: View {
    @Environment(AppModel.self) private var model
    @State private var tab = Tab.threads

    enum Tab: Hashable { case threads, project, more }

    var body: some View {
        TabView(selection: $tab) {
            ThreadsView()
                .tabItem { Label("Threads", systemImage: "bubble.left.and.bubble.right") }
                .badge(model.threads.filter { model.needsYou($0) }.count)
                .tag(Tab.threads)
            ProjectView()
                .tabItem { Label("Project", systemImage: "arrow.triangle.branch") }
                .tag(Tab.project)
            MoreView()
                .tabItem { Label("More", systemImage: "ellipsis") }
                .tag(Tab.more)
        }
    }
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
                        Menu {
                            ForEach(rows) { r in
                                Button { selected = r.id } label: {
                                    Label((r.depth > 0 ? "  " : "") + r.name, systemImage: r.id == cur.id ? "checkmark" : (r.depth > 0 ? "arrow.turn.down.right" : "folder"))
                                }
                            }
                        } label: {
                            HStack {
                                ProjectBadge(name: cur.name, workspaceId: cur.id)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(cur.name).font(.headline).foregroundStyle(.primary)
                                    Text(cur.path).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.head)
                                }
                                Spacer()
                                Image(systemName: "chevron.up.chevron.down").font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                    Section {
                        NavigationLink { GitSurface(cwdOverride: cur.path).navigationTitle("Changes").navigationBarTitleDisplayMode(.inline) } label: {
                            Label("Changes", systemImage: "plusminus.circle")
                        }
                        NavigationLink { PullRequestsView(cwd: cur.path).navigationTitle("Pull requests").navigationBarTitleDisplayMode(.inline) } label: {
                            Label("Pull requests", systemImage: "arrow.triangle.pull")
                        }
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
