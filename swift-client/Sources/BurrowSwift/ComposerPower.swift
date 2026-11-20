import AppKit
import SwiftUI

/// Composer state beyond drafts/queue: prompt stash, session cost, /context report, ACP selector catalogs,
/// plan usage, prompt-history cache, and the ⌘L focus registry.
@MainActor
final class ComposerExtras: ObservableObject {
    static let shared = ComposerExtras()
    @Published var stash: [StashEntry] = []
    @Published var cost: [Int64: Double] = [:]
    @Published var ctxReport: [Int64: [CtxRow]] = [:]
    @Published var ctxPending: Set<Int64> = []
    /// Live selector set per chat (`_burrow:"session"` handshake / selector replies on acp-data-<id>).
    @Published var acpCaps: [Int64: (modes: JSON?, options: [JSON])] = [:]
    @Published var planUsage: JSON?
    /// User rows of chats that are not b.selectedChat (sub-agent detail), for ↑ recall.
    var history: [Int64: [String]] = [:]
    private var attached = false
    private var pollers = 0

    private struct WeakTV { weak var tv: NSTextView? }
    private var editors: [Int64: WeakTV] = [:] // key -1 = the chat-less (new-thread) composer
    private var lastFocused: Int64?
    private weak var b: Burrow?

    init() {
        if let d = UserDefaults.standard.data(forKey: "composer.stash"),
           let s = try? JSONDecoder().decode([StashEntry].self, from: d) { stash = s }
    }

    func attach(_ b: Burrow) {
        guard !attached else { return }
        attached = true
        self.b = b
        b.subscribe("chat-event-") { [weak self] name, p in
            guard let self, let id = Int64(name.dropFirst("chat-event-".count)), let p = p as? JSON else { return }
            for e in p["events"] as? [JSON] ?? [] {
                switch e.s("type") {
                case "text.delta" where self.ctxPending.contains(id):
                    // ponytail: the report still lands in the transcript (Vue's fold writes chat_messages); we only lift the table.
                    if let rows = parseContextReport(e.s("text")) { self.ctxReport[id] = rows; self.ctxPending.remove(id) }
                case "turn.completed":
                    self.ctxPending.remove(id)
                    if let c = (e["costUsd"] as? NSNumber)?.doubleValue, c > 0 { self.cost[id, default: 0] += c }
                case "turn.failed": self.ctxPending.remove(id)
                case "session.reset" where (e["cleared"] as? Bool) == true: self.cost[id] = nil; self.ctxReport[id] = nil
                default: break
                }
            }
        }
        b.subscribe("acp-data-") { [weak self] name, p in
            guard let self, let id = Int64(name.dropFirst("acp-data-".count)), let line = (p as? JSON)?.s("line"),
                  let msg = (try? JSONSerialization.jsonObject(with: Data(line.utf8))) as? JSON else { return }
            if msg.s("_burrow") == "session" {
                self.acpCaps[id] = (msg["modes"] as? JSON, msg["configOptions"] as? [JSON] ?? [])
            } else if msg["method"] == nil, let r = msg["result"] as? JSON, r["configOptions"] != nil || r["modes"] != nil {
                let cur = self.acpCaps[id]
                self.acpCaps[id] = (r["modes"] as? JSON ?? cur?.modes, r["configOptions"] as? [JSON] ?? cur?.options ?? [])
            }
        }
        // ⌘L focuses the composer (keymap.ts focusComposer): the open thread's, else the one used last.
        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] e in
            guard let self, e.modifierFlags.intersection([.command, .shift, .option, .control]) == .command,
                  e.charactersIgnoringModifiers?.lowercased() == "l" else { return e }
            return self.focus() ? nil : e
        }
    }

    // MARK: focus registry

    func register(_ key: Int64, _ tv: NSTextView) { editors[key] = WeakTV(tv: tv) }
    func focused(_ key: Int64) { lastFocused = key }

    func focus() -> Bool {
        for k in [b?.selectedChat, lastFocused, -1].compactMap({ $0 }) {
            if let tv = editors[k]?.tv, let w = tv.window, w.isKeyWindow {
                w.makeFirstResponder(tv)
                tv.setSelectedRange(NSRange(location: (tv.string as NSString).length, length: 0))
                return true
            }
        }
        return false
    }

    // MARK: stash (Vue: localStorage burrow.promptStash — per device there too)

    @discardableResult func commitStash(_ next: [StashEntry]) -> Bool {
        guard let d = try? JSONEncoder().encode(next) else { return false }
        UserDefaults.standard.set(d, forKey: "composer.stash")
        stash = next
        return true
    }

    // MARK: history for chats that are not the selected one

    func loadHistory(_ b: Burrow, _ id: Int64) async {
        let page = try? await b.call("load_chat_messages_page", ["chatId": id, "before": -1, "limit": 200]) as? JSON
        history[id] = promptHistory(page?["messages"] as? [JSON] ?? [])
    }

    // MARK: /context

    func requestContextReport(_ b: Burrow, _ id: Int64) async {
        guard !ctxPending.contains(id) else { return }
        ctxPending.insert(id)
        do { try await b.call("chat_send", ["chatId": id, "text": "/context", "images": [String]()]) } catch { ctxPending.remove(id) }
    }

    // MARK: plan usage (polled while a composer shows the meter; 60 s like PlanUsageIndicator.vue)

    func startPolling(_ b: Burrow) {
        pollers += 1
        guard pollers == 1 else { return }
        Task { [weak self] in
            while let self, self.pollers > 0 {
                if let u = try? await b.call("plan_usage") as? JSON { self.planUsage = u }
                try? await Task.sleep(for: .seconds(60))
            }
        }
    }
    func stopPolling() { pollers = max(0, pollers - 1) }

    // MARK: ACP selectors

    /// The chat's live selector set, else what this agent reported last (config `chatAcpAgentCaps`, written by Vue).
    func caps(chat: Int64?, agent: String, config: JSON) -> (modes: JSON?, options: [JSON])? {
        if let id = chat, let c = acpCaps[id] { return c }
        guard let c = (config["chatAcpAgentCaps"] as? JSON)?[agent] as? JSON else { return nil }
        return (c["modes"] as? JSON, c["configOptions"] as? [JSON] ?? [])
    }
}

