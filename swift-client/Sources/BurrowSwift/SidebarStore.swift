import AppKit
import SwiftUI

/// Sidebar state that isn't on a chat row. Project pins / archive / manual order and the last "Open in" target are
/// config.json keys shared with the desktop (pinnedWorkspaces.ts, archivedWorkspaces.ts, projectOrder.ts, openIn.ts);
/// collapsed groups and shelf expansion are per-window (UserDefaults, like Vue's localStorage).
@MainActor
final class SidebarStore: ObservableObject {
    static let shared = SidebarStore()

    @Published var pinnedProjects: [Int64] = []
    @Published var archivedProjects: [Int64] = []
    @Published var projectOrder: [Int64] = []
    @Published var lastOpenIn: [String: String] = [:]
    @Published var collapsed: Set<Int64> = Set((UserDefaults.standard.array(forKey: "sidebar.collapsedRepos") as? [NSNumber] ?? []).map(\.int64Value)) {
        didSet { UserDefaults.standard.set(collapsed.map { NSNumber(value: $0) }, forKey: "sidebar.collapsedRepos") }
    }

    /// pendingDeletes.ts: rows hidden at once, the real delete runs when the undo window expires.
    @Published private(set) var hidden: Set<Int64> = []

    // Project dialogs (mounted once by the Sidebar, `sidebarDialogs()`).
    @Published var newWorktree: JSON?
    @Published var renamingProject: JSON?
    @Published var deletingProject: JSON?

    private var started = false
    private weak var b: Burrow?

