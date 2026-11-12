import AppKit
import UniformTypeIdentifiers
import SwiftUI

/// Chat composer. With `chatID` set it owns the whole send path (per-chat draft, images, queue, steer,
/// completion); without it (new-thread screen) it's a plain box that calls `send`.
struct Composer<Footer: View>: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var store = ComposerStore.shared
    @ObservedObject var x = ComposerExtras.shared
    @Binding var draft: String
    let running: Bool
    var placeholder: String? = nil
    var chatID: Int64? = nil
    let send: () -> Void
    let stop: () -> Void
    @ViewBuilder let footer: () -> Footer
    @State private var focused = false
    @State private var height: CGFloat = 44
    @State private var editor = EditorHandle()
    @State private var suggestions: [Suggestion] = []
    @State private var pick = 0
    @State private var trigger: ComposerTrigger?
    @State private var historyPos: PromptHistoryPos?
    @State private var pasteView: String?

    var body: some View {
        let text = textBinding
        VStack(alignment: .leading, spacing: 6) {
            if let id = chatID { QueueList(chatID: id, running: running) }
            if !suggestions.isEmpty { SuggestionList(items: suggestions, pick: pick) { apply($0) } }
            VStack(alignment: .leading, spacing: 8) {
                if let id = chatID, !(store.images[id] ?? []).isEmpty { Thumbnails(chatID: id) }
                ZStack(alignment: .topLeading) {
                    if text.wrappedValue.isEmpty {
                        Text(placeholder ?? (running ? "Enter queues it, ⌘↩ steers it into the turn. @ file · / command · $ skill" : "Message…  @ file · / command · $ skill"))
                            .font(.ui(1)).foregroundStyle(Theme.muted).lineLimit(1)
                            .padding(.leading, 5).allowsHitTesting(false)
                    }
                    ComposerTextView(text: text, height: $height, focused: $focused, handle: editor,
                                     registryKey: chatID ?? -1, collapsePastes: chatID != nil,
                                     onKey: key, onCaret: { complete($0, $1) }, onImages: addImages,
                                     onFiles: { paths in for p in paths { text.wrappedValue = appendRef(text.wrappedValue, path: p) } },
                                     onPasteClick: { pasteView = $0 })
                        .frame(height: height)
                }
                .padding(.horizontal, 4).padding(.top, 4)
                HStack(spacing: 4) {
                    if chatID != nil {
                        Button { pickImages() } label: { Image(systemName: "paperclip").foregroundStyle(Theme.text2) }
                            .buttonStyle(.plain).help("Attach images (or paste / drop them)").padding(.trailing, 4)
                    }
                    footer()
                    Spacer()
                    StashMenu(empty: Stash.isEmpty(text.wrappedValue, chatID.flatMap { store.images[$0] } ?? []),
                              canImages: chatID != nil, stash: stashNow, restore: restore)
                    if let id = chatID {
                        RecapButton(chatID: id)
                        PlanUsageMeter()
                        ContextRing(chatID: id, running: running)
                        CostBadge(chatID: id, running: running)
                    }
                    buttons(text.wrappedValue)
                }
            }
            .padding(12)
            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 20))
            .overlay(RoundedRectangle(cornerRadius: 20).strokeBorder(focused ? Theme.accent.opacity(0.6) : Theme.border, lineWidth: focused ? 1.5 : 1))
            .shadow(color: focused ? Theme.accent.opacity(0.18) : .black.opacity(0.25), radius: focused ? 14 : 8, y: 4)
            .animation(.easeOut(duration: 0.15), value: focused)
        }
        .padding(.horizontal, 20).padding(.bottom, 16).padding(.top, 8)
        .frame(maxWidth: 860)
        .onAppear { store.attach(b); x.attach(b); adoptDraft(); loadChat() }
        .onChange(of: chatID) { suggestions = []; historyPos = nil; adoptDraft(); loadChat() }
        .sheet(item: Binding(get: { pasteView.map(PasteText.init) }, set: { pasteView = $0?.text })) { p in
            PasteTextSheet(text: p.text) { pasteView = nil }
        }
        .onChange(of: draft) { adoptDraft() }
    }

    func loadChat() {
        guard let id = chatID else { return }
        Task { await store.loadQueue(b, id) }
        if b.selectedChat != id { Task { await x.loadHistory(b, id) } }
    }

    /// In chat mode the per-chat draft is the source of truth; text the host writes into `draft`
    /// (rewind, PR refill) moves into it.
    func adoptDraft() {
        guard let id = chatID, !draft.isEmpty else { return }
        store.setDraft(id, draft)
        draft = ""
    }

    var cwd: String {
        guard let id = chatID, let c = b.chat(id) else { return "" }
        return b.workspaces[c.i("workspace_id")]?.s("path") ?? ""
    }

    var textBinding: Binding<String> {
        guard let id = chatID else { return $draft }
        return Binding(get: { store.drafts[id] ?? "" }, set: { store.setDraft(id, $0) })
    }

    @ViewBuilder func buttons(_ text: String) -> some View {
        let empty = text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && (chatID.flatMap { store.images[$0] } ?? []).isEmpty
        HStack(spacing: 6) {
            if running && empty {
                Button(action: stop) { roundIcon("stop.fill", Theme.red) }.help("Interrupt")
            } else {
                if running, chatID != nil {
                    Button { submit(steer: true) } label: { roundIcon("arrow.turn.down.right", Theme.yellow) }
                        .help("Steer into the running turn (⌘↩)")
                }
                Button { submit(steer: false) } label: { roundIcon("arrow.up", empty ? Theme.hover : Theme.accent) }
                    .disabled(empty)
                    .help(running ? "Queue for when the turn ends (↩)" : "Send (↩)")
            }
        }
        .buttonStyle(.plain)
    }

    func roundIcon(_ name: String, _ c: Color) -> some View {
        Image(systemName: name).font(.system(size: 13, weight: .bold)).foregroundStyle(Theme.base)
            .frame(width: 30, height: 30).background(c, in: Circle())
    }

    func submit(steer: Bool) {
        suggestions = []
        historyPos = nil
        guard let id = chatID else { send(); return }
        Task {
            await store.submit(b, id, running: running, steer: steer)
            if b.selectedChat != id { await x.loadHistory(b, id) }
        }
    }

    // MARK: keys + completion

    /// AgentChat.vue onKeydown order: Esc interrupts a running turn, ⌘K clears, ⌘S stashes, then the
    /// suggestion list claims ↑↓ ↩ ⇥ ⎋, then ↑/↓ recall, then ⌘↩ steers while running, ↩ sends (⇧↩ newline).
    func key(_ k: ComposerKey) -> Bool {
        switch k {
        case .escape where running && chatID != nil: stop(); return true
        case .clear:
            guard let id = chatID else { return false }
            Task { await store.clear(b, id) }
            return true
        case .stash: stashNow(); return true
        default: break
        }
        if !suggestions.isEmpty {
            switch k {
            case .up: pick = (pick - 1 + suggestions.count) % suggestions.count; return true
            case .down: pick = (pick + 1) % suggestions.count; return true
            case .tab, .enter(false, _): apply(suggestions[pick]); return true
            case .escape: suggestions = []; return true
            default: break
            }
        }
        switch k {
        case .up: return stepHistory(back: true)
        case .down: return stepHistory(back: false)
        case .enter(false, let cmd): submit(steer: cmd && running); return true
        default: return false
        }
    }

    /// ↑ at the start of the box (or in an empty one) recalls what was typed earlier in this thread; ↓ walks
    /// forward and back to the draft. A recalled single line steps from any caret position.
    func stepHistory(back: Bool) -> Bool {
        guard let id = chatID, let tv = editor.tv else { return false }
        let text = textBinding.wrappedValue
        let caret = tv.selectedRange().location
        let oneLine = recalling(historyPos, text) && !text.contains("\n")
        if back ? !(text.isEmpty || caret == 0 || oneLine) : !(caret >= (tv.string as NSString).length || oneLine) { return false }
        let entries = b.selectedChat == id ? promptHistory(b.messages) : x.history[id] ?? []
        guard let step = stepPromptHistory(entries, historyPos, text, back: back) else { return false }
        historyPos = step.pos
        textBinding.wrappedValue = step.text
        return true
    }

    /// Park the composer's content (text + images) in the global stash and clear it.
    func stashNow() {
        let text = textBinding.wrappedValue, imgs = chatID.flatMap { store.images[$0] } ?? []
        guard !Stash.isEmpty(text, imgs), x.commitStash(Stash.push(x.stash, Stash.make(text, imgs))) else { return }
        textBinding.wrappedValue = ""
        if let id = chatID { store.images[id] = nil }
    }

    func restore(_ e: StashEntry) {
        let text = textBinding.wrappedValue, imgs = chatID.flatMap { store.images[$0] } ?? []
        let r = Stash.restore(x.stash, e.id, text: text, images: imgs)
        guard let got = r.restored, x.commitStash(r.entries) else { return }
        // The new-thread box sends `draft` verbatim, so it never holds paste markers.
        textBinding.wrappedValue = chatID == nil ? stripPasteMarkers(got.text) : got.text
        if let id = chatID { store.images[id] = got.images.isEmpty ? nil : got.images }
    }

    func complete(_ text: String, _ caret: Int) {
        guard let t = composerTrigger(text, caret: caret) else { suggestions = []; trigger = nil; return }
        trigger = t
        let agent = chatID.flatMap { b.chat($0)?.s("agent_kind") } ?? "claude"
        let cwd = cwd
        Task {
            let list = await store.suggestions(b, t, cwd: cwd, agent: agent.isEmpty ? "claude" : agent)
            guard trigger == t else { return } // the caret moved while git / list_skills ran
            suggestions = list; pick = 0
        }
    }

    func apply(_ s: Suggestion) {
        guard let t = trigger else { return }
        editor.replace(t.range, with: s.insert + " ")
        suggestions = []; trigger = nil
    }

    func addImages(_ urls: [String]) {
        guard let id = chatID else { return }
        store.images[id, default: []] += urls
    }

    func pickImages() {
        let p = NSOpenPanel()
        p.allowsMultipleSelection = true
        p.allowedContentTypes = [.image]
        guard p.runModal() == .OK else { return }
        addImages(p.urls.compactMap(dataURL(file:)))
    }
}

