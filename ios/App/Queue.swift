import SwiftUI

/// A `chat_queue` row as Go sends it (chatqueue.go).
struct QueueItem: Identifiable, Hashable {
    var id: Int
    var text: String
    var attachments: [String]
    var attempts: Int
    init(_ d: [String: Any]) {
        id = int(d["id"]); text = d["text"] as? String ?? ""
        attachments = d["attachments"] as? [String] ?? []; attempts = int(d["attempts"])
    }
}

/// The chat's queued follow-ups (Go's chat_queue): edit, reorder, remove, steer now.
/// Go drains it itself once the turn settles; this only mirrors and edits it.
struct QueuePanel: View {
    @Environment(AppModel.self) private var model
    let chatId: Int
    @State private var items: [QueueItem] = []
    @State private var token: UUID?
    @State private var editing: QueueItem?
    @State private var steering: Int?
    @State private var problem: String?

    private var canSteer: Bool { model.phase(chatId).state == "running" }

    var body: some View {
        Group {
            if !items.isEmpty || model.outbox.contains(where: { $0.chatId == chatId }) {
                VStack(alignment: .leading, spacing: 4) {
                    if !items.isEmpty {
                        Label("Queued · sent when the turn ends", systemImage: "text.line.first.and.arrowtriangle.forward")
                            .font(.caption2).foregroundStyle(.secondary)
                    }
                    ForEach(Array(items.enumerated()), id: \.element.id) { i, item in row(item, index: i) }
                    PendingOutboxRows(chatId: chatId)
                }
                .padding(.horizontal, 12).padding(.vertical, 6)
                .background(.bar)
                .animation(.default, value: items)
            }
        }
        .task(id: chatId) { await start() }
        .onDisappear { model.unsubscribe(token); token = nil }
        .sheet(item: $editing) { item in
            QueueEditor(item: item, canSteer: canSteer,
                        save: { text in Task { await update(item, text) } },
                        steer: { text in Task { await steer(item, text: text) } })
        }
        .alert("Not steered", isPresented: Binding(get: { problem != nil }, set: { if !$0 { problem = nil } })) {
            Button("OK") {}
        } message: { Text(problem ?? "") }
    }

