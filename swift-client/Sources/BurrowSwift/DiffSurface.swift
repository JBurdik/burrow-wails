import AppKit
import SwiftUI

// OWNER: gitdiff (wave 2). RightPanel.vue's "diff" surface + DiffTab.vue + DiffReviewComments.vue +
// DiffFeedbackComposer.vue: scope picker (last turn / uncommitted / branch / Turn N checkpoints), restore a
// checkpoint (confirm), per-file "Seen" receipts, line notes → `add_diff_comment`, "Send N notes" → the chat.

extension Notification.Name {
    static let diffCommentsChanged = Notification.Name("burrow.diffCommentsChanged")
}

@MainActor
final class DiffModel: ObservableObject {
    enum Scope: Hashable {
        case lastTurn, workspace, branch, turn(Int64)
        var key: String {
            switch self {
            case .lastTurn: "last-turn"
            case .workspace: "workspace"
            case .branch: "branch"
            case .turn(let id): "turn:\(id)"
            }
        }
    }

    @Published var scope: Scope = .workspace
    @Published var checkpoints: [JSON] = [] // newest first
    @Published var files: [DiffFile] = []
    @Published var base = ""
    @Published var manualBase = ""
    @Published var branches: [String] = []
    @Published var lastTurn: JSON?
    @Published var loading = false
    @Published var error = ""
    @Published var notes: [JSON] = []
    @Published var receipts: [String: String] = [:]
    @Published var collapsed: Set<String> = []
    /// Selected diff lines for a new note: file name + line indices into that file's `lines`.
    @Published var selection: (file: String, a: Int, b: Int)?
    @Published var noteDraft = ""
    @Published var status = ""
    @Published var busy = false
    @Published var restoreTarget: JSON?

    private var request = 0
    private var receiptsKey = ""
    private(set) var cwd = ""

    nonisolated(unsafe) private static var hooked = false
    static func hook(_ b: Burrow) {
        guard !hooked else { return }
        hooked = true
        b.subscribe("diff-comments-changed") { _, p in
            NotificationCenter.default.post(name: .diffCommentsChanged, object: (p as? NSNumber)?.int64Value)
        }
    }

    var pendingNotes: [JSON] { notes.filter { $0.i("sent_at") == 0 && $0.i("resolved_at") == 0 } }
    var seenCount: Int { files.filter(isSeen).count }
    func isSeen(_ f: DiffFile) -> Bool { receipts[f.name] == f.revision }

    func load(_ b: Burrow, cwd: String, subject: String, preserve: Bool = false) async {
        request += 1
        let req = request, scope = scope
        self.cwd = cwd
        if !preserve { files = []; base = ""; lastTurn = nil }
        error = ""
        guard !cwd.isEmpty else { return }
        loading = true
        defer { if req == request { loading = false } }
        do {
            let rows = try await b.call("list_checkpoints", ["cwd": cwd, "limit": 50]) as? [JSON] ?? []
            guard req == request else { return }
            checkpoints = rows
            var diff = "", base = "", turn: JSON?
            switch scope {
            case .workspace:
                diff = try await b.call("working_tree_diff", ["cwd": cwd]) as? String ?? ""
            case .lastTurn:
                if !subject.isEmpty {
                    turn = try await b.call("last_turn_audit", ["cwd": cwd, "subjectId": subject]) as? JSON
                    diff = turn?.s("diff") ?? ""
                }
            case .branch:
                base = manualBase.isEmpty ? (try await b.call("branch_diff_base", ["cwd": cwd]) as? String ?? "") : manualBase
                if !base.isEmpty {
                    let out = try await b.call("run_git", ["cwd": cwd, "args": ["diff", "--no-ext-diff", "--no-color", "\(base)...HEAD", "--"]]) as? JSON ?? [:]
                    if out.i("code") != 0 { throw Burrow.CallError(message: out.s("stderr").isEmpty ? "Could not compare this branch." : out.s("stderr")) }
                    diff = out.s("stdout")
                } else {
                    let out = try await b.call("run_git", ["cwd": cwd, "args": ["branch", "--format=%(refname:short)"]]) as? JSON ?? [:]
                    branches = out.s("stdout").split(separator: "\n").map(String.init)
                }
            case .turn(let id):
                if let commit = rows.first(where: { $0.i("id") == id })?.s("commit") {
                    diff = try await b.call("checkpoint_diff", ["cwd": cwd, "commit": commit]) as? String ?? ""
                }
            }
            guard req == request else { return }
            self.base = base
            lastTurn = turn
            setFiles(parseDiffFiles(diff), key: "\(cwd):\(subject):\(scope.key):\(turn.map { String($0.i("id")) } ?? base)")
        } catch {
            if req == request { self.error = "Could not load changes: \(error.localizedDescription)" }
        }
    }

