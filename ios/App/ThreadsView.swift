import SwiftUI

/// Desktop Sidebar order: states lead (Inbox, Working), projects fold.
struct ThreadsView: View {
    @Environment(AppModel.self) private var model
    @State private var selected: Int?
    @State private var composing = false
    @AppStorage("expandedProjects") private var expandedRaw = ""

    private var expanded: Set<Int> { Set(expandedRaw.split(separator: ",").compactMap { Int($0) }) }

    var body: some View {
        NavigationSplitView {
            List(selection: $selected) {
                let all = model.threads
                let inbox = all.filter { model.needsYou($0) || model.status($0.id) == .unread }
                let working = all.filter { model.status($0.id) == .running && !model.needsYou($0) }
                let pinned = all.filter { model.isPinned($0) && !inbox.contains($0) && !working.contains($0) }
                let shown = Set((inbox + working + pinned).map(\.id))
                let rest = all.filter { !shown.contains($0.id) && !model.settled($0) }
                let now = Date().timeIntervalSince1970 * 1000
                let snoozed = all.filter { !shown.contains($0.id) && $0.snoozedUntil > now }

                if !inbox.isEmpty {
                    Section { ForEach(inbox) { row($0, showProject: true) } } header: { header("Inbox", inbox.count, "tray.full") }
                }
                if !working.isEmpty {
                    Section { ForEach(working) { row($0, showProject: true) } } header: { header("Working", working.count, "bolt.horizontal") }
                }
                if !pinned.isEmpty {
                    Section { ForEach(pinned) { row($0, showProject: true) } } header: { header("Pinned", pinned.count, "pin") }
                }
                TerminalsSection()
                Section {
                    ForEach(projects(rest)) { g in
                        let project = g.workspace
                        DisclosureGroup(isExpanded: binding(project.id)) {
                            ForEach(g.chats) { row($0, showProject: false) }
                        } label: {
                            HStack {
                                ProjectBadge(name: project.name, workspaceId: project.id)
                                Text(project.name).font(.subheadline.weight(.semibold))
                                Spacer()
                                Text("\(g.chats.count)").font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                } header: { header("Projects", nil, "folder") }
                // Desktop's docked shelves: Snoozed, then Archived (settled + archived, by project).
                if !snoozed.isEmpty {
                    Section { ForEach(snoozed) { row($0, showProject: true) } } header: { header("Snoozed", snoozed.count, "moon.zzz") }
                }
                ArchivedSection()
            }
            .listStyle(.insetGrouped)
            .refreshable { await model.loadSnapshot() }
            .overlay {
                if !model.connected && model.threads.isEmpty { ProgressView("Connecting…").controlSize(.large) }
                else if model.threads.isEmpty {
                    ContentUnavailableView("No threads yet", systemImage: "tray",
                                           description: Text("Start one with the pencil button."))
                }
            }
            .navigationTitle("Threads")
            .chatSearch { id in selected = id }
            .animation(.default, value: model.threads.map { [$0.id, model.status($0.id).hashValue] })
            .sensoryFeedback(.warning, trigger: needsCount) { old, new in new > old }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Menu {
                        if let host = model.creds.flatMap({ URL(string: $0.baseURL)?.host }) { Text(host) }
                        Button("Unpair this iPhone", role: .destructive) { model.unpair() }
                    } label: {
                        Image(systemName: model.connected ? "desktopcomputer" : "desktopcomputer.trianglebadge.exclamationmark")
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { composing = true } label: { Image(systemName: "square.and.pencil") }
                        .disabled(!model.connected)
                }
            }
            .sheet(isPresented: $composing) {
                NewThreadView { id in selected = id }
            }
            .onChange(of: model.openChat) { _, id in
                if let id { selected = id; model.openChat = nil }
            }
        } detail: {
            NavigationStack {
                if let selected {
                    ChatView(chatId: selected).id(selected)
                        .toolbar(.hidden, for: .tabBar) // a chat takes the whole screen; the bar is for the lists
                } else {
                    ContentUnavailableView("Pick a thread", systemImage: "bubble.left.and.text.bubble.right",
                                           description: Text("Or start a new one with ✎"))
                }
            }
        }
    }

    private var needsCount: Int { model.threads.filter { model.needsYou($0) }.count }

    private func row(_ chat: Chat, showProject: Bool) -> some View {
        NavigationLink(value: chat.id) {
            ThreadRow(chat: chat, status: model.status(chat.id), phase: model.phase(chat.id),
                      quietDone: model.quietDone(chat), needsYou: model.needsYou(chat),
                      project: showProject ? model.projectName(chat) : nil)
        }
        .contextMenu { ThreadMenuItems(chat: chat) }
        .swipeActions(edge: .leading) {
            if model.status(chat.id) == .unread {
                Button { model.markSeen(chat.id) } label: { Label("Read", systemImage: "envelope.open") }.tint(.blue)
            } else {
                Button { model.markUnread(chat.id) } label: { Label("Unread", systemImage: "envelope.badge") }.tint(.blue)
            }
        }
    }

    private func header(_ title: String, _ count: Int?, _ icon: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: icon)
            Text(title)
            if let count { Text("\(count)").foregroundStyle(.secondary) }
        }
    }