// MARK: - footer pieces

/// Prompt stash menu (ComposerStash.vue): stash current (⌘S), restore, delete.
struct StashMenu: View {
    @ObservedObject var x = ComposerExtras.shared
    let empty: Bool
    var canImages = true
    let stash: () -> Void
    let restore: (StashEntry) -> Void

    var body: some View {
        Menu {
            Button("Stash current prompt  ⌘S", action: stash).disabled(empty)
            Divider()
            if x.stash.isEmpty { Text("Nothing stashed") }
            ForEach(x.stash) { e in
                Menu(preview(e)) {
                    Button("Restore") { restore(e) }.disabled(!canImages && !e.images.isEmpty)
                    Button("Delete", role: .destructive) { x.commitStash(x.stash.filter { $0.id != e.id }) }
                }
            }
        } label: {
            Label(x.stash.isEmpty ? "" : "\(x.stash.count)", systemImage: "square.stack.3d.up").font(.brand(-4)).foregroundStyle(Theme.text2)
        }
        .pillStyle()
        .help("Prompt stash (\(x.stash.count))")
    }

    func preview(_ e: StashEntry) -> String {
        let t = stripPasteMarkers(e.text).trimmingCharacters(in: .whitespacesAndNewlines).replacingOccurrences(of: "\n", with: " ")
        let ago = RelativeDateTimeFormatter().localizedString(for: Date(timeIntervalSince1970: e.createdAt / 1000), relativeTo: Date())
        var s = "\(t.isEmpty ? "(images only)" : String(t.prefix(60))) · \(ago)"
        if !e.images.isEmpty { s += " · \(e.images.count) image\(e.images.count == 1 ? "" : "s")" }
        if !e.droppedImages.isEmpty { s += " · dropped \(e.droppedImages.joined(separator: ", "))" }
        return s
    }
}

