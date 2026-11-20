import SwiftUI

/// Hook for the thread list: `ArchivedSection()` — a row that pushes the Archived list.
struct ArchivedSection: View {
    @Environment(AppModel.self) private var model
    var body: some View {
        let n = model.archivedAndSettled.count
        Section {
            NavigationLink { ArchivedView() } label: {
                HStack {
                    Label("Archived", systemImage: "archivebox")
                    Spacer()
                    if n > 0 { Text("\(n)").font(.caption).foregroundStyle(.secondary) }
                }
            }
        }
    }
}

extension AppModel {
    /// Desktop's Archived shelf: settled threads (process asleep, tab kept) and archived
    /// ones (tab closed) of every project, top-level only — one list, not two sections.
    var archivedAndSettled: [Chat] {
        let now = Date().timeIntervalSince1970 * 1000
        return chats.filter { $0.parentChatId == 0 && $0.kind != "side" && ($0.archived || (settled($0) && $0.snoozedUntil <= now)) }
    }
}

/// Settled + archived threads, one folded group per project, newest first (desktop's
/// Archived shelf). Rows open the thread via the stack's `navigationDestination(for: Int.self)`.
struct ArchivedView: View {
    @Environment(AppModel.self) private var model
    @State private var archivedAt: [Int: Double] = [:]
    @State private var deleting: Chat?
    @State private var open: Set<String> = []

    private func ts(_ c: Chat) -> Double { c.archived ? (archivedAt[c.id] ?? c.lastActivityAt) : c.lastActivityAt }

    /// [(project, rows newest first)], projects ordered by their newest row.
    private var groups: [(name: String, rows: [Chat])] {
        let rows = model.archivedAndSettled.sorted { ts($0) > ts($1) }
        var order: [String] = []
        var by: [String: [Chat]] = [:]
        for c in rows {
            let k = model.projectName(c)
            if by[k] == nil { order.append(k) }
            by[k, default: []].append(c)
        }
        return order.map { ($0, by[$0]!) }
    }

    var body: some View {
        List {
            ForEach(groups, id: \.name) { g in
                DisclosureGroup(isExpanded: Binding(get: { open.contains(g.name) }, set: { if $0 { open.insert(g.name) } else { open.remove(g.name) } })) {
                    ForEach(g.rows) { chat in row(chat) }
                } label: {
                    HStack {
                        if let first = g.rows.first { ProjectBadge(name: g.name, workspaceId: first.workspaceId) }
                        Text(g.name).font(.subheadline.weight(.semibold))
                        Spacer()
                        Text("\(g.rows.count)").font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
        }
        .overlay { if groups.isEmpty { ContentUnavailableView("Nothing archived", systemImage: "archivebox") } }
        .navigationTitle("Archived")
        .confirmationDialog("Delete \"\(deleting?.displayTitle ?? "")\"?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { chat in
            Button("Delete thread", role: .destructive) { model.act { try await model.deleteChat(chat.id) } }
        } message: { _ in Text("Its transcript and sub-agents are deleted too. This can't be undone.") }
        // archived_at isn't on the app's slim Chat; read it from the full rows.
        .task(id: model.chats.filter(\.archived).count) {
            let list = (try? await model.client?.call("list_chats") as? [[String: Any]]) ?? []
            archivedAt = Dictionary(list.map { (int($0["id"]), dbl($0["archived_at"])) }, uniquingKeysWith: max)
        }
        .refreshable { await model.loadChats() }
    }

    private func row(_ chat: Chat) -> some View {
        NavigationLink(value: chat.id) {
            VStack(alignment: .leading, spacing: 2) {
                Text(chat.displayTitle).lineLimit(1)
                let at = ts(chat)
                if at > 0 {
                    Text(Date(timeIntervalSince1970: at / 1000), format: .relative(presentation: .named))
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) { deleting = chat } label: { Label("Delete", systemImage: "trash") }
            Button { bringBack(chat) } label: { Label("Bring back", systemImage: "tray.and.arrow.up") }.tint(.blue)
        }
        .contextMenu {
            Button { bringBack(chat) } label: { Label("Bring back", systemImage: "tray.and.arrow.up") }
            Button(role: .destructive) { deleting = chat } label: { Label("Delete…", systemImage: "trash") }
        }
    }

    private func bringBack(_ chat: Chat) {
        model.act { if chat.archived { try await model.unarchive(chat.id) } else { try await model.unsettle(chat) } }
    }
}