    struct ProjectGroup: Identifiable {
        var workspace: Workspace
        var chats: [Chat]
        var id: Int { workspace.id }
    }

    private func projects(_ chats: [Chat]) -> [ProjectGroup] {
        // chats arrive newest first, so a group's first row is its latest activity
        let byRepo = Dictionary(grouping: chats) { model.workspace($0.workspaceId)?.repoId ?? $0.workspaceId }
        return byRepo.map { repo, list in
            ProjectGroup(workspace: model.workspace(repo) ?? Workspace(id: repo, name: model.projectName(list[0]), parentId: nil), chats: list)
        }
        .sorted { ($0.chats.first?.lastActivityAt ?? 0) > ($1.chats.first?.lastActivityAt ?? 0) }
    }

    private func binding(_ id: Int) -> Binding<Bool> {
        Binding(get: { expanded.contains(id) }, set: { on in
            var s = expanded
            if on { s.insert(id) } else { s.remove(id) }
            expandedRaw = s.map(String.init).joined(separator: ",")
        })
    }
}

struct ThreadRow: View {
    let chat: Chat
    let status: Status
    let phase: Phase
    let quietDone: Bool
    let needsYou: Bool
    let project: String?

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            StatusMark(status: status, quietDone: quietDone, needsYou: needsYou)
                .frame(width: 16, height: 18)
            if let project {
                ProjectBadge(name: project, workspaceId: chat.workspaceId, size: 18).padding(.top, 1)
            }
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(chat.displayTitle)
                        .font(.body.weight(status == .unread || needsYou ? .semibold : .regular))
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if chat.slept { Image(systemName: "moon.zzz").font(.caption2).foregroundStyle(.secondary) }
                    Image(systemName: chat.isCodex ? "chevron.left.forwardslash.chevron.right" : "sparkle")
                        .font(.caption2).foregroundStyle(.tertiary)
                    Text(Date(timeIntervalSince1970: max(chat.lastActivityAt, phase.updatedAt) / 1000), format: .relative(presentation: .numeric, unitsStyle: .narrow))
                        .font(.caption2).foregroundStyle(.secondary)
                }
                if let line = subtitle {
                    Text(line).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
        .padding(.vertical, 2)
    }

    private var subtitle: String? {
        if status == .permission || status == .waiting, !phase.detail.isEmpty { return phase.detail }
        if needsYou, let q = chat.turnNeeds?.asks.first?.question { return q }
        let parts = [project, chat.recap.isEmpty ? nil : chat.recap].compactMap { $0 }.filter { !$0.isEmpty }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
}

struct StatusMark: View {
    let status: Status
    var quietDone = false
    var needsYou = false

    var body: some View {
        switch status {
        case .running:
            Image(systemName: "circle.dotted").font(.caption.bold()).foregroundStyle(.blue)
                .symbolEffect(.pulse, options: .repeating)
        case .permission: Image(systemName: "lock.fill").font(.caption).foregroundStyle(.orange)
        case .waiting: Image(systemName: "hand.raised.fill").font(.caption).foregroundStyle(.orange)
        case .error: Image(systemName: "exclamationmark.circle.fill").font(.caption).foregroundStyle(.red)
        case .unread:
            if quietDone { Image(systemName: "checkmark").font(.caption.bold()).foregroundStyle(.green) }
            else if needsYou { Image(systemName: "questionmark.bubble.fill").font(.caption).foregroundStyle(.orange) }
            else { Circle().fill(.blue).frame(width: 9, height: 9) }
        case .idle:
            if needsYou { Image(systemName: "questionmark.bubble.fill").font(.caption).foregroundStyle(.orange) }
            else { Circle().strokeBorder(.secondary.opacity(0.4), lineWidth: 1).frame(width: 8, height: 8) }
        }
    }
}

struct ProjectBadge: View {
    let name: String
    var workspaceId: Int? = nil
    var size: CGFloat = 20
    @Environment(AppModel.self) private var model

    var body: some View {
        if let id = workspaceId, let img = model.projectIcon(id) {
            Image(uiImage: img).resizable().scaledToFill()
                .frame(width: size, height: size)
                .clipShape(RoundedRectangle(cornerRadius: size / 4, style: .continuous))
        } else {
            letter
        }
    }

