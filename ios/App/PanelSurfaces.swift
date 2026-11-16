import SwiftUI

// The Right Panel's Plan, Processes and Git surfaces. Same wire as the desktop:
// ProgressSurface.vue / chatTodos.ts, DevServersSurface.vue, BackgroundTasksSurface.vue
// + backgroundTasks.ts, GitPanel.vue / stores/git.ts.

/// The thread's workspace folder (list_workspaces carries `path`, the snapshot does not).
@MainActor
private func workspacePath(_ model: AppModel, _ chatId: Int) async -> String? {
    guard let ws = model.chat(chatId)?.workspaceId,
          let list = try? await model.client?.call("list_workspaces") as? [[String: Any]] else { return nil }
    return list.first { int($0["id"]) == ws }?["path"] as? String
}

/// Re-runs `body` every `seconds` while the view is on screen.
private func poll(every seconds: Double, _ body: @escaping () async -> Void) async {
    while !Task.isCancelled {
        await body()
        try? await Task.sleep(for: .seconds(seconds))
    }
}

// MARK: - Plan

struct TodoItem: Hashable {
    var content: String
    var status: String   // pending | in_progress | completed
    var activeForm: String
}

struct PlanSurface: View {
    let chatId: Int
    @Environment(AppModel.self) private var model
    @State private var todos: [TodoItem] = []
    @State private var explanation = ""
    @State private var planText = ""
    @State private var loaded = false
    @State private var token: UUID?
    @State private var reload: Task<Void, Never>?

    var body: some View {
        List {
            if !todos.isEmpty {
                Section {
                    if !explanation.isEmpty { Text(explanation).font(.callout).foregroundStyle(.secondary) }
                    ForEach(Array(todos.enumerated()), id: \.offset) { _, t in todoRow(t) }
                } header: {
                    HStack {
                        Text("Progress")
                        Spacer()
                        Text("\(todos.filter { $0.status == "completed" }.count)/\(todos.count) done")
                    }
                }
            }
            if !planText.isEmpty {
                Section("Plan") { MarkdownText(text: planText).padding(.vertical, 4) }
            }
        }
        .overlay {
            if !loaded { ProgressView() }
            else if todos.isEmpty && planText.isEmpty {
                ContentUnavailableView("No plan yet", systemImage: "checklist",
                                       description: Text("Progress on longer tasks appears here."))
            }
        }
        .refreshable { await load() }
        .task { await load() }
        .onAppear {
            token = model.subscribe { name, _ in
                guard name == "chat-event-\(chatId)" else { return }
                reload?.cancel()
                reload = Task { try? await Task.sleep(for: .seconds(1)); if !Task.isCancelled { await load() } }
            }
        }
        .onDisappear { model.unsubscribe(token); token = nil; reload?.cancel() }
    }

    private func todoRow(_ t: TodoItem) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            switch t.status {
            case "completed": Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
            case "in_progress": Image(systemName: "circle.dotted.circle").foregroundStyle(.tint)
                .symbolEffect(.pulse, options: .repeating)
            default: Image(systemName: "circle").foregroundStyle(.secondary)
            }
            Text(t.status == "in_progress" && !t.activeForm.isEmpty ? t.activeForm : t.content)
                .strikethrough(t.status == "completed")
                .foregroundStyle(t.status == "completed" ? .secondary : .primary)
                .fontWeight(t.status == "in_progress" ? .medium : .regular)
        }
        .accessibilityElement(children: .combine)
        .accessibilityValue(t.status.replacingOccurrences(of: "_", with: " "))
    }

    /// latestTodos: the newest TodoWrite row (Codex/ACP plans are projected as one
    /// by Go) is the whole list; plus the newest ExitPlanMode plan. Pages back
    /// from the end until both are found or the transcript starts.
    private func load() async {
        defer { loaded = true }
        var before = -1
        var foundTodos: [TodoItem]?, foundExpl = "", foundPlan: String?
        for _ in 0..<6 {
            guard let page = try? await model.client?.call("load_chat_messages_page",
                                                           ["chatId": chatId, "before": before, "limit": 300]) as? [String: Any] else { return }
            let msgs = page["messages"] as? [[String: Any]] ?? []
            for m in msgs.reversed() where m["role"] as? String == "tool" {
                let name = m["text"] as? String ?? ""
                let input = m["toolInput"] as? [String: Any] ?? [:]
                if foundTodos == nil, name == "TodoWrite", m["toolRawName"] != nil {
                    foundTodos = (input["todos"] as? [[String: Any]] ?? []).compactMap {
                        let c = $0["content"] as? String ?? ""
                        guard !c.isEmpty else { return nil }
                        let s = $0["status"] as? String ?? ""
                        return TodoItem(content: c, status: s == "in_progress" || s == "completed" ? s : "pending",
                                        activeForm: $0["activeForm"] as? String ?? "")
                    }
                    foundExpl = input["explanation"] as? String ?? ""
                }
                if foundPlan == nil, name == "ExitPlanMode", let p = input["plan"] as? String, !p.isEmpty { foundPlan = p }
            }
            before = int(page["start"])
            if (foundTodos != nil && foundPlan != nil) || before <= 0 { break }
        }
        todos = foundTodos ?? []
        explanation = foundExpl
        planText = foundPlan ?? ""
    }
}