/// Subscription usage: ring = highest limit, popover = every row (PlanUsageIndicator.vue).
struct PlanUsageMeter: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var x = ComposerExtras.shared
    @State private var open = false

    var body: some View {
        let shown = (x.planUsage?["providers"] as? [JSON] ?? []).filter { $0.s("status") != "not_applicable" }
        Group {
            if !shown.isEmpty {
                let peak = planPeak(x.planUsage)
                Button { open.toggle() } label: {
                    HStack(spacing: 3) {
                        ring(peak.map { $0 / 100 } ?? 0, color(peak))
                        Text(peak.map { "\(Int($0.rounded()))%" } ?? "–").font(.brand(-5)).foregroundStyle(Theme.text2)
                    }
                }
                .buttonStyle(.plain).help("Plan usage")
                .popover(isPresented: $open, arrowEdge: .top) { card(shown).padding(12).frame(width: 260) }
            }
        }
        .onAppear { x.startPolling(b) }
        .onDisappear { x.stopPolling() }
    }

    func color(_ p: Double?) -> Color {
        guard let p else { return Theme.muted }
        return switch usageLevel(p) { case .exceeded: Theme.red; case .warning: Theme.yellow; case .ok: Theme.text2 }
    }

    func ring(_ r: Double, _ c: Color) -> some View {
        ZStack {
            Circle().stroke(Theme.hover, lineWidth: 2)
            Circle().trim(from: 0, to: max(r, 0.02)).stroke(c, style: StrokeStyle(lineWidth: 2, lineCap: .round)).rotationEffect(.degrees(-90))
        }
        .frame(width: 13, height: 13)
    }

    func card(_ providers: [JSON]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(providers.indices, id: \.self) { i in
                let p = providers[i]
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        Text(p.s("kind") == "codex" ? "Codex" : p.s("kind") == "claude" ? "Claude" : p.s("kind")).font(.brand(-3, weight: .semibold))
                        Spacer()
                        Text(p.s("plan").capitalized).font(.ui(-4)).foregroundStyle(Theme.muted)
                    }
                    let rows = p["rows"] as? [JSON] ?? []
                    if p.s("status") != "ok" {
                        Text(p.s("reason").isEmpty ? "Unavailable" : p.s("reason")).font(.ui(-4)).foregroundStyle(Theme.muted)
                    } else if rows.isEmpty {
                        Text("No limits reported").font(.ui(-4)).foregroundStyle(Theme.muted)
                    }
                    ForEach(rows.indices, id: \.self) { j in
                        let r = rows[j], pct = (r["percent"] as? NSNumber)?.doubleValue ?? 0
                        VStack(alignment: .leading, spacing: 3) {
                            HStack {
                                Text(r.s("label")).font(.ui(-4)).foregroundStyle(Theme.muted).lineLimit(1)
                                Spacer()
                                let reset = formatReset(r.s("resets_at"))
                                Text("\(Int(pct.rounded()))%" + (reset.isEmpty ? "" : " · resets \(reset)")).font(.brand(-5))
                            }
                            GeometryReader { g in
                                ZStack(alignment: .leading) {
                                    RoundedRectangle(cornerRadius: 2).fill(Theme.hover)
                                    RoundedRectangle(cornerRadius: 2).fill(color(pct)).frame(width: g.size.width * min(pct, 100) / 100)
                                }
                            }
                            .frame(height: 4)
                        }
                    }
                    if !p.s("extra").isEmpty { Text(p.s("extra")).font(.ui(-4)).foregroundStyle(Theme.muted) }
                }
                if i < providers.count - 1 { Divider() }
            }
        }
        .foregroundStyle(Theme.text)
    }
}

/// Session cost, shown while idle (AgentChat.vue `sessionCost`). Live turns only — Go keeps no running total.
struct CostBadge: View {
    @ObservedObject var x = ComposerExtras.shared
    let chatID: Int64
    let running: Bool

    var body: some View {
        if let c = x.cost[chatID], c > 0, !running {
            Text(String(format: "$%.4f", c)).font(.brand(-5)).foregroundStyle(Theme.muted).padding(.horizontal, 2)
                .help("Cost of the turns this window has seen")
        }
    }
}

/// The chat's one-line recap (chats.recap, generated by Go after a turn — there is no command to ask for one).
struct RecapButton: View {
    @EnvironmentObject var b: Burrow
    let chatID: Int64
    @State private var open = false

    var body: some View {
        let recap = b.chat(chatID)?.s("recap") ?? ""
        if !recap.isEmpty {
            Button { open.toggle() } label: { Image(systemName: "text.quote").foregroundStyle(Theme.text2) }
                .buttonStyle(.plain).help(recap)
                .popover(isPresented: $open, arrowEdge: .top) {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("Where this chat is at").font(.brand(-3, weight: .semibold))
                        Text(recap).font(.ui(-1)).textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                    }
                    .foregroundStyle(Theme.text).padding(12).frame(width: 300, alignment: .leading)
                }
        }
    }
}

