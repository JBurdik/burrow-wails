import SwiftUI

/// Sheet from the chat ⋯ menu: what ThreadDetailsCard.vue + the Highlights
/// surface show on the desktop — where the thread runs, its sub-agents, PR,
/// changes, pins / files / links, loose ends and scheduled prompts.
struct ThreadDetailsView: View {
    let chatId: Int
    /// Inside the Right Panel sheet: no own NavigationStack / Done.
    var embedded = false
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL

    @State private var row: [String: Any] = [:]          // full list_chats row (branch, pr_*)
    @State private var ws: [String: Any] = [:]           // list_workspaces row (path, parent_id)
    @State private var pr: [String: Any]?                 // chat_pr_refresh
    @State private var files: [ChangedFile] = []
    @State private var pins: [[String: Any]] = []
    @State private var written: [(path: String, op: String)] = []
    @State private var links: [String] = []
    @State private var looseEnds: [[String: Any]] = []
    @State private var schedules: [[String: Any]] = []
    @State private var prInput = ""
    @State private var prError: String?
    @State private var linking = false
    @State private var token: UUID?

    private var chat: Chat? { model.chat(chatId) }
    private var cwd: String { ws["path"] as? String ?? "" }
    private var subagents: [Chat] {
        model.chats.filter { $0.parentChatId == chatId && $0.kind != "side" && !$0.archived }
            .sorted { $0.lastActivityAt > $1.lastActivityAt }
    }

    var body: some View {
        if embedded { content } else { NavigationStack { content } }
    }

