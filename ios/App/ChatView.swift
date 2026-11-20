import SwiftUI

struct Message: Identifiable, Hashable {
    var id: Int
    var role: String
    var text: String
    var partial: Bool
    var failed: Bool
    var toolOutput: String
    var toolTarget: String
    /// Tool rows: `text` is a raw tool name (native transport), not a human title.
    var rawName: Bool
    /// Go's fold: a finished plumbing call (taken end_turn, ToolSearch) — never shown.
    var plumbing: Bool
    /// Tool rows: no result yet (no `toolOutput` on the wire, not failed).
    var running: Bool
    var images: [String]
    /// User rows: the submit client id (Go's chat_submit) — what an echo settles by.
    var clientId: String
    /// Epoch ms the row arrived live (Go's fold); 0 on older rows.
    var at: Int
    /// User rows Go sent on its own (`auto.label`): drawn as a wait divider, not a bubble.
    var autoLabel: String?
    /// Echoes only: transcript length when sent. Go's copy lands at or after it.
    var after = 0

    init(_ d: [String: Any], index: Int) {
        id = (d["id"] as? NSNumber)?.intValue ?? index
        role = d["role"] as? String ?? ""
        text = d["text"] as? String ?? ""
        partial = d["partial"] as? Bool ?? false
        failed = d["toolFailed"] as? Bool ?? false
        toolOutput = d["toolOutput"] as? String ?? ""
        rawName = d["toolRawName"] as? Bool ?? false
        plumbing = d["plumbing"] as? Bool ?? false
        running = role == "tool" && !failed && d["toolOutput"] == nil
        images = d["images"] as? [String] ?? []
        clientId = d["clientId"] as? String ?? ""
        at = (d["at"] as? NSNumber)?.intValue ?? 0
        autoLabel = (d["auto"] as? [String: Any]).map { $0["label"] as? String ?? "" }
        let input = d["toolInput"] as? [String: Any] ?? [:]
        toolTarget = ["command", "file_path", "path", "pattern", "url", "description"]
            .lazy.compactMap { input[$0] as? String }.first ?? ""
    }
}

struct ChatView: View {
    let chatId: Int
    @Environment(AppModel.self) private var model
    /// Unfiltered newest window of the transcript, built by Go's revisioned
    /// splices (TranscriptWindow); nil until the first page / cached page.
    @State private var win: TranscriptWindow?
    @State private var older: [Message] = []
    @State private var olderStart = 0
    @State private var loadingOlder = false
    @State private var error: String?
    @State private var showDetails = false
    @State private var showPanel = false
    @State private var atBottom = true
    @State private var eventToken: UUID?
    /// Own sends not yet in the transcript (shown greyed until Go's copy lands).
    @State private var echoes: [Message] = []
    /// chat_subscribe is single-flight: a gap during one asks for another round.
    @State private var catching = false
    @State private var catchAgain = false
    /// Set by an own send: the next transcript change scrolls down whatever
    /// atBottom says (the composer / keyboard often hide the bottom marker).
    @State private var pinBottom = false
    /// The user's fold choice per settled turn (its user message id).
    @State private var foldOverride: [Int: Bool] = [:]
    @State private var bottomBeforeKeyboard = false
    @Environment(\.horizontalSizeClass) private var sizeClass

