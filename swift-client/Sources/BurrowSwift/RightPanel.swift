import AppKit
import SwiftUI
import UniformTypeIdentifiers

// MARK: - Right panel (two stacked panes of surfaces — RightPanel.vue + RPLayout.swift)

/// Open a Right Panel surface from outside it — a transcript `chat-note` row's `surface`
/// ("progress" / "plan", "background", "outputs" / "diff", "changes", "prs", "agents", "files", "terminal",
/// "browser:<url>", "doc:<path>", "sidechat:<id>", …). Shows the panel too. Unknown names are ignored.
@MainActor func openRightPanelSurface(_ surface: String) { RPStore.shared.openNamed(surface) }

struct RightPanel: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var store = RPStore.shared
    let chat: JSON
    @State private var hover: String?
    @State private var dropPane: Int?
    @State private var splitHint = false

    var cwd: String { b.workspaces[chat.i("workspace_id")]?.s("path") ?? "" }

    var body: some View {
        let l = store.layout, both = l.paneVisible(0) && l.paneVisible(1)
        GeometryReader { geo in
            VStack(spacing: 0) {
                if l.paneVisible(0) {
                    pane(0, height: geo.size.height).frame(height: both ? max(80, (geo.size.height - 5) * l.split) : nil)
                }
                if both {
                    Rectangle().fill(Theme.border).frame(height: 5)
                        .onHover { $0 ? NSCursor.resizeUpDown.push() : NSCursor.pop() }
                        .gesture(DragGesture(coordinateSpace: .named("rp")).onChanged { v in
                            store.layout.split = RPLayout.clampSplit(v.location.y / max(1, geo.size.height))
                        })
                        .help("Drag to resize")
                }
                if l.paneVisible(1) { pane(1, height: geo.size.height) }
            }
            .frame(maxHeight: .infinity, alignment: .top)
        }
        .coordinateSpace(name: "rp")
        .background(Theme.sidebar)
        .onAppear { store.attach(chat.i("workspace_id"), b) }
        .onChange(of: chat.i("workspace_id")) { _, ws in store.attach(ws, b) }
    }

    // MARK: Pane

    func pane(_ pi: Int, height: CGFloat) -> some View {
        let p = store.layout.panes[pi]
        let active = p.active.flatMap { RPSurface.of($0) != nil ? $0 : nil }
        return VStack(spacing: 0) {
            strip(pi, p)
            Group {
                if let active { surface(active) } else { picker(pi) }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(maxHeight: .infinity)
        .overlay {
            if pi == 0 && splitHint { dropHint("Drop to split below").padding(.top, height * 0.55) }
            else if dropPane == pi { dropHint("Move here") }
        }
        .onDrop(of: [.plainText], delegate: PaneDrop(pane: pi, height: height, store: store, dropPane: $dropPane, splitHint: $splitHint))
    }

    func dropHint(_ text: String) -> some View {
        Text(text).font(.brand(-3, weight: .bold)).foregroundStyle(Theme.accent)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Theme.accent.opacity(0.1), in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.accent, style: StrokeStyle(lineWidth: 2, dash: [6, 4])))
            .padding(6).allowsHitTesting(false)
    }

    func strip(_ pi: Int, _ p: RPLayout.Pane) -> some View {
        let l = store.layout, other = pi == 0 ? 1 : 0
        return HStack(spacing: 2) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 2) {
                    ForEach(p.tabs.compactMap(RPSurface.of)) { s in tab(s, pi: pi, on: p.active == s.id, other: other) }
                    iconButton("plus", "Open a surface") { store.layout.panes[pi].active = nil }
                }
            }
            if pi == 0 && !l.bottomOpen { iconButton("rectangle.split.1x2", "Split the panel") { store.layout.openBottom() } }
            if l.bottomOpen {
                iconButton(l.maximized == pi ? "arrow.down.right.and.arrow.up.left" : "arrow.up.left.and.arrow.down.right",
                           l.maximized == pi ? "Restore" : "Maximize") { store.layout.toggleMaximize(pi) }
            }
            if pi == 1 { iconButton("xmark", "Close pane (its surfaces move to the top)") { store.layout.closeBottom() } }
        }
        .padding(.horizontal, 8).frame(height: 40)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }

    func tab(_ s: RPSurface, pi: Int, on: Bool, other: Int) -> some View {
        let hovered = hover == "\(pi)\(s.id)", where_ = other == 1 ? "bottom" : "top"
        return HStack(spacing: 5) {
            ZStack {
                Image(systemName: s.icon).opacity(hovered ? 0 : 1)
                if hovered {
                    Button { store.close(s.id) } label: { Image(systemName: "xmark") }
                        .buttonStyle(.plain).help("Close \(s.label)")
                }
            }
            .frame(width: 13)
            Text(s.label).lineLimit(1)
            if hovered {
                Button { store.layout.moveTo(s.id, other) } label: { Image(systemName: other == 1 ? "arrow.down" : "arrow.up") }
                    .buttonStyle(.plain).help("Move to the \(where_) pane")
            }
        }
        .font(.brand(-4, weight: on ? .bold : .regular))
        .foregroundStyle(on ? Theme.text : Theme.muted)
        .padding(.horizontal, 9).frame(height: 28)
        .background(on ? Theme.selected : hovered ? Theme.hover : .clear, in: RoundedRectangle(cornerRadius: 7))
        .contentShape(Rectangle())
        .onTapGesture { store.layout.panes[pi].active = s.id }
        .onHover { hover = $0 ? "\(pi)\(s.id)" : (hover == "\(pi)\(s.id)" ? nil : hover) }
        .help("\(s.description) — drag or right-click to move to the \(where_) pane")
        .contextMenu {
            Button("Move to the \(where_) pane") { store.layout.moveTo(s.id, other) }
            Button("Close") { store.close(s.id) }
        }
        .onDrag {
            store.dragFrom = pi
            return NSItemProvider(object: (PaneDrop.prefix + s.id) as NSString)
        }
    }

    func iconButton(_ icon: String, _ help: String, _ run: @escaping () -> Void) -> some View {
        Button(action: run) { Image(systemName: icon).frame(width: 26, height: 26).contentShape(Rectangle()) }
            .buttonStyle(.plain).foregroundStyle(Theme.muted).help(help)
    }

    func picker(_ pi: Int) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                VStack(alignment: .leading, spacing: 3) {
                    Text("Open a surface").font(.brand(0, weight: .bold)).foregroundStyle(Theme.text)
                    Text("Choose what to show in this panel.").font(.ui(-3)).foregroundStyle(Theme.muted)
                }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 130), spacing: 8)], spacing: 8) {
                    ForEach(RPSurface.all) { s in
                        Button { store.pick(s.id, pane: pi) } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                Label(s.label, systemImage: s.icon).font(.brand(-3, weight: .bold)).foregroundStyle(Theme.text)
                                Text(s.description).font(.ui(-4)).foregroundStyle(Theme.muted).multilineTextAlignment(.leading)
                                Spacer(minLength: 0)
                            }
                            .padding(10).frame(maxWidth: .infinity, minHeight: 84, alignment: .topLeading)
                            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 12))
                            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.border))
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .padding(14)
        }
    }

    @ViewBuilder func surface(_ id: String) -> some View {
        let chatID = chat.i("id")
        switch id {
        case "changes": ChangesView(cwd: cwd, chatID: chatID).id(cwd)
        case "prs": PRPanel(cwd: cwd, workspaceID: chat.i("workspace_id")).id(cwd)
        case "files": EditorFilesView(root: cwd).id(cwd)
        case "diff": DiffSurfaceSlot(chat: chat)
        case "agents": AgentsView(parent: chat)
        case "terminal": TerminalPanelSlot(chat: chat)
        case "progress": ProgressSurface(chatID: chatID)
        case "background": BackgroundSurface(chatID: chatID).id(chatID)
        case "browser": RPBrowser(ws: chat.i("workspace_id"))
        case "devservers": RPDevServers(cwd: cwd).id(cwd)
        case "sidechat": RPSideChat(parent: chat)
        default:
            if id.hasPrefix("file:") {
                let p = String(id.dropFirst(5))
                EditorSurfaceSlot(path: p, line: store.fileLine[p] ?? 0).id(p)
            }
            else if id.hasPrefix("doc:") { RPDoc(path: String(id.dropFirst(4)), cwd: cwd).id(id) }
        }
    }
}

