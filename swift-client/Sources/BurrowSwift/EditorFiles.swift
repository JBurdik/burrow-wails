import AppKit
import SwiftUI

// OWNER: editor agent (wave 2). Port of FileTreeNode.vue + stores/fileTree.ts. Replaces RightPanel's FilesView.
// File clicks go through openFileInRP — the RP decides where the editor shows.
// Not here: rename / delete (no backend command for either), git status colours (the Vue tree has none).

/// The Files tab: a lazily-loaded tree rooted at `root`.
struct EditorFilesView: View {
    @EnvironmentObject var b: Burrow
    let root: String
    @StateObject private var tree = FileTreeModel()

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Text((root as NSString).lastPathComponent).font(.brand(-4, weight: .semibold)).foregroundStyle(Theme.text2).lineLimit(1)
                Spacer()
                iconButton("doc.badge.plus", "New file") { tree.prompt = .file(root) }
                iconButton("folder.badge.plus", "New folder") { tree.prompt = .folder(root) }
                iconButton("arrow.clockwise", "Refresh") { Task { await tree.refreshAll(b) } }
            }
            .padding(.horizontal, 12).padding(.vertical, 6)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
            ScrollView {
                // Not Lazy, and loads are kicked from actions / this always-present stack — a .task on an
                // empty ForEach (or a lazy stack's zero-height root) never fires.
                VStack(alignment: .leading, spacing: 0) {
                    if let err = tree.errors[root] {
                        Text(err).font(.brand(-5)).foregroundStyle(Theme.red).padding(12)
                    }
                    FileTreeChildren(dir: root, depth: 0, tree: tree)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.vertical, 4)
            }
            .task(id: root) { tree.root = root; await tree.load(b, root) }
        }
        .sheet(item: $tree.prompt) { p in NewEntrySheet(prompt: p, tree: tree) }
    }

    func iconButton(_ icon: String, _ help: String, _ action: @escaping () -> Void) -> some View {
        Button(action: action) { Image(systemName: icon).font(.system(size: 11)).foregroundStyle(Theme.muted) }
            .buttonStyle(.plain).help(help)
    }
}

@MainActor final class FileTreeModel: ObservableObject {
    struct Entry: Hashable { let name: String; let isDir: Bool }
    enum Prompt: Identifiable { case file(String), folder(String)
        var id: String { switch self { case .file(let d): "f" + d; case .folder(let d): "d" + d } }
        var dir: String { switch self { case .file(let d), .folder(let d): d } }
    }

    @Published var children: [String: [Entry]] = [:]
    @Published var expanded: Set<String> = []
    @Published var loading: Set<String> = []
    @Published var errors: [String: String] = [:]
    @Published var selected = ""
    @Published var prompt: Prompt?
    var root = ""

    static let hidden: Set<String> = [".git", "node_modules", "target", ".DS_Store", ".build"]

    nonisolated static func sorted(_ raw: [JSON]) -> [Entry] {
        raw.filter { !hidden.contains($0.s("name")) }
            .map { Entry(name: $0.s("name"), isDir: $0["isDir"] as? Bool == true) }
            .sorted { $0.isDir != $1.isDir ? $0.isDir : $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }

    func load(_ b: Burrow, _ dir: String, force: Bool = false) async {
        if !force, children[dir] != nil { return }
        loading.insert(dir); defer { loading.remove(dir) }
        do {
            children[dir] = Self.sorted(try await b.call("read_dir_shallow", ["path": dir]) as? [JSON] ?? [])
            errors[dir] = nil
        } catch {
            // ponytail (fileTree.ts): surface the failure — "expanded, 0 children" looked like an empty folder.
            errors[dir] = error.localizedDescription
        }
    }

    func toggle(_ b: Burrow, _ dir: String) {
        if expanded.remove(dir) == nil {
            expanded.insert(dir)
            Task { await load(b, dir) }
        }
    }

    /// Re-read the root and every open folder (new files from agents, deletions).
    func refreshAll(_ b: Burrow) async {
        for d in [root] + expanded.sorted() { await load(b, d, force: true) }
    }
}

private struct FileTreeChildren: View {
    @EnvironmentObject var b: Burrow
    let dir: String
    let depth: Int
    @ObservedObject var tree: FileTreeModel

    var body: some View {
        ForEach(tree.children[dir] ?? [], id: \.self) { e in
            let path = dir + "/" + e.name
            FileTreeRow(path: path, entry: e, depth: depth, tree: tree)
            if e.isDir, tree.expanded.contains(path) {
                if let err = tree.errors[path] {
                    Text(err).font(.brand(-6)).foregroundStyle(Theme.red).lineLimit(2)
                        .padding(.leading, CGFloat(depth + 1) * 12 + 24).padding(.vertical, 2)
                }
                FileTreeChildren(dir: path, depth: depth + 1, tree: tree)
            }
        }
    }
}

private struct FileTreeRow: View {
    @EnvironmentObject var b: Burrow
    let path: String
    let entry: FileTreeModel.Entry
    let depth: Int
    @ObservedObject var tree: FileTreeModel
    @State private var hover = false

