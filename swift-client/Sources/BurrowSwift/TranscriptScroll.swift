import AppKit
import SwiftUI

/// What every row of one transcript needs to know: whose it is (edit → that chat's draft, image stubs), where
/// relative links resolve, and the ⌘F query to highlight.
struct TranscriptEnv {
    var chatID: Int64 = 0
    var cwd = ""
    var find = ""
}
private struct TranscriptEnvKey: EnvironmentKey { static let defaultValue = TranscriptEnv() }
extension EnvironmentValues {
    var transcript: TranscriptEnv {
        get { self[TranscriptEnvKey.self] }
        set { self[TranscriptEnvKey.self] = newValue }
    }
}

/// Follows the bottom while the reader is there; a wheel-up over the transcript pauses it, getting back
/// within 64 pt of the bottom (or the jump button) resumes it. AgentChat's `atBottom`.
@MainActor final class TranscriptScrollState: ObservableObject {
    @Published var pinned = true
    var hovering = false
    var viewport: CGFloat = 0
    var bottomY: CGFloat = .infinity {
        didSet { if !pinned, bottomY - viewport < 64 { pinned = true } }
    }
    private var monitor: Any?

    // ponytail: only a wheel/trackpad scroll unpins; dragging the scroller doesn't (geometry can't tell a user
    // scroll from content growing under a pinned view). Add an NSScrollView bounds observer if that matters.
    func start() {
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] e in
            MainActor.assumeIsolated {
                if let self, self.hovering, self.pinned, e.scrollingDeltaY > 0 { self.pinned = false }
            }
            return e
        }
    }

    func stop() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
    }
}

private struct BottomYKey: PreferenceKey {
    static let defaultValue: CGFloat = .infinity
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = nextValue() }
}
private struct ViewportKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = nextValue() }
}

/// The whole scrolling transcript of one chat — the main thread (Thread.swift) and a sub-agent / side chat in
/// the RP (ChatFeed) both use it; it only takes messages. Folds settled turns, adds the files-edited card,
/// the "New" divider, autoscroll + jump-to-latest, ⌘F find and link handling.
struct TranscriptList<RowMenu: View, Footer: View>: View {
    @EnvironmentObject var b: Burrow
    let chatID: Int64
    let messages: [JSON]
    /// Transcript index of messages[0] (load_chat_messages_page's `start`).
    let start: Int
    let busy: Bool
    /// The RP's narrow column: tighter spacing, no ⌘F (the main thread owns the shortcut).
    let compact: Bool
    /// Fetch the page before messages[0]; false = nothing older.
    let loadOlder: (() async -> Bool)?
    /// Context menu for a user message (global index, message) — rewind / fork live in Thread.swift.
    let menu: (Int, JSON) -> RowMenu
    let footer: () -> Footer

    init(chatID: Int64, messages: [JSON], start: Int = 0, busy: Bool = false, compact: Bool = false,
         loadOlder: (() async -> Bool)? = nil,
         @ViewBuilder menu: @escaping (Int, JSON) -> RowMenu, @ViewBuilder footer: @escaping () -> Footer) {
        self.chatID = chatID; self.messages = messages; self.start = start; self.busy = busy
        self.compact = compact; self.loadOlder = loadOlder; self.menu = menu; self.footer = footer
    }

    @StateObject private var scroll = TranscriptScrollState()
    @StateObject private var find = TranscriptFindState()
    @State private var folded: [Int: Bool] = [:]
    @State private var visitSeen: Int64?
    @State private var olderDone = false
    @State private var repo: String?

    var cwd: String { b.chat(chatID).flatMap { b.workspaces[$0.i("workspace_id")]?.s("path") } ?? "" }