/// Tab drag & drop between panes. With only the top pane on screen its lower part (below 55 %) stands in
/// for the bottom pane: dropping there opens the split.
struct PaneDrop: DropDelegate {
    static let prefix = "burrow-rp-surface:"
    let pane: Int
    let height: CGFloat
    let store: RPStore
    @Binding var dropPane: Int?
    @Binding var splitHint: Bool

    @MainActor var bothVisible: Bool { store.layout.paneVisible(0) && store.layout.paneVisible(1) }

    func validateDrop(info: DropInfo) -> Bool { store.dragFrom != nil && info.hasItemsConforming(to: [.plainText]) }

    func dropUpdated(info: DropInfo) -> DropProposal? {
        MainActor.assumeIsolated {
            dropPane = pane != store.dragFrom && bothVisible ? pane : nil
            splitHint = pane == 0 && !store.layout.paneVisible(1) && info.location.y > height * 0.55
        }
        return DropProposal(operation: .move)
    }

    func dropExited(info: DropInfo) { dropPane = nil; splitHint = false }

    func performDrop(info: DropInfo) -> Bool {
        let to = pane == 0 && splitHint ? 1 : pane
        dropPane = nil; splitHint = false
        guard let item = info.itemProviders(for: [.plainText]).first else { return false }
        _ = item.loadObject(ofClass: NSString.self) { obj, _ in
            guard let s = obj as? String, s.hasPrefix(Self.prefix) else { return }
            let id = String(s.dropFirst(Self.prefix.count))
            Task { @MainActor in
                store.layout.moveTo(id, to)
                store.dragFrom = nil
            }
        }
        return true
    }
}

