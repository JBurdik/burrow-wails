import AppKit
import SwiftUI

// The Git manager window (src/components/git/GitWindow.vue + GitToolbar.vue + GitHistory.vue + GitCommitDetail.vue).
// Scene: `WindowGroup("Git", id: "git", for: Int64.self)` in App.swift; opened from the RP Changes header and
// Window → Git (⌘⇧G). The value is the workspace id to show first.

struct GitWindow: View {
    @EnvironmentObject var b: Burrow
    @State var wsID: Int64?
    @AppStorage("theme") private var theme = "plum"
    @AppStorage("fontSize") private var fontSize = 15.0

    var cwd: String { wsID.flatMap { b.workspaces[$0]?.s("path") } ?? "" }

    var body: some View {
        Group {
            if cwd.isEmpty {
                VStack(spacing: 10) {
                    Pet(state: "idle", size: 48)
                    Text(b.workspaces.isEmpty ? b.status : "Select a workspace.").font(.brand(-2)).foregroundStyle(Theme.muted)
                    if !b.workspaces.isEmpty { GitWorkspacePicker(wsID: $wsID).frame(width: 260) }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                GitWindowBody(wsID: $wsID, git: GitModel(cwd: cwd, b: b, full: true)).id("\(cwd)-\(fontSize)")
            }
        }
        .background(Theme.base)
        .frame(minWidth: 820, minHeight: 480)
        .id(theme)
        .tint(Theme.accent)
        .preferredColorScheme(Theme.isDark ? .dark : .light)
        .navigationTitle(cwd.isEmpty ? "Git" : "Git — \((cwd as NSString).lastPathComponent)")
        .onAppear { if wsID == nil { wsID = b.chat(b.selectedChat)?.i("workspace_id") ?? b.projects.first?.i("id") } }
    }
}

private struct GitWindowBody: View {
    @Binding var wsID: Int64?
    @StateObject var git: GitModel
    @State private var tab = "history"
    @AppStorage("git.sidebarWidth") private var sidebarW = 250.0

    init(wsID: Binding<Int64?>, git: @autoclosure @escaping () -> GitModel) {
        _wsID = wsID
        _git = StateObject(wrappedValue: git())
    }

    var changes: Int { git.status.staged.count + git.status.unstaged.count + git.status.untracked.count + git.status.conflicted.count }

    var body: some View {
        HSplitView {
            GitBranchSidebar(git: git, wsID: $wsID)
                .frame(minWidth: 200, idealWidth: sidebarW, maxWidth: 420)
                .background(Theme.sidebar)
            VStack(spacing: 0) {
                header
                if let e = git.error { banner(e, Theme.red) { git.error = nil } }
                else if let n = git.notice { banner(n, Theme.green) { git.notice = nil } }
                if git.notRepo {
                    VStack(spacing: 8) {
                        Spacer()
                        Pet(state: "idle", size: 44)
                        Text("Not a git repository.").font(.brand(-3)).foregroundStyle(Theme.muted)
                        chip("git init", Theme.text) { await git.gitInit() }
                        Spacer()
                    }.frame(maxWidth: .infinity)
                } else if tab == "history" {
                    GitHistoryView(git: git) { tab = "changes" }
                } else {
                    ChangesPane(git: git, compactHeader: false)
                }
            }
            .frame(minWidth: 520)
            .background(Theme.base)
        }
        .modifier(GitAskAlert(git: git))
        .task { await git.refresh() }
        // Coming back to the window after working in a terminal: pick up what changed.
        .onReceive(NotificationCenter.default.publisher(for: NSWindow.didBecomeKeyNotification)) { _ in
            Task { await git.refresh() }
        }
        .background { shortcuts }
    }