    /// useDiffReview.syncFiles: drop receipts whose file changed, collapse what is already seen.
    private func setFiles(_ fs: [DiffFile], key: String) {
        receiptsKey = "burrow.diffSeen.v1:" + key
        let saved = UserDefaults.standard.dictionary(forKey: receiptsKey) as? [String: String] ?? [:]
        let next = currentReceipts(saved, Dictionary(fs.map { ($0.name, $0.revision) }, uniquingKeysWith: { a, _ in a }))
        if next.count != saved.count { UserDefaults.standard.set(next, forKey: receiptsKey) }
        receipts = next
        files = fs
        collapsed = Set(fs.filter(isSeen).map(\.name))
        if let s = selection, !fs.contains(where: { $0.name == s.file }) { selection = nil }
    }

    func setSeen(_ f: DiffFile, _ seen: Bool) {
        if seen { receipts[f.name] = f.revision; collapsed.insert(f.name) } else { receipts[f.name] = nil; collapsed.remove(f.name) }
        UserDefaults.standard.set(receipts, forKey: receiptsKey)
    }

    func loadNotes(_ b: Burrow, ws: Int64) async {
        guard ws != 0 else { notes = []; return }
        let rows = (try? await b.call("list_diff_comments", ["wsId": ws])) as? [JSON] ?? []
        notes = rows
    }

    func select(_ file: String, _ i: Int, extend: Bool) {
        if extend, let s = selection, s.file == file { selection = (file, s.a, i) } else { selection = (file, i, i) }
        status = ""
    }

    var selectionLabel: String {
        guard let s = selection, let f = files.first(where: { $0.name == s.file }) else { return "Select diff lines to add a note." }
        let lo = f.lines[min(s.a, s.b)].anchor?.line ?? 0, hi = f.lines[max(s.a, s.b)].anchor?.line ?? lo
        return lo == hi ? "\(f.name):line \(lo)" : "\(f.name):lines \(lo)-\(hi)"
    }

    func addNote(_ b: Burrow, ws: Int64) async {
        let comment = noteDraft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let s = selection, ws != 0, !comment.isEmpty, let f = files.first(where: { $0.name == s.file }),
              let anchor = f.lines[min(s.a, s.b)].anchor else { return }
        busy = true
        defer { busy = false }
        let body = diffNoteBody(quote: diffQuote(f.lines, s.a, s.b), comment: comment)
        do {
            try await b.call("add_diff_comment", ["wsId": ws, "file": f.name, "line": anchor.line, "side": anchor.side, "body": body])
            selection = nil
            noteDraft = ""
            await loadNotes(b, ws: ws)
        } catch { status = "Could not add note." }
    }

    func setResolved(_ b: Burrow, ws: Int64, _ id: Int64, _ resolved: Bool) async {
        do {
            try await b.call("set_diff_comment_resolved", ["wsId": ws, "id": id, "resolved": resolved])
            await loadNotes(b, ws: ws)
        } catch { status = "Could not change the comment status. Try again." }
    }