extension Dictionary where Key == String, Value == Any {
    var name: String { s("name") }
}
// MARK: Agents

struct AgentsView: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var nav = SubagentNav.shared
    let parent: JSON
    @State private var task = ""
    @State private var spawning = false
    @State private var error = ""

    var body: some View {
        let kids = b.allChats.filter { $0.i("parent_chat_id") == parent.i("id") && $0.i("archived_at") == 0 && $0.s("kind") != "side" }
            .sorted { $0.i("last_activity_at") > $1.i("last_activity_at") }
        // Side chats aren't listed as sub-agents but open here too.
        if let open = nav.open, b.chat(open)?.i("parent_chat_id") == parent.i("id") {
            SubagentDetailView(chatID: open, b: b).id(open)
        } else {
        VStack(spacing: 0) {
            spawnBox
            if kids.isEmpty {
                VStack(spacing: 8) {
                    Spacer()
                    Pet(state: "idle", size: 48)
                    Text("No sub-agents in this thread").font(.brand(-3)).foregroundStyle(Theme.muted)
                    Spacer()
                }.frame(maxWidth: .infinity)
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 6) {
                        ForEach(kids, id: \.id) { c in SubagentRow(c: c) }
                    }.padding(10)
                }
            }
        }
        }
    }

    var spawnBox: some View {
        VStack(alignment: .leading, spacing: 6) {
            TextField("Task for a new sub-agent…", text: $task, axis: .vertical)
                .textFieldStyle(.plain).font(.ui(-2)).foregroundStyle(Theme.text).lineLimit(1...5)
                .padding(10)
                .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
                .onSubmit { Task { await spawn() } }
            HStack {
                if !error.isEmpty { Text(error).font(.brand(-6)).foregroundStyle(Theme.red).lineLimit(3) }
                Spacer()
                if spawning { ProgressView().controlSize(.mini) }
                chip("Spawn sub-agent", Theme.accent, disabled: spawning || task.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) {
                    await spawn()
                }
            }
        }
        .padding(10)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }

    /// spawn_subagent_chat = the same Go path as `burrow spawn` from the thread: default profile / agent,
    /// the parent's launch settings when on the same transport, the task sent at once.
    func spawn() async {
        let t = task.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty, !spawning else { return }
        spawning = true
        defer { spawning = false }
        do {
            try await b.call("spawn_subagent_chat", [
                "parentChatId": parent.i("id"), "task": t, "profile": "", "agent": "", "model": "", "cwd": "",
            ])
            task = ""; error = ""
            try? await b.refresh()
        } catch { self.error = error.localizedDescription }
    }
}

