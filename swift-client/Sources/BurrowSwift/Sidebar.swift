import AppKit
import SwiftUI

// MARK: - Sidebar

struct Sidebar: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var store = ThreadStore.shared
    @ObservedObject var sidebar = SidebarStore.shared
    @State private var query = ""
    @AppStorage("sidebar.workingExpanded") private var showWorking = true
    @AppStorage("sidebar.snoozedExpanded") private var showSnoozed = false
    @AppStorage("sidebar.settledExpanded") private var showSettled = false
    @AppStorage("sidebar.archivedExpanded") private var showArchived = false
    @State private var settledLimit = 100
    @State private var hits: [JSON] = []
    @State private var dropTarget: Int64?

    var body: some View {
        let now = nowMs()
        let match: (JSON) -> Bool = { query.isEmpty || $0.s("title").localizedCaseInsensitiveContains(query) }
        let visible = b.chats.filter { !sidebar.isHidden($0.i("id")) && match($0) }
        // resolveSidebarSection (sidebarGroups.ts): snoozed > settled > pinned > needs-you > working > active.
        let snoozed = visible.filter { $0.i("snoozed_until") > now && !b.inFlight($0.i("id")) }
            .sorted { $0.i("snoozed_until") < $1.i("snoozed_until") }
        let notSnoozed = visible.filter { !($0.i("snoozed_until") > now && !b.inFlight($0.i("id"))) }
        let settled = notSnoozed.filter { b.settled($0) }.sorted { $0.i("last_activity_at") > $1.i("last_activity_at") }
        let live = notSnoozed.filter { !b.settled($0) }
        let pinned = live.filter { store.pinned.contains($0.i("id")) }
            .sorted { (store.pinned.firstIndex(of: $0.i("id")) ?? 0) < (store.pinned.firstIndex(of: $1.i("id")) ?? 0) }
        let rest = live.filter { !store.pinned.contains($0.i("id")) }
        let needsYou = rest.filter { ["waiting_input", "waiting_approval"].contains(b.state($0.i("id"))) }
        let working = rest.filter { b.state($0.i("id")) == "running" }
        let active = rest.filter { !b.inFlight($0.i("id")) }
        let archived = b.allChats
            .filter { $0.i("archived_at") > 0 && $0.i("parent_chat_id") == 0 && $0.s("kind") != "side" && !sidebar.isHidden($0.i("id")) }
            .filter(match)
            .sorted { $0.i("archived_at") > $1.i("archived_at") }
        let byRepo = groups(active)
        let order = sidebar.orderRepos(Array(byRepo.keys), b)
        let draggable = order.filter { !sidebar.pinnedProjects.contains($0) && $0 != 0 }
        var seenIDs = Set(visible.map { $0.i("id") })
        let content = hits.filter { seenIDs.insert($0.i("chat_id")).inserted }

        List(selection: selection) {
            if !pinned.isEmpty {
                Section {
                    ForEach(pinned, id: \.id) { c in ChatRow(chat: c, showProject: true).tag(c.i("id")) }
                } header: { SectionHeader(text: "Pinned", icon: "pin.fill", color: Theme.accent) }
            }
            if !needsYou.isEmpty {
                Section {
                    ForEach(needsYou, id: \.id) { c in ChatRow(chat: c, showProject: true).tag(c.i("id")) }
                } header: { SectionHeader(text: "Needs you", icon: "bell.fill", color: Theme.yellow) }
            }
            ForEach(order, id: \.self) { repo in
                let list = byRepo[repo] ?? []
                Section(isExpanded: expanded(repo)) {
                    ForEach(list.sorted { $0.i("last_activity_at") > $1.i("last_activity_at") }, id: \.id) { c in
                        ChatRow(chat: c).tag(c.i("id"))
                    }
                    SidebarTabsSlot(workspaceID: repo)
                } header: {
                    ProjectHeader(ws: b.workspaces[repo] ?? [:], dot: groupDot(list), drop: dropTarget == repo)
                        .modifier(ProjectDrag(repo: repo, enabled: draggable.contains(repo), shown: draggable, dropTarget: $dropTarget))
                }
            }
            if !content.isEmpty {
                Section {
                    ForEach(content, id: \.chatID) { h in SearchHitRow(hit: h).tag(h.i("chat_id")) }
                } header: { SectionHeader(text: "In chats", icon: "text.magnifyingglass", color: Theme.accent) }
            }
            if !working.isEmpty {
                Section(isExpanded: query.isEmpty ? $showWorking : .constant(true)) {
                    ForEach(working, id: \.id) { c in ChatRow(chat: c, showProject: true).tag(c.i("id")) }
                } header: { SectionHeader(text: "Working · \(working.count)", icon: "bolt.fill", color: Theme.status("running")) }
            }
            if !snoozed.isEmpty {
                Section(isExpanded: query.isEmpty ? $showSnoozed : .constant(true)) {
                    ForEach(snoozed, id: \.id) { c in ChatRow(chat: c, showProject: true).tag(c.i("id")) }
                } header: { SectionHeader(text: "Snoozed · \(snoozed.count)", icon: "alarm.fill", color: Theme.muted) }
            }
            if !settled.isEmpty {
                Section(isExpanded: query.isEmpty ? $showSettled : .constant(true)) {
                    ForEach(settled.prefix(settledLimit), id: \.id) { c in
                        ChatRow(chat: c, showProject: true).tag(c.i("id"))
                    }
                    if settled.count > settledLimit {
                        Button("Show \(settled.count - settledLimit) more") { settledLimit += 100 }
                            .buttonStyle(.borderless).font(.brand(-4)).foregroundStyle(Theme.muted)
                    }
                } header: {
                    HStack {
                        SectionHeader(text: "Settled · \(settled.count)", icon: "moon.zzz.fill", color: Theme.muted)
                        Spacer()
                        Button {
                            // Pinned threads are kept; the delete itself can be undone for 8 s.
                            sidebar.deleteWithUndo(settled.filter { !store.pinned.contains($0.i("id")) }, b)
                        } label: { Image(systemName: "trash").font(.system(size: 10)) }
                            .buttonStyle(.borderless).foregroundStyle(Theme.muted).padding(.top, 6)
                            .help("Delete all settled threads (pinned ones are kept)")
                    }
                }
            }
            if !archived.isEmpty {
                Section(isExpanded: query.isEmpty ? $showArchived : .constant(true)) {
                    ForEach(archived.prefix(100), id: \.id) { c in
                        ChatRow(chat: c, showProject: true).tag(c.i("id"))
                    }
                } header: {
                    SectionHeader(text: "Archived · \(archived.count)", icon: "archivebox.fill", color: Theme.muted)
                }
            }
        }
        .listStyle(.sidebar)
        .scrollContentBackground(.hidden)
        .background(Theme.sidebar)
        .searchable(text: $query, placement: .sidebar, prompt: "Search threads")
        .task(id: query) {
            let q = query.trimmingCharacters(in: .whitespaces)
            guard q.count >= 2 else { hits = []; return }
            try? await Task.sleep(for: .milliseconds(250)) // debounce; a newer query cancels this task
            guard !Task.isCancelled else { return }
            let r = await b.searchChats(q)
            if !Task.isCancelled { hits = r }
        }
        .threadDialogs()
        .sidebarDialogs()
        .safeAreaInset(edge: .bottom) {
            HStack(spacing: 8) {
                if let logo = Pet.image("logo") {
                    Image(nsImage: logo).resizable().frame(width: 20, height: 20).clipShape(RoundedRectangle(cornerRadius: 5))
                }
                Text("burrow").font(.brand(-2, weight: .bold)).foregroundStyle(Theme.text2)
                Spacer()
                Button { NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil) } label: { Image(systemName: "gearshape") }
                    .buttonStyle(.borderless).foregroundStyle(Theme.text2).help("Settings (⌘,)")
                Button { Shell.shared.showSpotlight(.openProject) } label: { Image(systemName: "folder.badge.plus") }
                    .buttonStyle(.borderless).foregroundStyle(Theme.text2).help("Open folder…")
                Button { store.schedulesOpen = true } label: { Image(systemName: "calendar.badge.clock") }
                    .buttonStyle(.borderless).foregroundStyle(Theme.text2).help("Scheduled prompts")
                Circle().fill(b.connected ? Theme.green : Theme.yellow).frame(width: 6, height: 6)
                Text(b.connected ? "core" : b.status).font(.brand(-4)).foregroundStyle(Theme.muted).lineLimit(1)
            }
            .padding(.horizontal, 14).padding(.vertical, 10)
            .background(Theme.sidebar)
            .overlay(alignment: .top) { Rectangle().fill(Theme.border).frame(height: 1) }
        }
    }

    /// Chats by repo + repos with terminal tabs; pinned projects always get a group (their header is the door to
    /// New chat / New worktree).
    func groups(_ active: [JSON]) -> [Int64: [JSON]] {
        var byRepo = Dictionary(grouping: active) { b.rootWorkspace($0.i("workspace_id")).i("id") }
            .merging(TerminalStore.shared.tabRoots(b)) { a, _ in a }
        for p in sidebar.pinnedProjects where b.workspaces[p] != nil && byRepo[p] == nil { byRepo[p] = [] }
        return byRepo
    }

    /// Sub-agents and side chats (a search hit can be one) open in the RP, never in the main pane.
    var selection: Binding<Int64?> {
        Binding(get: { b.selectedChat }, set: { id in
            if let id, let c = b.chat(id), c.i("parent_chat_id") != 0 { openSubagent(id) } else { b.selectedChat = id }
        })
    }

    func expanded(_ repo: Int64) -> Binding<Bool> {
        Binding(get: { !query.isEmpty || !sidebar.collapsed.contains(repo) }, set: { open in
            if open == sidebar.collapsed.contains(repo) { sidebar.toggleCollapsed(repo) }
        })
    }

    /// groupDot (sidebarGroups.ts): the most urgent state among a project's threads, shown while collapsed.
    func groupDot(_ list: [JSON]) -> String? {
        let states = list.map { b.state($0.i("id")) }
        if states.contains(where: { $0.hasPrefix("waiting") }) { return "waiting_input" }
        if states.contains("failed") { return "failed" }
        if list.contains(where: { b.unread($0.i("id")) }) { return "done" }
        return nil
    }
}