    /// DiffTab.sendNotes → the thread itself. A running turn gets it queued (chat_queue_add) rather than steered.
    func sendNotes(_ b: Burrow, chatID: Int64, ws: Int64) async {
        let ids = pendingNotes.map { $0.i("id") }
        guard !ids.isEmpty, chatID != 0 else { return }
        busy = true
        defer { busy = false }
        do {
            let md = try await b.call("compose_diff_notes", ["ids": ids]) as? String ?? ""
            try await b.call(b.inFlight(chatID) ? "chat_queue_add" : "chat_send", ["chatId": chatID, "text": md, "images": [String]()])
            try await b.call("mark_diff_notes_sent", ["ids": ids])
            await loadNotes(b, ws: ws)
            status = "Sent \(ids.count) note\(ids.count == 1 ? "" : "s")."
        } catch { status = "Could not send notes." }
    }

    func restore(_ b: Burrow, cwd: String, subject: String) async {
        guard let cp = restoreTarget else { return }
        restoreTarget = nil
        busy = true
        defer { busy = false }
        do {
            try await b.call("restore_checkpoint", ["cwd": cwd, "commit": cp.s("commit")])
            status = "Restored. The previous state was saved as a checkpoint."
            await load(b, cwd: cwd, subject: subject, preserve: true)
        } catch { self.error = "Restore failed: \(error.localizedDescription)" }
    }
}

func checkpointTime(_ ms: Int64) -> String {
    Date(timeIntervalSince1970: Double(ms) / 1000).formatted(.dateTime.month(.abbreviated).day().hour().minute())
}

/// RP surface: turn / branch / worktree diff, checkpoints, review comments for one thread.
struct DiffSurfaceSlot: View {
    let chat: JSON
    @EnvironmentObject var b: Burrow
    @StateObject private var m = DiffModel()

    private var chatID: Int64 { chat.i("id") }
    private var ws: Int64 { chat.i("workspace_id") }
    private var cwd: String { b.workspaces[ws]?.s("path") ?? "" }
    private var subject: String { chatID == 0 ? "" : "chat:\(chatID)" }
    private var loadKey: String { "\(cwd)|\(subject)|\(m.scope.key)|\(m.manualBase)" }

    var body: some View {
        VStack(spacing: 0) {
            toolbar
            if !m.error.isEmpty { line(m.error, Theme.red) }
            if !m.status.isEmpty { line(m.status, Theme.muted) }
            if !m.notes.isEmpty { DiffNotesList(m: m, ws: ws) }
            if m.selection != nil { noteComposer }
            content
        }
        .background(Theme.base)
        .onAppear { DiffModel.hook(b) }
        .task(id: loadKey) { await m.load(b, cwd: cwd, subject: subject) }
        .task(id: ws) { await m.loadNotes(b, ws: ws) }
        .onReceive(NotificationCenter.default.publisher(for: .diffCommentsChanged)) { n in
            if (n.object as? Int64) == ws { Task { await m.loadNotes(b, ws: ws) } }
        }
        .onChange(of: b.inFlight(chatID)) { _, running in
            if !running, m.scope == .lastTurn { Task { await m.load(b, cwd: cwd, subject: subject, preserve: true) } }
        }
        .alert(restoreTitle, isPresented: Binding(get: { m.restoreTarget != nil }, set: { if !$0 { m.restoreTarget = nil } })) {
            Button("Cancel", role: .cancel) { m.restoreTarget = nil }
            Button("Restore", role: .destructive) { Task { await m.restore(b, cwd: cwd, subject: subject) } }
        } message: {
            Text("Every file in this workspace goes back to how it looked at \(checkpointTime(m.restoreTarget?.i("createdAt") ?? 0)) — later edits are overwritten and files created since are deleted. Your commits and the staging area are untouched.\n\nThe current state is saved as a new checkpoint first, so this is undoable.")
        }
    }

    private var restoreTitle: String {
        let cp = m.restoreTarget ?? [:]
        return "Restore checkpoint “\(cp.s("label").isEmpty ? String(cp.s("commit").prefix(7)) : cp.s("label"))”?"
    }

    private var scopeCheckpoint: JSON? {
        if case .turn(let id) = m.scope { return m.checkpoints.first { $0.i("id") == id } }
        return nil
    }