    private var chat: Chat? { model.chat(chatId) }
    private var phase: Phase { model.phase(chatId) }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                // Not Lazy: a LazyVStack under scrollTo + changing content (sends,
                // keyboard frame changes) rendered blank until scrolled by hand.
                // ponytail: renders the whole loaded window (≤ 150 rows + older
                // pages); go back to Lazy only with a fix for the blank render.
                VStack(alignment: .leading, spacing: 14) {
                    if let error { Text(error).font(.caption).foregroundStyle(.red) }
                    if remaining > 0 {
                        Button { Task { await loadOlder() } } label: {
                            HStack(spacing: 6) {
                                if loadingOlder { ProgressView().controlSize(.mini) }
                                Text("Load earlier messages (\(remaining))")
                            }
                            .font(.caption.weight(.medium))
                        }
                        .buttonStyle(.bordered).controlSize(.small)
                        .buttonBorderShape(.roundedRectangle(radius: 8))
                        .frame(maxWidth: .infinity)
                    }
                    ForEach(rows) { row in
                        switch row {
                        case .message(let m): MessageRow(message: m).equatable()
                        case .tools(_, let items): ToolGroupRow(items: items).equatable()
                        case .worked(let turn, let tools, let folded):
                            WorkedRow(tools: tools, folded: folded) { foldOverride[turn] = !folded }
                        case .gap(let m, let label): GapRow(message: m, label: label)
                        }
                    }
                    ForEach(echoes) { MessageRow(message: $0).equatable().opacity(0.6) }
                    if phase.state == "running" {
                        HStack(spacing: 8) {
                            Image(systemName: "ellipsis").symbolEffect(.variableColor.iterative, options: .repeating)
                            Text("Working…")
                        }
                        .font(.callout).foregroundStyle(.secondary)
                        .transition(.opacity)
                    }
                    if phase.state == "waiting_approval" || phase.state == "waiting_input" {
                        Label(phase.detail.isEmpty ? "Waiting for an answer on the Mac" : phase.detail, systemImage: "hand.raised.fill")
                            .font(.callout).foregroundStyle(.orange)
                            .padding(12).frame(maxWidth: .infinity, alignment: .leading)
                            .background(.orange.opacity(0.12), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                    }
                    if !phase.inFlight, let needs = chat?.turnNeeds {
                        TurnNeedsCard(needs: needs, chatId: chatId)
                    }
                    Color.clear.frame(height: 1).id("bottom")
                        .onAppear { atBottom = true }
                        .onDisappear { atBottom = false }
                }
                .padding()
            }
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboard(.interactively)
            .overlay {
                if win == nil {
                    ProgressView("Loading…").controlSize(.large)
                } else if rows.isEmpty && echoes.isEmpty && !phase.inFlight {
                    ContentUnavailableView("No messages yet", systemImage: "bubble.left.and.text.bubble.right",
                                           description: Text("Write below to start the thread."))
                }
            }
            // Follow new output only while already at the bottom; reading back
            // up the transcript must not be yanked down by every streamed line.
            .onChange(of: messages.last) { _, _ in
                if atBottom || pinBottom { pinBottom = false; scrollToEnd(proxy) }
            }
            .onChange(of: echoes.count) { old, new in if new > old { scrollToEnd(proxy) } }
            .overlay(alignment: .bottomTrailing) {
                if !atBottom {
                    Button { withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } } label: {
                        Image(systemName: "arrow.down").font(.body.weight(.semibold))
                            .frame(width: 44, height: 44)
                            .glassSurface(Circle(), interactive: true)
                            .shadow(color: .black.opacity(0.12), radius: 6, y: 2)
                    }
                    .buttonStyle(.plain)
                    .padding(12)
                    .transition(.scale.combined(with: .opacity))
                    .accessibilityLabel("Scroll to last message")
                }
            }
            .animation(.snappy, value: atBottom)
            // The keyboard shrinks the viewport from below, which leaves the
            // transcript scrolled to the middle. Remember whether we were at the
            // end when it starts to show, and go back there once it has.
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in
                bottomBeforeKeyboard = atBottom
            }
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardDidShowNotification)) { _ in
                if bottomBeforeKeyboard { scrollToEnd(proxy) }
            }
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillChangeFrameNotification)) { _ in
                if atBottom || bottomBeforeKeyboard { scrollToEnd(proxy) }
            }
        }
        .environment(\.chatId, chatId)
        .safeAreaInset(edge: .bottom) {
            VStack(spacing: 0) {
                PendingRequestsView(chatId: chatId)
                QueuePanel(chatId: chatId)
                Composer(chatId: chatId)
            }
        }
        .navigationTitle(chat?.displayTitle ?? "Thread")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                VStack(spacing: 0) {
                    Text(chat?.displayTitle ?? "Thread").font(.subheadline.weight(.semibold)).lineLimit(1)
                    if let chat { Text(model.projectName(chat)).font(.caption2).foregroundStyle(.secondary) }
                }
            }
            ToolbarItem(placement: .topBarTrailing) {
                if let chat {
                    Menu {
                        Button { showDetails = true } label: { Label("Details", systemImage: "info.circle") }
                        Button { showPanel.toggle() } label: { Label("Panel", systemImage: "sidebar.right") }
                        Button { model.startActivity(chat) } label: { Label("Follow on Lock Screen", systemImage: "lock.iphone") }
                        ThreadMenuItems(chat: chat)
                    } label: { Image(systemName: "ellipsis.circle") }
                }
            }
        }
        .sheet(isPresented: $showDetails) { ThreadDetailsView(chatId: chatId) }
        .modifier(PanelPresentation(chatId: chatId, isPresented: $showPanel, regular: sizeClass == .regular))
        // Swipe in from the right edge opens the panel (the RP lives on the right).
        .overlay(alignment: .trailing) {
            Color.clear.frame(width: 18).contentShape(Rectangle())
                .gesture(DragGesture(minimumDistance: 15).onEnded { g in
                    if g.translation.width < -50, abs(g.translation.height) < 80 { showPanel = true }
                })
        }
        .sensoryFeedback(.impact(weight: .light), trigger: showPanel) { _, new in new }
        .task { await load() }
        .onAppear {
            model.viewing = chatId
            model.markSeen(chatId)
            // Own subscription, not the single chatListeners slot: two views of the
            // same chat (split view, a sub-agent pushed in a sheet) used to clear
            // each other's listener on disappear, and the chat stopped updating.
            model.unsubscribe(eventToken) // a second onAppear without onDisappear
            eventToken = model.subscribe { name, payload in
                if name == "chat-local-send-\(chatId)" { localEcho(payload as? [String: Any]) }
                else if name == "chat-messages-changed-\(chatId)" { feed { $0.apply(splice: payload as? [String: Any] ?? [:]) } }
                else if name == "chat-tail-\(chatId)" { feed { $0.apply(tail: payload as? [String: Any] ?? [:]) } }
                // nil payload = AppModel.nudgeChat after a send / outbox delivery: a
                // no-op subscribe when nothing was missed.
                else if name == "chat-event-\(chatId)", payload == nil { Task { await catchUp() } }
                // Chat channels aren't ringed: whatever streamed while the socket was down is
                // only in chat_subscribe. onChange(of: connected) can miss a close→open that
                // lands inside one SwiftUI update, so catch up on the events themselves.
                else if name == "open" || name == "resync" { Task { await catchUp() } }
            }
            // A new thread's first prompt: sent here, after the subscription above, so its echo shows.
            if let first = model.pendingFirst.removeValue(forKey: chatId) {
                Task {
                    do { _ = try await model.composerSend(chatId, first, [], steer: false) }
                    catch { self.error = error.localizedDescription }
                }
            }
        }
        .onDisappear {
            if model.viewing == chatId { model.viewing = nil }
            model.unsubscribe(eventToken)
            if let win { model.cachePage(chatId, win.page) }
        }
        .onChange(of: model.connected) { _, c in if c { Task { await catchUp() } } }
        // ponytail: cheap insurance — a phase change (send, turn end) costs one
        // chat_subscribe that returns nothing when no splice was missed.
        .onChange(of: phase.state) { _, _ in
            Task { await catchUp() }
            if !phase.inFlight, let win { model.cachePage(chatId, win.page) }
        }
    }

    private var raw: [Message] { win?.rows ?? [] }
    private var earlier: Int { win?.start ?? 0 }
    private var messages: [Message] { Self.visible(raw) }

    /// One unanimated scroll after this update has laid out. Animated scrollTo
    /// calls racing a LazyVStack's content change (echo in, real row in, echo
    /// out) left it blank — a white transcript until the user scrolled.
    private func scrollToEnd(_ proxy: ScrollViewProxy) {
        DispatchQueue.main.async { proxy.scrollTo("bottom", anchor: .bottom) }
    }

    /// `{clientId, text}` adds an echo (or rewrites that one's text — the end_turn
    /// answer's preview becomes Go's composed text); `{clientId}` alone drops it.
    private func localEcho(_ p: [String: Any]?) {
        let cid = p?["clientId"] as? String ?? ""
        guard let text = p?["text"] as? String else { echoes.removeAll { $0.clientId == cid }; return }
        if !cid.isEmpty, let i = echoes.firstIndex(where: { $0.clientId == cid }) { echoes[i].text = text; return }
        var m = Message(["role": "user", "text": text, "clientId": cid], index: 0)
        m.id = -1 - echoes.count // never a transcript id
        m.after = earlier + raw.count
        echoes.append(m)
        pinBottom = true
    }

    /// Drop the echoes Go's transcript now carries: the user row with the echo's
    /// client id. Fallback for rows without one (an older core): a user row with
    /// the same text that landed after the echo was made, each row settling one
    /// echo (so a second "ok" isn't cleared by the first).
    private func settleEchoes() {
        guard !echoes.isEmpty else { return }
        var used = Set<Int>()
        echoes.removeAll { e in
            if !e.clientId.isEmpty, raw.contains(where: { $0.role == "user" && $0.clientId == e.clientId }) { return true }
            guard let i = raw.indices.first(where: { i in
                !used.contains(i) && earlier + i >= e.after && raw[i].role == "user" && raw[i].clientId.isEmpty
                    && raw[i].text.trimmingCharacters(in: .whitespacesAndNewlines) == e.text
            }) else { return false }
            used.insert(i)
            return true
        }
    }

    /// Older pages loaded on demand + the live newest page, deduped by id.
    private var shown: [Message] {
        let ids = Set(messages.map(\.id))
        return older.filter { !ids.contains($0.id) } + messages
    }
    private var remaining: Int { older.isEmpty ? earlier : olderStart }

    private var rows: [TranscriptRow] {
        Transcript.rows(shown, foldOverride: foldOverride, settledLast: !phase.inFlight)
    }

    private func loadOlder() async {
        let before = older.isEmpty ? earlier : olderStart
        guard before > 0, !loadingOlder else { return }
        loadingOlder = true; defer { loadingOlder = false }
        guard let page = try? await model.client?.call("load_chat_messages_page",
                                                       ["chatId": chatId, "before": before, "limit": 100]) as? [String: Any] else { return }
        let start = int(page["start"])
        let rows = page["messages"] as? [[String: Any]] ?? []
        older = Self.visible(rows.enumerated().map { Message($1, index: start + $0) }) + older
        olderStart = start
    }

    static func visible(_ ms: [Message]) -> [Message] { ms.filter(Transcript.isVisible) }

    /// One live event on the held window; a gap asks Go for what was missed.
    private func feed(_ op: (inout TranscriptWindow) -> TranscriptWindow.Outcome) {
        guard var w = win else { return } // load() subscribes once its page is in
        switch op(&w) {
        case .applied: adopt(w)
        case .gap: Task { await catchUp() }
        case .ignored: break
        }
    }

    /// Take a new window. Older pages end at the previous window start: when the
    /// window moved forward contiguously, keep the rows in between; when it moved
    /// back (clear / rewind) or jumped past a gap, the older pages are stale.
    private func adopt(_ w: TranscriptWindow) {
        if let prev = win, !older.isEmpty, w.start != prev.start {
            if w.start < prev.start || w.start > prev.start + prev.rows.count { older = []; olderStart = 0 }
            else { older += Self.visible(Array(prev.rows.prefix(w.start - prev.start))) }
        }
        win = w
        settleEchoes()
    }

    /// chat_subscribe(heldRev): the splices missed since (or one authoritative
    /// splice) + the streaming row's text so far. Reconnect, a gap, a nudge.
    private func catchUp() async {
        guard let held = win else { await load(); return }
        if catching { catchAgain = true; return }
        catching = true; defer { catching = false }
        repeat {
            catchAgain = false
            guard let reply = try? await model.client?.call("chat_subscribe", ["chatId": chatId, "sinceRev": win?.rev ?? held.rev]) as? [String: Any],
                  var w = win else { return }
            // A reply that doesn't chain (the ring moved under it): read the page again.
            guard w.apply(subscribe: reply) == .applied else { await load(); return }
            adopt(w)
        } while catchAgain
    }

    /// First open: the newest page (with its rev), then chat_subscribe from that
    /// rev to close the race with splices sent while the page was on the way.
    private func load() async {
        let live = try? await model.client?.call("load_chat_messages_page",
                                                 ["chatId": chatId, "before": -1, "limit": 150]) as? [String: Any]
        // Launch cache (Offline.swift): remember the live page, paint the cached one while offline.
        if let live { model.cachePage(chatId, live) }
        guard let page = live ?? (win == nil ? model.cachedPage(chatId) : nil) else { return }
        adopt(TranscriptWindow(page: page))
        guard let reply = try? await model.client?.call("chat_subscribe", ["chatId": chatId, "sinceRev": win?.rev ?? 0]) as? [String: Any],
              var w = win else { return }
        if w.apply(subscribe: reply) == .applied { adopt(w) }
    }

}