// MARK: - store

struct Suggestion: Identifiable {
    var id: String { insert }
    let label: String
    let hint: String
    let insert: String
    let icon: String
}

/// Composer state that outlives a view: drafts, pending images, Go's queue mirror, context usage, catalogs.
@MainActor
final class ComposerStore: ObservableObject {
    static let shared = ComposerStore()
    @Published var drafts: [Int64: String] = [:]
    @Published var images: [Int64: [String]] = [:]
    @Published var queue: [Int64: [JSON]] = [:]
    @Published var ctx: [Int64: (tokens: Int64, window: Int64)] = [:]
    @Published var config: JSON = [:]
    private var attached = false
    private var skills: [String: (at: Date, list: [JSON])] = [:]
    private var files: [String: [String]] = [:]

    init() {
        // ponytail: drafts persist as one UserDefaults dict; per-key storage if they grow large.
        for (k, v) in UserDefaults.standard.dictionary(forKey: "composer.drafts") ?? [:] {
            if let id = Int64(k), let s = v as? String { drafts[id] = s }
        }
    }

    func setDraft(_ id: Int64, _ s: String) {
        drafts[id] = s.isEmpty ? nil : s
        UserDefaults.standard.set(Dictionary(uniqueKeysWithValues: drafts.map { ("\($0)", $1) }), forKey: "composer.drafts")
    }