    /// GitWindow.vue ⌘1/⌘2/⌘R + GitToolbar.vue ⌘⇧F/L/P/S/B, as hidden buttons (only live while this window is key).
    var shortcuts: some View {
        Group {
            Button("") { tab = "history" }.keyboardShortcut("1")
            Button("") { tab = "changes" }.keyboardShortcut("2")
            Button("") { Task { await git.refresh() } }.keyboardShortcut("r")
            Button("") { Task { await git.fetch() } }.keyboardShortcut("f", modifiers: [.command, .shift])
            Button("") { Task { await git.pull() } }.keyboardShortcut("l", modifiers: [.command, .shift])
            Button("") { Task { await git.push() } }.keyboardShortcut("p", modifiers: [.command, .shift])
            Button("") { if git.hasChanges { Task { await git.stashPush() } } }.keyboardShortcut("s", modifiers: [.command, .shift])
            Button("") { newBranch() }.keyboardShortcut("b", modifiers: [.command, .shift])
        }
        .opacity(0).frame(width: 0, height: 0).allowsHitTesting(false)
    }

    func newBranch() {
        git.ask = GitAsk(title: "New branch", body: "From \(git.status.branch.isEmpty ? "HEAD" : git.status.branch)", input: true,
                         confirm: "Create") { name in await git.createBranch(name) }
    }

    var header: some View {
        HStack(spacing: 4) {
            HStack(spacing: 2) {
                tabButton("history", "History", "⌘1")
                tabButton("changes", "Changes", "⌘2")
            }
            .padding(2)
            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 8))
            Spacer(minLength: 8)
            if let busy = git.busy {
                ProgressView().controlSize(.mini)
                Text(busy).font(.brand(-5)).foregroundStyle(Theme.muted)
            }
            toolButton("arrow.triangle.2.circlepath", "Fetch", "Fetch (⌘⇧F)") { await git.fetch() }
            toolButton("arrow.down.to.line", "Pull" + (git.status.behind > 0 ? " \(git.status.behind)" : ""),
                       git.hasUpstream ? "Pull, fast-forward only (⌘⇧L)" : "No upstream to pull from",
                       disabled: !git.hasUpstream) { await git.pull() }
            toolButton("arrow.up.to.line", "Push" + (git.status.ahead > 0 ? " \(git.status.ahead)" : ""),
                       "\(git.hasUpstream ? "Push" : "Push and set upstream") (⌘⇧P)",
                       disabled: git.status.branch.isEmpty || (git.hasUpstream && git.status.ahead == 0)) { await git.push() }
            Rectangle().fill(Theme.border).frame(width: 1, height: 16).padding(.horizontal, 4)
            toolButton("archivebox", "Stash", "Stash all changes, incl. untracked (⌘⇧S)", disabled: !git.hasChanges) { await git.stashPush() }
            toolButton("arrow.triangle.branch", "Branch", "New branch (⌘⇧B)") { newBranch() }
            GitAutoRefresh { await git.refresh() }.padding(.leading, 4)
        }
        .padding(.horizontal, 10).frame(height: 44)
        .background(Theme.panel.opacity(0.6))
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }

    func tabButton(_ id: String, _ title: String, _ key: String) -> some View {
        Button { tab = id } label: {
            HStack(spacing: 5) {
                Text(title)
                if id == "changes" && changes > 0 {
                    Text("\(changes)").font(.brand(-6, weight: .bold))
                        .foregroundStyle(Theme.base)
                        .padding(.horizontal, 5).padding(.vertical, 1)
                        .background(git.status.conflicted.isEmpty ? Theme.accent : Theme.red, in: RoundedRectangle(cornerRadius: 5))
                }
            }
            .font(.brand(-4, weight: .medium))
            .foregroundStyle(tab == id ? Theme.text : Theme.muted)
            .padding(.horizontal, 10).padding(.vertical, 4)
            .background(tab == id ? Theme.hover : .clear, in: RoundedRectangle(cornerRadius: 6))
        }
        .buttonStyle(.plain).help("\(title) (\(key))")
    }

    func toolButton(_ icon: String, _ title: String, _ help: String, disabled: Bool = false, _ run: @escaping () async -> Void) -> some View {
        Button { Task { await run() } } label: {
            HStack(spacing: 5) { Image(systemName: icon); Text(title) }
                .font(.brand(-4, weight: .medium))
                .padding(.horizontal, 8).padding(.vertical, 4)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .foregroundStyle(disabled ? Theme.muted.opacity(0.5) : Theme.text2)
        .disabled(disabled || git.busy != nil).help(help)
    }

    func banner(_ text: String, _ color: Color, close: @escaping () -> Void) -> some View {
        HStack(alignment: .top, spacing: 6) {
            Text(text).font(.brand(-5)).foregroundStyle(color).textSelection(.enabled).lineLimit(6)
            Spacer(minLength: 0)
            Button(action: close) { Image(systemName: "xmark") }.buttonStyle(.plain).foregroundStyle(Theme.muted).font(.brand(-6))
        }
        .padding(8)
        .background(color.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        .padding(.horizontal, 10).padding(.top, 6)
    }
}

// MARK: - menu command

struct GitWindowCommands: Commands {
    @ObservedObject var b: Burrow
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        CommandGroup(before: .windowArrangement) {
            Button("Git") {
                let ws = b.chat(b.selectedChat)?.i("workspace_id") ?? b.projects.first?.i("id")
                if let ws { openWindow(id: "git", value: ws) }
            }
            .keyboardShortcut("g", modifiers: [.command, .shift])
            .disabled(b.workspaces.isEmpty)
        }
    }
}