/// A project's group header: icon, name, pin, collapsed-state dot, hover "+", context menu.
struct ProjectHeader: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var sidebar = SidebarStore.shared
    let ws: JSON
    var dot: String?
    var drop = false
    @State private var hover = false

    var body: some View {
        let id = ws.i("id")
        HStack(spacing: 7) {
            ProjectIcon(ws: ws, size: 16)
            Text(ws.s("name").isEmpty ? "Other" : ws.s("name")).font(.brand(-4, weight: .bold)).foregroundStyle(Theme.text2).lineLimit(1)
            if sidebar.pinnedProjects.contains(id) { Image(systemName: "pin.fill").font(.system(size: 8)).foregroundStyle(Theme.muted).help("Pinned project") }
            if sidebar.collapsed.contains(id), let dot { Circle().fill(Theme.status(dot, unread: dot == "done")).frame(width: 6, height: 6) }
            Spacer(minLength: 0)
            if hover, id != 0 {
                Button { newThread(in: id, b) } label: { Image(systemName: "plus").font(.system(size: 10, weight: .semibold)) }
                    .buttonStyle(.borderless).foregroundStyle(Theme.text2).help("New thread in this project")
            }
        }
        .padding(.top, 6)
        .overlay(alignment: .top) { if drop { Rectangle().fill(Theme.accent).frame(height: 2) } }
        .contentShape(Rectangle())
        .onHover { hover = $0 }
        .contextMenu { if id != 0 { ProjectMenuItems(ws: ws) } }
    }
}