    func attach(_ b: Burrow) {
        guard !attached else { return }
        attached = true
        b.subscribe("chat-queue-changed") { [weak self] _, p in
            guard let p = p as? JSON else { return }
            self?.queue[p.i("chatId")] = p["items"] as? [JSON] ?? []
        }
        b.subscribe("chat-event-") { [weak self] name, p in
            guard let self, let id = Int64(name.dropFirst("chat-event-".count)), let p = p as? JSON else { return }
            for e in p["events"] as? [JSON] ?? [] {
                var c = self.ctx[id] ?? (0, 0)
                if e.s("type") == "context.usage", e.i("contextTokens") > 0 { c.tokens = e.i("contextTokens") }
                if e.s("type") == "turn.completed", e.i("contextWindow") > 0 { c.window = e.i("contextWindow") }
                if c != (self.ctx[id] ?? (0, 0)) { self.ctx[id] = c }
            }
        }
        Task { await loadConfig(b) }
    }

    func loadConfig(_ b: Burrow) async {
        guard let s = try? await b.call("read_config") as? String,
              let c = (try? JSONSerialization.jsonObject(with: Data(s.utf8))) as? JSON else { return }
        config = c
    }

    func loadQueue(_ b: Burrow, _ id: Int64) async {
        queue[id] = (try? await b.call("chat_queue_list", ["chatId": id])) as? [JSON] ?? []
    }

    func submit(_ b: Burrow, _ id: Int64, running: Bool, steer: Bool) async {
        let raw = drafts[id] ?? ""
        // Paste chips go out as their plain text (AgentChat.vue stripPasteMarkers).
        let text = stripPasteMarkers(raw).trimmingCharacters(in: .whitespacesAndNewlines)
        let imgs = images[id] ?? []
        guard !text.isEmpty || !imgs.isEmpty else { return }
        if text == "/clear", imgs.isEmpty { await clear(b, id); return }
        setDraft(id, ""); images[id] = nil
        // Pending transcript comments ride along with the message (TranscriptFeedbackBar.vue).
        let fb = TranscriptFeedback.forChat(id), batch = fb.pending
        let sent = batch.isEmpty ? text : composeTranscriptFeedback(batch, request: text)
        let args: JSON = ["chatId": id, "text": sent, "images": imgs]
        do {
            if running && steer {
                // A refusal (not steerable now / generic ACP) leaves it queued, like the Vue composer.
                do { try await b.call("chat_steer", args) } catch { try await b.call("chat_queue_add", args) }
            } else if running {
                try await b.call("chat_queue_add", args)
            } else {
                try await b.call("chat_send", args)
            }
            if !batch.isEmpty { fb.markSent(batch, text: sent) }
        } catch {
            setDraft(id, raw); images[id] = imgs
            b.status = "Send failed: \(error.localizedDescription)"
        }
    }

    /// ⌘K / `/clear` (AgentChat.vue clearChat): drop this view's queue + images + draft, Go wipes the transcript
    /// and session (chat_clear → session.reset{cleared}). ponytail: Vue also renames the chat to "Chat"; skipped.
    func clear(_ b: Burrow, _ id: Int64) async {
        setDraft(id, ""); images[id] = nil
        for q in queue[id] ?? [] { _ = try? await b.call("chat_queue_remove", ["id": q.i("id")]) }
        do { try await b.call("chat_clear", ["chatId": id]) } catch { b.status = "Clear failed: \(error.localizedDescription)" }
    }

    // queue ops (Go owns the queue; chat-queue-changed refreshes the mirror)
    func update(_ b: Burrow, _ item: JSON, _ text: String) async { try? await b.call("chat_queue_update", ["id": item.i("id"), "text": text]) }
    func remove(_ b: Burrow, _ item: JSON) async { try? await b.call("chat_queue_remove", ["id": item.i("id")]) }
    func move(_ b: Burrow, _ chatID: Int64, _ item: JSON, by d: Int) async {
        var ids = (queue[chatID] ?? []).map { $0.i("id") }
        guard let i = ids.firstIndex(of: item.i("id")), ids.indices.contains(i + d) else { return }
        ids.swapAt(i, i + d)
        try? await b.call("chat_queue_reorder", ["chatId": chatID, "ids": ids])
    }
    /// Steer from the queue = remove first, then chat_steer (CLAUDE.md "Queued follow-ups"); idle → plain send.
    func sendNow(_ b: Burrow, _ chatID: Int64, _ item: JSON, running: Bool) async {
        let args: JSON = ["chatId": chatID, "text": item.s("text"), "images": item["attachments"] as? [String] ?? []]
        do {
            try await b.call("chat_queue_remove", ["id": item.i("id")])
            try await b.call(running ? "chat_steer" : "chat_send", args)
        } catch {
            try? await b.call("chat_queue_add", args)
            b.status = "Steer failed: \(error.localizedDescription)"
        }
    }