    func start(_ b: Burrow) {
        guard !started else { return }
        started = true
        self.b = b
        #if DEBUG
        sidebarSelfCheck()
        #endif
        b.subscribe("config-changed") { [weak self] _, p in
            guard let self, let p = p as? JSON else { return }
            self.apply(p.s("key"), p["value"])
        }
        Task {
            let cfg = await b.readConfig()
            for k in ["sidebarPinned", "sidebarArchived", "sidebarProjectOrder", "titlebarLastOpenInByProject"] { apply(k, cfg[k]) }
        }
        // ⌘B toggles the sidebar (keymap.ts). A local monitor, so it works whatever has focus.
        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { e in
            guard e.modifierFlags.intersection(.deviceIndependentFlagsMask) == .command, e.charactersIgnoringModifiers == "b" else { return e }
            NSApp.sendAction(#selector(NSSplitViewController.toggleSidebar(_:)), to: nil, from: nil)
            return nil
        }
    }

    private func apply(_ key: String, _ v: Any?) {
        switch key {
        case "sidebarPinned": pinnedProjects = ThreadStore.ids(v)
        case "sidebarArchived": archivedProjects = ThreadStore.ids(v)
        case "sidebarProjectOrder": projectOrder = ThreadStore.ids(v)
        case "titlebarLastOpenInByProject": lastOpenIn = v as? [String: String] ?? [:]
        default: break
        }
    }

    private func save(_ key: String, _ value: Any) {
        guard let b else { return }
        Task { try? await b.call("config_set", ["key": key, "value": value]) }
    }

    func togglePinProject(_ id: Int64) {
        pinnedProjects = pinnedProjects.contains(id) ? pinnedProjects.filter { $0 != id } : Array((pinnedProjects + [id]).suffix(10))
        save("sidebarPinned", pinnedProjects)
    }

    func toggleArchiveProject(_ id: Int64) {
        archivedProjects = archivedProjects.contains(id) ? archivedProjects.filter { $0 != id } : archivedProjects + [id]
        save("sidebarArchived", archivedProjects)
    }

    /// moveProject: `shown` = the draggable groups as on screen; repos not on screen keep their old relative order.
    func moveProject(shown: [Int64], drag: Int64, before target: Int64) {
        let next = reorderProjects(shown, drag: drag, target: target)
        projectOrder = next + projectOrder.filter { !next.contains($0) }
        save("sidebarProjectOrder", projectOrder)
    }

    func toggleCollapsed(_ id: Int64) {
        if collapsed.contains(id) { collapsed.remove(id) } else { collapsed.insert(id) }
    }

    /// Group order: pinned projects (pin order), then the manual order, then sort_order / id like the old feed.
    func orderRepos(_ ids: [Int64], _ b: Burrow) -> [Int64] {
        ids.sorted { l, r in
            let lp = pinnedProjects.firstIndex(of: l) ?? .max, rp = pinnedProjects.firstIndex(of: r) ?? .max
            if lp != rp { return lp < rp }
            let lo = projectOrder.firstIndex(of: l) ?? .max, ro = projectOrder.firstIndex(of: r) ?? .max
            if lo != ro { return lo < ro }
            let a = b.workspaces[l]?.i("sort_order") ?? 0, c = b.workspaces[r]?.i("sort_order") ?? 0
            return a != c ? a < c : l < r
        }
    }

    // MARK: delete with undo

    /// deleteChatsWithUndo (Sidebar.vue): no confirm; hide now, delete after 8 s unless Undo.
    func deleteWithUndo(_ rows: [JSON], _ b: Burrow) {
        guard !rows.isEmpty else { return }
        let ids = Set(rows.map { $0.i("id") })
        if let sel = b.selectedChat, ids.contains(sel) { b.selectedChat = nil }
        hidden.formUnion(ids)
        let title = rows.count == 1 ? "Deleted “\(rows[0].s("title").isEmpty ? "Untitled" : rows[0].s("title"))”" : "Deleted \(rows.count) threads"
        let done = Flag()
        let release = { [weak self] in _ = self?.hidden.subtract(ids) }
        ToastCenter.shared.push(title, kind: .info, action: ("Undo", { if !done.on { done.on = true; release() } }), duration: 8)
        Task {
            try? await Task.sleep(for: .seconds(8))
            guard !done.on else { return }
            done.on = true // Undo is a no-op once the commit started
            for c in rows { await b.deleteThread(c) }
            release() // unhide only after the commit, or rows flash back
        }
    }

    final class Flag { var on = false }

    func isHidden(_ id: Int64) -> Bool { hidden.contains(id) }

    // MARK: open in (openIn.ts)

    func lastTarget(_ path: String) -> String { lastOpenIn[path] ?? "finder" }

    func open(_ path: String, in target: String, remember: Bool = true) {
        guard let b else { return }
        if remember {
            lastOpenIn[path] = target
            save("titlebarLastOpenInByProject", lastOpenIn)
        }
        Task { await b.act("Open") { try await b.call("open_path_in", ["path": path, "target": target, "line": 0]) } }
    }
}

/// projectOrder.ts reorderProjects: drop `drag` before `target`.
func reorderProjects(_ shown: [Int64], drag: Int64, target: Int64) -> [Int64] {
    guard drag != target else { return shown }
    var rest = shown.filter { $0 != drag }
    guard let at = rest.firstIndex(of: target) else { return shown }
    rest.insert(drag, at: at)
    return rest
}

func sidebarSelfCheck() {
    assert(reorderProjects([1, 2, 3], drag: 3, target: 1) == [3, 1, 2])
    assert(reorderProjects([1, 2, 3], drag: 1, target: 3) == [2, 1, 3])
    assert(reorderProjects([1, 2, 3], drag: 2, target: 2) == [1, 2, 3])
    assert(reorderProjects([1, 2], drag: 1, target: 9) == [1, 2])
    assert(parseNumstat("3\t1\ta.swift\n-\t-\tbin.png\n") == [FileStat(path: "a.swift", added: 3, removed: 1), FileStat(path: "bin.png", added: 0, removed: 0)])
}

// MARK: - Project menu + dialogs

/// The project header's (and a worktree row's) context menu — Sidebar.vue's ws half of the row menu.
struct ProjectMenuItems: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var store = SidebarStore.shared
    let ws: JSON

    var body: some View {
        let id = ws.i("id"), repo = ws["parent_id"] == nil
        Button("New chat") { newThread(in: id, b) }
        Button("New terminal") { Task { await TerminalStore.shared.newTab(workspaceID: id) } }
        if repo { Button("New worktree…") { store.newWorktree = ws } }
        Divider()
        if repo {
            Button("Rename project…") { store.renamingProject = ws }
            Button(store.pinnedProjects.contains(id) ? "Unpin project" : "Pin project to top") { store.togglePinProject(id) }
        }
        Button(store.archivedProjects.contains(id) ? "Unarchive" : "Archive") { store.toggleArchiveProject(id) }
        Divider()
        Button("Delete…", role: .destructive) { store.deletingProject = ws }
    }
}

