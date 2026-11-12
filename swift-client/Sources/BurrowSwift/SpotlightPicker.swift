import AppKit
import SwiftUI

// In-app project pickers inside Spotlight (PathPicker.vue + App.vue's openNewWorkspace). Every path is the CORE's:
// listings via read_dir_shallow, ~ via home_dir, folders via create_dir — this client may be driving another
// machine, where NSOpenPanel would browse the wrong disk.

enum SpotlightMode { case search, openProject, newProject }

extension Shell {
    /// Opens Spotlight in a mode from anywhere (menus, sidebar, the Settings window): main window to front first.
    func showSpotlight(_ mode: SpotlightMode) {
        spotlightMode = mode
        NSApp.activate(ignoringOtherApps: true)
        NSApp.windows.first { $0.identifier?.rawValue.hasPrefix("main") == true }?.makeKeyAndOrderFront(nil)
        spotlight = true
    }
}

/// Add an existing core-side folder as a project (create_workspace, like ws.create) — or open it if it already is
/// one — then show the new-thread view with it preselected.
@MainActor func addProject(path abs: String, _ b: Burrow) async throws {
    if let w = b.workspaces.values.first(where: { $0.s("path") == abs }) { newThread(in: w.i("id"), b); return }
    let name = abs.split(separator: "/").last.map(String.init) ?? abs
    let row = try await b.call("create_workspace", ["name": name, "path": abs]) as? JSON ?? [:]
    try await b.refresh()
    newThread(in: row.i("id"), b)
}

private struct PickerRow: View {
    let icon: String, title: String, subtitle: String, selected: Bool
    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: icon).foregroundStyle(icon == "folder.fill" ? Theme.accent : Theme.text2).frame(width: 18)
            Text(title).font(.brand(-1)).foregroundStyle(Theme.text).lineLimit(1).truncationMode(.middle)
            Text(subtitle).font(.ui(-3)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.middle)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 14).padding(.vertical, 7)
        .background(selected ? Theme.selected : .clear, in: RoundedRectangle(cornerRadius: 7))
        .padding(.horizontal, 6)
        .contentShape(Rectangle())
    }
}

private func keyHint(_ s: String) -> some View {
    Text(s).font(.brand(-4)).foregroundStyle(Theme.muted)
}

@MainActor private func defaultProjectDir() -> String {
    let d = ConfigStore.shared.ui.s("defaultProjectDir").trimmingCharacters(in: .whitespaces)
    return d.isEmpty ? "~/" : (d.hasSuffix("/") ? d : d + "/")
}

// MARK: - Open project