    // MARK: completion sources

    /// Built-ins that work in stream-json mode (AgentChat.vue BUILTIN_COMMANDS).
    static let commands = [("pr", "Write a PR description from recent git diff"), ("clear", "Clear conversation history"),
                           ("compact", "Compact conversation with summary"), ("help", "Show available commands"),
                           ("review", "Review changes in current directory"), ("init", "Initialize project with CLAUDE.md"),
                           ("cost", "Show token and cost usage for this session")]

    func suggestions(_ b: Burrow, _ t: ComposerTrigger, cwd: String, agent: String) async -> [Suggestion] {
        let q = t.query
        func rank<T>(_ xs: [T], _ key: (T) -> String) -> [T] {
            xs.compactMap { x in fuzzyScore(q, key(x)).map { (x, $0) } }.sorted { $0.1 > $1.1 }.map(\.0)
        }
        if t.kind == "@" {
            guard !cwd.isEmpty else { return [] }
            return rank(await fileList(b, cwd)) { $0 }.prefix(8).map {
                Suggestion(label: ($0 as NSString).lastPathComponent, hint: $0, insert: "@\($0)", icon: "doc")
            }
        }
        var out: [Suggestion] = []
        if t.kind == "/", agent == "claude" {
            out += rank(Self.commands) { $0.0 }.map { Suggestion(label: "/\($0.0)", hint: $0.1, insert: "/\($0.0)", icon: "command") }
        }
        out += rank(await skillList(b, cwd)) { $0.s("name") }.map {
            Suggestion(label: $0.s("name"), hint: $0.s("description"), insert: "/\($0.s("name"))",
                       icon: $0.s("source") == "project" ? "folder" : "sparkles")
        }
        return Array(out.prefix(10))
    }

    private func fileList(_ b: Burrow, _ cwd: String) async -> [String] {
        if let f = files[cwd] { return f }
        // ponytail: cached per cwd for the app's life (same ceiling as composerCompletion.ts loadFiles).
        let out = (try? await b.call("run_git", ["cwd": cwd, "args": ["ls-files", "--cached", "--others", "--exclude-standard"]])) as? JSON
        let list = Array(out?.s("stdout").split(separator: "\n").map(String.init).prefix(20000) ?? [])
        files[cwd] = list
        return list
    }

    private func skillList(_ b: Burrow, _ cwd: String) async -> [JSON] {
        if let s = skills[cwd], Date().timeIntervalSince(s.at) < 60 { return s.list }
        let raw = (try? await b.call("list_skills", ["cwd": cwd])) as? [JSON] ?? []
        var byName: [String: JSON] = [:] // a project skill shadows a personal one of the same name
        for s in raw where s["enabled"] as? Bool ?? true { byName[s.s("name")] = s }
        let list = byName.values.sorted { $0.s("name") < $1.s("name") }
        skills[cwd] = (Date(), list)
        return list
    }

    // MARK: catalog (config.json via read_config; Claude's list is client-side in Vue too)

    var agents: [(String, String)] {
        let p = (config["providers"] as? [JSON] ?? []).filter { $0["enabled"] as? Bool ?? false }
        return p.isEmpty ? Launch.agents : p.map { ($0.s("id"), $0.s("name")) }
    }

    /// Claude provider instances (each a config dir) — the profile switcher's list.
    var claudeProfiles: [JSON] {
        (config["providers"] as? [JSON] ?? []).filter { $0.s("providerId") == "claude" && ($0["enabled"] as? Bool ?? false) }
    }

    func models(_ agent: String, current: String) -> [(id: String, label: String, efforts: [String])] {
        let hidden = Set(config["chatHiddenModels"] as? [String] ?? [])
        let seen = (config["chatModelsSeen"] as? JSON)?[agent] as? [JSON] ?? []
        var out: [(id: String, label: String, efforts: [String])] = [("", "Default model", [])]
        let catalog = agent == "claude" ? Launch.claudeModels
            : seen.map { (id: $0.s("id"), label: $0.s("label"), efforts: $0["efforts"] as? [String] ?? []) }
        out += catalog.filter { !hidden.contains("\(agent)/\($0.id)") || $0.id == current }
        if !current.isEmpty, !out.contains(where: { $0.id == current }) { out.append((current, current, [])) }
        return out
    }
}

// MARK: - pieces

