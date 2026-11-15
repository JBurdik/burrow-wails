import SwiftUI

struct Message: Identifiable, Hashable {
    var id: Int
    var role: String
    var text: String
    var partial: Bool
    var failed: Bool
    var toolOutput: String
    var toolTarget: String

    init(_ d: [String: Any], index: Int) {
        id = (d["id"] as? NSNumber)?.intValue ?? index
        role = d["role"] as? String ?? ""
        text = d["text"] as? String ?? ""
        partial = d["partial"] as? Bool ?? false
        failed = d["toolFailed"] as? Bool ?? false
        toolOutput = d["toolOutput"] as? String ?? ""
        let input = d["toolInput"] as? [String: Any] ?? [:]
        toolTarget = ["command", "file_path", "path", "pattern", "url", "description"]
            .lazy.compactMap { input[$0] as? String }.first ?? ""
    }
}

struct ChatView: View {
    let chatId: Int
    @Environment(AppModel.self) private var model
    /// Unfiltered newest window of the transcript; raw[0] is transcript index `earlier`.
    @State private var raw: [Message] = []
    @State private var earlier = 0
    @State private var older: [Message] = []
    @State private var olderStart = 0
    @State private var loadingOlder = false
    @State private var error: String?
    @State private var reloadTask: Task<Void, Never>?
    @State private var showDetails = false
    @State private var showPanel = false
    @State private var atBottom = true
    @State private var eventToken: UUID?
    @State private var bottomBeforeKeyboard = false
    @Environment(\.horizontalSizeClass) private var sizeClass

    private var chat: Chat? { model.chat(chatId) }
    private var phase: Phase { model.phase(chatId) }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 14) {
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
                    ForEach(shown) { MessageRow(message: $0) }
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
                            .padding(10).frame(maxWidth: .infinity, alignment: .leading)
                            .background(.orange.opacity(0.1), in: RoundedRectangle(cornerRadius: 10))
                    }
                    if !phase.inFlight, let needs = chat?.turnNeeds {
                        TurnNeedsCard(needs: needs) { text in Task { await send(text) } }
                    }
                    Color.clear.frame(height: 1).id("bottom")
                        .onAppear { atBottom = true }
                        .onDisappear { atBottom = false }
                }
                .padding()
            }
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboard(.interactively)
            // Follow new output only while already at the bottom; reading back
            // up the transcript must not be yanked down by every streamed line.
            .onChange(of: messages.last) { _, _ in if atBottom { withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } } }
            .overlay(alignment: .bottomTrailing) {
                if !atBottom {
                    Button { withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } } label: {
                        Image(systemName: "arrow.down").font(.body.weight(.semibold))
                            .frame(width: 40, height: 40)
                            .background(.regularMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(Color(.separator).opacity(0.4), lineWidth: 0.5))
                            .shadow(color: .black.opacity(0.12), radius: 6, y: 2)
                    }
                    .buttonStyle(.plain)
                    .padding(16)
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
                if bottomBeforeKeyboard { withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } }
            }
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillChangeFrameNotification)) { _ in
                if atBottom || bottomBeforeKeyboard {
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) { proxy.scrollTo("bottom", anchor: .bottom) }
                }
            }
        }
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
            eventToken = model.subscribe { name, payload in
                if name == "chat-messages-changed-\(chatId)" { applySplice(payload) }
                else if name == "chat-event-\(chatId)", Self.endsTurn(payload) { scheduleReload() }
            }
        }
        .onDisappear {
            if model.viewing == chatId { model.viewing = nil }
            model.unsubscribe(eventToken)
        }
        .onChange(of: model.connected) { _, c in if c { Task { await load() } } }
    }

    private var messages: [Message] { Self.visible(raw) }

    /// Older pages loaded on demand + the live newest page, deduped by id.
    private var shown: [Message] {
        let ids = Set(messages.map(\.id))
        return older.filter { !ids.contains($0.id) } + messages
    }
    private var remaining: Int { older.isEmpty ? earlier : olderStart }

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

    static func visible(_ ms: [Message]) -> [Message] {
        ms.filter { $0.role != "queued" && $0.role != "thinking" && !($0.role == "tool" && $0.text.hasPrefix("mcp__burrow__end_turn")) }
    }

    private func scheduleReload() {
        reloadTask?.cancel()
        reloadTask = Task {
            try? await Task.sleep(nanoseconds: 250_000_000)
            if !Task.isCancelled { await load() }
        }
    }

    /// Full read of the newest page: first open, reconnect, a hole in the
    /// splices, a reset, and once per turn end (restores image stubs, which the
    /// splice doesn't apply).
    private func load() async {
        let live = try? await model.client?.call("load_chat_messages_page",
                                                 ["chatId": chatId, "before": -1, "limit": 150]) as? [String: Any]
        // Launch cache (Offline.swift): remember the live page, paint the cached one while offline.
        if let live { model.cachePage(chatId, live) }
        guard let page = live ?? (raw.isEmpty ? model.cachedPage(chatId) : nil) else { return }
        let rows = page["messages"] as? [[String: Any]] ?? []
        earlier = int(page["start"])
        raw = rows.enumerated().map { Message($1, index: earlier + $0) }
    }

    /// While a turn streams, Go's fold (chattranscript.go) emits the changed tail
    /// as `chat-messages-changed-<id>` {fromIndex, messages}; splicing it is the
    /// increment, so no client-side projection of chat-event deltas is needed.
    private func applySplice(_ payload: Any?) {
        guard let d = payload as? [String: Any], let from = (d["fromIndex"] as? NSNumber)?.intValue,
              let rows = d["messages"] as? [[String: Any]],
              let next = Self.splice(raw, start: earlier, from: from,
                                     rows.enumerated().map { Message($1, index: from + $0) })
        else { scheduleReload(); return }
        raw = next
    }

    /// `raw` starts at transcript index `start`; replace everything from `from`
    /// on with `new`. nil = a hole (a splice was missed), caller reloads.
    /// A splice reaching back past the window keeps only the part inside it.
    static func splice(_ raw: [Message], start: Int, from: Int, _ new: [Message]) -> [Message]? {
        if from > start + raw.count { return nil }
        if from < start { return Array(new.dropFirst(start - from)) }
        return Array(raw.prefix(from - start)) + new
    }

    /// A chat-event batch that closes the turn or resets the session.
    static func endsTurn(_ payload: Any?) -> Bool {
        let events = (payload as? [String: Any])?["events"] as? [[String: Any]] ?? []
        return events.contains { ["turn.completed", "turn.failed", "session.reset", "session.exited"].contains($0["type"] as? String ?? "") }
    }

    private func send(_ raw: String) async {
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        do { try await model.send(chatId, text); error = nil } catch { self.error = error.localizedDescription }
    }
}