/// The welcome composer with this project preselected (NewThreadView reads newThread.workspace).
@MainActor func newThread(in wsID: Int64, _ b: Burrow) {
    UserDefaults.standard.set(Int(b.rootWorkspace(wsID).i("id")), forKey: "newThread.workspace")
    b.selectedChat = nil; b.selectedTab = nil
    // ponytail: a worktree id isn't in NewThreadView's repo picker, so it falls back to the repo — the composer's
    // "New worktree" toggle is the way to start in one.
}

struct SidebarDialogs: ViewModifier {
    @EnvironmentObject var b: Burrow
    @ObservedObject var store = SidebarStore.shared
    @State private var name = ""

    func body(content: Content) -> some View {
        content
            .sheet(item: Binding(get: { store.newWorktree.map(IdentifiedJSON.init) }, set: { store.newWorktree = $0?.json })) { w in
                NewWorktreeSheet(parent: w.json).environmentObject(b)
            }
            .alert("Rename project", isPresented: shown(\.renamingProject)) {
                TextField("Name", text: $name)
                Button("Rename") {
                    let ws = store.renamingProject, n = name.trimmingCharacters(in: .whitespaces)
                    if let ws, !n.isEmpty { Task { await b.act("Rename") { try await b.call("rename_workspace", ["id": ws.i("id"), "name": n]) } } }
                }
                Button("Cancel", role: .cancel) {}
            }
            .confirmationDialog(deleteTitle, isPresented: shown(\.deletingProject)) {
                if let ws = store.deletingProject {
                    if ws["parent_id"] != nil {
                        Button("Delete worktree", role: .destructive) { Task { await deleteProject(ws, force: false) } }
                        Button("Delete, discarding uncommitted changes", role: .destructive) { Task { await deleteProject(ws, force: true) } }
                    } else {
                        Button("Delete project", role: .destructive) { Task { await deleteProject(ws, force: false) } }
                    }
                }
            } message: { Text(deleteMessage) }
            .onChange(of: store.renamingProject?.i("id")) { name = store.renamingProject?.s("name") ?? "" }
            .task { store.start(b) }
    }

    var deleteTitle: String {
        guard let ws = store.deletingProject else { return "" }
        return "Delete \(ws["parent_id"] != nil ? "worktree" : "project") “\(ws.s("worktree_branch").isEmpty ? ws.s("name") : ws.s("worktree_branch"))”?"
    }

    var deleteMessage: String {
        guard let ws = store.deletingProject else { return "" }
        if ws["parent_id"] != nil { return "Runs git worktree remove — the directory on disk is deleted." }
        let n = worktrees(of: ws.i("id")).count
        return "Removes it from Burrow" + (n > 0 ? " and deletes its \(n) worktree director\(n == 1 ? "y" : "ies") from disk" : "")
            + ". Your project folder itself stays untouched."
    }

    func worktrees(of id: Int64) -> [JSON] { b.workspaces.values.filter { ($0["parent_id"] as? NSNumber)?.int64Value == id } }

    /// workspace store remove / removeWorktree: a repo's worktrees go first (best effort), then the row.
    func deleteProject(_ ws: JSON, force: Bool) async {
        let id = ws.i("id")
        if b.chat(b.selectedChat).map({ b.rootWorkspace($0.i("workspace_id")).i("id") == id || $0.i("workspace_id") == id }) == true { b.selectedChat = nil }
        await b.act("Delete") {
            if ws["parent_id"] != nil {
                try await b.call("remove_worktree", ["id": id, "force": force])
            } else {
                for wt in worktrees(of: id) { _ = try? await b.call("remove_worktree", ["id": wt.i("id"), "force": false]) }
                try await b.call("delete_workspace", ["id": id])
            }
        }
        if store.pinnedProjects.contains(id) { store.togglePinProject(id) }
        if store.archivedProjects.contains(id) { store.toggleArchiveProject(id) }
    }