/// Drag a project header onto another to put it before that one (projectOrder.ts). Pinned projects don't move.
struct ProjectDrag: ViewModifier {
    let repo: Int64
    let enabled: Bool
    let shown: [Int64]
    @Binding var dropTarget: Int64?

    func body(content: Content) -> some View {
        if enabled {
            content
                .draggable("burrow-project:\(repo)")
                .dropDestination(for: String.self) { items, _ in
                    dropTarget = nil
                    guard let s = items.first, s.hasPrefix("burrow-project:"), let from = Int64(s.dropFirst("burrow-project:".count)) else { return false }
                    SidebarStore.shared.moveProject(shown: shown, drag: from, before: repo)
                    return true
                } isTargeted: { dropTarget = $0 ? repo : (dropTarget == repo ? nil : dropTarget) }
        } else {
            content
        }
    }
}

/// One search_chats hit: the chat's title + the FTS snippet.
struct SearchHitRow: View {
    @EnvironmentObject var b: Burrow
    let hit: JSON
    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(hit.s("title").isEmpty ? "Untitled" : hit.s("title")).font(.system(size: 13)).foregroundStyle(Theme.text).lineLimit(1)
            Text(hit.s("snippet")).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(2)
        }
        .padding(.vertical, 3)
        .contextMenu { if let c = b.chat(hit.i("chat_id")) { ThreadMenuItems(chat: c) } }
    }
}