    var body: some View {
        let rows = transcriptRows(messages, start: start, busy: busy, folded: folded, cwd: cwd)
        let divider = watermarkRow(rows)
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: compact ? 12 : 18) {
                    if let loadOlder, start > 0, !olderDone {
                        ProgressView().controlSize(.small).frame(maxWidth: .infinity)
                            .task(id: start) {
                                let anchor = rows.first?.id
                                olderDone = !(await loadOlder())
                                if let anchor { proxy.scrollTo(anchor, anchor: .top) }
                            }
                    }
                    ForEach(rows) { row in
                        if row.id == divider {
                            VStack(alignment: .leading, spacing: compact ? 12 : 18) { NewDivider(); rowView(row) }
                        } else {
                            rowView(row)
                        }
                    }
                    footer()
                    Color.clear.frame(height: 1).id("bottom")
                        .background(GeometryReader { g in
                            Color.clear.preference(key: BottomYKey.self, value: g.frame(in: .named("tscroll")).minY)
                        })
                }
                .font(compact ? .ui(-1) : .ui())
                .foregroundStyle(Theme.text)
                .padding(.horizontal, compact ? 12 : 32).padding(.vertical, compact ? 12 : 24)
                .frame(maxWidth: compact ? .infinity : 860)
                .frame(maxWidth: .infinity)
            }
            .coordinateSpace(name: "tscroll")
            .background(GeometryReader { Color.clear.preference(key: ViewportKey.self, value: $0.size.height) })
            .onPreferenceChange(ViewportKey.self) { v in MainActor.assumeIsolated { scroll.viewport = v } }
            .onPreferenceChange(BottomYKey.self) { y in MainActor.assumeIsolated { scroll.bottomY = y } }
            .onHover { scroll.hovering = $0 }
            .onChange(of: signature) { if scroll.pinned { proxy.scrollTo("bottom", anchor: .bottom) } }
            .onAppear { scroll.start(); proxy.scrollTo("bottom", anchor: .bottom) }
            .onDisappear { scroll.stop() }
            .overlay(alignment: .bottom) {
                if !scroll.pinned {
                    Button {
                        scroll.pinned = true
                        withAnimation(.easeOut(duration: 0.2)) { proxy.scrollTo("bottom", anchor: .bottom) }
                    } label: {
                        Label("Jump to latest", systemImage: "arrow.down")
                            .font(.brand(-3)).foregroundStyle(Theme.text)
                            .padding(.horizontal, 12).padding(.vertical, 7)
                            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
                            .shadow(color: .black.opacity(0.25), radius: 8, y: 2)
                    }
                    .buttonStyle(.plain).padding(.bottom, 10)
                }
            }
            .overlay(alignment: .topTrailing) {
                if find.open {
                    TranscriptFindBar(find: find, total: findMatches(messages, start: start, find.query).count)
                        .padding(.top, 8).padding(.trailing, 14)
                }
            }
            .background {
                if !compact {
                    Button("") { find.open = true; find.focus += 1 }
                        .keyboardShortcut("f", modifiers: .command).opacity(0).frame(width: 0, height: 0)
                }
            }
            .onChange(of: find.query) { find.index = 0; reveal(proxy) }
            .onChange(of: find.index) { reveal(proxy) }
            .onChange(of: find.step) { reveal(proxy) }
        }
        .environment(\.transcript, TranscriptEnv(chatID: chatID, cwd: cwd, find: find.open ? find.query : ""))
        .environment(\.openURL, OpenURLAction { open($0) })
        .task(id: chatID) {
            visitSeen = b.seen["chat:\(chatID)"]
            folded = [:]; olderDone = false
        }
        .task(id: cwd) {
            guard !cwd.isEmpty else { return }
            let r = try? await b.call("run_git", ["cwd": cwd, "args": ["remote", "get-url", "origin"]]) as? JSON
            repo = repoKey(r?.s("stdout") ?? "")
        }
    }

    /// Changes whenever the bottom moves: new rows, a streaming row growing, the working line toggling.
    /// End index, not count: prepending older history must not jump to the bottom.
    var signature: String {
        let last = messages.last
        return "\(start + messages.count):\(last?.s("text").count ?? 0):\(last?.s("toolOutput").count ?? 0):\(busy)"
    }

    @ViewBuilder func rowView(_ row: TRow) -> some View {
        switch row {
        case .message(let g, let m):
            MessageView(m: m).contextMenu { if m.s("role") == "user" { menu(g, m) } }
        case .tools(_, let run):
            ToolRun(tools: run)
        case .fold(let turn, let label, let isFolded, let tools, let files):
            TurnFoldRow(label: label, folded: isFolded, tools: tools, files: files) { folded[turn] = !isFolded }
        case .files(_, let edits):
            ChangedFilesCard(files: edits)
        }
    }

    /// The row the "New" divider sits above: the first one showing a message at or after the watermark.
    func watermarkRow(_ rows: [TRow]) -> String? {
        guard let w = visitWatermark(messages, seenAt: visitSeen) else { return nil }
        return rows.first { $0.indexes.contains { $0 >= start + w } }?.id
    }

    /// ⌘F landed on a message: unfold its turn when a fold hides it, scroll it to the middle.
    func reveal(_ proxy: ScrollViewProxy) {
        let matches = findMatches(messages, start: start, find.query)
        guard find.open, !matches.isEmpty else { return }
        let g = matches[min(find.index, matches.count - 1)]
        let local = g - start
        if ["tool", "thinking"].contains(messages[local].s("role")),
           let u = messages[..<local].lastIndex(where: { $0.s("role") == "user" }) { folded[start + u] = false }
        scroll.pinned = false
        DispatchQueue.main.async {
            let rows = transcriptRows(messages, start: start, busy: busy, folded: folded, cwd: cwd)
            if let id = rows.first(where: { $0.indexes.contains(g) })?.id {
                withAnimation(.easeOut(duration: 0.15)) { proxy.scrollTo(id, anchor: .center) }
            }
        }
    }

    /// Links in transcript text (chatLinks.ts + localLink.ts): a file → the RP editor, the workspace's own PR → the
    /// RP's PRs surface, a dev server → the RP browser; everything else goes to the system browser.
    func open(_ url: URL) -> OpenURLAction.Result {
        let s = url.absoluteString
        if url.scheme == "burrow-file" {
            let raw = String(s.dropFirst("burrow-file:".count)).removingPercentEncoding ?? ""
            if let l = resolveLocalLink(raw, baseDir: cwd) { openFileInRP(l.path, line: l.line) }
            return .handled
        }
        if url.scheme == "http" || url.scheme == "https" {
            if let pr = prRef(s) {
                guard pr.repo == repo else { return .systemAction }
                RPOpenRequests.shared.surface = "prs"
                return .handled
            }
            if let local = localURL(s) { RPOpenRequests.shared.surface = "browser:\(local)"; return .handled }
            return .systemAction
        }
        if let l = resolveLocalLink(s, baseDir: cwd) { openFileInRP(l.path, line: l.line); return .handled }
        return .systemAction
    }
}

extension TranscriptList where RowMenu == EmptyView, Footer == EmptyView {
    init(chatID: Int64, messages: [JSON], start: Int = 0, busy: Bool = false, compact: Bool = false) {
        self.init(chatID: chatID, messages: messages, start: start, busy: busy, compact: compact,
                  menu: { _, _ in EmptyView() }, footer: { EmptyView() })
    }
}

extension TranscriptList where RowMenu == EmptyView {
    init(chatID: Int64, messages: [JSON], start: Int = 0, busy: Bool = false, compact: Bool = false,
         @ViewBuilder footer: @escaping () -> Footer) {
        self.init(chatID: chatID, messages: messages, start: start, busy: busy, compact: compact,
                  menu: { _, _ in EmptyView() }, footer: footer)
    }
}

/// visitWatermark's divider: everything below arrived since the last visit.
struct NewDivider: View {
    var body: some View {
        HStack(spacing: 8) {
            Rectangle().fill(Theme.accent.opacity(0.4)).frame(height: 1)
            Text("New").font(.brand(-4, weight: .bold)).foregroundStyle(Theme.accent)
            Rectangle().fill(Theme.accent.opacity(0.4)).frame(height: 1)
        }
        .accessibilityLabel("New messages")
    }
}