    func shown(_ kp: ReferenceWritableKeyPath<SidebarStore, JSON?>) -> Binding<Bool> {
        Binding(get: { store[keyPath: kp] != nil }, set: { if !$0 { store[keyPath: kp] = nil } })
    }
}

struct IdentifiedJSON: Identifiable {
    let json: JSON
    var id: Int64 { json.i("id") }
}

extension View {
    func sidebarDialogs() -> some View { modifier(SidebarDialogs()) }
}

/// Sidebar.vue's "New worktree" dialog: branch + base (local + remote branches, default = origin/HEAD), path preview.
struct NewWorktreeSheet: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.dismiss) private var dismiss
    let parent: JSON
    @State private var branch = ""
    @State private var base = ""
    @State private var branches: [String] = []
    @State private var path = ""
    @State private var error = ""
    @State private var busy = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("New worktree — \(parent.s("name"))").font(.brand(0, weight: .bold)).foregroundStyle(Theme.text)
            Text("Branch").font(.brand(-4, weight: .bold)).foregroundStyle(Theme.text2)
            TextField("feature/my-branch", text: $branch).textFieldStyle(.roundedBorder).onSubmit(create)
            Text("Base branch (only for a new branch)").font(.brand(-4, weight: .bold)).foregroundStyle(Theme.text2)
            HStack {
                TextField("Empty = current HEAD", text: $base).textFieldStyle(.roundedBorder).onSubmit(create)
                Menu {
                    ForEach(branches.filter { base.isEmpty || branches.contains(base) || $0.localizedCaseInsensitiveContains(base) }.prefix(60), id: \.self) { br in
                        Button(br) { base = br }
                    }
                } label: { Image(systemName: "chevron.down") }
                    .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
            }
            Text(path).font(.brand(-5)).foregroundStyle(Theme.text2).lineLimit(1).truncationMode(.middle)
                .padding(6).frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.panel, in: RoundedRectangle(cornerRadius: 6))
            if !error.isEmpty { Text(error).font(.ui(-2)).foregroundStyle(Theme.red).textSelection(.enabled) }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button(busy ? "Creating…" : "Create", action: create).keyboardShortcut(.defaultAction)
                    .disabled(branch.trimmingCharacters(in: .whitespaces).isEmpty || busy)
            }
        }
        .padding(20).frame(width: 420)
        .background(Theme.base)
        .task { await loadBranches() }
        .task(id: branch) { path = await b.worktreePath(parent, branch: branch.trimmingCharacters(in: .whitespaces).isEmpty ? "<branch>" : branch.trimmingCharacters(in: .whitespaces)) }
    }

    func loadBranches() async {
        let cwd = parent.s("path")
        let refs = (try? await b.git(cwd, ["for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes"]))?.stdout ?? ""
        let def = (try? await b.git(cwd, ["symbolic-ref", "--short", "refs/remotes/origin/HEAD"]))?.stdout.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let lines = refs.split(separator: "\n").map(String.init)
        let local = lines.filter { $0.hasPrefix("refs/heads/") }.map { String($0.dropFirst("refs/heads/".count)) }
        let remote = lines.filter { $0.hasPrefix("refs/remotes/") && !$0.hasSuffix("/HEAD") }.map { String($0.dropFirst("refs/remotes/".count)) }
        branches = local + remote
        let d = def.isEmpty ? (["main", "master"].first { local.contains($0) } ?? "") : String(def.replacingOccurrences(of: "origin/", with: ""))
        if base.isEmpty, branches.contains(d) { base = d }
    }

    func create() {
        let br = branch.trimmingCharacters(in: .whitespaces)
        guard !br.isEmpty, !busy else { return }
        busy = true; error = ""
        Task {
            defer { busy = false }
            do {
                let p = await b.worktreePath(parent, branch: br)
                let wt = try await b.call("create_worktree", [
                    "repoPath": parent.s("path"), "name": br, "path": p, "branch": br, "baseRef": base.trimmingCharacters(in: .whitespaces),
                ]) as? JSON
                try? await b.refresh()
                dismiss()
                // Vue opens the new workspace; here a shell tab in it is what makes it show up (and usable) in the sidebar.
                if let wt { await TerminalStore.shared.newTab(workspaceID: wt.i("id")) }
            } catch { self.error = error.localizedDescription }
        }
    }
}