struct QueueList: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var store = ComposerStore.shared
    let chatID: Int64
    let running: Bool
    @State private var editing: Int64?
    @State private var text = ""

    var body: some View {
        let items = store.queue[chatID] ?? []
        if !items.isEmpty {
            VStack(alignment: .leading, spacing: 4) {
                Text("Queued · sent when the turn ends").font(.brand(-5)).foregroundStyle(Theme.muted)
                ForEach(items.indices, id: \.self) { i in row(items[i], i, items.count) }
            }
            .padding(10)
            .background(Theme.sidebar, in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.border))
        }
    }

    @ViewBuilder func row(_ q: JSON, _ i: Int, _ n: Int) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "clock").font(.system(size: 10)).foregroundStyle(Theme.muted)
            if editing == q.i("id") {
                TextField("", text: $text).textFieldStyle(.plain).font(.ui(-1))
                    .onSubmit { Task { await store.update(b, q, text) }; editing = nil }
                    .onExitCommand { editing = nil }
            } else {
                Text(q.s("text")).font(.ui(-1)).foregroundStyle(Theme.text).lineLimit(2)
                if !(q["attachments"] as? [String] ?? []).isEmpty { Image(systemName: "photo").foregroundStyle(Theme.muted) }
                if q.i("attempts") >= 5 { Text("send failed").font(.brand(-5)).foregroundStyle(Theme.red) }
            }
            Spacer()
            Group {
                icon("arrow.up", "Move up", i > 0) { await store.move(b, chatID, q, by: -1) }
                icon("arrow.down", "Move down", i < n - 1) { await store.move(b, chatID, q, by: 1) }
                Button { editing = q.i("id"); text = q.s("text") } label: { Image(systemName: "pencil") }.help("Edit")
                icon("arrow.turn.down.right", running ? "Steer into the running turn now" : "Send now", true) {
                    await store.sendNow(b, chatID, q, running: running)
                }
                icon("trash", "Delete", true) { await store.remove(b, q) }
            }
            .buttonStyle(.plain).foregroundStyle(Theme.text2).font(.system(size: 11))
        }
    }

    func icon(_ name: String, _ help: String, _ on: Bool, _ act: @escaping () async -> Void) -> some View {
        Button { Task { await act() } } label: { Image(systemName: name) }.disabled(!on).help(help).opacity(on ? 1 : 0.3)
    }
}

struct SuggestionList: View {
    let items: [Suggestion]
    let pick: Int
    let apply: (Suggestion) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(items.indices, id: \.self) { i in
                let s = items[i]
                HStack(spacing: 8) {
                    Image(systemName: s.icon).foregroundStyle(Theme.accent).frame(width: 16)
                    Text(s.label).font(.brand(-2)).foregroundStyle(Theme.text).lineLimit(1)
                    Text(s.hint).font(.ui(-3)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.head)
                    Spacer()
                }
                .padding(.horizontal, 10).padding(.vertical, 5)
                .background(i == pick ? Theme.selected : .clear, in: RoundedRectangle(cornerRadius: 7))
                .contentShape(Rectangle())
                .onTapGesture { apply(s) }
            }
        }
        .padding(4)
        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.border))
    }
}

struct Thumbnails: View {
    @ObservedObject var store = ComposerStore.shared
    let chatID: Int64

    var body: some View {
        let imgs = store.images[chatID] ?? []
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(imgs.indices, id: \.self) { i in
                    ZStack(alignment: .topTrailing) {
                        if let img = nsImage(dataURL: imgs[i]) {
                            Image(nsImage: img).resizable().scaledToFill().frame(width: 56, height: 56)
                                .clipShape(RoundedRectangle(cornerRadius: 7))
                        }
                        Button { store.images[chatID]?.remove(at: i) } label: {
                            Image(systemName: "xmark.circle.fill").foregroundStyle(Theme.text, Theme.base)
                        }
                        .buttonStyle(.plain).offset(x: 4, y: -4)
                    }
                }
            }
            .padding(.top, 4).padding(.trailing, 4)
        }
    }
}

/// Context window fill from the last `context.usage` / `turn.completed` events on chat-event-<id>.
struct ContextRing: View {
    @ObservedObject var store = ComposerStore.shared
    let chatID: Int64
    var running = false
    @State private var open = false

    var body: some View {
        if let c = store.ctx[chatID], c.tokens > 0 {
            // ponytail: no window reported yet → 200k, or 1M once usage passes 200k. Read the model's window if it matters.
            let window = c.window > 0 ? c.window : c.tokens > 200_000 ? 1_000_000 : 200_000
            let r = min(Double(c.tokens) / Double(window), 1)
            let col = r > 0.85 ? Theme.red : r > 0.6 ? Theme.yellow : Theme.text2
            Button { open.toggle() } label: {
                ZStack {
                    Circle().stroke(Theme.hover, lineWidth: 2.5)
                    Circle().trim(from: 0, to: r).stroke(col, style: StrokeStyle(lineWidth: 2.5, lineCap: .round)).rotationEffect(.degrees(-90))
                }
                .frame(width: 16, height: 16).padding(.horizontal, 6)
            }
            .buttonStyle(.plain)
            .help("Context \(Int(r * 100))% · \(c.tokens.formatted()) / \(window.formatted()) tokens")
            .popover(isPresented: $open, arrowEdge: .top) { ContextCard(chatID: chatID, tokens: c.tokens, window: window, running: running) }
        }
    }
}

// MARK: - text view

enum ComposerKey { case enter(shift: Bool, cmd: Bool), tab, escape, up, down, clear, stash }

struct PasteText: Identifiable { let text: String; var id: String { text } }

/// Lets the SwiftUI side splice a completion into the NSTextView (with undo).
final class EditorHandle {
    weak var tv: NSTextView?
    func replace(_ r: NSRange, with s: String) {
        guard let tv, NSMaxRange(r) <= (tv.string as NSString).length else { return }
        tv.insertText(s, replacementRange: r)
    }
}