/// Context card body (AgentChat.vue ctx HoverCard): tokens, the CLI's /context breakdown, Breakdown / Compact.
struct ContextCard: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var x = ComposerExtras.shared
    let chatID: Int64
    let tokens: Int64
    let window: Int64
    let running: Bool

    var body: some View {
        let t = b.chat(chatID)?.s("transport") ?? ""
        let canCompact = t.isEmpty || t == "claude-cli"
        let pending = x.ctxPending.contains(chatID)
        let r = min(Double(tokens) / Double(max(window, 1)), 1)
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("Context").font(.brand(-3, weight: .semibold))
                Spacer()
                Text("\(Int(r * 100))%").font(.brand(-3))
            }
            Text("\(tokens.formatted()) / \(window.formatted()) tokens").font(.brand(-5)).foregroundStyle(Theme.muted)
            ProgressView(value: r).tint(r > 0.85 ? Theme.red : r > 0.6 ? Theme.yellow : Theme.accent)
            if let rows = x.ctxReport[chatID] {
                ForEach(rows.indices, id: \.self) { i in
                    HStack {
                        Text(rows[i].label).font(.ui(-4)).foregroundStyle(Theme.muted).lineLimit(1)
                        Spacer()
                        Text("\(rows[i].tokens) ").font(.brand(-5)) + Text(rows[i].pct).font(.brand(-5)).foregroundColor(Theme.muted)
                    }
                }
            }
            HStack(spacing: 6) {
                Button(pending ? "Loading…" : x.ctxReport[chatID] == nil ? "Breakdown" : "Refresh") {
                    Task { await x.requestContextReport(b, chatID) }
                }
                .disabled(!canCompact || running || pending)
                Button("Compact") { Task { try? await b.call("chat_send", ["chatId": chatID, "text": "/compact", "images": [String]()]) } }
                    .disabled(!canCompact || running)
            }
            .controlSize(.small)
            if x.ctxReport[chatID] == nil {
                Text("Breakdown asks the CLI's /context — local, no tokens.").font(.ui(-5)).foregroundStyle(Theme.muted)
            }
        }
        .foregroundStyle(Theme.text).padding(12).frame(width: 248, alignment: .leading)
    }
}

/// Full text of a collapsed paste (PasteTextDialog.vue).
struct PasteTextSheet: View {
    let text: String
    let close: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Pasted text").font(.brand(-2)).foregroundStyle(Theme.text2)
                Spacer()
                Button(action: close) { Image(systemName: "xmark") }.buttonStyle(.plain).keyboardShortcut(.cancelAction)
            }
            .padding(10)
            Divider()
            ScrollView {
                Text(text).font(.brand(-2)).foregroundStyle(Theme.text).textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(12)
            }
        }
        .frame(minWidth: 560, idealWidth: 760, minHeight: 320, idealHeight: 560)
        .background(Theme.panel)
    }
}

// MARK: - paste chip in the NSTextView

/// An attachment that stands for a collapsed paste; the model string keeps the text between markers.
final class PasteAttachment: NSTextAttachment {
    let pasted: String
    init(_ text: String, font: NSFont) {
        pasted = text
        super.init(data: nil, ofType: nil)
        let label = "Pasted text (\(text.components(separatedBy: "\n").count) lines)" as NSString
        let f = NSFont.systemFont(ofSize: font.pointSize - 2, weight: .medium)
        let attrs: [NSAttributedString.Key: Any] = [.font: f, .foregroundColor: NSColor(Theme.accent)]
        let icon = NSImage(systemSymbolName: "doc.on.clipboard", accessibilityDescription: nil)?
            .withSymbolConfiguration(.init(pointSize: f.pointSize, weight: .medium))
        let ts = label.size(withAttributes: attrs)
        let size = NSSize(width: ts.width + 30, height: ceil(ts.height) + 4)
        image = NSImage(size: size, flipped: false) { r in
            NSColor(Theme.hover).setFill()
            NSBezierPath(roundedRect: r.insetBy(dx: 0.5, dy: 0.5), xRadius: 6, yRadius: 6).fill()
            if let icon {
                let tinted = icon.copy() as! NSImage
                tinted.lockFocus(); NSColor(Theme.accent).set(); NSRect(origin: .zero, size: tinted.size).fill(using: .sourceAtop); tinted.unlockFocus()
                tinted.draw(in: NSRect(x: 6, y: (r.height - tinted.size.height) / 2, width: tinted.size.width, height: tinted.size.height))
            }
            label.draw(at: NSPoint(x: 24, y: (r.height - ts.height) / 2), withAttributes: attrs)
            return true
        }
        bounds = NSRect(x: 0, y: font.descender, width: size.width, height: size.height)
    }
    required init?(coder: NSCoder) { fatalError("not coded") }
}

/// Model string (with paste markers) → what the text view shows (chips for pastes).
func composerDisplay(_ model: String, font: NSFont) -> NSAttributedString {
    let attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: NSColor(Theme.text)]
    let out = NSMutableAttributedString()
    for seg in pasteSegments(model) {
        if seg.paste {
            let a = NSMutableAttributedString(attachment: PasteAttachment(seg.text, font: font))
            a.addAttributes(attrs, range: NSRange(location: 0, length: a.length))
            out.append(a)
        } else {
            out.append(NSAttributedString(string: seg.text, attributes: attrs))
        }
    }
    return out
}

/// What the text view shows → model string (each paste chip expanded back between markers).
func composerModel(_ s: NSAttributedString) -> String {
    var out = ""
    let ns = s.string as NSString
    s.enumerateAttribute(.attachment, in: NSRange(location: 0, length: s.length)) { v, r, _ in
        if let p = v as? PasteAttachment { out += wrapPaste(p.pasted) } else { out += ns.substring(with: r) }
    }
    return out
}