struct SubagentRow: View {
    @EnvironmentObject var b: Burrow
    let c: JSON
    @State private var result: String?
    @State private var open = false
    @State private var confirmDelete = false

    var body: some View {
        let id = c.i("id"), st = b.state(id)
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 10) {
                Pet(state: st, size: 28)
                VStack(alignment: .leading, spacing: 3) {
                    Text(c.s("title")).font(.system(size: 13)).foregroundStyle(Theme.text).lineLimit(2)
                    Text(c.s("recap").isEmpty ? label(st) : c.s("recap")).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(2)
                }
                Spacer(minLength: 0)
                if ["done", "failed", "idle"].contains(st) {
                    Button { open.toggle() } label: { Image(systemName: open ? "chevron.up" : "text.alignleft") }
                        .buttonStyle(.plain).foregroundStyle(Theme.text2).help("Show result")
                }
                Button { openSubagent(id) } label: { Image(systemName: "chevron.right") }
                    .buttonStyle(.plain).foregroundStyle(Theme.text2).help("Open sub-agent")
                Button { confirmDelete = true } label: { Image(systemName: "xmark") }
                    .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Close sub-agent (deletes it)")
            }
            .closeSubagentDialog($confirmDelete, c)
            if open {
                Text(result ?? "Loading…").font(.ui(-2)).foregroundStyle(Theme.text2).textSelection(.enabled)
                    .padding(8).frame(maxWidth: .infinity, alignment: .leading)
                    .background(Theme.terminal, in: RoundedRectangle(cornerRadius: 10))
            }
        }
        .padding(10)
        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
        .task(id: "\(open)\(b.phase(id).i("turn_ended_at"))") { if open { await loadResult() } }
    }

    /// The sub-agent's last assistant message — what `collect_results` hands the parent.
    func loadResult() async {
        let page = (try? await b.call("load_chat_messages_page", ["chatId": c.i("id"), "before": -1, "limit": 40])) as? JSON
        let msgs = page?["messages"] as? [JSON] ?? []
        result = msgs.last { $0.s("role") == "assistant" && !$0.s("text").isEmpty }?.s("text") ?? "No answer yet."
    }
}

// MARK: Surfaces (chat-note rows open these via openRightPanelSurface)

/// The latest TodoWrite plan of the chat on screen (src/components/ProgressSurface.vue).
struct ProgressSurface: View {
    @EnvironmentObject var b: Burrow
    let chatID: Int64

