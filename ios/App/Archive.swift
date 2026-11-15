import SwiftUI

/// Hook for the thread list: `ArchivedSection()` — a row that pushes the Archived list.
struct ArchivedSection: View {
    var body: some View {
        Section {
            NavigationLink { ArchivedView() } label: { Label("Archived", systemImage: "archivebox") }
        }
    }
}

/// Archived threads of every project, newest archived first (desktop's Archived shelf).
/// Rows open the thread via the stack's `navigationDestination(for: Int.self)`.
struct ArchivedView: View {
    @Environment(AppModel.self) private var model
    @State private var archivedAt: [Int: Double] = [:]
    @State private var deleting: Chat?

    private var rows: [Chat] {
        model.chats.filter { $0.archived && $0.parentChatId == 0 }
            .sorted { (archivedAt[$0.id] ?? $0.lastActivityAt) > (archivedAt[$1.id] ?? $1.lastActivityAt) }
    }

    var body: some View {
        List {
            ForEach(rows) { chat in
                NavigationLink(value: chat.id) {
                    HStack(spacing: 10) {
                        ProjectBadge(name: model.projectName(chat))
                        VStack(alignment: .leading, spacing: 2) {
                            Text(chat.displayTitle).lineLimit(1)
                            HStack(spacing: 4) {
                                Text(model.projectName(chat))
                                if let at = archivedAt[chat.id], at > 0 {
                                    Text("·")
                                    Text(Date(timeIntervalSince1970: at / 1000), format: .relative(presentation: .named))
                                }
                            }
                            .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                }
                .swipeActions(edge: .trailing) {
                    Button(role: .destructive) { deleting = chat } label: { Label("Delete", systemImage: "trash") }
                    Button { model.act { try await model.unarchive(chat.id) } } label: {
                        Label("Bring back", systemImage: "tray.and.arrow.up")
                    }.tint(.blue)
                }
                .contextMenu {
                    Button { model.act { try await model.unarchive(chat.id) } } label: { Label("Bring back", systemImage: "tray.and.arrow.up") }
                    Button(role: .destructive) { deleting = chat } label: { Label("Delete…", systemImage: "trash") }
                }
            }
        }
        .overlay { if rows.isEmpty { ContentUnavailableView("Nothing archived", systemImage: "archivebox") } }
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
}