    private var toolbar: some View {
        VStack(spacing: 0) {
            HStack(spacing: 6) {
                Picker("", selection: $m.scope) {
                    Text("Last turn").tag(DiffModel.Scope.lastTurn)
                    Text("Uncommitted").tag(DiffModel.Scope.workspace)
                    Text("Branch changes").tag(DiffModel.Scope.branch)
                    if !m.checkpoints.isEmpty {
                        Divider()
                        // Turn numbers count up from the oldest, so "Turn 1" stays stable as turns land.
                        ForEach(Array(m.checkpoints.enumerated()), id: \.offset) { i, cp in
                            let n = m.checkpoints.count - i
                            Text("Turn \(n)\(i == 0 ? " (latest)" : "") · \(checkpointTime(cp.i("createdAt")))").tag(DiffModel.Scope.turn(cp.i("id")))
                        }
                    }
                }
                .labelsHidden().pickerStyle(.menu).font(.brand(-4)).frame(maxWidth: .infinity, alignment: .leading)
                iconButton("arrow.clockwise", "Refresh diff") { await m.load(b, cwd: cwd, subject: subject, preserve: true) }
                if let cp = scopeCheckpoint {
                    iconButton("arrow.uturn.backward", "Restore the working tree to before this turn") { m.restoreTarget = cp }
                }
                Button {
                    Task {
                        do {
                            let r = try await b.call("review_changes", ["workspaceId": ws, "parentChatId": chatID, "base": m.scope == .branch ? "auto" : ""]) as? JSON
                            if let id = r?["chat_id"] as? NSNumber { openSubagent(id.int64Value) }
                        } catch { m.error = error.localizedDescription }
                    }
                } label: { Label("Review", systemImage: "sparkles").font(.brand(-4)) }
                    .buttonStyle(.plain).foregroundStyle(Theme.text2).disabled(ws == 0)
                    .help(m.scope == .branch ? "Have an agent review the branch changes" : "Have an agent review the uncommitted changes")
            }
            .padding(.horizontal, 8).padding(.vertical, 6)
            if !m.files.isEmpty {
                HStack(spacing: 8) {
                    Text("\(m.seenCount)/\(m.files.count) seen").font(.brand(-5)).foregroundStyle(Theme.muted)
                    Spacer()
                    if m.files.count > 1 {
                        let allCollapsed = m.collapsed.count == m.files.count
                        diffChip(allCollapsed ? "Expand all" : "Collapse all") {
                            m.collapsed = allCollapsed ? [] : Set(m.files.map(\.name))
                        }
                    }
                    let n = m.pendingNotes.count
                    diffChip(m.busy ? "Sending…" : "Send \(n) note\(n == 1 ? "" : "s")") {
                        Task { await m.sendNotes(b, chatID: chatID, ws: ws) }
                    }
                    .disabled(n == 0 || m.busy || chatID == 0)
                }
                .padding(.horizontal, 10).padding(.bottom, 6)
            }
        }
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }

    @ViewBuilder private var content: some View {
        if m.loading && m.files.isEmpty {
            centered("Loading changes…")
        } else if m.scope == .branch && m.base.isEmpty {
            VStack(spacing: 8) {
                Text("Couldn't find a default branch to diff against.").font(.brand(-4)).foregroundStyle(Theme.muted)
                Menu("Pick a branch…") {
                    ForEach(m.branches, id: \.self) { br in Button(br) { m.manualBase = br } }
                }
                .fixedSize()
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if m.scope == .lastTurn && m.lastTurn == nil {
            centered(subject.isEmpty ? "Open an agent thread to review its last turn." : "No completed turn in this thread yet.")
        } else {
            VStack(spacing: 0) {
                if m.scope == .lastTurn, let t = m.lastTurn { caption(t.s("label").isEmpty ? "Last completed turn" : t.s("label")) }
                if m.scope == .branch, !m.base.isEmpty { caption("Committed changes since \(m.base)") }
                if m.files.isEmpty {
                    VStack(spacing: 8) { Pet(state: "idle", size: 40); Text("No changes").font(.brand(-4)).foregroundStyle(Theme.muted) }
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 8) {
                            ForEach(m.files) { f in DiffFileView(m: m, file: f, ws: ws) }
                        }
                        .padding(8)
                    }
                }
            }
        }
    }