    private var content: some View {
            List {
                overview
                if !subagents.isEmpty {
                    Section("Sub-agents") {
                        ForEach(subagents) { c in
                            NavigationLink { ChatView(chatId: c.id) } label: {
                                HStack {
                                    StatusGlyph(status: model.status(c.id))
                                    VStack(alignment: .leading) {
                                        Text(c.displayTitle).lineLimit(1)
                                        if !c.recap.isEmpty { Text(c.recap).font(.caption).foregroundStyle(.secondary).lineLimit(2) }
                                    }
                                }
                            }
                        }
                    }
                }
                prSection
                if !files.isEmpty {
                    Section {
                        ForEach(files) { f in
                            NavigationLink { DiffView(cwd: cwd, file: f) } label: { FileRow(file: f) }
                        }
                    } header: {
                        HStack {
                            Text("Changes · \(files.count)")
                            Spacer()
                            Text("+\(files.reduce(0) { $0 + $1.added })").foregroundStyle(.green)
                            Text("−\(files.reduce(0) { $0 + $1.removed })").foregroundStyle(.red)
                        }
                    }
                }
                highlights
                if !looseEnds.isEmpty {
                    Section("Loose ends") {
                        ForEach(looseEnds.indices, id: \.self) { i in
                            let e = looseEnds[i]
                            let child = int(e["chat_id"])
                            if child > 0 {
                                NavigationLink { ChatView(chatId: child) } label: { looseLabel(e) }
                            } else {
                                looseLabel(e)
                            }
                        }
                    }
                }
                if !schedules.isEmpty {
                    Section("Scheduled") {
                        ForEach(schedules.indices, id: \.self) { i in
                            let s = schedules[i]
                            VStack(alignment: .leading, spacing: 2) {
                                Text(s["text"] as? String ?? "").lineLimit(2)
                                Text(scheduleLabel(s)).font(.caption).foregroundStyle(.secondary)
                            }
                            .swipeActions {
                                Button("Cancel", role: .destructive) { Task { await cancelSchedule(int(s["id"])) } }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Details")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { if !embedded { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } } }
            .refreshable { await load() }
        .task {
            await load()
            Task { await refreshPr() }
            await loadHighlights()
        }
        .onAppear {
            token = model.subscribe { name, payload in
                let mine = (payload as? [String: Any]).map { int($0["chat_id"]) == chatId } ?? false
                if name == "chats-changed" || (name == "chat-pins-changed" && mine) {
                    Task { @MainActor in await load() }
                }
            }
        }
        .onDisappear { model.unsubscribe(token) }
    }

    // MARK: sections

    @ViewBuilder private var overview: some View {
        Section("Overview") {
            if let c = chat {
                if !c.recap.isEmpty { Text(c.recap).font(.callout) }
                LabeledContent("Agent", value: c.agentKind.isEmpty ? "claude" : c.agentKind)
                if !c.isCodex { LabeledContent("Model", value: Catalog.modelLabel(c.model)) } else if !c.model.isEmpty {
                    LabeledContent("Model", value: c.model)
                }
                LabeledContent("Effort", value: Catalog.effortLabel(c.effort))
                LabeledContent("Permissions") {
                    let p = Catalog.permission(c.permissionMode)
                    Label(p.label, systemImage: p.icon)
                }
            }
            if let b = row["branch"] as? String, !b.isEmpty {
                LabeledContent { Text(b).lineLimit(1) } label: { Label("Branch", systemImage: "arrow.triangle.branch") }
            }
            if !ws.isEmpty {
                let worktree = int(ws["parent_id"]) > 0
                LabeledContent {
                    Text(ws["name"] as? String ?? "").lineLimit(1)
                } label: {
                    Label(worktree ? "Worktree" : "Workspace", systemImage: worktree ? "arrow.triangle.branch" : "folder")
                }
                if !cwd.isEmpty {
                    Text(cwd).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
                }
            }
        }
    }

    @ViewBuilder private var prSection: some View {
        Section("Pull request") {
            let n = int(row["pr_number"])
            if n > 0 {
                let url = (pr?["url"] as? String) ?? (row["pr_url"] as? String ?? "")
                let draft = pr?["isDraft"] as? Bool ?? (row["pr_state"] as? String == "draft")
                let state = draft ? "draft" : ((pr?["state"] as? String) ?? (row["pr_state"] as? String ?? "open")).lowercased()
                Button {
                    if let u = URL(string: url) { openURL(u) }
                } label: {
                    HStack {
                        Image(systemName: "arrow.triangle.pull").foregroundStyle(state == "merged" ? .purple : state == "open" ? .green : .secondary)
                        VStack(alignment: .leading) {
                            Text("#\(n) \(pr?["title"] as? String ?? "")").lineLimit(2).foregroundStyle(.primary)
                            Text(state).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                Button("Unlink #\(n)", role: .destructive) { Task { await unlink() } }.disabled(linking)
            } else {
                HStack {
                    TextField("#123 or URL", text: $prInput)
                        .textInputAutocapitalization(.never).autocorrectionDisabled().keyboardType(.URL)
                        .onSubmit { Task { await link() } }
                    Button("Link") { Task { await link() } }
                        .disabled(linking || prInput.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
            if let prError { Text(prError).font(.caption).foregroundStyle(.red) }
        }
    }

    @ViewBuilder private var highlights: some View {
        if !pins.isEmpty || !written.isEmpty || !links.isEmpty {
            Section("Highlights") {
                ForEach(pins.indices, id: \.self) { i in
                    let p = pins[i]
                    let target = p["target"] as? String ?? ""
                    let web = target.hasPrefix("http://") || target.hasPrefix("https://")
                    let label = Label {
                        VStack(alignment: .leading) {
                            Text(web ? target : shortPath(target)).lineLimit(1).foregroundStyle(.primary)
                            if let note = p["note"] as? String, !note.isEmpty { Text(note).font(.caption).foregroundStyle(.secondary) }
                        }
                    } icon: { Image(systemName: "pin") }
                    Group {
                        if web { Button { if let u = URL(string: target) { openURL(u) } } label: { label } }
                        else { NavigationLink { FileViewer(path: target) } label: { label } }
                    }
                    .swipeActions {
                        Button("Remove", role: .destructive) { Task { await removePin(int(p["id"])) } }
                    }
                }
                ForEach(written, id: \.path) { f in
                    NavigationLink { FileViewer(path: f.path) } label: { Label {
                        HStack { Text(shortPath(f.path)).lineLimit(1); Spacer(); Text(f.op).font(.caption).foregroundStyle(.secondary) }
                    } icon: { Image(systemName: "doc") } }
                }
                ForEach(links, id: \.self) { l in
                    Button { if let u = URL(string: l) { openURL(u) } } label: {
                        Label(l, systemImage: l.range(of: #"/(pull|merge_requests|pullrequest)/\d+"#, options: .regularExpression) != nil
                              ? "arrow.triangle.pull" : "link").lineLimit(1)
                    }
                }
            }
        }
    }

    private func looseLabel(_ e: [String: Any]) -> some View {
        let icon = ["uncommitted": "pencil", "unpushed": "arrow.up.circle", "pr": "arrow.triangle.pull",
                    "subagent": "person.2", "schedule": "clock"][e["kind"] as? String ?? ""] ?? "circle"
        return Label(e["text"] as? String ?? "", systemImage: icon)
    }

    private func scheduleLabel(_ s: [String: Any]) -> String {
        if s["paused"] as? Bool == true { return "paused" }
        // Go marshals RFC 3339, with fractional seconds only when non-zero.
        let due = (s["due_at"] as? String).flatMap {
            (try? Date($0, strategy: .iso8601)) ?? (try? Date($0, strategy: Date.ISO8601FormatStyle(includingFractionalSeconds: true)))
        }
        let when = due.map { Calendar.current.isDateInToday($0) ? $0.formatted(date: .omitted, time: .shortened)
                                                               : $0.formatted(.dateTime.month(.abbreviated).day()) } ?? ""
        let kind = (s["rule"] as? [String: Any])?["kind"] as? String ?? "once"
        let recurs = s["recurs"] as? String ?? ""
        return kind != "once" && !recurs.isEmpty ? "\(recurs) · \(when)" : when
    }

    private func shortPath(_ p: String) -> String {
        let base = cwd.hasSuffix("/") ? String(cwd.dropLast()) : cwd
        return !base.isEmpty && p.hasPrefix(base + "/") ? String(p.dropFirst(base.count + 1)) : p
    }

    // MARK: loading

    @MainActor private func load() async {
        guard let client = model.client else { return }
        async let chatsR = try? client.call("list_chats") as? [[String: Any]]
        async let wsR = try? client.call("list_workspaces") as? [[String: Any]]
        async let pinsR = try? client.call("chat_pins_list", ["chatId": chatId]) as? [[String: Any]]
        async let looseR = try? client.call("chat_loose_ends", ["chatId": chatId]) as? [[String: Any]]
        async let schedR = try? client.call("chat_schedule_list", ["chatId": chatId]) as? [[String: Any]]
        row = (await chatsR)?.first { int($0["id"]) == chatId } ?? row
        let wsId = int(row["workspace_id"])
        ws = (await wsR)?.first { int($0["id"]) == wsId } ?? ws
        pins = await pinsR ?? []
        looseEnds = await looseR ?? []
        schedules = await schedR ?? []
        await loadChanges()
    }

    /// ThreadDetailsCard.refreshGit: `diff HEAD --numstat` + untracked from status.
    @MainActor private func loadChanges() async {
        guard !cwd.isEmpty, let client = model.client else { return }
        async let diff = try? client.call("run_git", ["cwd": cwd, "args": ["diff", "HEAD", "--numstat"]]) as? [String: Any]
        async let status = try? client.call("run_git", ["cwd": cwd, "args": ["status", "--porcelain"]]) as? [String: Any]
        var out: [ChangedFile] = []
        if let d = await diff, int(d["code"]) == 0 {
            for line in (d["stdout"] as? String ?? "").split(separator: "\n") {
                let p = line.split(separator: "\t", maxSplits: 2)
                if p.count == 3 { out.append(ChangedFile(path: String(p[2]), added: Int(p[0]) ?? 0, removed: Int(p[1]) ?? 0)) }
            }
        }
        if let s = await status, int(s["code"]) == 0 {
            for line in (s["stdout"] as? String ?? "").split(separator: "\n") where line.hasPrefix("?? ") {
                out.append(ChangedFile(path: String(line.dropFirst(3)), added: 0, removed: 0, untracked: true))
            }
        }
        files = out
    }

    @MainActor private func refreshPr() async {
        guard int(row["pr_number"]) > 0 || chat != nil else { return }
        if let p = try? await model.client?.call("chat_pr_refresh", ["chatId": chatId]) as? [String: Any], int(p["number"]) > 0 { pr = p }
    }

    /// chatHighlights.ts over the stored transcript (load_chat_messages, a JSON string).
    @MainActor private func loadHighlights() async {
        guard let raw = try? await model.client?.call("load_chat_messages", ["chatId": chatId]) as? String,
              let msgs = try? JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [[String: Any]] else { return }
        let (f, l) = Highlights.project(msgs)
        written = f; links = l
    }

    // MARK: actions

    @MainActor private func link() async {
        let ref = prInput.trimmingCharacters(in: .whitespaces)
        guard ref.range(of: #"^(#?\d+|https?://\S+)$"#, options: .regularExpression) != nil else {
            prError = "Enter #123 or a pull request URL"; return
        }
        linking = true; defer { linking = false }
        do {
            pr = try await model.client?.call("chat_link_pr", ["chatId": chatId, "ref": ref]) as? [String: Any]
            prInput = ""; prError = nil
            await load()
        } catch { prError = error.localizedDescription }
    }

    @MainActor private func unlink() async {
        linking = true; defer { linking = false }
        do {
            try await model.client?.call("chat_unlink_pr", ["chatId": chatId])
            pr = nil; prError = nil
            await load()
        } catch { prError = error.localizedDescription }
    }

    @MainActor private func removePin(_ id: Int) async {
        _ = try? await model.client?.call("chat_pin_remove", ["id": id])
        pins.removeAll { int($0["id"]) == id }
    }

    @MainActor private func cancelSchedule(_ id: Int) async {
        _ = try? await model.client?.call("chat_schedule_cancel", ["id": id])
        await load()
    }
}

/// src/lib/chatHighlights.ts: files tools wrote/edited, links in replies — newest first.
enum Highlights {
    static func project(_ msgs: [[String: Any]]) -> (files: [(path: String, op: String)], links: [String]) {
        var files: [(path: String, op: String)] = []
        var links: [String] = []
        let urlRe = try! NSRegularExpression(pattern: #"https?://[^\s<>()"'`\]]+"#)
        let hostRe = #"^https?://([^/:]+\.[^/:]+|localhost|127\.0\.0\.1)(:\d+)?(/|$)"#
        for m in msgs.reversed() {
            let role = m["role"] as? String ?? "", text = m["text"] as? String ?? ""
            if role == "tool" {
                let op = ["Write", "NotebookEdit"].contains(text) ? "wrote" : ["Edit", "MultiEdit"].contains(text) ? "edited" : nil
                let input = m["toolInput"] as? [String: Any] ?? [:]
                guard let op, let p = (input["file_path"] ?? input["notebook_path"] ?? input["path"]) as? String, !p.isEmpty else { continue }
                if let i = files.firstIndex(where: { $0.path == p }) { if op == "wrote" { files[i].op = "wrote" } } else { files.append((p, op)) }
            } else if role == "assistant" {
                let prose = text.replacingOccurrences(of: #"```[\s\S]*?(```|$)"#, with: " ", options: .regularExpression)
                    .replacingOccurrences(of: #"`[^`\n]*`"#, with: " ", options: .regularExpression)
                let ns = prose as NSString
                for hit in urlRe.matches(in: prose, range: NSRange(location: 0, length: ns.length)) {
                    let url = ns.substring(with: hit.range).replacingOccurrences(of: #"[.,;:!?)*_]+$"#, with: "", options: .regularExpression)
                    if links.contains(url) || url.count > 2000 || url.range(of: hostRe, options: .regularExpression) == nil { continue }
                    links.append(url)
                }
            }
        }
        return (files, links)
    }
}

struct ChangedFile: Identifiable, Hashable {
    var path: String
    var added: Int
    var removed: Int
    var untracked = false
    var id: String { path }
}

private struct FileRow: View {
    let file: ChangedFile
    var body: some View {
        HStack {
            VStack(alignment: .leading) {
                Text((file.path as NSString).lastPathComponent).lineLimit(1)
                let dir = (file.path as NSString).deletingLastPathComponent
                if !dir.isEmpty { Text(dir).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
            }
            Spacer()
            if file.untracked {
                Text("new").font(.caption).foregroundStyle(.green)
            } else {
                if file.added > 0 { Text("+\(file.added)").font(.caption.monospacedDigit()).foregroundStyle(.green) }
                if file.removed > 0 { Text("−\(file.removed)").font(.caption.monospacedDigit()).foregroundStyle(.red) }
            }
        }
    }
}

private struct StatusGlyph: View {
    let status: Status
    var body: some View {
        switch status {
        case .running: ProgressView().controlSize(.small)
        case .permission, .waiting: Image(systemName: "hand.raised.fill").foregroundStyle(.orange)
        case .error: Image(systemName: "exclamationmark.circle.fill").foregroundStyle(.red)
        case .unread: Image(systemName: "circle.fill").font(.caption2).foregroundStyle(.blue)
        case .idle: Image(systemName: "person.crop.circle").foregroundStyle(.secondary)
        }
    }
}

/// One file's diff (run_git `diff HEAD -- path`; untracked = `--no-index /dev/null`), colorized.
private struct DiffView: View {
    let cwd: String
    let file: ChangedFile
    @Environment(AppModel.self) private var model
    @State private var lines: [String]?

    var body: some View {
        ScrollView([.vertical, .horizontal]) {
            if let lines {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(lines.indices, id: \.self) { i in
                        let l = lines[i]
                        Text(l.isEmpty ? " " : l)
                            .font(.caption.monospaced())
                            .foregroundStyle(color(l))
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 8)
                            .background(bg(l))
                    }
                }
                .textSelection(.enabled)
            } else {
                ProgressView().padding()
            }
        }
        .navigationTitle((file.path as NSString).lastPathComponent)
        .navigationBarTitleDisplayMode(.inline)
        .task {
            let args = file.untracked ? ["diff", "--no-index", "--", "/dev/null", file.path] : ["diff", "HEAD", "--", file.path]
            let out = try? await model.client?.call("run_git", ["cwd": cwd, "args": args]) as? [String: Any]
            let text = out?["stdout"] as? String ?? ""
            // Skip git's header lines; hunks start at the first "@@".
            let all = text.components(separatedBy: "\n")
            lines = Array(all.drop { !$0.hasPrefix("@@") })
            if lines?.isEmpty == true { lines = [text.isEmpty ? "No diff." : text] }
        }
    }

    private func color(_ l: String) -> Color {
        l.hasPrefix("@@") ? .blue : l.hasPrefix("+") ? .green : l.hasPrefix("-") ? .red : .primary
    }
    private func bg(_ l: String) -> Color {
        l.hasPrefix("+") ? .green.opacity(0.12) : l.hasPrefix("-") ? .red.opacity(0.12) : .clear
    }
}