extension Dictionary where Key == String, Value == Any {
    var id: Int64 { i("id") }
    var chatID: Int64 { i("chat_id") }
}

struct SectionHeader: View {
    let text: String
    let icon: String
    let color: Color
    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: icon).font(.system(size: 9)).foregroundStyle(color)
            Text(text).font(.brand(-4, weight: .bold)).foregroundStyle(Theme.text2)
        }
        .padding(.top, 6)
    }
}

struct ChatRow: View {
    @EnvironmentObject var b: Burrow
    let chat: JSON
    var showProject = false
    @State private var hovering = false
    @State private var hoverTask: Task<Void, Never>?

    var body: some View {
        let id = chat.i("id")
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            StatusIcon(state: b.state(id), unread: b.unread(id)).frame(width: 12)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 5) {
                    Text(chat.s("title").isEmpty ? "Untitled" : chat.s("title"))
                        .font(.system(size: 13, weight: b.unread(id) ? .semibold : .regular))
                        .foregroundStyle(b.unread(id) || b.inFlight(id) ? Theme.text : Theme.text.opacity(0.82))
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    if chat.i("pr_number") > 0 {
                        let g = prGlyph(chat.s("pr_state"))
                        Image(systemName: g.icon).font(.system(size: 9)).foregroundStyle(g.color).help("PR #\(chat.i("pr_number")) · \(chat.s("pr_state"))")
                    }
                    if chat.i("snoozed_until") > nowMs() { Image(systemName: "clock").font(.system(size: 9)).foregroundStyle(Theme.muted) }
                    if chat.i("slept_at") > 0 { Image(systemName: "moon").font(.system(size: 9)).foregroundStyle(Theme.muted) }
                }
                Text(subtitle(id)).font(.brand(-5)).foregroundStyle(b.inFlight(id) ? Theme.status(b.state(id)) : Theme.muted).lineLimit(1)
            }
        }
        .padding(.vertical, 3)
        .contextMenu {
            ThreadMenuItems(chat: chat)
            if let ws = b.workspaces[chat.i("workspace_id")] {
                Divider()
                Menu(ws["parent_id"] == nil ? "Project" : "Worktree") { ProjectMenuItems(ws: ws) }
            }
        }
        .onHover { inside in
            hoverTask?.cancel()
            if inside {
                hoverTask = Task { try? await Task.sleep(for: .milliseconds(700)); if !Task.isCancelled { hovering = true } }
            } else { hovering = false }
        }
        .popover(isPresented: $hovering, arrowEdge: .trailing) { ChatHoverCard(chat: chat).environmentObject(b) }
    }

    func subtitle(_ id: Int64) -> String {
        let detail = b.phase(id).s("detail")
        if b.inFlight(id), !detail.isEmpty { return detail }
        var parts: [String] = []
        if showProject { parts.append(b.rootWorkspace(chat.i("workspace_id")).s("name")) }
        if !chat.s("branch").isEmpty { parts.append(chat.s("branch")) }
        let at = chat.i("last_activity_at")
        parts.append(at > 0 ? relative(at) : "never used")
        if !chat.s("recap").isEmpty, !showProject { parts = [chat.s("recap")] }
        return parts.joined(separator: " · ")
    }
}