    private func row(_ item: QueueItem, index: Int) -> some View {
        HStack(spacing: 8) {
            Button { editing = item } label: {
                HStack(spacing: 6) {
                    Text(item.text.isEmpty ? "(attachment)" : item.text)
                        .font(.callout).lineLimit(2).multilineTextAlignment(.leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if !item.attachments.isEmpty { Image(systemName: "paperclip").font(.caption) }
                    if item.attempts >= 5 {
                        Image(systemName: "exclamationmark.triangle.fill").font(.caption).foregroundStyle(.orange)
                            .accessibilityLabel("Failed to send, edit to retry")
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if steering == item.id {
                ProgressView().controlSize(.small)
            } else if canSteer {
                Button { Task { await steer(item, text: item.text) } } label: {
                    Image(systemName: "arrow.turn.down.right").font(.callout.weight(.semibold))
                }
                .accessibilityLabel("Send as steer now")
            }
            Menu {
                Button { editing = item } label: { Label("Edit", systemImage: "pencil") }
                if canSteer {
                    Button { Task { await steer(item, text: item.text) } } label: { Label("Send as steer now", systemImage: "arrow.turn.down.right") }
                }
                if index > 0 {
                    Button { move(index, to: index - 1) } label: { Label("Move up", systemImage: "arrow.up") }
                }
                if index < items.count - 1 {
                    Button { move(index, to: index + 1) } label: { Label("Move down", systemImage: "arrow.down") }
                }
                Button(role: .destructive) { Task { await remove(item) } } label: { Label("Delete", systemImage: "trash") }
            } label: {
                Image(systemName: "ellipsis").frame(width: 28, height: 28).contentShape(Rectangle())
            }
            .foregroundStyle(.secondary)
        }
        .padding(8)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
    }

    // MARK: wire

    @MainActor private func start() async {
        model.unsubscribe(token)
        token = model.subscribe { name, payload in
            if name == "chat-queue-changed", let d = payload as? [String: Any], int(d["chatId"]) == chatId {
                items = (d["items"] as? [[String: Any]] ?? []).map(QueueItem.init)
            } else if name == "open" || name == "resync" {
                Task { await load() }
            }
        }
        await load()
    }

    @MainActor private func load() async {
        if let list = try? await model.client?.call("chat_queue_list", ["chatId": chatId]) as? [[String: Any]] {
            items = list.map(QueueItem.init)
        }
    }

    @MainActor private func update(_ item: QueueItem, _ text: String) async {
        if let i = items.firstIndex(of: item) { items[i].text = text }
        do { try await model.client?.call("chat_queue_update", ["id": item.id, "text": text]) }
        catch { problem = error.localizedDescription; await load() }
    }

    @MainActor private func remove(_ item: QueueItem) async {
        items.removeAll { $0.id == item.id }
        _ = try? await model.client?.call("chat_queue_remove", ["id": item.id])
    }

    private func move(_ from: Int, to: Int) {
        var next = items
        next.swapAt(from, to)
        items = next
        let ids = next.map(\.id)
        Task { _ = try? await model.client?.call("chat_queue_reorder", ["chatId": chatId, "ids": ids]); await load() }
    }

    /// chatSession.steer: take it out of Go's queue first (so the drain can't also
    /// send it), then chat_steer; a refusal puts it back where it was.
    @MainActor private func steer(_ item: QueueItem, text: String) async {
        guard let client = model.client, let index = items.firstIndex(where: { $0.id == item.id }) else { return }
        steering = item.id; defer { steering = nil }
        do { try await client.call("chat_queue_remove", ["id": item.id]) } catch {
            await load(); return // already sent by the drain, or removed elsewhere
        }
        items.removeAll { $0.id == item.id }
        do {
            try await client.call("chat_steer", ["chatId": chatId, "text": text, "images": item.attachments,
                                                 "clientId": UUID().uuidString])
            UIImpactFeedbackGenerator(style: .light).impactOccurred()
        } catch {
            let reason = error.localizedDescription
            if let back = try? await client.call("chat_queue_add", ["chatId": chatId, "text": text, "images": item.attachments,
                                                                 "clientId": UUID().uuidString]) as? [String: Any] {
                var ids = items.map(\.id).filter { $0 != int(back["id"]) }
                ids.insert(int(back["id"]), at: min(index, ids.count))
                _ = try? await client.call("chat_queue_reorder", ["chatId": chatId, "ids": ids])
            }
            await load()
            problem = "It's back in the queue and will be sent when the turn ends.\n\n\(reason)"
        }
    }
}

private struct QueueEditor: View {
    @Environment(\.dismiss) private var dismiss
    let item: QueueItem
    let canSteer: Bool
    let save: (String) -> Void
    let steer: (String) -> Void
    @State private var text = ""
    @FocusState private var focused: Bool

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Message", text: $text, axis: .vertical).lineLimit(4...16).focused($focused)
                } footer: {
                    Text(canSteer ? "Steer now delivers it into the running turn; the agent picks it up at its next step."
                                  : "Sent as the next message when the current turn ends.")
                }
                if canSteer {
                    Section {
                        Button {
                            steer(trimmed); dismiss()
                        } label: { Label("Save and steer now", systemImage: "arrow.turn.down.right") }
                        .disabled(trimmed.isEmpty)
                    }
                }
            }
            .navigationTitle("Queued message")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        if trimmed != item.text { save(trimmed) }
                        dismiss()
                    }
                    .disabled(trimmed.isEmpty)
                }
            }
            .onAppear { text = item.text; focused = true }
        }
        .presentationDetents([.medium, .large])
    }
}
