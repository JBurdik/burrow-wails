import SwiftUI

/// Full-text search over every chat's messages (search_chats → chat_fts).
/// iOS 18+: the search tab (`ChatSearchScreen`); iOS 17: a drawer on the Threads list.
struct ChatSearchModifier: ViewModifier {
    let open: (Int) -> Void
    /// Set by the search tab: its field is the TabView's `.searchable`, so the query comes from there.
    var external: Binding<String>? = nil
    @Environment(AppModel.self) private var model
    @State private var own = ""
    private var query: String {
        get { external?.wrappedValue ?? own }
        nonmutating set { if let external { external.wrappedValue = newValue } else { own = newValue } }
    }
    @State private var hits: [Hit] = []
    @State private var task: Task<Void, Never>?

    struct Hit: Identifiable, Hashable {
        var id: Int
        var workspaceId: Int
        var title: String
        var snippet: String
    }

    func body(content: Content) -> some View {
        if external != nil {
            results(content)
        } else if #available(iOS 18, *) {
            content // the search tab has it
        } else {
            results(content.searchable(text: $own, placement: .navigationBarDrawer(displayMode: .automatic), prompt: "Search chats"))
        }
    }

    private func results(_ content: some View) -> some View {
        content
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

/// The search tab's screen (role `.search`). The field is `.searchable` on the TabView (MainTabs):
/// only there does iOS 26 put it in the bottom bar, beside the tabs, like Phone.
struct ChatSearchScreen: View {
    @Binding var query: String
    var ownField = false
    let open: (Int) -> Void

    var body: some View {
        NavigationStack {
            ContentUnavailableView("Search chats", systemImage: "magnifyingglass",
                                   description: Text("Every message of every thread."))
                .modifier(ChatSearchModifier(open: open, external: $query))
                .modifier(OwnField(on: ownField, query: $query))
                .navigationTitle("Search")
        }
    }
}

private struct OwnField: ViewModifier {
    let on: Bool
    @Binding var query: String
    func body(content: Content) -> some View {
        if on { content.searchable(text: $query, prompt: "Search chats") } else { content }
    }
}