/// NSTextView instead of TextField: caret position (completion), key routing, image paste / drop.
struct ComposerTextView: NSViewRepresentable {
    @Binding var text: String
    @Binding var height: CGFloat
    @Binding var focused: Bool
    let handle: EditorHandle
    var registryKey: Int64 = -1
    var collapsePastes = false
    let onKey: (ComposerKey) -> Bool
    let onCaret: (String, Int) -> Void
    let onImages: ([String]) -> Void
    var onFiles: ([String]) -> Void = { _ in }
    var onPasteClick: (String) -> Void = { _ in }

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSTextView.scrollableTextView()
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        let old = scroll.documentView as! NSTextView
        let tv = KeyTextView(frame: old.frame, textContainer: old.textContainer)
        tv.autoresizingMask = old.autoresizingMask
        tv.isVerticallyResizable = true
        tv.minSize = old.minSize; tv.maxSize = old.maxSize
        scroll.documentView = tv
        tv.delegate = context.coordinator
        tv.coord = context.coordinator
        tv.drawsBackground = false
        tv.isRichText = true // paste chips are attachments; typed / pasted text stays plain (pasteAsPlainText)
        tv.importsGraphics = false
        tv.usesFontPanel = false
        tv.allowsUndo = true
        tv.isAutomaticQuoteSubstitutionEnabled = false
        tv.isAutomaticDashSubstitutionEnabled = false
        tv.textContainerInset = NSSize(width: 0, height: 2)
        tv.font = Self.font
        tv.textColor = NSColor(Theme.text)
        tv.insertionPointColor = NSColor(Theme.accent)
        tv.textStorage?.setAttributedString(composerDisplay(text, font: Self.font))
        handle.tv = tv
        ComposerExtras.shared.register(registryKey, tv)
        context.coordinator.restyle(tv)
        DispatchQueue.main.async { tv.window?.makeFirstResponder(tv) }
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        context.coordinator.parent = self
        guard let tv = scroll.documentView as? NSTextView else { return }
        if tv.font != Self.font { tv.font = Self.font }
        ComposerExtras.shared.register(registryKey, tv)
        if let ts = tv.textStorage, composerModel(ts) != text { // chat switched, sent, recalled, or a draft restored
            ts.setAttributedString(composerDisplay(text, font: Self.font))
            // An external write is a new baseline, not something ⌘Z walks back through (ComposerTextInput.vue).
            tv.undoManager?.removeAllActions(withTarget: ts)
            tv.undoManager?.removeAllActions(withTarget: tv)
            tv.setSelectedRange(NSRange(location: ts.length, length: 0))
            context.coordinator.restyle(tv)
        }
    }

    static var font: NSFont {
        let v = UserDefaults.standard.double(forKey: "fontSize")
        return .systemFont(ofSize: (v > 0 ? v : 15) + 1)
    }

    final class Coordinator: NSObject, NSTextViewDelegate {
        var parent: ComposerTextView
        init(_ p: ComposerTextView) { parent = p }

        func textDidChange(_ n: Notification) {
            guard let tv = n.object as? NSTextView, let ts = tv.textStorage else { return }
            parent.text = composerModel(ts)
            restyle(tv)
            parent.onCaret(tv.string, tv.selectedRange().location)
        }

        func textViewDidChangeSelection(_ n: Notification) {
            guard let tv = n.object as? NSTextView, !tv.hasMarkedText() else { return }
            parent.onCaret(tv.string, tv.selectedRange().location)
        }

        func textView(_ tv: NSTextView, clickedOn cell: any NSTextAttachmentCellProtocol, in cellFrame: NSRect, at charIndex: Int) {
            if let p = cell.attachment as? PasteAttachment { parent.onPasteClick(p.pasted) }
        }

        /// Tokens render inline (accent), never as chips; also re-measures the height (2…12 lines).
        func restyle(_ tv: NSTextView) {
            if let ts = tv.textStorage, !tv.hasMarkedText() {
                let all = NSRange(location: 0, length: ts.length)
                ts.beginEditing()
                ts.addAttribute(.foregroundColor, value: NSColor(Theme.text), range: all)
                for r in composerTokenRanges(tv.string) { ts.addAttribute(.foregroundColor, value: NSColor(Theme.accent), range: r) }
                ts.endEditing()
                tv.typingAttributes = [.font: tv.font ?? ComposerTextView.font, .foregroundColor: NSColor(Theme.text)]
            }
            tv.layoutManager?.ensureLayout(for: tv.textContainer!)
            let used = tv.layoutManager?.usedRect(for: tv.textContainer!).height ?? 0
            let line = (tv.font?.boundingRectForFont.height ?? 18)
            let h = min(max(used + 6, line * 1.4), line * 12)
            if abs(h - parent.height) > 0.5 { DispatchQueue.main.async { self.parent.height = h } }
        }
    }
}

final class KeyTextView: NSTextView {
    weak var coord: ComposerTextView.Coordinator?

    override func keyDown(with e: NSEvent) {
        guard !hasMarkedText(), let c = coord else { return super.keyDown(with: e) }
        let m = e.modifierFlags
        let plain = m.intersection([.shift, .command, .option, .control]).isEmpty
        let k: ComposerKey? = switch e.keyCode {
        // Vue: Enter without Shift sends (⌥ too), ⌘/⌃ steers while running.
        case 36, 76: .enter(shift: m.contains(.shift), cmd: m.contains(.command) || m.contains(.control))
        case 48 where plain: .tab
        case 53: .escape
        case 126 where plain: .up
        case 125 where plain: .down
        default: nil
        }
        if let k, c.parent.onKey(k) { return }
        super.keyDown(with: e)
    }

    /// ⌘K clear chat, ⌘S stash — only while this box has focus (AgentChat.vue onKeydown).
    override func performKeyEquivalent(with e: NSEvent) -> Bool {
        guard window?.firstResponder === self, let c = coord,
              e.modifierFlags.intersection([.command, .shift, .option, .control]) == .command else { return super.performKeyEquivalent(with: e) }
        switch e.charactersIgnoringModifiers?.lowercased() {
        case "k" where c.parent.onKey(.clear): return true
        case "s" where c.parent.onKey(.stash): return true
        default: return super.performKeyEquivalent(with: e)
        }
    }

