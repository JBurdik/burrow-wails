import AppKit
import SwiftUI

// Thread details card — port of ThreadDetailsCard.vue + lib/threadDetails.ts: where this thread runs (workspace,
// Open in, scripts), then its git state (branch, PR, commit/push, changes), what's running, scheduled prompts, lineage.

struct FileStat: Equatable {
    let path: String
    var added = 0
    var removed = 0
    var untracked = false
}

/// `git diff --numstat` → one row per file; binary files ("-") count as nothing.
func parseNumstat(_ out: String) -> [FileStat] {
    out.split(separator: "\n").filter { $0.contains("\t") }.map { l in
        let p = l.split(separator: "\t", maxSplits: 2, omittingEmptySubsequences: false)
        return FileStat(path: p.count > 2 ? String(p[2]) : "", added: Int(p[0]) ?? 0, removed: Int(p[1]) ?? 0)
    }
}

struct ThreadDetailsCard: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var sidebar = SidebarStore.shared
    @ObservedObject private var threads = ThreadStore.shared
    let chat: JSON
    @StateObject private var git: GitModel
    @State private var files: [FileStat] = []
    @State private var targets: [JSON] = [["id": "finder", "name": "Finder"]]
    @State private var scripts: [ProjectScript] = []
    @State private var branches: [String] = []
    @State private var servers: [JSON] = []
    @State private var tasks: [(id: String, label: String)] = []
    @State private var fold = BgTaskFold()
    @State private var schedules: [JSON] = []
    @State private var pr: JSON?
    @State private var error = ""
    @AppStorage("details.changesOpen") private var changesOpen = false
    @AppStorage("details.runningOpen") private var runningOpen = false

    init(chat: JSON, cwd: String, b: Burrow) {
        self.chat = chat
        _git = StateObject(wrappedValue: GitModel(cwd: cwd, b: b))
    }

    var ws: JSON { b.workspaces[chat.i("workspace_id")] ?? [:] }
    var path: String { ws.s("path") }
    var branch: String { git.notRepo ? "" : git.status.branch }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            workspaceRow
            scriptsRow
            if !branch.isEmpty {
                Divider().padding(.vertical, 4)
                branchRow
                prRow
                commitRow
                if !files.isEmpty { changesRow }
            } else if chat.i("pr_number") > 0 {
                Divider().padding(.vertical, 4)
                prRow
            }
            if !servers.isEmpty || !tasks.isEmpty { runningRow }
            ForEach(schedules, id: \.id) { s in scheduleRow(s) }
            ForEach(lineage, id: \.chat.id) { l in lineageRow(l) }
            if !error.isEmpty {
                Text(error).font(.ui(-3)).foregroundStyle(Theme.red).lineLimit(3).padding(.horizontal, 10).padding(.vertical, 4)
            }
        }
        .font(.ui(-2))
        .padding(.vertical, 6)
        .frame(width: 300, alignment: .leading)
        .background(Theme.panel)
        .task(id: path) {
            await loadOnce()
            while !Task.isCancelled {
                await poll()
                try? await Task.sleep(for: .seconds(5))
            }
        }
    }

    // MARK: rows

    func row<C: View>(_ icon: String, tint: Color = Theme.muted, @ViewBuilder _ content: () -> C) -> some View {
        HStack(spacing: 7) {
            Image(systemName: icon).font(.system(size: 11)).foregroundStyle(tint).frame(width: 16)
            content()
        }
        .padding(.horizontal, 10).frame(minHeight: 28)
    }

    var workspaceRow: some View {
        let last = sidebar.lastTarget(path)
        let lastName = targets.first { $0.s("id") == last }?.s("name") ?? "Finder"
        return row(ws["parent_id"] == nil ? "folder" : "arrow.triangle.branch") {
            Text(ws.s("name")).foregroundStyle(Theme.text).lineLimit(1).help(path)
            Spacer(minLength: 4)
            Button { sidebar.open(path, in: last) } label: { Image(systemName: "arrow.up.forward.app") }
                .buttonStyle(.borderless).foregroundStyle(Theme.text2)
                .help(last == "finder" ? "Reveal in Finder (⌘O)" : "Open in \(lastName) (⌘O)")
            Menu {
                ForEach(targets, id: \.targetID) { t in
                    Button(t.s("id") == "finder" ? "Reveal in Finder" : "Open in \(t.s("name"))") { sidebar.open(path, in: t.s("id")) }
                }
                Button("Copy Path") { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(path, forType: .string) }
                Divider()
                if ws["parent_id"] == nil {
                    Button("Move to worktree…") { threads.moving = chat }
                } else {
                    Button("Leave worktree") { Task { await b.leaveWorktree(chat.i("id")) } }
                }
            } label: { Image(systemName: "chevron.down") }
                .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize().help("Open in…")
        }
    }

    var scriptsRow: some View {
        row("play") {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 4) {
                    ForEach(scripts.filter { !$0.commandLine.isEmpty }) { sc in
                        Button(sc.name) { Task { await TerminalStore.shared.newTab(workspaceID: ws.i("id"), cmd: sc.commandLine, name: sc.name) } }
                            .help(sc.commandLine)
                    }
                    Button { NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil) } label: { Label("Add script", systemImage: "plus") }
                        .foregroundStyle(Theme.muted)
                }
                .buttonStyle(.borderless).font(.brand(-4))
            }
        }
    }

    var branchRow: some View {
        row("arrow.triangle.branch") {
            Text(branch).foregroundStyle(Theme.text).lineLimit(1).help(branch)
            Spacer(minLength: 4)
            if !git.hasUpstream {
                Text("not pushed").foregroundStyle(Theme.muted).font(.brand(-5))
            } else if git.status.ahead + git.status.behind > 0 {
                Text("↑\(git.status.ahead) ↓\(git.status.behind)").foregroundStyle(Theme.muted).font(.brand(-5)).help("Ahead / behind upstream")
            }
            Menu {
                ForEach(branches, id: \.self) { br in
                    Button(br == branch ? "✓ \(br)" : br) { Task { await switchBranch(br) } }
                }
            } label: { Image(systemName: "chevron.down") }
                .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize().help("Switch branch")
        }
    }

    var prRow: some View {
        let n = chat.i("pr_number")
        let state = pr?.s("state").lowercased() ?? chat.s("pr_state")
        let draft = (pr?["isDraft"] as? Bool) ?? (chat.s("pr_state") == "draft")
        let checks = (pr?["checks"] as? [JSON]).map(rollupChecks) ?? "none"
        return row("arrow.triangle.pull", tint: state == "merged" ? Theme.accent : state == "open" ? Theme.green : Theme.muted) {
            if n > 0 {
                Button {
                    if let u = URL(string: pr?.s("url") ?? chat.s("pr_url")), !(pr?.s("url") ?? chat.s("pr_url")).isEmpty { NSWorkspace.shared.open(u) }
                    else { openRightPanelSurface("prs") }
                } label: {
                    HStack(spacing: 4) {
                        Text("#\(n)").foregroundStyle(Theme.text)
                        Text(draft ? "draft" : state).foregroundStyle(Theme.muted)
                        Spacer(minLength: 4)
                        if checks != "none" {
                            Text(["pass": "checks pass", "fail": "checks failing", "pending": "checks running"][checks] ?? "")
                                .foregroundStyle(checks == "pass" ? Theme.green : checks == "fail" ? Theme.red : Theme.muted)
                        }
                    }
                }.buttonStyle(.borderless)
            } else {
                Text("No pull request").foregroundStyle(Theme.muted)
                Spacer(minLength: 4)
            }
            Menu {
                Button("Link PR…") { threads.linkingPR = chat }
                if n > 0 { Button("Unlink #\(n)") { Task { await b.unlinkPR(chat.i("id")); pr = nil } } }
            } label: { Image(systemName: "chevron.down") }
                .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize().help("Link pull request")
        }
    }

    var commitRow: some View {
        row("smallcircle.filled.circle") {
            if let last = git.log.first {
                (Text(last.subject).foregroundStyle(Theme.text) + Text(" · \(last.rel)").foregroundStyle(Theme.muted))
                    .lineLimit(1).help("\(last.hash.prefix(7)) \(last.subject) — \(last.author)")
            } else {
                Text("Git").foregroundStyle(Theme.muted)
            }
            Spacer(minLength: 4)
            if let busy = git.busy { ProgressView().controlSize(.mini).help(busy) }
            Menu {
                Button("Commit") { Task { await git.commit(); await poll() } }.disabled(git.status.isEmpty)
                Button("Commit & push") { Task { if await git.commit() { await git.push() }; await poll() } }.disabled(git.status.isEmpty)
                Button("Push") { Task { await git.push(); await poll() } }
                if git.hasUpstream { Button("Pull") { Task { await git.pull(); await poll() } } }
            } label: { Image(systemName: "chevron.down") }
                .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize().help("Commit / push")
        }
    }

    var changesRow: some View {
        let added = files.reduce(0) { $0 + $1.added }, removed = files.reduce(0) { $0 + $1.removed }
        return VStack(alignment: .leading, spacing: 0) {
            Button { changesOpen.toggle() } label: {
                row(changesOpen ? "chevron.down" : "chevron.right") {
                    Text("Changes · \(files.count) \(files.count == 1 ? "file" : "files")").foregroundStyle(Theme.text2)
                    Spacer(minLength: 4)
                    Text("+\(added)").foregroundStyle(Theme.green)
                    Text("−\(removed)").foregroundStyle(Theme.red)
                }.contentShape(Rectangle())
            }.buttonStyle(.plain)
            if changesOpen {
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(files, id: \.path) { f in
                            Button { openRightPanelSurface("diff") } label: {
                                HStack(spacing: 5) {
                                    Text((f.path as NSString).lastPathComponent).foregroundStyle(Theme.text)
                                    Text((f.path as NSString).deletingLastPathComponent).foregroundStyle(Theme.muted)
                                    Spacer(minLength: 4)
                                    if f.untracked { Text("new").foregroundStyle(Theme.green) } else {
                                        if f.added > 0 { Text("+\(f.added)").foregroundStyle(Theme.green) }
                                        if f.removed > 0 { Text("−\(f.removed)").foregroundStyle(Theme.red) }
                                    }
                                }
                                .lineLimit(1).font(.ui(-3)).padding(.leading, 33).padding(.trailing, 10).frame(minHeight: 22)
                                .contentShape(Rectangle())
                            }.buttonStyle(.plain).help(f.path)
                        }
                    }
                }.frame(maxHeight: 180)
            }
        }
    }

    var runningRow: some View {
        let label = [servers.isEmpty ? nil : "\(servers.count) \(servers.count == 1 ? "server" : "servers")",
                     tasks.isEmpty ? nil : "\(tasks.count) \(tasks.count == 1 ? "task" : "tasks")"].compactMap { $0 }.joined(separator: " · ")
        return VStack(alignment: .leading, spacing: 0) {
            Button { runningOpen.toggle() } label: {
                row(runningOpen ? "chevron.down" : "chevron.right") {
                    Image(systemName: "powerplug").foregroundStyle(Theme.green).font(.system(size: 11))
                    Text("Running").foregroundStyle(Theme.text2)
                    Spacer(minLength: 4)
                    Text(label).foregroundStyle(Theme.muted)
                }.contentShape(Rectangle())
            }.buttonStyle(.plain)
            if runningOpen {
                ForEach(servers, id: \.serverPid) { s in
                    sub("globe", (s.s("command").split(separator: " ").first.map { ($0 as NSString).lastPathComponent } ?? "server"), ":\(s.i("port"))") {
                        if let u = URL(string: "http://localhost:\(s.i("port"))") { NSWorkspace.shared.open(u) }
                    }.help(s.s("command"))
                }
                ForEach(tasks, id: \.id) { t in sub("terminal", t.label, "") { openRightPanelSurface("background") } }
            }
        }
    }

    func sub(_ icon: String, _ text: String, _ trailing: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Image(systemName: icon).font(.system(size: 10)).foregroundStyle(Theme.muted)
                Text(text).foregroundStyle(Theme.text).lineLimit(1)
                Spacer(minLength: 4)
                Text(trailing).foregroundStyle(Theme.muted)
            }
            .font(.ui(-3)).padding(.leading, 33).padding(.trailing, 10).frame(minHeight: 22).contentShape(Rectangle())
        }.buttonStyle(.plain)
    }

    func scheduleRow(_ s: JSON) -> some View {
        let due = dueLabel(s.s("due_at"))
        let recurring = ((s["rule"] as? JSON)?.s("kind")).map { $0 != "once" && !$0.isEmpty } ?? false
        return row("clock") {
            Text(s.s("text")).foregroundStyle(Theme.text).lineLimit(1).help(s.s("text"))
            Spacer(minLength: 4)
            Text((s["paused"] as? Bool) == true ? "paused" : recurring ? "\(s.s("recurs")) · \(due)" : due).foregroundStyle(Theme.muted).lineLimit(1)
            Button { Task { _ = try? await b.call("chat_schedule_cancel", ["id": s.i("id")]); await poll() } } label: { Image(systemName: "xmark") }
                .buttonStyle(.borderless).foregroundStyle(Theme.muted).help("Cancel")
        }
    }

    func dueLabel(_ iso: String) -> String {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let d = f.date(from: iso) ?? ISO8601DateFormatter().date(from: iso) else { return "" }
        return Calendar.current.isDateInToday(d) ? d.formatted(date: .omitted, time: .shortened) : d.formatted(.dateTime.month(.abbreviated).day())
    }

    var lineage: [(rel: String, chat: JSON)] {
        let id = chat.i("id")
        let parent = chat.i("parent_chat_id") > 0 ? b.chat(chat.i("parent_chat_id")).map { ("parent", $0) } : nil
        let kids = b.allChats.filter { $0.i("parent_chat_id") == id && $0.i("archived_at") == 0 }
            .map { ($0.s("kind") == "side" ? "side chat" : "sub-agent", $0) }
        return (parent.map { [$0] } ?? []) + kids
    }

    func lineageRow(_ l: (rel: String, chat: JSON)) -> some View {
        Button {
            // Sub-agents and side chats never take the main pane.
            if l.rel == "parent" { b.selectedChat = l.chat.i("id") } else { openSubagent(l.chat.i("id")) }
        } label: {
            row(l.rel == "parent" ? "arrow.turn.left.up" : l.rel == "side chat" ? "bubble.left.and.bubble.right" : "person.crop.square") {
                Text(l.chat.s("title").isEmpty ? "Chat \(l.chat.i("id"))" : l.chat.s("title")).foregroundStyle(Theme.text).lineLimit(1)
                Spacer(minLength: 4)
                Text(l.rel).foregroundStyle(Theme.muted)
            }.contentShape(Rectangle())
        }.buttonStyle(.plain)
    }

    // MARK: data

    func loadOnce() async {
        if let t = try? await b.call("list_open_targets") as? [JSON], !t.isEmpty { targets = t }
        scripts = await ProjectScripts.load(b, path: path).scripts
        let refs = (try? await b.git(path, ["for-each-ref", "--format=%(refname:short)", "refs/heads"]))?.stdout ?? ""
        branches = refs.split(separator: "\n").map(String.init)
        if chat.i("pr_number") > 0, let p = try? await b.call("chat_pr_refresh", ["chatId": chat.i("id")]) as? JSON { pr = p }
    }

    func poll() async {
        await git.refresh()
        let id = chat.i("id")
        let d = try? await b.git(path, ["diff", "HEAD", "--numstat"])
        files = (d?.code == 0 ? parseNumstat(d!.stdout) : []) + git.status.untracked.map { FileStat(path: $0.path, untracked: true) }
        servers = (try? await b.call("list_dev_servers", ["workspacePath": path])) as? [JSON] ?? []
        schedules = (try? await b.call("chat_schedule_list", ["chatId": id])) as? [JSON] ?? []
        let lines = (try? await b.call("load_chat_task_lines_since", ["chatId": "\(id)", "since": fold.lastOrd + 1])) as? [JSON] ?? []
        for l in lines { fold.apply(l.s("line"), ord: Int(l.i("ord"))) }
        let live = (try? await b.call("chat_list_bg_terminals", ["chatId": id])) as? [JSON] ?? []
        tasks = fold.list.filter { $0.status == "running" }.map { ($0.id, $0.description.isEmpty ? $0.command : $0.description) }
            + live.map { ($0.s("processId"), $0.s("command")) }
    }

    func switchBranch(_ br: String) async {
        guard br != branch, let o = try? await b.git(path, ["switch", br]) else { return }
        error = o.code == 0 ? "" : o.message
        await poll()
    }
}

private extension Dictionary where Key == String, Value == Any {
    var targetID: String { s("id") }
    var serverPid: Int64 { i("pid") }
}

/// The header button + ⌘O. Shown in ThreadHeader.
struct ThreadDetailsButton: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var sidebar = SidebarStore.shared
    let chat: JSON
    @State private var open = false

    var body: some View {
        let path = b.workspaces[chat.i("workspace_id")]?.s("path") ?? ""
        HStack(spacing: 2) {
            Button { open.toggle() } label: { Image(systemName: "info.circle") }
                .buttonStyle(.borderless).foregroundStyle(Theme.text2).help("Thread details")
                .popover(isPresented: $open, arrowEdge: .bottom) {
                    ThreadDetailsCard(chat: chat, cwd: path, b: b).environmentObject(b)
                }
            // ⌘O: open the workspace in the project's last "Open in" target (openIn.ts).
            Button("") { sidebar.open(path, in: sidebar.lastTarget(path)) }
                .keyboardShortcut("o").frame(width: 0, height: 0).opacity(0).disabled(path.isEmpty)
        }
    }
}