// MARK: - Processes

struct DevServer: Identifiable, Hashable {
    var pid: Int, port: Int, addr: String, command: String
    var id: Int { pid }
}

/// One background shell task — src/lib/backgroundTasks.ts BgTask.
struct BgTask: Identifiable, Hashable {
    var id: String
    var toolUseId = ""
    var description = ""
    var command = ""
    var outputFile = ""
    var cwd = ""
    var status = "running"      // running | completed | failed | killed | ended
    var exitCode: Int?
    var summary = ""
    var startedAt: Double?
    var endedAt: Double?
    var ord = 0
    var backgrounded = false
    var shell = true

    var statusLabel: String {
        switch status {
        case "running": "running"
        case "completed": "finished"
        case "failed": exitCode.map { "exit \($0)" } ?? "failed"
        default: status
        }
    }
}

/// Port of applyBgTaskLine: folds the Claude stream lines Go filters for
/// (`load_chat_task_lines_since`) into the chat's background tasks.
struct BgTaskState {
    var tasks: [String: BgTask] = [:]
    var calls: [String: (command: String, description: String, at: Double?)] = [:]
    var byToolUse: [String: String] = [:]
    var lastOrd = -1

    private static let terminal: Set<String> = ["completed", "failed", "killed"]
    private static let iso: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter(); f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]; return f
    }()

    private static func ts(_ v: Any?) -> Double? {
        guard let s = v as? String else { return nil }
        let d = iso.date(from: s) ?? ISO8601DateFormatter().date(from: s)
        return d.map { $0.timeIntervalSince1970 * 1000 }
    }

    private static func firstMatch(_ pattern: String, _ s: String) -> String? {
        guard let r = s.range(of: pattern, options: .regularExpression) else { return nil }
        return String(s[r])
    }

    private mutating func entry(_ id: String, _ ord: Int) -> BgTask {
        if let e = tasks[id] { return e }
        let e = BgTask(id: id, ord: ord)
        tasks[id] = e
        return e
    }

    private mutating func link(_ e: inout BgTask, _ toolUseId: Any?) {
        guard let tu = toolUseId as? String, !tu.isEmpty else { return }
        e.toolUseId = tu
        byToolUse[tu] = e.id
        if let call = calls[tu] {
            if e.command.isEmpty { e.command = call.command }
            if e.description.isEmpty, !call.description.isEmpty { e.description = call.description }
            if e.startedAt == nil { e.startedAt = call.at }
        }
    }

    private static func setStatus(_ e: inout BgTask, _ st: String, _ at: Any? = nil) {
        if terminal.contains(e.status) && !terminal.contains(st) { return }
        e.status = st
        if st != "running", e.endedAt == nil, let n = at as? NSNumber { e.endedAt = n.doubleValue }
    }

    mutating func apply(_ line: String, ord: Int) {
        lastOrd = max(lastOrd, ord)
        guard let m = (try? JSONSerialization.jsonObject(with: Data(line.utf8))) as? [String: Any] else { return }
        let type = m["type"] as? String
        if type == "assistant" {
            let at = Self.ts(m["timestamp"])
            let content = (m["message"] as? [String: Any])?["content"] as? [[String: Any]] ?? []
            for b in content where b["type"] as? String == "tool_use" && b["name"] as? String == "Bash" {
                guard let id = b["id"] as? String else { continue }
                let input = b["input"] as? [String: Any] ?? [:]
                calls[id] = (input["command"] as? String ?? "", input["description"] as? String ?? "", at)
                if let known = byToolUse[id], var e = tasks[known] { link(&e, id); tasks[known] = e }
            }
            return
        }
        if type == "user" {
            let res = m["tool_use_result"] as? [String: Any] ?? [:]
            guard let id = res["backgroundTaskId"] as? String, !id.isEmpty else { return }
            var e = entry(id, ord)
            e.backgrounded = true
            let content = (m["message"] as? [String: Any])?["content"] as? [[String: Any]] ?? []
            for b in content where b["type"] as? String == "tool_result" {
                link(&e, b["tool_use_id"])
                let text: String = (b["content"] as? String)
                    ?? (b["content"] as? [[String: Any]] ?? []).compactMap { $0["text"] as? String }.joined(separator: "\n")
                if e.outputFile.isEmpty, let hit = Self.firstMatch(#"Output is being written to: \S+?\.output\b"#, text) {
                    e.outputFile = String(hit.dropFirst("Output is being written to: ".count))
                }
            }
            if e.startedAt == nil, let at = Self.ts(m["timestamp"]) {
                e.startedAt = at - dbl(res["timedOutAfterMs"])
            }
            tasks[id] = e
            return
        }
        guard type == "system" else { return }
        let taskId = m["task_id"] as? String ?? ""
        switch m["subtype"] as? String {
        case "task_started":
            guard !taskId.isEmpty else { return }
            var e = entry(taskId, ord)
            if let tt = m["task_type"] as? String, !tt.isEmpty, tt != "local_bash" { e.shell = false }
            if m["is_backgrounded"] as? Bool == true { e.backgrounded = true }
            if let d = m["description"] as? String, !d.isEmpty { e.description = d }
            link(&e, m["tool_use_id"])
            tasks[taskId] = e
        case "task_updated":
            guard var e = tasks[taskId] else { return }
            let p = m["patch"] as? [String: Any] ?? [:]
            if p["is_backgrounded"] as? Bool == true {
                e.backgrounded = true
                if e.status == "ended" { e.status = "running" }
            }
            if let st = p["status"] as? String, Self.terminal.contains(st) { Self.setStatus(&e, st, p["end_time"]) }
            else if let end = p["end_time"] as? NSNumber, e.endedAt == nil { e.endedAt = end.doubleValue }
            tasks[taskId] = e
        case "task_notification":
            guard var e = tasks[taskId] else { return }
            if let f = m["output_file"] as? String, !f.isEmpty, e.outputFile.isEmpty { e.outputFile = f }
            if let s = m["summary"] as? String {
                e.summary = s
                if let hit = Self.firstMatch(#"exit code -?\d+"#, s) { e.exitCode = Int(hit.dropFirst("exit code ".count)) }
            }
            let st: String = switch m["status"] as? String {
            case "completed": e.exitCode.map { $0 != 0 } == true ? "failed" : "completed"
            case "failed": "failed"
            case "stopped": "killed"
            default: "ended"
            }
            Self.setStatus(&e, st)
            tasks[taskId] = e
        case "background_tasks_changed":
            let live = Set((m["tasks"] as? [[String: Any]] ?? []).compactMap { $0["task_id"] as? String })
            for (id, var e) in tasks where e.backgrounded && e.status == "running" && e.ord < ord && !live.contains(id) {
                Self.setStatus(&e, "ended")
                tasks[id] = e
            }
        default: break
        }
    }

    var list: [BgTask] { tasks.values.filter { $0.backgrounded && $0.shell }.sorted { $0.ord < $1.ord } }
}