// MARK: - history (GitHistory.vue)

private let rowH: CGFloat = 30
private let laneW: CGFloat = 15
private let maxLanesDrawn = 14

private func laneX(_ i: Int) -> CGFloat { 10 + CGFloat(min(i, maxLanesDrawn)) * laneW }
private func laneColor(_ i: Int) -> Color {
    let c = [Theme.accent, Theme.green, Theme.yellow, Theme.red, Theme.text2, Theme.accentDim]
    return c[i % c.count]
}

struct GitHistoryView: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var git: GitModel
    let openChanges: () -> Void
    @State private var commits: [GraphCommit] = []
    @State private var layout: (rows: [GraphRow], maxLanes: Int) = ([], 1)
    @State private var loaded = false
    @State private var limit = 500
    @State private var selected: String?
    @FocusState private var focused: Bool
    @AppStorage("git.split.history") private var split = 0.56

    var headHash: String? { commits.first { $0.refs.contains { $0 == "HEAD" || $0.hasPrefix("HEAD -> ") } }?.hash }
    var graphW: CGFloat { laneX(min(layout.maxLanes, maxLanesDrawn)) + 4 }
    var dirty: Int { git.status.staged.count + git.status.unstaged.count + git.status.untracked.count + git.status.conflicted.count }

    var body: some View {
        Group {
            if commits.isEmpty {
                Text(loaded ? "No commits yet. Make the first one in Changes." : "Loading history…")
                    .font(.brand(-4)).foregroundStyle(Theme.muted).frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                GeometryReader { geo in
                    HStack(spacing: 0) {
                        list.frame(width: max(300, geo.size.width * split))
                        Rectangle().fill(Theme.border).frame(width: 1)
                            .overlay(Color.clear.frame(width: 8).contentShape(Rectangle())
                                .onHover { $0 ? NSCursor.resizeLeftRight.push() : NSCursor.pop() }
                                .gesture(DragGesture().onChanged { v in
                                    split = min(0.8, max(0.25, (geo.size.width * split + v.translation.width) / geo.size.width))
                                }))
                        Group {
                            if let selected {
                                GitCommitDetail(cwd: git.cwd, hash: selected) { self.selected = $0 }.id(selected)
                            } else {
                                Text("Select a commit.").font(.brand(-4)).foregroundStyle(Theme.muted)
                                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                            }
                        }
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .background(Theme.panel.opacity(0.6))
                    }
                }
            }
        }
        // `rev` moves on every refresh (any git op), so the graph re-reads then.
        .task(id: "\(git.rev)-\(limit)") { await load() }
    }

    var list: some View {
        VStack(spacing: 0) {
            if dirty > 0 {
                Button(action: openChanges) {
                    HStack(spacing: 12) {
                        Canvas { ctx, _ in
                            let x = laneX(headHash.flatMap { h in commits.firstIndex { $0.hash == h } }.map { layout.rows[$0].lane } ?? 0)
                            ctx.stroke(Path(ellipseIn: CGRect(x: x - 5, y: 12, width: 10, height: 10)), with: .color(Theme.accent),
                                       style: StrokeStyle(lineWidth: 1.6, dash: [2.5, 2]))
                        }
                        .frame(width: graphW, height: 34)
                        Text("Uncommitted changes").foregroundStyle(Theme.accent)
                        Text("\(dirty) \(dirty == 1 ? "file" : "files")").foregroundStyle(Theme.muted)
                        Spacer()
                    }
                    .font(.brand(-4)).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
            }
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(Array(commits.enumerated()), id: \.element.hash) { i, c in
                            row(i, c)
                                .id(c.hash)
                                .onAppear { if i >= commits.count - 50 && commits.count >= limit { limit += 500 } }
                        }
                    }
                }
                .focusable()
                .focusEffectDisabled()
                .focused($focused)
                .onKeyPress(.downArrow) { move(1, proxy); return .handled }
                .onKeyPress(.upArrow) { move(-1, proxy); return .handled }
            }
        }
    }

    func move(_ d: Int, _ proxy: ScrollViewProxy) {
        let cur = commits.firstIndex { $0.hash == selected } ?? -1
        let next = max(0, min(commits.count - 1, cur + d))
        selected = commits[next].hash
        proxy.scrollTo(commits[next].hash)
    }

    func row(_ i: Int, _ c: GraphCommit) -> some View {
        let r = layout.rows[i]
        let sel = c.hash == selected
        return HStack(spacing: 0) {
            GraphCell(row: r, isHead: c.hash == headHash).frame(width: graphW, height: rowH)
            HStack(spacing: 5) {
                ForEach(refPills(c.refs, localBranches: git.localBranches), id: \.self) { p in pill(p, laneColor(r.color)) }
                Text(c.subject).font(.brand(-4))
                    .foregroundStyle(r.isMerge ? Theme.muted : sel || c.hash == headHash ? Theme.text : Theme.text2)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            Text(c.author).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(1).frame(width: 110, alignment: .trailing)
            Text(c.relTime).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(1).frame(width: 86, alignment: .trailing)
                .padding(.trailing, 12)
        }
        .frame(height: rowH)
        .background(sel ? Theme.accent.opacity(0.14) : .clear)
        .contentShape(Rectangle())
        .onTapGesture { selected = c.hash; focused = true }
        .contextMenu { menu(c) }
    }

    func pill(_ p: RefPill, _ lane: Color) -> some View {
        let color = p.kind == .tag ? Theme.yellow : p.kind == .remote ? Theme.muted : lane
        return HStack(spacing: 3) {
            if p.head { Circle().fill(Theme.accent).frame(width: 5, height: 5) }
            if p.kind == .tag { Image(systemName: "tag") }
            if p.kind == .remote { Image(systemName: "cloud").opacity(0.7) }
            Text(p.label).fontWeight(p.head ? .bold : .medium)
            if p.synced { Image(systemName: "cloud").opacity(0.7) }
        }
        .font(.brand(-6)).foregroundStyle(color).lineLimit(1)
        .padding(.horizontal, 5).frame(height: 19)
        .background(color.opacity(p.kind == .remote ? 0 : 0.13), in: RoundedRectangle(cornerRadius: 5))
        .overlay(RoundedRectangle(cornerRadius: 5).strokeBorder(p.kind == .tag ? .clear : color.opacity(0.45)))
        .fixedSize()
    }

    @ViewBuilder func menu(_ c: GraphCommit) -> some View {
        let branch = git.status.branch
        let short = c.shortHash.isEmpty ? String(c.hash.prefix(7)) : c.shortHash
        Text("\(short) · \(c.subject)")
        ForEach(refPills(c.refs, localBranches: git.localBranches).filter { $0.kind == .local && $0.label != branch }.map(\.label), id: \.self) { br in
            Button("Checkout \(br)") { Task { await git.switchBranch(br) } }
            if !branch.isEmpty {
                Button("Merge \(br) into current") {
                    git.ask = GitAsk(title: "Merge \(br) into \(branch)?", body: "Creates a merge commit unless it can fast-forward.",
                                     confirm: "Merge") { _ in await git.mergeBranch(br) }
                }
                Button("Rebase current onto \(br)") { askRebase(br) }
            }
            Divider()
        }
        Button("Checkout (detached)") { Task { await git.checkoutCommit(c.hash) } }
        Button("New branch here…") {
            git.ask = GitAsk(title: "New branch", body: "From \(short)", input: true, confirm: "Create") { n in await git.createBranch(n, at: c.hash) }
        }
        Button("Tag here…") {
            git.ask = GitAsk(title: "New tag", body: "On \(short)", input: true, confirm: "Create") { n in await git.tagAt(n, c.hash) }
        }
        Divider()
        Button("Cherry-pick") { Task { await git.cherryPick(c.hash) } }.disabled(c.hash == headHash)
        Button("Revert") { Task { await git.revertCommit(c.hash) } }
        Divider()
        Button("Reset here (soft)") { askReset(c, "soft") }
        Button("Reset here (mixed)") { askReset(c, "mixed") }
        Button("Reset here (hard)…") { askReset(c, "hard") }
        Divider()
        Button("Copy hash") { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(c.hash, forType: .string) }
    }

    func askRebase(_ br: String) {
        let branch = git.status.branch
        // ponytail: "has upstream" ≈ "already shared"; exact check would compare with the upstream's merge-base
        let shared = git.hasUpstream
        git.ask = GitAsk(title: "Rebase \(branch) onto \(br)?",
                         body: shared ? "\(branch) is already pushed. Rebasing rewrites its history and the next push will need --force."
                                      : "Replays the commits of \(branch) on top of \(br).",
                         confirm: "Rebase", danger: shared) { _ in await git.rebaseOnto(br) }
    }

    func askReset(_ c: GraphCommit, _ mode: String) {
        let body = [
            "soft": "Commits after this point go back to staged changes. Nothing is lost.",
            "mixed": "Commits after this point come back as unstaged changes. Nothing is lost.",
            "hard": "Commits after this point AND every uncommitted change are thrown away. This cannot be undone.",
        ][mode]!
        git.ask = GitAsk(title: "Reset \(git.status.branch.isEmpty ? "HEAD" : git.status.branch) to \(c.shortHash) (\(mode))?",
                         body: body, confirm: "Reset", danger: mode == "hard") { _ in await git.resetTo(c.hash, mode) }
    }

    /// Branches + remotes + tags + HEAD, not --all: --all drags in refs/prefetch, refs/pull, notes and the stash.
    func load() async {
        let out = (try? await b.git(git.cwd, [
            "log", "--branches", "--remotes", "--tags", "HEAD",
            "--decorate-refs=refs/heads", "--decorate-refs=refs/remotes", "--decorate-refs=refs/tags", "--decorate-refs=HEAD",
            "--topo-order", "-n\(limit)",
            "--format=%H%x1f%h%x1f%P%x1f%an%x1f%cr%x1f%D%x1f%s%x1e",
        ]))
        let list = out?.code == 0 ? parseGraphLog(out!.stdout) : [] // empty repo: no HEAD yet
        commits = list
        layout = layoutGraph(list)
        loaded = true
        if selected == nil || !list.contains(where: { $0.hash == selected }) { selected = headHash ?? list.first?.hash }
    }
}