struct ProjectOpenPicker: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.dismiss) private var dismiss
    @State private var path = ""
    @State private var home = ""
    @State private var dirs: [String] = []
    @State private var missing = false
    @State private var error = ""
    @State private var index = 0
    @FocusState private var focused: Bool

    enum Item { case project(JSON), browse(String), open(String), up, dir(String) }

    var split: (dir: String, segment: String) { PathLogic.split(path) }

    var items: [Item] {
        if path.isEmpty {
            let starts = Array(Set([defaultProjectDir(), "~/"])).sorted().map(Item.browse)
            return starts + b.orderedProjects.map(Item.project)
        }
        let (dir, seg) = split
        guard !dir.isEmpty else { return [] }
        return (missing ? [] : [.open(dir)]) + [.up] + PathLogic.filter(dirs, segment: seg).map(Item.dir)
    }

    /// Return lands on the first folder, not on "Open": typing then ↵ descends, like a shell.
    var defaultIndex: Int {
        items.firstIndex { if case .dir = $0 { return true }; return false } ?? 0
    }

    var body: some View {
        let items = items
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "folder").foregroundStyle(Theme.accent)
                TextField("~/code/ — a folder on the Burrow core", text: $path)
                    .textFieldStyle(.plain).font(.brand(1))
                    .focused($focused)
                    .onSubmit { enter(items, add: false) }
                    .onKeyPress(.tab) { complete(items); return .handled }
                Button("") { enter(items, add: true) }.keyboardShortcut(.return, modifiers: .command)
                    .frame(width: 0, height: 0).opacity(0)
            }
            .padding(14)
            Divider().overlay(Theme.border)
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        Text(path.isEmpty ? "START IN · PROJECTS" : missing ? "NO SUCH FOLDER" : "FOLDERS")
                            .font(.brand(-4, weight: .bold)).foregroundStyle(Theme.muted)
                            .padding(.horizontal, 14).padding(.top, 10).padding(.bottom, 4)
                        ForEach(Array(items.enumerated()), id: \.offset) { i, item in
                            row(item, selected: i == index)
                                .onTapGesture { index = i; enter(items, add: false) }
                                .id(i)
                        }
                        if !path.isEmpty, items.count <= 2, !missing {
                            Text("no subfolders\(split.segment.isEmpty ? "" : " matching “\(split.segment)”")")
                                .font(.brand(-3)).foregroundStyle(Theme.muted).padding(.horizontal, 20).padding(.vertical, 6)
                        }
                    }
                    .padding(.bottom, 8)
                }
                .onChange(of: index) { proxy.scrollTo(index) }
            }
            Divider().overlay(Theme.border)
            HStack(spacing: 14) {
                keyHint("↑↓ select"); keyHint("⇥ / ↵ enter folder"); keyHint("⌘↵ open as project")
                Spacer()
                Text(error.isEmpty ? PathLogic.target(path, home: home) : error).font(.brand(-4))
                    .foregroundStyle(error.isEmpty ? Theme.muted : Theme.red).lineLimit(1).truncationMode(.head)
            }
            .padding(.horizontal, 14).padding(.vertical, 8)
            .background(Theme.base)
        }
        .frame(width: 600, height: 440)
        .background(Theme.panel)
        .onAppear { focused = true }
        .onKeyPress(.downArrow) { index = min(index + 1, max(items.count - 1, 0)); return .handled }
        .onKeyPress(.upArrow) { index = max(index - 1, 0); return .handled }
        .onKeyPress(.escape) { dismiss(); return .handled }
        .task {
            #if DEBUG
            PathLogic.selfCheck()
            #endif
            if home.isEmpty { home = (try? await b.call("home_dir")) as? String ?? "" }
            if ConfigStore.shared.values.isEmpty { await ConfigStore.shared.attach(b) }
        }
        .onChange(of: path) { _, p in
            if p == "~" { path = "~/"; return }
            error = ""
            index = defaultIndex
        }
        .task(id: "\(split.dir)|\(home)") { await load(split.dir) }
    }

    @ViewBuilder func row(_ item: Item, selected: Bool) -> some View {
        switch item {
        case .project(let w): PickerRow(icon: "shippingbox", title: w.s("name"), subtitle: w.s("path"), selected: selected)
        case .browse(let d): PickerRow(icon: "arrow.right.circle", title: d, subtitle: "browse", selected: selected)
        case .open(let d): PickerRow(icon: "plus.rectangle.on.folder", title: "Open \(d) as project", subtitle: "⌘↵", selected: selected)
        case .up: PickerRow(icon: "arrow.uturn.left", title: "..", subtitle: "", selected: selected)
        case .dir(let n): PickerRow(icon: "folder.fill", title: n, subtitle: "", selected: selected)
        }
    }

    func load(_ dir: String) async {
        guard !dir.isEmpty, dir.hasPrefix("/") || dir.hasPrefix("~") else { dirs = []; missing = false; return }
        if dir.hasPrefix("~"), home.isEmpty { return } // wait for home_dir; the task re-runs when it lands
        do {
            let list = try await b.call("read_dir_shallow", ["path": PathLogic.expand(dir, home: home)]) as? [JSON] ?? []
            guard !Task.isCancelled else { return }
            dirs = list.filter { ($0["isDir"] as? Bool ?? $0["is_dir"] as? Bool) == true }.map { $0.s("name") }
            missing = false
        } catch {
            guard !Task.isCancelled else { return }
            dirs = []; missing = true; self.error = error.localizedDescription
        }
        index = defaultIndex
    }

    func descend(into name: String) {
        let dir = split.dir
        path = name == ".." ? PathLogic.parent(dir, home: home) : dir + name + "/"
    }

    /// ⇥: complete the highlighted folder; on an empty field jump to the default project folder.
    func complete(_ items: [Item]) {
        guard items.indices.contains(index) else { return }
        switch items[index] {
        case .dir(let n): descend(into: n)
        case .up: descend(into: "..")
        case .browse(let d): path = d
        default: break
        }
    }

    /// ↵ walks (folder → into it, project → open it); ⌘↵ adds the highlighted folder, or the listed one.
    func enter(_ items: [Item], add: Bool) {
        guard items.indices.contains(index) else {
            if add, !path.isEmpty, !missing { commit(PathLogic.target(split.dir, home: home)) }
            return
        }
        switch items[index] {
        case .project(let w): dismiss(); b.openProject(w)
        case .browse(let d): path = d
        case .open(let d): commit(PathLogic.target(d, home: home))
        case .up: add ? commit(PathLogic.target(split.dir, home: home)) : descend(into: "..")
        case .dir(let n): add ? commit(PathLogic.target(split.dir + n, home: home)) : descend(into: n)
        }
    }

    func commit(_ abs: String) {
        guard abs.hasPrefix("/") else { error = "needs an absolute or ~/ path"; return }
        Task {
            do { try await addProject(path: abs, b); dismiss() } catch { self.error = "Add project: \(error.localizedDescription)" }
        }
    }
}