func relative(_ ms: Int64) -> String {
    let f = RelativeDateTimeFormatter()
    f.unitsStyle = .short
    return f.localizedString(for: Date(timeIntervalSince1970: Double(ms) / 1000), relativeTo: Date())
}

struct StatusIcon: View {
    let state: String
    var unread = false
    @State private var pulse = false

    var body: some View {
        let c = Theme.status(state, unread: unread)
        ZStack {
            if state == "running" || state.hasPrefix("waiting") {
                Circle().fill(c.opacity(0.35)).frame(width: 12, height: 12)
                    .scaleEffect(pulse ? 1.25 : 0.6).opacity(pulse ? 0 : 1)
                    .animation(.easeOut(duration: 1.2).repeatForever(autoreverses: false), value: pulse)
                    .onAppear { pulse = true }
            }
            Circle().fill(c).frame(width: 7, height: 7)
        }
        .frame(width: 12, height: 12)
    }
}

/// The row hover card (Sidebar.vue hoverInfo): title, project, path, branch, model, activity, session id, PR, status.
struct ChatHoverCard: View {
    @EnvironmentObject var b: Burrow
    let chat: JSON

    var body: some View {
        let ws = b.workspaces[chat.i("workspace_id")] ?? [:]
        let id = chat.i("id"), state = b.state(id)
        VStack(alignment: .leading, spacing: 5) {
            Text(chat.s("title").isEmpty ? "Untitled" : chat.s("title")).font(.ui(-2, weight: .semibold)).foregroundStyle(Theme.text)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 6) {
                ProjectIcon(ws: b.rootWorkspace(chat.i("workspace_id")), size: 12)
                Text(b.rootWorkspace(chat.i("workspace_id")).s("name")).foregroundStyle(Theme.text2)
            }
            Text(ws.s("path")).font(.brand(-6)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.middle)
            if !chat.s("branch").isEmpty { Label(chat.s("branch"), systemImage: "arrow.triangle.branch").foregroundStyle(Theme.text2) }
            if !chat.s("model").isEmpty { Label(chat.s("model"), systemImage: "cpu").foregroundStyle(Theme.text2) }
            if !chat.s("recap").isEmpty { Text(chat.s("recap")).foregroundStyle(Theme.text2).fixedSize(horizontal: false, vertical: true) }
            Text("Last activity \(chat.i("last_activity_at") > 0 ? relative(chat.i("last_activity_at")) : "never")").foregroundStyle(Theme.muted)
            if !chat.s("claude_session_id").isEmpty {
                Text(chat.s("claude_session_id")).font(.brand(-6)).foregroundStyle(Theme.muted).lineLimit(1).textSelection(.enabled)
            }
            if chat.i("pr_number") > 0 {
                let g = prGlyph(chat.s("pr_state"))
                Label("#\(chat.i("pr_number")) \(chat.s("pr_state"))", systemImage: g.icon).foregroundStyle(g.color)
            }
            if state != "idle" {
                HStack(spacing: 6) { StatusIcon(state: state); Text(label(state)).foregroundStyle(Theme.status(state)) }
            }
        }
        .font(.ui(-3))
        .padding(10)
        .frame(width: 260, alignment: .leading)
        .background(Theme.panel)
    }
}