struct ProcessesSurface: View {
    let chatId: Int
    @Environment(AppModel.self) private var model
    @State private var cwd: String?
    @State private var servers: [DevServer] = []
    @State private var bg = BgTaskState()
    @State private var codex: [BgTask] = []
    @State private var error: String?
    @State private var confirmKill: DevServer?

    private var tasks: [BgTask] { bg.list + codex }

    var body: some View {
        List {
            if let error { Text(error).font(.footnote).foregroundStyle(.red) }
            Section("Dev servers") {
                if servers.isEmpty {
                    Text(cwd == nil ? "Workspace folder unknown." : "No dev server is running in this workspace.")
                        .foregroundStyle(.secondary)
                }
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
            Section("Background tasks") {
                if tasks.isEmpty { Text("The agent has no background tasks.").foregroundStyle(.secondary) }
                ForEach(tasks) { t in
                    NavigationLink { TaskOutputView(chatId: chatId, task: t, stop: { await stop(t) }) } label: { taskRow(t) }
                        .swipeActions {
                            if t.status == "running" { Button("Stop", role: .destructive) { Task { await stop(t) } } }
                        }
                }
            }
        }
        .confirmationDialog("Kill this dev server?", isPresented: Binding(get: { confirmKill != nil }, set: { if !$0 { confirmKill = nil } }),
                            titleVisibility: .visible, presenting: confirmKill) { s in
            Button("Kill :\(s.port) (pid \(s.pid))", role: .destructive) { Task { await kill(s) } }
        }
        .refreshable { await refresh() }
        .task {
            cwd = await workspacePath(model, chatId)
            await poll(every: 3) { await refresh() }
        }
    }

    private func taskRow(_ t: BgTask) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                switch t.status {
                case "running": ProgressView().controlSize(.mini)
                case "completed": Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                case "failed": Image(systemName: "xmark.circle.fill").foregroundStyle(.red)
                default: Image(systemName: "minus.circle").foregroundStyle(.secondary)
                }
                Text(t.description.isEmpty ? t.command : t.description).lineLimit(1)
                Spacer()
                Text(t.statusLabel).font(.caption).foregroundStyle(.secondary)
            }
            if !t.description.isEmpty, !t.command.isEmpty {
                Text(t.command).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(2)
            }
        }
    }

    private func refresh() async {
        guard let client = model.client else { return }
        if let cwd, let list = try? await client.call("list_dev_servers", ["workspacePath": cwd]) as? [[String: Any]] {
            servers = list.map { DevServer(pid: int($0["pid"]), port: int($0["port"]), addr: $0["addr"] as? String ?? "",
                                           command: $0["command"] as? String ?? "") }
        }
        if let lines = try? await client.call("load_chat_task_lines_since",
                                              ["chatId": String(chatId), "since": bg.lastOrd + 1]) as? [[String: Any]], !lines.isEmpty {
            var s = bg
            for l in lines { s.apply(l["line"] as? String ?? "", ord: int(l["ord"])) }
            bg = s
        }
        // Codex background terminals: polled, no stream event marks them. A failed
        // list must not end live rows (mergeCodexTerminals).
        if let live = try? await client.call("chat_list_bg_terminals", ["chatId": chatId]) as? [[String: Any]] {
            let ids = Set(live.compactMap { $0["processId"] as? String })
            var rows = codex.map { r in var r = r; if r.status == "running", !ids.contains(r.id) { r.status = "ended" }; return r }
            for t in live {
                guard let pid = t["processId"] as? String, !rows.contains(where: { $0.id == pid && $0.status == "running" }) else { continue }
                rows.removeAll { $0.id == pid }
                rows.append(BgTask(id: pid, command: t["command"] as? String ?? "", cwd: t["cwd"] as? String ?? "",
                                   backgrounded: true))
            }
            codex = rows
        }
    }

    private func kill(_ s: DevServer) async {
        do { try await model.client?.call("kill_dev_server", ["pid": s.pid]); error = nil }
        catch { self.error = error.localizedDescription }
        await refresh()
    }

    private func stop(_ t: BgTask) async {
        do { try await model.client?.call("chat_stop_task", ["chatId": chatId, "taskId": t.id]); error = nil }
        catch { self.error = error.localizedDescription }
        await refresh()
    }
}

