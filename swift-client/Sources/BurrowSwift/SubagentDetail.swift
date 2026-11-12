import AppKit
import SwiftUI

// A sub-agent lives in the Right Panel (Vue: RP Sub-agents surface + subAgentView.ts), never in the main pane.

/// Which sub-agent the RP's Agents tab shows in detail (nil = the list).
@MainActor final class SubagentNav: ObservableObject {
    static let shared = SubagentNav()
    @Published var open: Int64?
}

/// Every "open this sub-agent" door goes here: shows the RP on the Agents tab with that chat's detail.
/// A side chat (kind "side") opens the Side chat surface on it instead.
@MainActor func openSubagent(_ id: Int64) {
    let rp = RPStore.shared
    if rp.b?.chat(id)?.s("kind") == "side" {
        rp.sidePick = id
        rp.open("sidechat")
        return
    }
    SubagentNav.shared.open = id
    rp.open("agents")
}

/// Request cards answer the chat they are shown for; nil = the main pane's selected chat.
private struct RequestChatIDKey: EnvironmentKey { static let defaultValue: Int64? = nil }
extension EnvironmentValues {
    var requestChatID: Int64? {
        get { self[RequestChatIDKey.self] }
        set { self[RequestChatIDKey.self] = newValue }
    }
}

/// Transcript + pending requests of one chat that is NOT b.selectedChat. Mirrors Burrow's own
/// selected-chat plumbing (load page, splice chat-messages-changed, reload requests on phase change).
@MainActor final class ChatFeed: ObservableObject {
    let id: Int64
    @Published var messages: [JSON] = []
    @Published var requests: [PendingRequest] = []
    private var start = 0
    private weak var b: Burrow?

    // ponytail: Burrow.subscribe has no unsubscribe, so one subscription fans out to live feeds by weak ref.
    private static var live: [Int64: WeakFeed] = [:]
    private static var subscribed = false
    private struct WeakFeed { weak var feed: ChatFeed? }

    init(id: Int64, b: Burrow) {
        self.id = id
        self.b = b
        Self.live[id] = WeakFeed(feed: self)
        if !Self.subscribed {
            Self.subscribed = true
            b.subscribe("chat-messages-changed-") { name, payload in
                guard let id = Int64(name.dropFirst("chat-messages-changed-".count)),
                      let f = ChatFeed.live[id]?.feed, let p = payload as? JSON else { return }
                f.splice(from: Int(p.i("fromIndex")), p["messages"] as? [JSON] ?? [])
            }
            b.subscribe("phase-chat:") { name, _ in
                guard let id = Int64(name.dropFirst("phase-chat:".count)), let f = ChatFeed.live[id]?.feed else { return }
                Task { await f.loadRequests() }
            }
        }
        Task { await load(); await loadRequests() }
    }

    func load() async {
        guard let b else { return }
        let page = try? await b.call("load_chat_messages_page", ["chatId": id, "before": -1, "limit": 120]) as? JSON
        guard let page, let msgs = page["messages"] as? [JSON] else { return }
        start = Int(page.i("start"))
        messages = msgs
    }

    func splice(from: Int, _ tail: [JSON]) {
        let local = from - start
        guard local >= 0, local <= messages.count else { Task { await load() }; return }
        messages = Array(messages[..<local]) + tail
    }

    func loadRequests() async {
        guard let b else { return }
        let lines = (try? await b.call("load_pending_chat_requests", ["chatId": "\(id)"])) as? [JSON] ?? []
        requests = lines.compactMap { PendingRequest(line: $0.s("line")) }
            + AttentionStore.shared.secretRequests(owner: "chat:\(id)")
    }
}

struct SubagentDetailView: View {
    @EnvironmentObject var b: Burrow
    @StateObject private var feed: ChatFeed
    let chatID: Int64
    /// false = hosted by the Side chat surface (no back / delete — it has its own header).
    let nav: Bool
    @State private var draft = ""
    @State private var confirmDelete = false

    init(chatID: Int64, b: Burrow, nav: Bool = true) {
        self.chatID = chatID
        self.nav = nav
        _feed = StateObject(wrappedValue: ChatFeed(id: chatID, b: b))
    }

    var body: some View {
        let c = b.chat(chatID) ?? [:]
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                if nav {
                    Button { SubagentNav.shared.open = nil } label: { Image(systemName: "chevron.left") }
                        .buttonStyle(.plain).foregroundStyle(Theme.accent).help("Back to sub-agents")
                }
                StatusIcon(state: b.state(chatID))
                Text(c.s("title").isEmpty ? "Sub-agent" : c.s("title"))
                    .font(.brand(-3, weight: .bold)).foregroundStyle(Theme.text).lineLimit(1)
                Spacer()
                if !c.s("model").isEmpty { Text(c.s("model")).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(1) }
                if nav {
                    Button { confirmDelete = true } label: { Image(systemName: "xmark") }
                        .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Close sub-agent (deletes it)")
                }
            }
            .closeSubagentDialog($confirmDelete, c)
            .padding(.horizontal, 12).padding(.vertical, 8)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }

            // Transcript rendering + autoscroll: TranscriptScroll.swift (transcript agent).
            TranscriptList(chatID: chatID, messages: feed.messages, busy: b.inFlight(chatID), compact: true) {
                if b.inFlight(chatID) {
                    HStack(spacing: 8) {
                        Pet(state: b.state(chatID), size: 26)
                        Text(b.phase(chatID).s("detail").isEmpty ? label(b.state(chatID)) : b.phase(chatID).s("detail"))
                            .font(.brand(-4)).foregroundStyle(Theme.status(b.state(chatID)))
                    }
                }
            }

            ForEach(feed.requests) { RequestCard(r: $0) }
                .environment(\.requestChatID, chatID)
            ReviewFeedbackBar(chatID: chatID)
            Composer(draft: $draft, running: b.inFlight(chatID), chatID: chatID, send: {},
                     stop: { Task { try? await b.call("chat_interrupt", ["chatId": chatID]) } }) { EmptyView() }
        }
        .background(Theme.base)
    }
}

extension Burrow {
    /// `answer` for a specific chat (a sub-agent's card in the RP), falling back to the selected one.
    func answerIn(_ chatID: Int64?, _ r: PendingRequest, _ a: RequestAnswer) async {
        await answer(r, a, chatID: chatID)
    }
}

extension View {
    /// RightPanel.vue's "Close sub-agent?" confirm: closing a sub-agent deletes it (Go cascades its children).
    func closeSubagentDialog(_ shown: Binding<Bool>, _ c: JSON) -> some View {
        modifier(CloseSubagent(shown: shown, c: c))
    }
}

private struct CloseSubagent: ViewModifier {
    @EnvironmentObject var b: Burrow
    @Binding var shown: Bool
    let c: JSON

    func body(content: Content) -> some View {
        content.confirmationDialog("Close \(c.s("title").isEmpty ? "this sub-agent" : "“\(c.s("title"))”")?", isPresented: $shown) {
            Button("Close", role: .destructive) {
                if SubagentNav.shared.open == c.i("id") { SubagentNav.shared.open = nil }
                Task { await b.deleteThread(c); try? await b.refresh() }
            }
        } message: { Text("Its process is stopped and the chat is deleted.") }
    }
}