    override func becomeFirstResponder() -> Bool {
        let ok = super.becomeFirstResponder()
        if ok, let c = coord { ComposerExtras.shared.focused(c.parent.registryKey) }
        if ok { DispatchQueue.main.async { self.coord?.parent.focused = true } }
        return ok
    }

    override func resignFirstResponder() -> Bool {
        let ok = super.resignFirstResponder()
        if ok { DispatchQueue.main.async { self.coord?.parent.focused = false } }
        return ok
    }

    /// Images → attachments; a paste of more than 4 lines → a paste chip (ComposerTextInput.vue onPaste); else plain text.
    override func paste(_ sender: Any?) {
        let imgs = images(from: .general)
        if !imgs.isEmpty { coord?.parent.onImages(imgs); return }
        if coord?.parent.collapsePastes == true, let s = NSPasteboard.general.string(forType: .string), shouldCollapsePaste(s) {
            let f = font ?? ComposerTextView.font
            let chip = NSMutableAttributedString(attachment: PasteAttachment(s, font: f))
            chip.addAttributes([.font: f, .foregroundColor: NSColor(Theme.text)], range: NSRange(location: 0, length: chip.length))
            insertText(chip, replacementRange: selectedRange())
            return
        }
        pasteAsPlainText(sender)
    }

    override var acceptableDragTypes: [NSPasteboard.PasteboardType] { super.acceptableDragTypes + [.fileURL] }

    /// Finder drop (useFileDrop.ts): images attach, any other file becomes an `@path` in the text.
    override func performDragOperation(_ s: NSDraggingInfo) -> Bool {
        let pb = s.draggingPasteboard
        let urls = pb.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        if !urls.isEmpty {
            let imgs = urls.compactMap(dataURL(file:))
            if !imgs.isEmpty { coord?.parent.onImages(imgs) }
            let others = urls.filter { dataURL(file: $0) == nil }.map(\.path)
            if !others.isEmpty { coord?.parent.onFiles(others) }
            return true
        }
        let imgs = images(from: pb)
        if imgs.isEmpty { return super.performDragOperation(s) }
        coord?.parent.onImages(imgs)
        return true
    }

    /// Image files, or raw image data when there is no text alongside (a screenshot to the clipboard).
    func images(from pb: NSPasteboard) -> [String] {
        let urls = (pb.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? [])
        let files = urls.compactMap(dataURL(file:))
        if !files.isEmpty { return files }
        guard pb.string(forType: .string) == nil, let img = NSImage(pasteboard: pb) else { return [] }
        return pngDataURL(img).map { [$0] } ?? []
    }
}

func dataURL(file url: URL) -> String? {
    let mime = ["png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"]
    guard let m = mime[url.pathExtension.lowercased()], let d = try? Data(contentsOf: url) else { return nil }
    return "data:\(m);base64,\(d.base64EncodedString())"
}

func pngDataURL(_ img: NSImage) -> String? {
    guard let tiff = img.tiffRepresentation, let rep = NSBitmapImageRep(data: tiff),
          let png = rep.representation(using: .png, properties: [:]) else { return nil }
    return "data:image/png;base64,\(png.base64EncodedString())"
}

func nsImage(dataURL s: String) -> NSImage? {
    guard let comma = s.firstIndex(of: ","), let d = Data(base64Encoded: String(s[s.index(after: comma)...])) else { return nil }
    return NSImage(data: d)
}

// MARK: - launch bar

