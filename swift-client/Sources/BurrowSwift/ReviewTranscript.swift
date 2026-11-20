import SwiftUI

// OWNER: gitdiff (wave 2). Transcript review comments (TranscriptComments.vue, TranscriptFeedbackBar.vue,
// useTranscriptFeedback.ts) as an API for the transcript + composer owners:
//
//   let fb = TranscriptFeedback.forChat(chatID)              // per-chat, persisted (UserDefaults, Vue's key)
//   if let q = transcriptQuote(messageText, textView.selectedRange()) { fb.add(messageId:, source: messageText, quote: q, body:) }
//   ReviewNoteList(chatID:, messageID:, source:)            // under an assistant message: its notes, resolve/remove
//   ReviewFeedbackBar(chatID:)                              // above the composer: "N pending comments · Send comments"
//   composer send: let batch = fb.pending; text = composeTranscriptFeedback(batch, request: text); after send fb.markSent(batch, text)
//   Quote / Ask about this: insertQuote(draft, caret:, quote:, ask:) → new draft + caret (UTF-16)

@MainActor
final class TranscriptFeedback: ObservableObject {
    struct Delivery: Codable, Hashable { var text: String; var sentAt: Int64 }
    private struct Saved: Codable { var notes: [TranscriptNote]; var deliveries: [String: Delivery] }

    @Published private(set) var notes: [TranscriptNote] = []
    @Published private(set) var deliveries: [String: Delivery] = [:]
    @Published var error = ""
    let chatID: Int64
    private var key: String { "burrow.transcriptNotes.v1:\(chatID)" }

    private static var cache: [Int64: TranscriptFeedback] = [:]
    static func forChat(_ id: Int64) -> TranscriptFeedback {
        if let f = cache[id] { return f }
        let f = TranscriptFeedback(id)
        cache[id] = f
        return f
    }

    private init(_ id: Int64) {
        chatID = id
        if let data = UserDefaults.standard.data(forKey: key) {
            if let s = try? JSONDecoder().decode(Saved.self, from: data) { notes = s.notes; deliveries = s.deliveries }
            else { error = "Saved comments could not be loaded." }
        }
    }

    /// Not yet sent, not resolved — what the next message carries.
    var pending: [TranscriptNote] { notes.filter { $0.delivery == nil && ($0.resolvedAt ?? 0) == 0 } }
    func notes(for messageID: Int64) -> [TranscriptNote] { notes.filter { $0.messageId == messageID } }

    func add(messageId: Int64, source: String, quote: TranscriptQuote, body: String) {
        let body = body.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !quote.quote.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, !body.isEmpty else { return }
        notes.append(TranscriptNote(id: UUID().uuidString, messageId: messageId, source: source, quote: quote.quote,
                                    before: quote.before, after: quote.after, body: body))
        persist()
    }

    /// Only a pending note can be removed; a sent one stays as the record of what the agent was told.
    func remove(_ id: String) {
        notes.removeAll { $0.id == id && $0.delivery == nil }
        persist()
    }

    func markSent(_ batch: [TranscriptNote], text: String) {
        let delivery = UUID().uuidString, ids = Set(batch.map(\.id))
        for i in notes.indices where ids.contains(notes[i].id) && notes[i].delivery == nil { notes[i].delivery = delivery }
        deliveries[delivery] = Delivery(text: text, sentAt: Int64(Date().timeIntervalSince1970 * 1000))
        persist()
    }

    func setResolved(_ id: String, _ resolved: Bool) {
        for i in notes.indices where notes[i].id == id { notes[i].resolvedAt = resolved ? Int64(Date().timeIntervalSince1970 * 1000) : 0 }
        persist()
    }

    /// TranscriptFeedbackBar's "Send comments": the pending batch as its own message. Refused while a turn runs (Vue too).
    func send(_ b: Burrow) async {
        let batch = pending
        guard !batch.isEmpty, !b.inFlight(chatID) else { return }
        let text = composeTranscriptFeedback(batch)
        do {
            try await b.call("chat_send", ["chatId": chatID, "text": text, "images": [String]()])
            markSent(batch, text: text)
            error = ""
        } catch { self.error = "Could not send comments: \(error.localizedDescription)" }
    }