/// One row of lanes: top half converges into the node, bottom half leaves it. HEAD gets a halo; merges are hollow.
private struct GraphCell: View {
    let row: GraphRow
    let isHead: Bool

    var body: some View {
        Canvas { ctx, _ in
            let mid = rowH / 2
            func curve(_ x1: CGFloat, _ y1: CGFloat, _ x2: CGFloat, _ y2: CGFloat) -> Path {
                var p = Path()
                p.move(to: CGPoint(x: x1, y: y1))
                if x1 == x2 { p.addLine(to: CGPoint(x: x2, y: y2)) }
                else { p.addCurve(to: CGPoint(x: x2, y: y2), control1: CGPoint(x: x1, y: (y1 + y2) / 2), control2: CGPoint(x: x2, y: (y1 + y2) / 2)) }
                return p
            }
            let style = StrokeStyle(lineWidth: 1.8, lineCap: .round)
            for e in row.up {
                ctx.stroke(curve(laneX(e.from), 0, laneX(e.to), e.from == e.to && e.to != row.lane ? mid + 0.5 : mid),
                           with: .color(laneColor(e.color)), style: style)
            }
            for e in row.down {
                ctx.stroke(curve(laneX(e.from), e.from == e.to && e.from != row.lane ? mid - 0.5 : mid, laneX(e.to), rowH),
                           with: .color(laneColor(e.color)), style: style)
            }
            let x = laneX(row.lane), c = laneColor(row.color)
            func dot(_ r: CGFloat) -> Path { Path(ellipseIn: CGRect(x: x - r, y: mid - r, width: r * 2, height: r * 2)) }
            if isHead {
                ctx.fill(dot(9), with: .color(c.opacity(0.18)))
                ctx.fill(dot(5), with: .color(c))
            } else if row.isMerge {
                ctx.fill(dot(4), with: .color(Theme.panel))
                ctx.stroke(dot(4), with: .color(c), lineWidth: 2)
            } else {
                ctx.fill(dot(5.5), with: .color(Theme.panel))
                ctx.fill(dot(4.5), with: .color(c))
            }
        }
    }
}