/// Equatable + `.equatable()`: a streamed update re-renders only the rows that
/// changed, not ~150 rows re-parsing their Markdown.
struct MessageRow: View, Equatable {
    let message: Message
    static func == (a: Self, b: Self) -> Bool { a.message == b.message }
    @State private var open = false

    var body: some View {
        switch message.role {
        case "user":
            VStack(alignment: .trailing, spacing: 6) {
                if !message.images.isEmpty {
                    HStack { Spacer(minLength: 40); ChatImageStrip(images: message.images).fixedSize(horizontal: true, vertical: true) }
                }
                if !message.text.isEmpty {
                    HStack {
                        Spacer(minLength: 40)
                        Text(message.text)
                            .padding(.horizontal, 12).padding(.vertical, 9)
                            .background(Color.accentColor.opacity(0.16), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
                            .contextMenu { Button { UIPasteboard.general.string = message.text } label: { Label("Copy", systemImage: "doc.on.doc") } }
                    }
                }
            }
        case "assistant":
            MarkdownText(text: message.text)
                .contextMenu { Button { UIPasteboard.general.string = message.text } label: { Label("Copy", systemImage: "doc.on.doc") } }
        case "thinking":
            ThinkingRow(message: message)
        case "tool":
            VStack(alignment: .leading, spacing: 6) {
                Button { open.toggle() } label: {
                    HStack(spacing: 6) {
                        Image(systemName: message.failed ? "exclamationmark.triangle" : "chevron.right")
                            .rotationEffect(.degrees(open && !message.failed ? 90 : 0))
                            .font(.caption2)
                        Text(message.text).font(.caption.weight(.medium))
                        if !message.toolTarget.isEmpty {
                            Text(message.toolTarget).font(.caption.monospaced()).lineLimit(1).foregroundStyle(.secondary)
                        }
                    }
                    .foregroundStyle(message.failed ? .red : .secondary)
                }
                .buttonStyle(.plain)
                if open && !message.toolOutput.isEmpty {
                    Text(message.toolOutput.prefix(1500))
                        .font(.caption2.monospaced()).foregroundStyle(.secondary)
                        .padding(8).frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 8))
                        .textSelection(.enabled)
                }
            }
        default:
            Text(message.text).font(.caption).foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .center)
        }
    }
}