// MARK: - New project

struct NewProjectPicker: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var parent = ""
    @AppStorage("newProject.gitInit") private var gitInit = true
    @State private var home = ""
    @State private var error = ""
    @State private var busy = false
    @FocusState private var focus: Field?
    enum Field { case name, parent }

    var target: String { PathLogic.target(PathLogic.join(parent.isEmpty ? "~/" : parent, name.trimmingCharacters(in: .whitespaces)), home: home) }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "folder.badge.plus").foregroundStyle(Theme.accent)
                TextField("New project name", text: $name)
                    .textFieldStyle(.plain).font(.ui(2))
                    .focused($focus, equals: .name)
                    .onSubmit(create)
            }
            .padding(14)
            Divider().overlay(Theme.border)
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 10) {
                    Text("IN").font(.brand(-4, weight: .bold)).foregroundStyle(Theme.muted).frame(width: 40, alignment: .leading)
                    TextField("~/code/", text: $parent)
                        .textFieldStyle(.plain).font(.brand(-1))
                        .padding(.horizontal, 10).padding(.vertical, 6)
                        .background(Theme.base, in: RoundedRectangle(cornerRadius: 7))
                        .overlay(RoundedRectangle(cornerRadius: 7).stroke(Theme.border))
                        .focused($focus, equals: .parent)
                        .onSubmit(create)
                }
                HStack(spacing: 10) {
                    Text("GIT").font(.brand(-4, weight: .bold)).foregroundStyle(Theme.muted).frame(width: 40, alignment: .leading)
                    Toggle("Run git init", isOn: $gitInit).toggleStyle(.checkbox).font(.ui(-1))
                        .keyboardShortcut("i", modifiers: .command)
                    keyHint("⌘I")
                }
                Text(PathLogic.validName(name) ? target : "folder name: one path component")
                    .font(.brand(-3)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.head)
                if !error.isEmpty { Text(error).font(.ui(-2)).foregroundStyle(Theme.red) }
                Spacer(minLength: 0)
                HStack {
                    if busy { Pet(state: "running", size: 28) }
                    Spacer()
                    Button("Create project") { create() }
                        .keyboardShortcut(.return, modifiers: .command)
                        .disabled(!PathLogic.validName(name) || busy)
                }
            }
            .padding(14)
            Divider().overlay(Theme.border)
            HStack(spacing: 14) {
                keyHint("⇥ next field"); keyHint("↵ / ⌘↵ create"); keyHint("⌘I git init"); keyHint("esc close")
                Spacer()
            }
            .padding(.horizontal, 14).padding(.vertical, 8)
            .background(Theme.base)
        }
        .frame(width: 600, height: 300)
        .background(Theme.panel)
        .onAppear { focus = .name; parent = defaultProjectDir() }
        .onKeyPress(.escape) { dismiss(); return .handled }
        .task {
            home = (try? await b.call("home_dir")) as? String ?? ""
            if ConfigStore.shared.values.isEmpty { await ConfigStore.shared.attach(b); parent = defaultProjectDir() }
        }
    }

    /// workspace_create's create + git_init semantics over the wire commands (the verb itself is local-only):
    /// refuse a non-empty existing folder, create_dir, git init unless .git exists, create_workspace, open.
    func create() {
        guard PathLogic.validName(name), !busy else { return }
        guard !home.isEmpty || !parent.hasPrefix("~") else { error = "still resolving ~ on the core"; return }
        let abs = target
        guard abs.hasPrefix("/") else { error = "parent folder needs an absolute or ~/ path"; return }
        busy = true; error = ""
        Task {
            defer { busy = false }
            do {
                let existing = (try? await b.call("read_dir_shallow", ["path": abs])) as? [JSON]
                if let existing, !existing.isEmpty {
                    error = "\(abs) already exists and is not empty — use Open project to add it"; return
                }
                try await b.call("create_dir", ["path": abs])
                if gitInit {
                    let out = try await b.call("run_git", ["cwd": abs, "args": ["init"]]) as? JSON ?? [:]
                    if out.i("code") != 0 { error = "git init: \(out.s("stderr").trimmingCharacters(in: .whitespacesAndNewlines))"; return }
                }
                try await addProject(path: abs, b)
                dismiss()
            } catch { self.error = error.localizedDescription }
        }
    }
}