/// A background task's output: the tail of the file the CLI writes it to,
/// read through a TextTerm so \r progress bars and colours render as text.
private struct TaskOutputView: View {
    let chatId: Int
    let task: BgTask
    let stop: () async -> Void
    @Environment(AppModel.self) private var model
    @State private var lines: [String] = []
    @State private var error: String?
    @State private var confirmStop = false

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView([.vertical, .horizontal]) {
                LazyVStack(alignment: .leading, spacing: 0) {
                    if !task.command.isEmpty {
                        Text("$ " + task.command).foregroundStyle(.secondary).padding(.bottom, 6)
                    }
                    if let error { Text(error).foregroundStyle(.red) }
                    ForEach(Array(lines.enumerated()), id: \.offset) { _, l in
                        Text(l.isEmpty ? " " : l).fixedSize(horizontal: true, vertical: true)
                    }
                    Color.clear.frame(height: 1).id("end")
                }
                .font(.system(.caption, design: .monospaced))
                .textSelection(.enabled)
                .padding(8)
            }
            .defaultScrollAnchor(.bottom)
            .background(Color(.secondarySystemBackground))
            .onChange(of: lines) { proxy.scrollTo("end", anchor: .bottom) }
        }
        .overlay {
            if task.outputFile.isEmpty {
                ContentUnavailableView("No output", systemImage: "text.alignleft",
                                       description: Text(task.cwd.isEmpty ? "This task has no output file." : "Codex lists no output for its background terminals."))
            }
        }
        .navigationTitle(task.description.isEmpty ? "Task" : task.description)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if task.status == "running" {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Stop", systemImage: "stop.circle", role: .destructive) { confirmStop = true }
                }
            }
        }
        .confirmationDialog("Stop this background task?", isPresented: $confirmStop, titleVisibility: .visible) {
            Button("Stop task", role: .destructive) { Task { await stop() } }
        }
        .task {
            guard !task.outputFile.isEmpty else { return }
            await poll(every: 2) {
                do {
                    let text = try await model.client?.call("read_file_tail", ["path": task.outputFile, "maxBytes": 32 << 10]) as? String ?? ""
                    let term = TextTerm()
                    term.feed(Array(text.utf8))
                    lines = term.render()
                    error = nil
                } catch {
                    self.error = "\(error.localizedDescription)".contains("no such file") ? "Output file is gone." : error.localizedDescription
                }
            }
        }
    }
}