/// Block-level markdown: headings, lists, quotes, code (with copy), tables as
/// monospaced text; inline styling via AttributedString.
// ponytail: hand-rolled block parser for what agents actually write; swap for a
// markdown package if nested lists / real tables start to matter.
struct MarkdownText: View {
    let text: String

    enum Block: Hashable {
        case para(String), heading(Int, String), bullet(String, String), quote(String), code(String, String), rule, table(String)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(Self.parse(text).enumerated()), id: \.offset) { _, b in view(b) }
        }
        .textSelection(.enabled)
    }

    @ViewBuilder
    private func view(_ b: Block) -> some View {
        switch b {
        case .para(let t): Text(inline(t))
        case .heading(let level, let t):
            Text(inline(t)).font(level == 1 ? .title3.bold() : level == 2 ? .headline : .subheadline.bold()).padding(.top, 4)
        case .bullet(let mark, let t):
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(mark).foregroundStyle(.secondary).monospacedDigit()
                Text(inline(t))
            }
            .padding(.leading, 4)
        case .quote(let t):
            Text(inline(t)).foregroundStyle(.secondary)
                .padding(.leading, 10)
                .overlay(alignment: .leading) { Rectangle().fill(.tertiary).frame(width: 3) }
        case .code(let lang, let code): CodeBlock(lang: lang, code: code)
        case .rule: Divider()
        case .table(let t):
            ScrollView(.horizontal, showsIndicators: false) { Text(t).font(.caption.monospaced()).padding(8) }
                .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    private func inline(_ s: String) -> AttributedString {
        (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
    }

    static func parse(_ text: String) -> [Block] {
        var out: [Block] = []
        var para: [String] = []
        var code: [String]? = nil
        var lang = ""
        var table: [String] = []
        func flushPara() { if !para.isEmpty { out.append(.para(para.joined(separator: "\n"))); para = [] } }
        func flushTable() { if !table.isEmpty { out.append(.table(table.joined(separator: "\n"))); table = [] } }
        for raw in text.components(separatedBy: "\n") {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.hasPrefix("```") {
                if let c = code { out.append(.code(lang, c.joined(separator: "\n"))); code = nil }
                else { flushPara(); flushTable(); code = []; lang = String(line.dropFirst(3)) }
                continue
            }
            if code != nil { code!.append(raw); continue }
            if line.hasPrefix("|") { flushPara(); table.append(line); continue } else { flushTable() }
            if line.isEmpty { flushPara(); continue }
            if let h = line.firstIndex(where: { $0 != "#" }), line.hasPrefix("#"), line[h] == " " {
                flushPara(); out.append(.heading(line.distance(from: line.startIndex, to: h), String(line[h...]).trimmingCharacters(in: .whitespaces))); continue
            }
            if line == "---" || line == "***" { flushPara(); out.append(.rule); continue }
            if line.hasPrefix("- ") || line.hasPrefix("* ") || line.hasPrefix("+ ") {
                flushPara(); out.append(.bullet("•", String(line.dropFirst(2)))); continue
            }
            if let dot = line.firstIndex(of: "."), line[..<dot].allSatisfy(\.isNumber), !line[..<dot].isEmpty,
               line.index(after: dot) < line.endIndex, line[line.index(after: dot)] == " " {
                flushPara(); out.append(.bullet(String(line[...dot]), String(line[line.index(dot, offsetBy: 2)...]))); continue
            }
            if line.hasPrefix(">") { flushPara(); out.append(.quote(String(line.dropFirst()).trimmingCharacters(in: .whitespaces))); continue }
            para.append(raw)
        }
        if let c = code { out.append(.code(lang, c.joined(separator: "\n"))) }
        flushPara(); flushTable()
        return out
    }
}

struct CodeBlock: View {
    let lang: String
    let code: String
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(lang.isEmpty ? "code" : lang).font(.caption2.weight(.medium)).foregroundStyle(.secondary)
                Spacer()
                Button {
                    UIPasteboard.general.string = code
                    copied = true
                    Task { try? await Task.sleep(nanoseconds: 1_500_000_000); copied = false }
                } label: {
                    Image(systemName: copied ? "checkmark" : "doc.on.doc").font(.caption2)
                }
                .buttonStyle(.plain).foregroundStyle(.secondary)
                .sensoryFeedback(.success, trigger: copied) { _, new in new }
            }
            .padding(.horizontal, 10).padding(.vertical, 6)
            Divider()
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code).font(.caption.monospaced()).padding(10)
            }
        }
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Color(.separator).opacity(0.5), lineWidth: 0.5))
    }
}

/// iPad: the panel sits beside the chat like the desktop RP (inspector); iPhone: a sheet.
/// Only one of the two is attached: an attached-but-closed inspector still put the
/// panel's toolbar (surface picker, Done) into the chat's navigation bar on iPhone.
struct PanelPresentation: ViewModifier {
    let chatId: Int
    @Binding var isPresented: Bool
    let regular: Bool

    func body(content: Content) -> some View {
        if regular {
            content.inspector(isPresented: $isPresented) {
                RightPanelView(chatId: chatId).inspectorColumnWidth(min: 320, ideal: 420, max: 560)
            }
        } else {
            content.sheet(isPresented: $isPresented) { RightPanelView(chatId: chatId) }
        }
    }
}