    var body: some View {
        // b.messages is the selected chat's transcript page; the panel follows the selected chat.
        let todos = b.selectedChat == chatID ? latestTodos(b.messages) : []
        if todos.isEmpty {
            VStack(spacing: 8) {
                Spacer()
                Pet(state: "idle", size: 44)
                Text("No plan in this thread yet").font(.brand(-3)).foregroundStyle(Theme.muted)
                Spacer()
            }.frame(maxWidth: .infinity)
        } else {
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    let done = todos.filter { $0.status == "completed" }.count
                    Text("\(done) / \(todos.count) done").font(.brand(-5)).foregroundStyle(Theme.muted)
                    ForEach(Array(todos.enumerated()), id: \.offset) { _, t in
                        HStack(alignment: .top, spacing: 8) {
                            Image(systemName: t.status == "completed" ? "checkmark.circle.fill" : t.status == "in_progress" ? "circle.dotted" : "circle")
                                .foregroundStyle(t.status == "completed" ? Theme.green : t.status == "in_progress" ? Theme.accent : Theme.muted)
                            Text(t.status == "in_progress" && !t.activeForm.isEmpty ? t.activeForm : t.content)
                                .font(.ui(-2)).foregroundStyle(t.status == "completed" ? Theme.muted : Theme.text)
                                .strikethrough(t.status == "completed")
                        }
                    }
                }
                .padding(12).frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

/// Claude background shell tasks (src/components/BackgroundTasksSurface.vue), polled every 2 s while shown.
struct BackgroundSurface: View {
    @EnvironmentObject var b: Burrow
    let chatID: Int64
    @State private var fold = BgTaskFold()
    @State private var expanded: String?
    @State private var output = ""

    var body: some View {
        let tasks = fold.list
        Group {
            if tasks.isEmpty {
                VStack(spacing: 8) {
                    Spacer()
                    Pet(state: "idle", size: 44)
                    Text("No background tasks").font(.brand(-3)).foregroundStyle(Theme.muted)
                    Spacer()
                }.frame(maxWidth: .infinity)
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 6) {
                        ForEach(tasks) { t in row(t) }
                    }.padding(10)
                }
            }
        }
        .task {
            while !Task.isCancelled {
                await poll()
                try? await Task.sleep(for: .seconds(2))
            }
        }
    }

    func row(_ t: BgTask) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Circle().fill(t.status == "running" ? Theme.accent : t.status == "failed" ? Theme.red : t.status == "completed" ? Theme.green : Theme.muted)
                    .frame(width: 7, height: 7)
                VStack(alignment: .leading, spacing: 2) {
                    Text(t.description.isEmpty ? t.command : t.description).font(.ui(-2)).foregroundStyle(Theme.text).lineLimit(2)
                    Text(t.command).font(.brand(-6)).foregroundStyle(Theme.muted).lineLimit(1)
                }
                Spacer(minLength: 0)
                Text(t.status + (t.exitCode.map { " (\($0))" } ?? "")).font(.brand(-6)).foregroundStyle(Theme.muted)
                if t.status == "running" {
                    chip("Stop", Theme.red) { _ = try? await b.call("chat_stop_task", ["chatId": chatID, "taskId": t.id]) }
                }
            }
            .contentShape(Rectangle())
            .onTapGesture { expanded = expanded == t.id ? nil : t.id; output = "" }
            if expanded == t.id {
                ScrollView([.vertical, .horizontal]) {
                    Text(output.isEmpty ? (t.outputFile.isEmpty ? t.summary : "…") : output)
                        .font(.brand(-6)).foregroundStyle(Theme.text2).textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading).padding(8)
                }
                .frame(maxHeight: 260)
                .background(Theme.terminal, in: RoundedRectangle(cornerRadius: 10))
            }
        }
        .padding(10)
        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
    }

    func poll() async {
        let lines = (try? await b.call("load_chat_task_lines_since", ["chatId": "\(chatID)", "since": fold.lastOrd + 1])) as? [JSON] ?? []
        for l in lines { fold.apply(l.s("line"), ord: Int(l.i("ord"))) }
        guard let id = expanded, let t = fold.tasks[id], !t.outputFile.isEmpty else { return }
        let raw = (try? await b.call("read_file_tail", ["path": t.outputFile, "maxBytes": 32 << 10]) as? String) ?? ""
        output = raw.replacingOccurrences(of: #"\u{1b}\[[0-9;?]*[A-Za-z]"#, with: "", options: .regularExpression)
    }
}