    private var noteComposer: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(m.selectionLabel).font(.brand(-5)).foregroundStyle(Theme.muted)
            TextField("Add a note about this line…", text: $m.noteDraft, axis: .vertical)
                .textFieldStyle(.plain).font(.brand(-4)).foregroundStyle(Theme.text).lineLimit(3...8)
                .padding(8)
                .background(Theme.base, in: RoundedRectangle(cornerRadius: 7))
                .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(Theme.border))
            HStack {
                Text("Shift-click a line to extend the range").font(.brand(-6)).foregroundStyle(Theme.muted)
                Spacer()
                diffChip("Cancel") { m.selection = nil; m.noteDraft = "" }
                diffChip(m.busy ? "Adding…" : "Add note") { Task { await m.addNote(b, ws: ws) } }
                    .keyboardShortcut(.return, modifiers: .command)
                    .disabled(m.noteDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || m.busy || ws == 0)
            }
        }
        .padding(10)
        .background(Theme.hover)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }

    private func line(_ t: String, _ c: Color) -> some View {
        Text(t).font(.brand(-5)).foregroundStyle(c).textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 10).padding(.vertical, 5)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }
    private func caption(_ t: String) -> some View {
        Text(t).font(.brand(-6)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.tail)
            .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 10).padding(.vertical, 5)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }
    private func centered(_ t: String) -> some View {
        Text(t).font(.brand(-4)).foregroundStyle(Theme.muted).multilineTextAlignment(.center).padding()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
    private func iconButton(_ icon: String, _ help: String, _ action: @escaping () async -> Void) -> some View {
        Button { Task { await action() } } label: { Image(systemName: icon).font(.system(size: 11)) }
            .buttonStyle(.plain).foregroundStyle(Theme.muted).help(help)
    }
}

func diffChip(_ title: String, _ action: @escaping () -> Void) -> some View {
    Button(action: action) {
        Text(title).font(.brand(-5)).foregroundStyle(Theme.text2)
            .padding(.horizontal, 7).padding(.vertical, 3)
            .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(Theme.border))
    }
    .buttonStyle(.plain)
}

/// DiffReviewComments.vue: collapsible list of every note in the workspace, resolve / reopen.
struct DiffNotesList: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var m: DiffModel
    let ws: Int64
    @State private var open = false

    var body: some View {
        let openCount = m.notes.filter { $0.i("resolved_at") == 0 }.count
        DisclosureGroup(isExpanded: $open) {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(m.notes.enumerated()), id: \.offset) { _, n in DiffNoteRow(m: m, note: n, ws: ws) }
                }
            }
            .frame(maxHeight: 240)
        } label: {
            Text("Review comments · \(openCount) open · \(m.notes.count - openCount) resolved").font(.brand(-5)).foregroundStyle(Theme.text2)
        }
        .padding(.horizontal, 10).padding(.vertical, 5)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }
}

struct DiffNoteRow: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var m: DiffModel
    let note: JSON
    let ws: Int64
    var showLocation = true

    var body: some View {
        let resolved = note.i("resolved_at") != 0
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                if showLocation {
                    Text("\(note.s("file")):\(note.i("line"))").font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.head)
                }
                Spacer()
                Text(resolved ? "Resolved" : note.i("sent_at") != 0 ? "Sent" : "Pending").font(.brand(-6)).foregroundStyle(Theme.muted)
                Button(resolved ? "Reopen" : "Resolve") { Task { await m.setResolved(b, ws: ws, note.i("id"), !resolved) } }
                    .buttonStyle(.plain).font(.brand(-5)).foregroundStyle(Theme.text2).disabled(m.busy)
            }
            Text(note.s("body")).font(.brand(-5)).foregroundStyle(Theme.text2).textSelection(.enabled)
                .opacity(note.i("sent_at") != 0 || resolved ? 0.6 : 1)
        }
        .padding(.vertical, 5)
        .overlay(alignment: .top) { Rectangle().fill(Theme.border).frame(height: 1) }
    }
}

