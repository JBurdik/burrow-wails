import AppKit
import SwiftUI

// Git window sidebar (src/components/git/GitSidebar.vue): workspace picker, branches as a `/` tree, worktrees,
// remotes, stashes, tags — each with its context menu. Reads `git.refs/worktrees/stashes` (GitModel, full mode).

/// Each repo followed by its worktrees, like GitPanel's picker.
struct GitWorkspacePicker: View {
    @EnvironmentObject var b: Burrow
    @Binding var wsID: Int64?

    var list: [(id: Int64, label: String)] {
        b.projects.flatMap { p -> [(Int64, String)] in
            let wts = b.workspaces.values.filter { ($0["parent_id"] as? NSNumber)?.int64Value == p.i("id") }
                .sorted { $0.s("name") < $1.s("name") }
            return [(p.i("id"), p.s("name"))]
                + wts.map { ($0.i("id"), "\(p.s("name")) / \($0.s("worktree_branch").isEmpty ? $0.s("name") : $0.s("worktree_branch"))") }
        }
    }

    var body: some View {
        Picker("", selection: $wsID) {
            ForEach(list, id: \.id) { w in Text(w.label).tag(Optional(w.id)) }
        }
        .labelsHidden().font(.brand(-4))
    }
}

struct GitBranchSidebar: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var git: GitModel
    @Binding var wsID: Int64?
    @State private var query = ""
    @State private var collapsed: Set<String> = []
    /// Section open state; remotes and tags start closed (GitSidebar.vue `collapsed` prop).
    @State private var closed: Set<String> = ["Tags"]
    @State private var openedRemotes: Set<String> = []

    var q: String { query.trimmingCharacters(in: .whitespaces).lowercased() }
    func matches(_ r: GitRef) -> Bool { q.isEmpty || r.name.lowercased().contains(q) }
    var local: [GitRef] { git.refs.filter { $0.kind == .local } }
    var tags: [GitRef] { git.refs.filter { $0.kind == .tag }.reversed() }
    var remoteGroups: [(String, [GitRef])] {
        var g: [String: [GitRef]] = [:]
        for r in git.refs where r.kind == .remote && matches(r) { g[String(r.name.split(separator: "/").first ?? ""), default: []].append(r) }
        return g.sorted { $0.key < $1.key }
    }
    /// branch → path of the *other* worktree that has it checked out
    var wtOf: [String: String] {
        Dictionary(git.worktrees.compactMap { w in w.branch.flatMap { w.path != git.cwd ? ($0, w.path) : nil } }, uniquingKeysWith: { a, _ in a })
    }
    func wsByPath(_ p: String) -> Int64? { b.workspaces.first { $0.value.s("path") == p }?.key }
    func home(_ p: String) -> String { p.replacingOccurrences(of: NSHomeDirectory(), with: "~") }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            GitWorkspacePicker(wsID: $wsID).padding(.horizontal, 10).padding(.top, 10).padding(.bottom, 6)
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass").foregroundStyle(Theme.muted)
                TextField("Filter branches", text: $query).textFieldStyle(.plain)
            }
            .font(.brand(-4))
            .padding(.horizontal, 8).padding(.vertical, 5)
            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 7))
            .padding(.horizontal, 10)
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    branches
                    if git.worktrees.count > 1 { worktreesSection }
                    ForEach(remoteGroups, id: \.0) { remote, refs in remoteSection(remote, refs) }
                    stashSection
                    if !tags.isEmpty { tagSection }
                }
                .padding(.horizontal, 8).padding(.bottom, 12)
            }
        }
    }

    // MARK: sections

    func section<C: View, A: View>(_ title: String, _ count: Int, open: Bool, toggle: @escaping () -> Void,
                                   @ViewBuilder action: () -> A, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack(spacing: 5) {
                Button(action: toggle) {
                    HStack(spacing: 5) {
                        Image(systemName: open ? "chevron.down" : "chevron.right").font(.system(size: 8, weight: .bold)).frame(width: 10)
                        Text(title).lineLimit(1)
                        Text("\(count)").opacity(0.7)
                        Spacer(minLength: 0)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                action()
            }
            .font(.brand(-5, weight: .medium)).foregroundStyle(Theme.muted)
            .padding(.horizontal, 4).frame(height: 24)
            if open { content() }
        }
        .padding(.top, 10)
    }

    func toggleSection(_ t: String) { if closed.contains(t) { closed.remove(t) } else { closed.insert(t) } }

    func row<C: View>(active: Bool = false, depth: Int = 0, @ViewBuilder _ content: () -> C) -> some View {
        HStack(spacing: 7) { content() }
            .font(.brand(-4))
            .foregroundStyle(active ? Theme.text : Theme.text2)
            .padding(.leading, 8 + CGFloat(depth) * 14).padding(.trailing, 8)
            .frame(height: 26)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(active ? Theme.accent.opacity(0.15) : .clear, in: RoundedRectangle(cornerRadius: 6))
            .contentShape(Rectangle())
    }

    func folder(_ key: String, _ label: String, _ depth: Int, _ count: Int, _ open: Bool, ns: String) -> some View {
        row(depth: depth) {
            Image(systemName: open ? "chevron.down" : "chevron.right").font(.system(size: 8, weight: .bold)).foregroundStyle(Theme.muted).frame(width: 12)
            Text(label).lineLimit(1)
            Spacer(minLength: 0)
            Text("\(count)").font(.brand(-6)).foregroundStyle(Theme.muted)
        }
        .help(key)
        .onTapGesture { let k = ns + key; if collapsed.contains(k) { collapsed.remove(k) } else { collapsed.insert(k) } }
    }

    var branches: some View {
        let rows = branchTreeRows(local.filter(matches), name: { $0.name }, isCollapsed: { collapsed.contains("local:" + $0) }, expandAll: !q.isEmpty)
        return section("Branches", local.count, open: !closed.contains("Branches") || !q.isEmpty, toggle: { toggleSection("Branches") }) {
            Button { newBranch() } label: { Image(systemName: "plus") }.buttonStyle(.plain).help("New branch (⌘⇧B)")
        } _: {
            if !q.isEmpty && rows.isEmpty { empty("No match.") }
            ForEach(rows, id: \.key) { r in
                switch r {
                case let .folder(key, label, depth, count, open): folder(key, label, depth, count, open, ns: "local:")
                case let .leaf(_, label, depth, ref): branchRow(ref, label, depth)
                }
            }
        }
    }

    func branchRow(_ br: GitRef, _ label: String, _ depth: Int) -> some View {
        let wt = wtOf[br.name]
        return row(active: br.isHead, depth: depth) {
            ZStack {
                if br.isHead { Circle().fill(Theme.accent).frame(width: 7, height: 7).shadow(color: Theme.accent, radius: 4) }
                else { Image(systemName: "arrow.triangle.branch").font(.system(size: 10)).foregroundStyle(Theme.muted) }
            }.frame(width: 12)
            Text(label).fontWeight(br.isHead ? .semibold : .regular).lineLimit(1)
            Spacer(minLength: 0)
            if let wt { Image(systemName: "folder").font(.system(size: 9)).foregroundStyle(Theme.muted).help("Checked out in worktree \(home(wt))") }
            if br.track == "[gone]" { Text("gone").font(.brand(-6)).foregroundStyle(Theme.muted).help("Upstream branch is gone") }
            else {
                if let a = br.trackNum("ahead") { Text("↑\(a)").font(.brand(-6)).foregroundStyle(Theme.green) }
                if let bh = br.trackNum("behind") { Text("↓\(bh)").font(.brand(-6)).foregroundStyle(Theme.yellow) }
            }
        }
        .help(br.name)
        .onTapGesture(count: 2) {
            if let wt { openPath(wt) } else if !br.isHead { Task { await git.switchBranch(br.name) } }
        }
        .contextMenu {
            if let wt { Button("Open its worktree") { openPath(wt) } }
            else { Button("Checkout \(br.name)") { Task { await git.switchBranch(br.name) } }.disabled(br.isHead) }
            Button("Merge into current") { Task { await git.mergeBranch(br.name) } }.disabled(br.isHead)
            Button("Rebase current onto \(br.name)") { Task { await git.rebaseOnto(br.name) } }.disabled(br.isHead)
            Divider()
            Button("New branch from here…") { newBranch(at: br.hash) }
            Button("Copy name") { copy(br.name) }
            Button("Delete…") { askDelete(br) }.disabled(br.isHead)
        }
    }

    func askDelete(_ br: GitRef) {
        let gone = br.track == "[gone]"
        git.ask = GitAsk(title: "Delete branch \(br.name)?",
                         body: gone ? "Its upstream is gone; unmerged commits on it are lost."
                                    : "Refused by git if it has commits not merged anywhere.",
                         confirm: "Delete", danger: true) { _ in await git.deleteBranch(br.name, force: gone) }
    }

    var worktreesSection: some View {
        section("Worktrees", git.worktrees.count, open: !closed.contains("Worktrees"), toggle: { toggleSection("Worktrees") }) {
            EmptyView()
        } _: {
            ForEach(git.worktrees) { w in
                let isCur = w.path == git.cwd, known = wsByPath(w.path) != nil
                row(active: isCur) {
                    Image(systemName: "folder").font(.system(size: 10)).foregroundStyle(isCur ? Theme.accent : Theme.muted).frame(width: 12)
                    Text(w.branch ?? String(w.head.prefix(7))).lineLimit(1)
                    Spacer(minLength: 0)
                    Text((w.path as NSString).lastPathComponent).font(.brand(-6)).foregroundStyle(Theme.muted).lineLimit(1)
                }
                .help(home(w.path) + (known ? "" : " (not a Burrow workspace)"))
                .onTapGesture { if !isCur { openPath(w.path) } }
                .contextMenu {
                    Button("Open") { openPath(w.path) }.disabled(isCur || !known)
                    Button("Reveal in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: w.path)]) }
                    Button("Copy path") { copy(w.path) }
                    Divider()
                    Button("Remove worktree…") { askRemove(w, force: false) }.disabled(w.path == git.worktrees.first?.path)
                }
            }
        }
    }

    func askRemove(_ w: GitWorktree, force: Bool) {
        git.ask = GitAsk(title: "\(force ? "Force-remove" : "Remove") worktree \(w.branch ?? (w.path as NSString).lastPathComponent)?",
                         body: "Git deletes the directory \(home(w.path))."
                             + (force ? " Uncommitted changes in it are lost." : " Refused if it has uncommitted changes."),
                         confirm: force ? "Force remove" : "Remove", danger: true) { _ in await removeWorktree(w, force: force) }
    }

    func removeWorktree(_ w: GitWorktree, force: Bool) async {
        let main = git.worktrees.first?.path ?? git.cwd
        let ws = wsByPath(w.path)
        do {
            if let ws, b.workspaces[ws]?["parent_id"] as? NSNumber != nil {
                // Burrow's own path: same Go call as the Sidebar's delete, drops the workspace row too.
                try await b.call("remove_worktree", ["id": ws, "force": force])
            } else {
                let out = try await b.git(main, ["-c", "status.showUntrackedFiles=normal", "worktree", "remove", w.path] + (force ? ["--force"] : []), stdin: "")
                guard out.code == 0 else { throw NSError(domain: "git", code: Int(out.code), userInfo: [NSLocalizedDescriptionKey: out.message]) }
            }
            git.notice = "Removed worktree \(home(w.path))"
            if w.path == git.cwd { wsID = wsByPath(main) ?? (b.workspaces[ws ?? 0]?["parent_id"] as? NSNumber)?.int64Value }
            else { await git.refresh() }
        } catch {
            git.error = "Remove worktree failed: \(error.localizedDescription)"
            if !force { askRemove(w, force: true) }
        }
    }

    func remoteSection(_ remote: String, _ refs: [GitRef]) -> some View {
        let rows = branchTreeRows(refs, name: { String($0.name.dropFirst(remote.count + 1)) },
                                  isCollapsed: { collapsed.contains("\(remote):\($0)") }, expandAll: !q.isEmpty)
        return section(remote, refs.count, open: openedRemotes.contains(remote) || !q.isEmpty,
                       toggle: { if openedRemotes.contains(remote) { openedRemotes.remove(remote) } else { openedRemotes.insert(remote) } }) {
            EmptyView()
        } _: {
            ForEach(rows, id: \.key) { r in
                switch r {
                case let .folder(key, label, depth, count, open): folder(key, label, depth, count, open, ns: "\(remote):")
                case let .leaf(_, label, depth, ref):
                    row(depth: depth) {
                        Image(systemName: "cloud").font(.system(size: 10)).foregroundStyle(Theme.muted).frame(width: 12)
                        Text(label).lineLimit(1)
                        Spacer(minLength: 0)
                    }
                    .help(ref.name)
                    .onTapGesture(count: 2) { Task { await git.checkoutRemote(ref.name) } }
                    .contextMenu {
                        Button("Checkout as local branch") { Task { await git.checkoutRemote(ref.name) } }
                        Button("Merge into current") { Task { await git.mergeBranch(ref.name) } }
                        Button("Rebase current onto \(ref.name)") { Task { await git.rebaseOnto(ref.name) } }
                        Divider()
                        Button("New branch from here…") { newBranch(at: ref.hash) }
                        Button("Copy name") { copy(ref.name) }
                    }
                }
            }
        }
    }

    var stashSection: some View {
        section("Stashes", git.stashes.count, open: !closed.contains("Stashes"), toggle: { toggleSection("Stashes") }) {
            Button { Task { await git.stashPush() } } label: { Image(systemName: "plus") }
                .buttonStyle(.plain).disabled(!git.hasChanges).help("Stash all changes (⌘⇧S)")
        } _: {
            if git.stashes.isEmpty { empty("Nothing stashed.") }
            ForEach(git.stashes) { s in
                row {
                    Image(systemName: "archivebox").font(.system(size: 10)).foregroundStyle(Theme.muted).frame(width: 12)
                    Text(s.message).lineLimit(1)
                    Spacer(minLength: 0)
                    Text(s.relTime).font(.brand(-6)).foregroundStyle(Theme.muted).lineLimit(1)
                }
                .help("\(s.ref) · \(s.relTime)")
                .onTapGesture(count: 2) { Task { await git.stashPop(s.ref) } }
                .contextMenu {
                    Button("Pop (apply and drop)") { Task { await git.stashPop(s.ref) } }
                    Button("Apply (keep)") { Task { await git.stashApply(s.ref) } }
                    Divider()
                    Button("Drop…") {
                        git.ask = GitAsk(title: "Drop \(s.ref)?", body: "\(s.message)\nThe stashed changes are lost.",
                                         confirm: "Drop", danger: true) { _ in await git.stashDrop(s.ref) }
                    }
                }
            }
        }
    }

    var tagSection: some View {
        section("Tags", tags.count, open: !closed.contains("Tags"), toggle: { toggleSection("Tags") }) {
            EmptyView()
        } _: {
            ForEach(tags) { t in
                row {
                    Image(systemName: "tag").font(.system(size: 10)).foregroundStyle(Theme.yellow).frame(width: 12)
                    Text(t.name).lineLimit(1)
                    Spacer(minLength: 0)
                }
                .help(t.name)
                .contextMenu {
                    Button("Checkout (detached)") { Task { await git.checkoutCommit(t.name) } }
                    Button("New branch from here…") { newBranch(at: t.hash) }
                    Button("Copy name") { copy(t.name) }
                    Divider()
                    Button("Delete…") {
                        git.ask = GitAsk(title: "Delete tag \(t.name)?", body: "Only the local tag; a pushed tag stays on the remote.",
                                         confirm: "Delete", danger: true) { _ in await git.deleteTag(t.name) }
                    }
                }
            }
        }
    }

    // MARK: helpers

    func empty(_ t: String) -> some View {
        Text(t).font(.brand(-5)).foregroundStyle(Theme.muted).padding(.horizontal, 8).padding(.vertical, 4)
    }

    func newBranch(at: String = "") {
        git.ask = GitAsk(title: "New branch", body: at.isEmpty ? "From \(git.status.branch.isEmpty ? "HEAD" : git.status.branch)" : "From \(at.prefix(7))",
                         input: true, confirm: "Create") { n in await git.createBranch(n, at: at) }
    }

    /// ponytail: worktrees Burrow doesn't know as workspaces can't be selected; add one to open it here.
    func openPath(_ p: String) { if let id = wsByPath(p) { wsID = id } }

    func copy(_ s: String) { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(s, forType: .string) }
}