    var body: some View {
        let open = tree.expanded.contains(path)
        HStack(spacing: 5) {
            Group {
                if tree.loading.contains(path) { ProgressView().controlSize(.mini).scaleEffect(0.6) }
                else if entry.isDir { Image(systemName: open ? "chevron.down" : "chevron.right").font(.system(size: 8, weight: .bold)) }
                else { Color.clear }
            }
            .frame(width: 10).foregroundStyle(Theme.muted)
            let icon = entry.isDir ? (open ? "folder.fill" : "folder.fill", Theme.accent) : Self.icon(entry.name)
            Image(systemName: icon.0).font(.system(size: 11)).foregroundStyle(icon.1).frame(width: 14)
            Text(entry.name).font(.brand(-4)).foregroundStyle(entry.name.hasPrefix(".") ? Theme.muted : Theme.text)
                .lineLimit(1).truncationMode(.middle)
            Spacer(minLength: 0)
        }
        .padding(.leading, CGFloat(depth) * 12 + 8).padding(.trailing, 8)
        .frame(height: 22)
        .background(RoundedRectangle(cornerRadius: 4).fill(tree.selected == path ? Theme.selected : hover ? Theme.hover : .clear))
        .padding(.horizontal, 4)
        .contentShape(Rectangle())
        .onHover { hover = $0 }
        .onTapGesture {
            tree.selected = path
            if entry.isDir { tree.toggle(b, path) } else { openFileInRP(path) }
        }
        .contextMenu {
            if !entry.isDir { Button("Open") { openFileInRP(path) } }
            Button("Open in default app") { openIn("editor") }
            Button("Reveal in Finder") { openIn("finder") }
            Divider()
            Button("Copy path") { copy(path) }
            Button("Copy relative path") { copy(String(path.dropFirst(tree.root.count + 1))) }
            Divider()
            let dir = entry.isDir ? path : (path as NSString).deletingLastPathComponent
            Button("New file…") { tree.prompt = .file(dir) }
            Button("New folder…") { tree.prompt = .folder(dir) }
        }
        .help(path)
    }

    func openIn(_ target: String) { Task { try? await b.call("open_path_in", ["path": path, "target": target, "line": 0]) } }
    func copy(_ s: String) { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(s, forType: .string) }

    /// FileTreeNode.vue's extension → icon/colour map, in SF Symbols + theme colours.
    static func icon(_ name: String) -> (String, Color) {
        switch (name as NSString).pathExtension.lowercased() {
        case "ts", "tsx", "mts", "js", "jsx", "mjs", "cjs", "vue", "go", "rs", "py", "swift", "c", "h", "cpp", "java", "kt", "rb", "sh":
            ("chevron.left.forwardslash.chevron.right", Theme.accent)
        case "json", "jsonc", "yaml", "yml", "toml": ("curlybraces", Theme.yellow)
        case "md", "mdx", "txt": ("doc.text", Theme.green)
        case "png", "jpg", "jpeg", "gif", "webp", "ico", "svg", "heic": ("photo", Theme.red)
        case "zip", "gz", "tar", "tgz": ("doc.zipper", Theme.yellow)
        case "lock": ("lock", Theme.muted)
        default: ("doc", Theme.muted)
        }
    }
}

/// New file / folder name prompt. Refuses names that already exist — write_text_file would overwrite.
private struct NewEntrySheet: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.dismiss) var dismiss
    let prompt: FileTreeModel.Prompt
    @ObservedObject var tree: FileTreeModel
    @State private var name = ""
    @State private var error = ""

    var isFile: Bool { if case .file = prompt { true } else { false } }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(isFile ? "New file" : "New folder").font(.brand(-1, weight: .semibold)).foregroundStyle(Theme.text)
            Text(prompt.dir).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.head)
            TextField(isFile ? "name.ext (a/b.ts makes folders)" : "folder name", text: $name)
                .textFieldStyle(.roundedBorder).font(.brand(-3)).onSubmit { Task { await create() } }
            if !error.isEmpty { Text(error).font(.brand(-5)).foregroundStyle(Theme.red) }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Create") { Task { await create() } }.keyboardShortcut(.defaultAction)
                    .disabled(FileTreeModel.validName(name) != nil)
            }
        }
        .padding(16).frame(width: 340).background(Theme.panel)
    }

    func create() async {
        if let why = FileTreeModel.validName(name) { error = why; return }
        let path = prompt.dir + "/" + name
        let parent = (path as NSString).deletingLastPathComponent
        do {
            // Fresh listing of the target folder: never overwrite something that appeared since the tree loaded.
            let existing = (try? await b.call("read_dir_shallow", ["path": parent]) as? [JSON]) ?? []
            if existing.contains(where: { $0.s("name") == (path as NSString).lastPathComponent }) { error = "Already exists"; return }
            if isFile {
                if parent != prompt.dir { try await b.call("create_dir", ["path": parent]) }
                try await b.call("write_text_file", ["path": path, "content": ""])
            } else {
                try await b.call("create_dir", ["path": path])
            }
        } catch { self.error = error.localizedDescription; return }
        tree.expanded.insert(prompt.dir)
        await tree.load(b, prompt.dir, force: true)
        tree.selected = path
        dismiss()
        if isFile { openFileInRP(path) }
    }
}

extension FileTreeModel {
    /// nil = ok. Relative only: no leading "/", no "." / ".." segments.
    nonisolated static func validName(_ n: String) -> String? {
        let segs = n.split(separator: "/", omittingEmptySubsequences: false)
        if n.trimmingCharacters(in: .whitespaces).isEmpty { return "Name required" }
        if n.hasPrefix("/") || segs.contains(where: { $0.isEmpty || $0 == "." || $0 == ".." }) { return "Invalid name" }
        return nil
    }
}
