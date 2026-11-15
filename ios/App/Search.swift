import SwiftUI

/// Full-text search over every chat's messages (search_chats → chat_fts).
struct ChatSearchModifier: ViewModifier {
    let open: (Int) -> Void
    @Environment(AppModel.self) private var model
    @State private var query = ""
    @State private var hits: [Hit] = []
    @State private var task: Task<Void, Never>?

    struct Hit: Identifiable, Hashable {
        var id: Int
        var workspaceId: Int
        var title: String
        var snippet: String
    }

    func body(content: Content) -> some View {
        content
            .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .automatic), prompt: "Search chats")
            .overlay {
                if !query.trimmingCharacters(in: .whitespaces).isEmpty {
                    List(hits) { h in
                        Button { open(h.id); query = "" } label: { row(h) }
                    }
                    .listStyle(.plain)
                    .overlay { if hits.isEmpty { ContentUnavailableView.search(text: query) } }
                    .background(Color(.systemBackground))
                }
            }
            .onChange(of: query) { _, q in search(q) }
    }

    private func row(_ h: Hit) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(h.title.isEmpty ? "Chat \(h.id)" : h.title).font(.subheadline.weight(.semibold)).lineLimit(1)
                Spacer()
                if model.chat(h.id)?.archived == true { Image(systemName: "archivebox").font(.caption2).foregroundStyle(.secondary) }
            }
            if let ws = model.workspace(h.workspaceId) { Text(model.workspace(ws.repoId)?.name ?? ws.name).font(.caption2).foregroundStyle(.secondary) }
            Text(highlighted(h.snippet)).font(.caption).foregroundStyle(.secondary).lineLimit(2)
        }
        .foregroundStyle(.primary)
    }

    /// Query words in bold.
    private func highlighted(_ s: String) -> AttributedString {
        var a = AttributedString(s)
        for w in query.lowercased().split(separator: " ") where w.count > 1 {
            var from = a.startIndex
            while let r = a[from...].range(of: String(w), options: .caseInsensitive) {
                a[r].font = .caption.bold()
                a[r].foregroundColor = .primary
                from = r.upperBound
            }
        }
        return a
    }

    private func search(_ q: String) {
        task?.cancel()
        let q = q.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { hits = []; return }
        task = Task {
            try? await Task.sleep(nanoseconds: 250_000_000)
            guard !Task.isCancelled,
                  let rows = try? await model.client?.call("search_chats", ["query": q, "limit": 50]) as? [[String: Any]],
                  !Task.isCancelled else { return }
            hits = rows.map { Hit(id: int($0["chat_id"]), workspaceId: int($0["workspace_id"]),
                                  title: $0["title"] as? String ?? "", snippet: $0["snippet"] as? String ?? "") }
        }
    }
}

extension View {
    func chatSearch(open: @escaping (Int) -> Void) -> some View { modifier(ChatSearchModifier(open: open)) }
}