    private func persist() {
        guard let data = try? JSONEncoder().encode(Saved(notes: notes, deliveries: deliveries)) else {
            error = "Comments could not be saved on this device. Keep this chat open until you send them."
            return
        }
        UserDefaults.standard.set(data, forKey: key)
        error = ""
    }
}

/// The notes on one assistant message (TranscriptComments.vue's list part).
struct ReviewNoteList: View {
    let chatID: Int64
    let messageID: Int64
    /// The message's current text — a note whose `source` differs was written against an older version.
    let source: String
    @ObservedObject private var fb: TranscriptFeedback
    @State private var showDelivery: String?

    init(chatID: Int64, messageID: Int64, source: String) {
        self.chatID = chatID
        self.messageID = messageID
        self.source = source
        fb = TranscriptFeedback.forChat(chatID)
    }

    var body: some View {
        ForEach(fb.notes(for: messageID)) { n in
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: "text.bubble").font(.system(size: 11)).foregroundStyle(Theme.muted)
                VStack(alignment: .leading, spacing: 3) {
                    Text(n.quote).font(.ui(-2)).foregroundStyle(Theme.muted).lineLimit(2).help(n.quote)
                    if n.source != source {
                        Text("This response has changed since you commented. The original passage is preserved.")
                            .font(.ui(-3)).foregroundStyle(Theme.yellow)
                    }
                    Text(n.body).font(.ui(-1)).foregroundStyle(Theme.text).textSelection(.enabled)
                    HStack(spacing: 10) {
                        if n.delivery == nil && (n.resolvedAt ?? 0) == 0 {
                            Text("Pending, included with your next message when the agent is ready").font(.ui(-3)).foregroundStyle(Theme.muted)
                        }
                        if let d = n.delivery {
                            Button("View follow-up") { showDelivery = d }.buttonStyle(.plain).font(.ui(-3)).foregroundStyle(Theme.text2)
                        }
                        if (n.resolvedAt ?? 0) != 0 { Text("Resolved").font(.ui(-3)).foregroundStyle(Theme.muted) }
                        Button((n.resolvedAt ?? 0) != 0 ? "Reopen" : "Resolve") { fb.setResolved(n.id, (n.resolvedAt ?? 0) == 0) }
                            .buttonStyle(.plain).font(.ui(-3)).foregroundStyle(Theme.text2)
                    }
                }
                Spacer(minLength: 0)
                if n.delivery == nil {
                    Button { fb.remove(n.id) } label: { Image(systemName: "xmark").font(.system(size: 10)) }
                        .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Remove pending comment")
                }
            }
            .padding(8)
            .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(Theme.border))
        }
        .popover(item: Binding(get: { showDelivery.map(DeliveryID.init) }, set: { showDelivery = $0?.id })) { d in
            ScrollView { Text(fb.deliveries[d.id]?.text ?? "").font(.ui(-1)).textSelection(.enabled).padding(12) }
                .frame(width: 420, height: 320)
        }
    }

    private struct DeliveryID: Identifiable { let id: String }
}

/// Above the composer (TranscriptFeedbackBar.vue): pending count + Send comments.
struct ReviewFeedbackBar: View {
    @EnvironmentObject var b: Burrow
    let chatID: Int64
    @ObservedObject private var fb: TranscriptFeedback
    @State private var sending = false

    init(chatID: Int64) {
        self.chatID = chatID
        fb = TranscriptFeedback.forChat(chatID)
    }

    var body: some View {
        let count = fb.pending.count, busy = b.inFlight(chatID)
        if count > 0 || !fb.error.isEmpty {
            HStack(spacing: 8) {
                if count > 0 { Text("\(count) pending comment\(count == 1 ? "" : "s")").font(.ui(-2)).foregroundStyle(Theme.text2) }
                if !fb.error.isEmpty { Text(fb.error).font(.ui(-2)).foregroundStyle(Theme.red) }
                Spacer()
                if busy && count > 0 { Text("Send after the current turn finishes").font(.ui(-3)).foregroundStyle(Theme.muted) }
                if count > 0 {
                    diffChip(sending ? "Sending…" : "Send comments") {
                        sending = true
                        Task { await fb.send(b); sending = false }
                    }
                    .disabled(sending || busy)
                }
            }
            .padding(.horizontal, 10).padding(.vertical, 7)
            .background(Theme.hover, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
        }
    }
}