struct MessageRow: View {
    let message: Message
    @State private var open = false

    var body: some View {
        switch message.role {
        case "user":
            HStack {
                Spacer(minLength: 40)
                Text(message.text)
                    .padding(.horizontal, 12).padding(.vertical, 9)
                    .background(Color.accentColor.opacity(0.16), in: RoundedRectangle(cornerRadius: 16))
                    .contextMenu { Button { UIPasteboard.general.string = message.text } label: { Label("Copy", systemImage: "doc.on.doc") } }
            }
        case "assistant":
            MarkdownText(text: message.text)
                .contextMenu { Button { UIPasteboard.general.string = message.text } label: { Label("Copy", systemImage: "doc.on.doc") } }
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

/// end_turn's card: Needs you = one ask at a time with options; Done = follow-ups.
struct TurnNeedsCard: View {
    let needs: TurnNeeds
    let send: (String) -> Void
    @State private var answers: [Int: Set<String>] = [:]

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if needs.needsYou {
                Label("Needs you", systemImage: "questionmark.bubble.fill").font(.caption.weight(.semibold)).foregroundStyle(.orange)
                ForEach(Array(needs.asks.enumerated()), id: \.offset) { i, ask in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(ask.question).font(.subheadline.weight(.medium))
                        ForEach(Array(ask.options.enumerated()), id: \.offset) { j, opt in
                            option(i, ask, j, opt)
                        }
                    }
                }
                if needs.asks.contains(where: { !$0.options.isEmpty }) {
                    Button("Send answers") { send(composed) }
                        .buttonStyle(.borderedProminent)
                        .buttonBorderShape(.roundedRectangle(radius: 10))
                        .disabled(answers.values.allSatisfy(\.isEmpty))
                }
            } else {
                Label("Done", systemImage: "checkmark.circle.fill").font(.caption.weight(.semibold)).foregroundStyle(.green)
                ForEach(needs.next, id: \.self) { n in
                    Button { send(n) } label: {
                        HStack { Text(n).multilineTextAlignment(.leading); Spacer(); Image(systemName: "arrow.up.right") }
                            .font(.subheadline)
                    }
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.roundedRectangle(radius: 10))
                }
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
    }

    private func option(_ i: Int, _ ask: TurnAsk, _ j: Int, _ opt: String) -> some View {
        let picked = answers[i, default: []].contains(opt)
        return Button {
            var s = answers[i, default: []]
            if ask.multi { if picked { s.remove(opt) } else { s.insert(opt) } } else { s = picked ? [] : [opt] }
            answers[i] = s
        } label: {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: picked ? (ask.multi ? "checkmark.square.fill" : "largecircle.fill.circle") : (ask.multi ? "square" : "circle"))
                    .foregroundStyle(picked ? Color.accentColor : .secondary)
                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text(opt).font(.subheadline)
                        if ask.suggested == j { Text("suggested").font(.caption2).foregroundStyle(.secondary) }
                    }
                    if j < ask.details.count, !ask.details[j].isEmpty {
                        Text(ask.details[j]).font(.caption).foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 0)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// composeAnswers in src/lib/turnNeeds.ts.
    private var composed: String {
        let answered = needs.asks.enumerated().compactMap { i, a -> (String, String)? in
            let picked = a.options.filter { answers[i, default: []].contains($0) }
            return picked.isEmpty ? nil : (a.question, picked.joined(separator: ", "))
        }
        if needs.asks.count == 1, let one = answered.first { return one.1 }
        return answered.map { "> \($0.0.replacingOccurrences(of: "\n", with: "\n> "))\n\($0.1)" }.joined(separator: "\n\n")
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