    @ViewBuilder
    private var letter: some View {
        // stable across launches (hashValue is seeded per process)
        let hue = Double(name.unicodeScalars.reduce(0) { ($0 &* 31 &+ Int($1.value)) % 360 }) / 360
        Text(name.prefix(1).uppercased())
            .font(.system(size: size * 0.55, weight: .bold)).foregroundStyle(.white)
            .frame(width: size, height: size)
            .background(Color(hue: hue, saturation: 0.45, brightness: 0.75), in: RoundedRectangle(cornerRadius: size / 4, style: .continuous))
    }
}

/// A project's icon (or its letter badge) as a menu / picker row image. Menus draw
/// only a rendered image, not a View, so the badge is rasterised.
struct ProjectOptionLabel: View {
    let name: String
    let workspaceId: Int
    @Environment(AppModel.self) private var model

    var body: some View {
        Label {
            Text(name)
        } icon: {
            let r = ImageRenderer(content: ProjectBadge(name: name, workspaceId: workspaceId, size: 22).environment(model))
            if let ui = r.uiImage { Image(uiImage: ui).renderingMode(.original) } else { Image(systemName: "folder") }
        }
    }
}

struct NewThreadView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var opened: (Int) -> Void

    @AppStorage("new.workspace") private var workspace = 0
    @AppStorage("new.agent") private var agent = "claude"
    @AppStorage("new.model") private var modelId = ""
    @AppStorage("new.effort") private var effort = ""
    @AppStorage("new.permission") private var permission = "default"
    @State private var text = ""
    @State private var busy = false
    @State private var error: String?
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Project", selection: $workspace) {
                        ForEach(model.workspaces.filter { $0.parentId == nil }) { ProjectOptionLabel(name: $0.name, workspaceId: $0.id).tag($0.id) }
                    }
                    Picker("Agent", selection: $agent) {
                        Text("Claude").tag("claude")
                        Text("Codex").tag("codex")
                    }.pickerStyle(.segmented)
                }
                Section {
                    TextField("What should it do?", text: $text, axis: .vertical).lineLimit(5...14).focused($focused)
                } footer: {
                    LaunchBar(isCodex: agent == "codex", model: $modelId, effort: $effort, permission: $permission).padding(.top, 6)
                }
                if let error { Text(error).foregroundStyle(.red) }
            }
            .navigationTitle("New thread")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Start") { Task { await start() } }
                        .disabled(busy || workspace == 0 || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            .onAppear {
                if model.workspace(workspace) == nil { workspace = model.workspaces.first(where: { $0.parentId == nil })?.id ?? 0 }
                focused = true
            }
        }
    }

    private func start() async {
        busy = true; defer { busy = false }
        do {
            let id = try await model.createChat(workspace: workspace, agent: agent, model: agent == "codex" ? "" : modelId,
                                                effort: effort, permission: permission, text: text)
            dismiss()
            opened(id)
        } catch { self.error = error.localizedDescription }
    }
}

/// Model · effort · permission menus, shared by the new-thread sheet and the chat composer.
struct LaunchBar: View {
    var isCodex: Bool
    @Binding var model: String
    @Binding var effort: String
    @Binding var permission: String

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                if !isCodex {
                    Menu {
                        Picker("Model", selection: $model) {
                            ForEach(Catalog.claudeModels, id: \.id) { Text($0.label).tag($0.id) }
                        }
                    } label: { chip("cpu", Catalog.modelLabel(model)) }
                }
                Menu {
                    Picker("Effort", selection: $effort) {
                        ForEach(isCodex ? Catalog.codexEfforts : Catalog.claudeEfforts, id: \.self) { Text(Catalog.effortLabel($0)).tag($0) }
                    }
                } label: { chip("gauge.with.dots.needle.50percent", Catalog.effortLabel(effort)) }
                Menu {
                    Picker("Permissions", selection: $permission) {
                        ForEach(Catalog.permissions, id: \.id) { Label($0.label, systemImage: $0.icon).tag($0.id) }
                    }
                } label: {
                    let p = Catalog.permission(permission)
                    chip(p.icon, p.label, tint: permission == "bypassPermissions" ? .red : permission == "plan" || permission == "acceptEdits" ? .accentColor : nil)
                }
            }
        }
    }

    private func chip(_ icon: String, _ text: String, tint: Color? = nil) -> some View {
        Label(text, systemImage: icon)
            .font(.caption.weight(.medium))
            .padding(.horizontal, 8).padding(.vertical, 5)
            .foregroundStyle(tint ?? .primary)
            .background((tint ?? .secondary).opacity(0.14), in: RoundedRectangle(cornerRadius: 7))
    }
}