/// One file of the diff: header (name, +/−, Seen, collapse) + unified lines with old/new gutters.
struct DiffFileView: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var m: DiffModel
    let file: DiffFile
    let ws: Int64

    var body: some View {
        let collapsed = m.collapsed.contains(file.name)
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                Button {
                    if collapsed { m.collapsed.remove(file.name) } else { m.collapsed.insert(file.name) }
                } label: {
                    Image(systemName: collapsed ? "chevron.right" : "chevron.down").font(.system(size: 9)).foregroundStyle(Theme.muted).frame(width: 12)
                }
                .buttonStyle(.plain)
                Text(file.name).font(.brand(-4, weight: .semibold)).foregroundStyle(Theme.text).lineLimit(1).truncationMode(.head)
                Text("+\(file.adds)").font(.brand(-5)).foregroundStyle(Theme.green)
                Text("−\(file.dels)").font(.brand(-5)).foregroundStyle(Theme.red)
                Spacer()
                Toggle("Seen", isOn: Binding(get: { m.isSeen(file) }, set: { m.setSeen(file, $0) }))
                    .toggleStyle(.checkbox).font(.brand(-5)).foregroundStyle(Theme.muted)
            }
            .padding(.horizontal, 8).padding(.vertical, 6)
            .background(Theme.panel)
            .contextMenu { Button("Open file") { openFileInRP(m.cwd + "/" + file.name) } }
            if !collapsed {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(file.lines.enumerated()), id: \.offset) { i, l in row(i, l) }
                }
                .background(Theme.terminal)
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
    }

    private func selected(_ i: Int) -> Bool {
        guard let s = m.selection, s.file == file.name else { return false }
        return i >= min(s.a, s.b) && i <= max(s.a, s.b)
    }

    @ViewBuilder private func row(_ i: Int, _ l: DiffLine) -> some View {
        let (fg, bg): (Color, Color) = switch l.kind {
        case .hunk: (Theme.accent, Theme.accent.opacity(0.08))
        case .add: (Theme.green, Theme.green.opacity(0.10))
        case .del: (Theme.red, Theme.red.opacity(0.10))
        case .meta: (Theme.muted, .clear)
        case .ctx: (Theme.text2, .clear)
        }
        let anchor = l.anchor
        let here = anchor.map { a in m.notes.filter { $0.s("file") == file.name && $0.i("line") == Int64(a.line) && ($0.s("side").isEmpty ? "additions" : $0.s("side")) == a.side } } ?? []
        HStack(alignment: .top, spacing: 0) {
            Text(l.old > 0 ? "\(l.old)" : "").frame(width: 34, alignment: .trailing)
            Text(l.new > 0 ? "\(l.new)" : "").frame(width: 34, alignment: .trailing)
            Text(here.isEmpty ? " " : here.allSatisfy { $0.i("resolved_at") != 0 } ? "✓" : "●")
                .foregroundStyle(Theme.accent).frame(width: 14)
            Text(l.kind == .add ? "+" : l.kind == .del ? "−" : " ").foregroundStyle(fg).frame(width: 10)
            Text(l.text.isEmpty ? " " : l.text).foregroundStyle(fg).frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.brand(-5))
        .foregroundStyle(Theme.muted)
        .padding(.trailing, 6)
        .background(selected(i) ? Theme.accent.opacity(0.22) : bg)
        .contentShape(Rectangle())
        .onTapGesture {
            guard anchor != nil else { return }
            m.select(file.name, i, extend: NSEvent.modifierFlags.contains(.shift))
        }
        .help(anchor == nil ? "" : "Click to comment · shift-click to extend")
        .contextMenu {
            if l.new > 0 { Button("Open file at line \(l.new)") { openFileInRP(m.cwd + "/" + file.name, line: l.new) } }
        }
        ForEach(Array(here.enumerated()), id: \.offset) { _, n in
            DiffNoteRow(m: m, note: n, ws: ws, showLocation: false)
                .padding(.leading, 82).padding(.trailing, 8)
                .background(Theme.hover)
        }
    }
}