// MARK: - commit detail (GitCommitDetail.vue)

struct GitCommitDetail: View {
    @EnvironmentObject var b: Burrow
    let cwd: String
    let hash: String
    let select: (String) -> Void
    @State private var meta: [String] = []
    @State private var files: [PatchFile] = []
    @State private var fileIdx = 0
    @State private var err: String?
    @State private var copied = false

    var body: some View {
        if let err {
            Text(err).font(.brand(-4)).foregroundStyle(Theme.muted).frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if meta.count < 7 {
            ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity).task { await load() }
        } else {
            VStack(alignment: .leading, spacing: 0) {
                header.padding(16)
                    .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
                fileList
                    .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
                if files.indices.contains(fileIdx) {
                    GitDiffText(text: files[fileIdx].text).id(fileIdx)
                } else {
                    Spacer()
                }
            }
        }
    }

    var message: String { meta[6].trimmingCharacters(in: .whitespacesAndNewlines) }
    var subject: String { message.components(separatedBy: "\n").first ?? "" }
    var bodyText: String { message.components(separatedBy: "\n").dropFirst().joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines) }
    var parents: [String] { meta[5].split(separator: " ").map(String.init) }

    var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(subject).font(.ui(0, weight: .semibold)).foregroundStyle(Theme.text).textSelection(.enabled)
            if !bodyText.isEmpty {
                ScrollView { Text(bodyText).font(.ui(-2)).foregroundStyle(Theme.text2).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading) }
                    .frame(maxHeight: 140).fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 10) {
                Text(initials).font(.brand(-5, weight: .bold)).foregroundStyle(Theme.accent)
                    .frame(width: 30, height: 30).background(Theme.accent.opacity(0.2), in: Circle())
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 4) {
                        Text(meta[1]).foregroundStyle(Theme.text)
                        Text(meta[2]).foregroundStyle(Theme.muted)
                    }.font(.brand(-4)).lineLimit(1)
                    Text(date + (meta[4] != meta[1] ? ", committed by \(meta[4])" : "")).font(.brand(-5)).foregroundStyle(Theme.muted)
                }
                Spacer()
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(meta[0], forType: .string)
                    copied = true
                } label: {
                    HStack(spacing: 4) { Text(String(meta[0].prefix(8))); Image(systemName: copied ? "checkmark" : "doc.on.doc") }
                }
                .buttonStyle(.plain).font(.brand(-5)).foregroundStyle(copied ? Theme.green : Theme.text2).help("Copy hash")
            }
            if parents.count > 1 {
                HStack(spacing: 4) {
                    Text("Merge of").foregroundStyle(Theme.muted)
                    ForEach(parents, id: \.self) { p in
                        Button(String(p.prefix(7))) { select(p) }.buttonStyle(.plain).foregroundStyle(Theme.text2)
                    }
                    Text("· diff is against the first parent.").foregroundStyle(Theme.muted)
                }.font(.brand(-5))
            }
        }
    }

    var fileList: some View {
        ScrollView {
            VStack(spacing: 1) {
                ForEach(Array(files.enumerated()), id: \.offset) { i, f in
                    HStack(spacing: 8) {
                        Text(f.status).font(.brand(-5, weight: .bold))
                            .foregroundStyle(f.status == "A" ? Theme.green : f.status == "D" ? Theme.red : f.status == "R" ? Theme.accent : Theme.yellow)
                            .frame(width: 12)
                        HStack(spacing: 0) {
                            Text(dir(f.path)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.head)
                            Text((f.path as NSString).lastPathComponent).foregroundStyle(Theme.text).lineLimit(1).layoutPriority(1)
                        }
                        Spacer(minLength: 4)
                        if f.adds > 0 { Text("+\(f.adds)").foregroundStyle(Theme.green) }
                        if f.dels > 0 { Text("−\(f.dels)").foregroundStyle(Theme.red) }
                    }
                    .font(.brand(-4))
                    .padding(.horizontal, 8).frame(height: 26)
                    .background(i == fileIdx ? Theme.hover : .clear, in: RoundedRectangle(cornerRadius: 6))
                    .contentShape(Rectangle())
                    .onTapGesture { fileIdx = i }
                }
                if files.isEmpty { Text("Empty commit.").font(.brand(-4)).foregroundStyle(Theme.muted).frame(maxWidth: .infinity, alignment: .leading).padding(8) }
            }
            .padding(6)
        }
        .frame(maxHeight: 200).fixedSize(horizontal: false, vertical: true)
    }

    func dir(_ p: String) -> String { let d = (p as NSString).deletingLastPathComponent; return d.isEmpty ? "" : d + "/" }

    var initials: String {
        meta[1].split(separator: " ").prefix(2).compactMap(\.first).map { String($0).uppercased() }.joined()
    }

    var date: String {
        guard let d = ISO8601DateFormatter().date(from: meta[3]) else { return meta[3] }
        return d.formatted(date: .abbreviated, time: .shortened)
    }

    func load() async {
        do {
            async let m = b.git(cwd, ["show", "-s", "--format=%H%x1f%an%x1f%ae%x1f%aI%x1f%cn%x1f%P%x1f%B", hash])
            async let p = b.git(cwd, ["show", "--format=", "--diff-merges=first-parent", "-M", hash])
            let (mo, po) = try await (m, p)
            guard mo.code == 0 else { err = mo.message.isEmpty ? "git error" : mo.message; return }
            files = splitPatch(po.stdout)
            let parts = mo.stdout.components(separatedBy: "\u{1f}")
            meta = parts.count >= 7 ? Array(parts.prefix(6)) + [parts[6...].joined(separator: "\u{1f}")] : []
            if meta.isEmpty { err = "Can't read commit \(hash.prefix(8))" }
        } catch { err = error.localizedDescription }
    }
}