// MARK: - Git

struct GitFile: Identifiable, Hashable {
    enum Kind: Hashable { case staged, unstaged, untracked }
    var path: String
    var code: Character
    var kind: Kind
    var id: String { "\(kind)-\(path)" }
}

struct GitCommit: Identifiable, Hashable {
    var hash: String, subject: String, author: String, when: String
    var id: String { hash }
}

struct GitSurface: View {
    let chatId: Int
    @Environment(AppModel.self) private var model
    @State private var cwd: String?
    @State private var branch = ""
    @State private var upstream = false
    @State private var ahead = 0
    @State private var behind = 0
    @State private var files: [GitFile] = []
    @State private var commits: [GitCommit] = []
    @State private var message = ""
    @State private var busy: String?
    @State private var error: String?
    @State private var discard: GitFile?
    @State private var loaded = false

    private func of(_ k: GitFile.Kind) -> [GitFile] { files.filter { $0.kind == k } }

    var body: some View {
        List {
            if let error {
                Text(error).font(.footnote.monospaced()).foregroundStyle(.red).textSelection(.enabled)
            }
            Section {
                HStack {
                    Label(branch.isEmpty ? "(detached)" : branch, systemImage: "arrow.triangle.branch").fontWeight(.medium)
                    Spacer()
                    if upstream {
                        Text("↑\(ahead) ↓\(behind)").font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                            .accessibilityLabel("\(ahead) ahead, \(behind) behind")
                    } else if !branch.isEmpty {
                        Text("No upstream").font(.caption).foregroundStyle(.secondary)
                    }
                }
                HStack(spacing: 8) {
                    action("Fetch", "arrow.down.circle") { try await git(["fetch"]) }
                    action("Pull", "arrow.down.to.line") { try await git(["pull", "--ff-only"]) }
                    action("Push", "arrow.up.to.line") {
                        try await git(upstream ? ["push"] : ["push", "-u", "origin", branch])
                    }
                }
                .buttonStyle(.bordered).buttonBorderShape(.roundedRectangle)
            }
            Section {
                NavigationLink { PullRequestsView(cwd: cwd ?? "") } label: { Label("Pull requests", systemImage: "arrow.triangle.pull") }
                    .disabled(cwd == nil)
            }
            Section("Commit") {
                TextField("Message (empty = generate)", text: $message, axis: .vertical).lineLimit(1...6)
                HStack {
                    action("Generate", "sparkles") { try await generate() }
                        .buttonStyle(.bordered).buttonBorderShape(.roundedRectangle)
                    Spacer()
                    action("Commit", "checkmark") { try await commit() }
                        .buttonStyle(.borderedProminent).buttonBorderShape(.roundedRectangle)
                        .disabled(files.isEmpty)
                }
                if let busy { Label(busy, systemImage: "hourglass").font(.caption).foregroundStyle(.secondary) }
            }
            fileSection("Staged", .staged)
            fileSection("Changes", .unstaged)
            fileSection("Untracked", .untracked)
            if loaded && files.isEmpty {
                Section { Text("Working tree clean.").foregroundStyle(.secondary) }
            }
            if !commits.isEmpty {
                Section("Recent commits") {
                    ForEach(commits) { c in
                        NavigationLink {
                            GitDiffView(cwd: cwd ?? "", title: c.hash, args: ["show", "--stat", "--patch", c.hash])
                        } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(c.subject).lineLimit(2)
                                Text("\(c.hash) · \(c.author) · \(c.when)").font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
        }
        .overlay { if !loaded { ProgressView() } }
        .confirmationDialog("Discard changes to this file?", isPresented: Binding(get: { discard != nil }, set: { if !$0 { discard = nil } }),
                            titleVisibility: .visible, presenting: discard) { f in
            Button("Discard \(f.path)", role: .destructive) { run("Discarding…") { try await git(["checkout", "--", f.path]) } }
        } message: { _ in Text("The uncommitted changes are lost for good.") }
        .refreshable { await refresh() }
        .task {
            cwd = await workspacePath(model, chatId)
            if cwd == nil { error = "Workspace folder unknown."; loaded = true; return }
            await poll(every: 5) { await refresh() }
        }
    }

    @ViewBuilder
    private func fileSection(_ title: String, _ kind: GitFile.Kind) -> some View {
        let list = of(kind)
        if !list.isEmpty {
            Section {
                ForEach(list) { f in
                    NavigationLink {
                        GitDiffView(cwd: cwd ?? "", title: f.path, args: diffArgs(f))
                    } label: {
                        HStack(spacing: 10) {
                            Text(String(f.code)).font(.caption.monospaced().weight(.bold)).foregroundStyle(color(f.code)).frame(width: 14)
                            VStack(alignment: .leading, spacing: 1) {
                                Text((f.path as NSString).lastPathComponent).lineLimit(1)
                                let dir = (f.path as NSString).deletingLastPathComponent
                                if !dir.isEmpty { Text(dir).font(.caption).foregroundStyle(.secondary).lineLimit(1).truncationMode(.head) }
                            }
                        }
                    }
                    .swipeActions(edge: .leading) {
                        if kind == .staged {
                            Button("Unstage") { run("Unstaging…") { try await git(["reset", "HEAD", "--", f.path]) } }.tint(.orange)
                        } else {
                            Button("Stage") { run("Staging…") { try await git(["add", "--", f.path]) } }.tint(.green)
                        }
                    }
                    .swipeActions(edge: .trailing) {
                        if kind == .unstaged { Button("Discard", role: .destructive) { discard = f } }
                    }
                }
            } header: {
                HStack {
                    Text("\(title) (\(list.count))")
                    Spacer()
                    if kind == .staged {
                        Button("Unstage all") { run("Unstaging…") { try await git(["reset", "HEAD"]) } }
                    } else {
                        Button("Stage all") { run("Staging…") { try await git(["add", "-A"]) } }
                    }
                }
                .textCase(nil).font(.caption).disabled(busy != nil)
            }
        }
    }

    private func diffArgs(_ f: GitFile) -> [String] {
        switch f.kind {
        case .staged: ["diff", "--cached", "--", f.path]
        case .unstaged: ["diff", "--", f.path]
        case .untracked: ["diff", "--no-index", "--", "/dev/null", f.path]
        }
    }

    private func color(_ c: Character) -> Color {
        switch c {
        case "A", "?": .green
        case "D": .red
        case "R", "C": .blue
        case "U": .purple
        default: .orange
        }
    }

    private func action(_ title: String, _ icon: String, _ body: @escaping () async throws -> Void) -> some View {
        Button { run(title + "…", body) } label: { Label(title, systemImage: icon).frame(maxWidth: .infinity) }
            .disabled(busy != nil)
    }

    private func run(_ label: String, _ body: @escaping () async throws -> Void) {
        guard busy == nil else { return }
        busy = label
        Task {
            do { try await body(); error = nil } catch { self.error = error.localizedDescription }
            busy = nil
            await refresh()
        }
    }

    /// run_git; a non-zero exit is an error carrying git's stderr.
    @discardableResult
    private func git(_ args: [String]) async throws -> String {
        guard let cwd, let client = model.client else { throw BurrowError.server("Not connected.") }
        let out = try await client.call("run_git", ["cwd": cwd, "args": args]) as? [String: Any] ?? [:]
        guard out["success"] as? Bool == true else {
            let err = (out["stderr"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            throw BurrowError.server(err.isEmpty ? "git \(args.first ?? "") failed" : err)
        }
        return out["stdout"] as? String ?? ""
    }

    /// Nothing staged ⇒ stage everything first, as the desktop commit does.
    private func stageAllIfNeeded() async throws {
        if of(.staged).isEmpty { try await git(["add", "-A"]) }
    }

    private func generate() async throws {
        guard let cwd, let client = model.client else { return }
        try await stageAllIfNeeded()
        // stores/git.ts textGenPrefs(): uiPrefs in the Mac's config.json.
        var prefs: [String: Any] = [:]
        if let raw = try? await client.call("read_config") as? String,
           let cfg = try? JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [String: Any] {
            if let p = cfg["uiPrefs"] as? [String: Any] { prefs = p }
            else if let s = cfg["uiPrefs"] as? String, let p = try? JSONSerialization.jsonObject(with: Data(s.utf8)) as? [String: Any] { prefs = p }
        }
        let out = try await client.call("generate_commit_message", [
            "cwd": cwd,
            "model": prefs["textGenerationModel"] as? String ?? "",
            "policy": prefs["textGenerationPolicy"] as? String ?? "",
            "rules": prefs["textGenerationRules"] as? String ?? "",
        ]) as? [String: Any] ?? [:]
        guard int(out["code"]) == 0 else {
            throw BurrowError.server(out["stderr"] as? String ?? "Commit message generation failed.")
        }
        message = (out["stdout"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func commit() async throws {
        try await stageAllIfNeeded()
        if message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { try await generate() }
        let msg = message.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !msg.isEmpty else { throw BurrowError.server("No commit message.") }
        try await git(["commit", "-m", msg])
        message = ""
    }

    private func refresh() async {
        guard cwd != nil else { return }
        defer { loaded = true }
        guard let status = try? await git(["status", "--porcelain"]) else { return }
        var fs: [GitFile] = []
        for line in status.split(separator: "\n") where line.count > 3 {
            let x = line[line.startIndex], y = line[line.index(after: line.startIndex)]
            var path = String(line.dropFirst(3))
            if let r = path.range(of: " -> ") { path = String(path[r.upperBound...]) }
            if path.hasPrefix("\""), path.hasSuffix("\""), path.count > 1 { path = String(path.dropFirst().dropLast()) }
            if x == "?" { fs.append(GitFile(path: path, code: "?", kind: .untracked)); continue }
            if x != " " { fs.append(GitFile(path: path, code: x, kind: .staged)) }
            if y != " " { fs.append(GitFile(path: path, code: y, kind: .unstaged)) }
        }
        files = fs
        branch = (try? await git(["branch", "--show-current"]))?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if let counts = try? await git(["rev-list", "--left-right", "--count", "@{upstream}...HEAD"]) {
            let n = counts.split(whereSeparator: \.isWhitespace).compactMap { Int($0) }
            upstream = n.count == 2
            if n.count == 2 { behind = n[0]; ahead = n[1] }
        } else {
            upstream = false
        }
        if let log = try? await git(["log", "-20", "--format=%h%x1f%s%x1f%an%x1f%cr"]) {
            commits = log.split(separator: "\n").compactMap {
                let p = $0.split(separator: "\u{1f}", omittingEmptySubsequences: false).map(String.init)
                return p.count == 4 ? GitCommit(hash: p[0], subject: p[1], author: p[2], when: p[3]) : nil
            }
        }
    }
}