/// Agent · model · effort · permission mode (+ Claude profile on an open chat) — the Vue composer pills.
struct LaunchBar: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var store = ComposerStore.shared
    @Binding var agent: String
    var agentLocked = false
    @Binding var model: String
    @Binding var effort: String
    @Binding var mode: String
    var chatID: Int64? = nil
    @ObservedObject var x = ComposerExtras.shared

    /// ponytail: like `profile`, falls back to b.selectedChat when the (agent-locked) thread bar passes no chatID.
    var openChat: Int64? { chatID ?? (agentLocked ? b.selectedChat : nil) }

    var transport: String {
        if let id = openChat, let t = b.chat(id)?.s("transport"), !t.isEmpty { return t }
        let p = (store.config["providers"] as? [JSON] ?? []).first { $0.s("id") == agent }
        if let t = p?.s("transport"), !t.isEmpty { return t }
        return agent == "codex" || p?.s("providerId") == "codex" ? "codex-app-server" : agent == "claude" || p?.s("providerId") == "claude" ? "claude-cli" : "acp"
    }

    var body: some View {
        // ACP / Codex: the selectors the adapter reported (live session, else its last probe) — AgentChat.vue acp*Option.
        let caps = transport == "claude-cli" ? nil : x.caps(chat: openChat, agent: agent, config: store.config)
        let acpModel = caps.flatMap { acpOption($0.options, "model") }
        let acpEffort = caps.flatMap { acpOption($0.options, "effort") }
        let acpModes: [(String, String)]? = transport == "acp" ? caps.flatMap { c in
            let m = (c.modes?["availableModes"] as? [JSON] ?? []).map { ($0.s("id"), $0.s("name").isEmpty ? $0.s("id") : $0.s("name")) }
            return m.isEmpty ? acpOption(c.options, "mode").map(acpChoices) : m
        } : nil
        HStack(spacing: 2) {
            if agentLocked {
                pill(label(store.agents, agent), "cpu")
            } else {
                Menu { choices(store.agents, $agent) } label: { pill(label(store.agents, agent), "cpu") }.pillStyle()
            }
            let models = store.models(agent, current: model)
            let pairs = acpModel.map { withCurrent([("", "Default model")] + acpChoices($0), model) } ?? models.map { ($0.id, $0.label) }
            Menu { choices(pairs, $model) } label: { pill(label(pairs, model), "sparkles") }.pillStyle()
            let known = models.first { $0.id == model }?.efforts ?? []
            let efforts = acpEffort.map { [("", "Default effort")] + acpChoices($0) }
                ?? (known.isEmpty ? Launch.efforts : [("", "Default effort")] + known.map { ($0, Launch.effortLabel($0)) })
            Menu { choices(efforts, $effort) } label: { pill(label(efforts, effort), "gauge.with.dots.needle.50percent") }.pillStyle()
            let modes = acpModes.flatMap { $0.isEmpty ? nil : withCurrent($0, mode) } ?? Launch.modes(agent)
            Menu { choices(modes, $mode) } label: {
                pill(label(modes, mode), mode == "bypassPermissions" || mode == "dontAsk" ? "exclamationmark.shield" : "lock.shield")
            }.pillStyle()
            profile
        }
        .onAppear { store.attach(b); x.attach(b) }
    }

    /// ponytail: reads the open chat from b.selectedChat (the thread's LaunchBar is agent-locked) rather than a new
    /// parameter, so Thread.swift's call site stays as is. Only shown with 2+ Claude profiles.
    @ViewBuilder var profile: some View {
        let profiles = store.claudeProfiles
        if agentLocked, agent == "claude", profiles.count > 1, let id = b.selectedChat {
            let cur = b.chat(id)?.s("profile_id") ?? ""
            let opts = profiles.map { ($0.s("id"), $0.s("name")) }
            Menu { choices(opts, Binding(get: { cur }, set: { v in Task { await b.setLaunch(id, "profile_id", v) } })) } label: {
                pill(label(opts, cur), "person.crop.circle")
            }.pillStyle()
        }
    }

    func withCurrent(_ opts: [(String, String)], _ cur: String) -> [(String, String)] {
        cur.isEmpty || opts.contains { $0.0 == cur } ? opts : opts + [(cur, cur)]
    }

    @ViewBuilder func choices(_ opts: [(String, String)], _ sel: Binding<String>) -> some View {
        ForEach(opts, id: \.0) { id, name in
            Button { sel.wrappedValue = id } label: {
                if id == sel.wrappedValue { Label(name, systemImage: "checkmark") } else { Text(name) }
            }
        }
    }

    func label(_ opts: [(String, String)], _ v: String) -> String {
        opts.first { $0.0 == v }?.1 ?? (v.isEmpty ? opts.first?.1 ?? "Default" : v)
    }

    func pill(_ text: String, _ icon: String) -> some View {
        Label(text, systemImage: icon).font(.brand(-4)).foregroundStyle(Theme.text2)
    }
}

extension View {
    func pillStyle() -> some View {
        menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize().padding(.horizontal, 6).padding(.vertical, 3)
            .background(Theme.hover.opacity(0.7), in: RoundedRectangle(cornerRadius: 7))
    }
}

/// Fallbacks. Claude's catalog is client-side in Vue as well (src/lib/chatModels.ts MODELS_BY_AGENT);
/// other agents' models come from config.json `chatModelsSeen` (ComposerStore.models).
enum Launch {
    static let agents = [("claude", "Claude"), ("codex", "Codex")]
    static let efforts = [("", "Default effort"), ("low", "Low"), ("medium", "Medium"), ("high", "High"), ("xhigh", "Extra high"), ("max", "Max")]
    static func effortLabel(_ e: String) -> String { efforts.first { $0.0 == e }?.1 ?? e.capitalized }

    static let opus = ["low", "medium", "high", "xhigh", "max"], sonnet = ["low", "medium", "high", "max"]
    static let claudeModels: [(id: String, label: String, efforts: [String])] = [
        ("claude-opus-5-5", "Opus 5.5", opus), ("claude-fable-5-1", "Fable 5.1", opus), ("claude-opus-5", "Opus 5", opus),
        ("claude-sonnet-5-5", "Sonnet 5.5", sonnet), ("claude-sonnet-5", "Sonnet 5", sonnet), ("claude-haiku-5-5", "Haiku 5.5", opus),
        ("claude-fable-5", "Fable 5", opus), ("claude-opus-4-8", "Opus 4.8", opus), ("claude-sonnet-4-6", "Sonnet 4.6", sonnet),
        ("claude-haiku-4-5-20251001", "Haiku 4.5", []),
    ]

    static func models(_ agent: String, current: String) -> [(String, String)] {
        var out = [("", "Default model")] + (agent == "claude" ? claudeModels.map { ($0.id, $0.label) } : [])
        if !current.isEmpty, !out.contains(where: { $0.0 == current }) { out.append((current, current)) }
        return out
    }

    static func modes(_ agent: String) -> [(String, String)] {
        agent == "codex"
            ? [("default", "Ask"), ("auto", "Auto review"), ("acceptEdits", "Accept edits"), ("plan", "Plan"), ("dontAsk", "Full access")]
            : [("default", "Ask"), ("auto", "Auto"), ("acceptEdits", "Accept edits"), ("plan", "Plan"), ("bypassPermissions", "Bypass")]
    }
}
