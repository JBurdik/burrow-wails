import SwiftUI

// MARK: feed — the desktop's agent tabs (src/lib/threadFeed.ts tab rows)

struct TermTab: Identifiable, Hashable {
    var id: Int            // pty id
    var workspaceId: Int
    var title: String
}

struct PtyPerm: Identifiable, Hashable {
    var id: String
    var ptyId: Int
    var tool: String
    var target: String

    init(_ d: [String: Any]) {
        id = d["id"] as? String ?? ""
        ptyId = Int(d["pty_id"] as? String ?? "") ?? 0
        tool = d["tool_name"] as? String ?? ""
        // permTarget in threadFeed.ts
        let i = d["tool_input"] as? [String: Any] ?? [:]
        let v = ["command", "file_path", "path", "url", "pattern"].lazy.compactMap { i[$0] }.first
        target = String(String(describing: v ?? "").prefix(200))
    }
}

/// Lives as long as ThreadsView: tabs + `pty:<id>` phases from shell_snapshot,
/// kept live by phase-pty:<id>, pty-permissions and seen-changed.
@MainActor @Observable
final class TerminalFeed {
    var tabs: [TermTab] = []
    var phases: [Int: Phase] = [:]
    var agents: Set<Int> = []
    var seen: [Int: Double] = [:]   // receipts keyed by the bare pty id, like Terminal.vue
    var perms: [PtyPerm] = []

    @ObservationIgnored weak var model: AppModel?
    @ObservationIgnored private var token: UUID?
    @ObservationIgnored private var loading = false

    func attach(_ model: AppModel) {
        guard token == nil else { return }
        self.model = model
        token = model.subscribe { [weak self] name, payload in
            MainActor.assumeIsolated { self?.event(name, payload) }
        }
    }

    deinit { let (m, t) = (model, token); DispatchQueue.main.async { m?.unsubscribe(t) } }

    /// Agent tabs, newest phase first; a tab is listed once its phase says is_agent.
    var rows: [TermTab] {
        tabs.filter { agents.contains($0.id) }
            .sorted { (phases[$0.id]?.updatedAt ?? 0) > (phases[$1.id]?.updatedAt ?? 0) }
    }

    func phase(_ id: Int) -> Phase { phases[id] ?? Phase() }
    func perms(for id: Int) -> [PtyPerm] { perms.filter { $0.ptyId == id } }

    /// AppModel.status with the PTY's receipt.
    func status(_ id: Int) -> Status {
        let p = phase(id)
        let unseen = p.turnEndedAt > (seen[id] ?? 0)
        switch p.state {
        case "running": return .running
        case "waiting_input": return .waiting
        case "waiting_approval": return .permission
        case "failed": return unseen ? .error : .idle
        case "done": return unseen ? .unread : .idle
        default: return .idle
        }
    }

    func markSeen(_ id: Int) {
        let p = phase(id)
        guard !p.inFlight, p.turnEndedAt > (seen[id] ?? 0) else { return }
        seen[id] = p.turnEndedAt
        Task { _ = try? await model?.client?.call("seen_mark", ["key": String(id)]) }
    }

    func load() async {
        guard !loading, let client = model?.client,
              let snap = try? await client.call("shell_snapshot") as? [String: Any] else { return }
        loading = true; defer { loading = false }
        var t: [TermTab] = []
        for (ws, list) in snap["tabs"] as? [String: Any] ?? [:] {
            for r in list as? [[String: Any]] ?? [] {
                let pty = int(r["pty_id"]); guard pty > 0 else { continue }
                let title = [r["title"], r["default_title"]].compactMap { $0 as? String }.first { !$0.isEmpty } ?? "Terminal"
                t.append(TermTab(id: pty, workspaceId: Int(ws) ?? 0, title: title))
            }
        }
        var ph: [Int: Phase] = [:], ag: Set<Int> = []
        for (k, v) in snap["phases"] as? [String: Any] ?? [:] where k.hasPrefix("pty:") {
            guard let id = Int(k.dropFirst(4)), let d = v as? [String: Any] else { continue }
            ph[id] = Phase(d)
            if d["is_agent"] as? Bool == true { ag.insert(id) }
        }
        tabs = t; phases = ph; agents = ag
        if let list = try? await client.call("seen_list") as? [[String: Any]] {
            var s: [Int: Double] = [:]
            for r in list { if let k = r["key"] as? String, let id = Int(k) { s[id] = dbl(r["at"]) } }
            seen = s
        }
        await loadPerms()
    }

    func loadPerms() async {
        guard let list = try? await model?.client?.call("list_pty_permissions") as? [[String: Any]] else { return }
        perms = list.map(PtyPerm.init)
    }

    func answer(_ p: PtyPerm, _ behavior: String) {
        perms.removeAll { $0.id == p.id }
        Task {
            _ = try? await model?.client?.call("answer_pty_permission", ["id": p.id, "behavior": behavior, "message": ""])
            await loadPerms()
        }
    }

    private func event(_ name: String, _ payload: Any?) {
        switch name {
        case "open", "resync": Task { await load() }
        case "pty-permissions": Task { await loadPerms() }
        case "seen-changed":
            if let d = payload as? [String: Any], let k = d["key"] as? String, let id = Int(k) {
                let at = dbl(d["at"]); seen[id] = at == 0 ? 0 : max(seen[id] ?? 0, at)
            }
        default:
            guard name.hasPrefix("phase-pty:"), let id = Int(name.dropFirst(10)), let d = payload as? [String: Any] else { return }
            phases[id] = Phase(d)
            if d["is_agent"] as? Bool == true { agents.insert(id) } else { agents.remove(id) }
            if !tabs.contains(where: { $0.id == id }) { Task { await load() } } // a tab we haven't seen yet
        }
    }
}

// MARK: list section

struct TerminalsSection: View {
    @Environment(AppModel.self) private var model
    @State private var feed = TerminalFeed()

    var body: some View {
        let rows = feed.rows
        Group {
            if !rows.isEmpty {
                Section {
                    ForEach(rows) { tab in
                        NavigationLink(destination: TerminalScreen(tab: tab, feed: feed)) { TermRow(tab: tab, feed: feed, model: model) }
                    }
                } header: {
                    HStack(spacing: 6) {
                        Image(systemName: "apple.terminal")
                        Text("Terminals")
                        Text("\(rows.count)").foregroundStyle(.secondary)
                    }
                }
            }
        }
        .task {
            feed.attach(model)
            // ponytail: a 15 s re-read for what has no event (a tab opened or
            // renamed), as threadFeed.ts does.
            while !Task.isCancelled {
                await feed.load()
                try? await Task.sleep(for: .seconds(15))
            }
        }
    }
}

private struct TermRow: View {
    let tab: TermTab
    let feed: TerminalFeed
    let model: AppModel

    var body: some View {
        let status = feed.status(tab.id)
        let phase = feed.phase(tab.id)
        let asks = feed.perms(for: tab.id).count
        HStack(alignment: .top, spacing: 10) {
            StatusMark(status: status).frame(width: 16, height: 18)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(tab.title)
                        .font(.body.weight(status == .unread || status.needsYou ? .semibold : .regular))
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if asks > 0 {
                        Label("\(asks)", systemImage: "lock.fill")
                            .font(.caption2.bold()).foregroundStyle(.white)
                            .padding(.horizontal, 5).padding(.vertical, 1)
                            .background(.orange, in: RoundedRectangle(cornerRadius: 4))
                            .accessibilityLabel("\(asks) permission requests")
                    }
                    Text(Date(timeIntervalSince1970: phase.updatedAt / 1000), format: .relative(presentation: .numeric, unitsStyle: .narrow))
                        .font(.caption2).foregroundStyle(.secondary)
                }
                Text(subtitle(status, phase)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        }
        .padding(.vertical, 2)
    }

    private func subtitle(_ s: Status, _ p: Phase) -> String {
        let ws = model.workspace(tab.workspaceId)?.name ?? ""
        let detail = s == .permission || s == .waiting ? p.detail : ""
        return [ws, detail].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

// MARK: the tab screen

/// One PTY mirrored as text: pty_attach (or the ring via pty_since) to start,
/// pty-data-<id> {off,data} live, pty_since to fill holes — src/runtime/ptyStream.ts.
/// Never resizes the PTY: the grid is the desktop's.
@MainActor @Observable
final class TermSession {
    let id: Int
    var lines: [String] = []
    var exited = false
    var version = 0

    @ObservationIgnored let term = TextTerm()
    @ObservationIgnored weak var model: AppModel?
    @ObservationIgnored private var token: UUID?
    @ObservationIgnored private var next: Int?
    @ObservationIgnored private var catching = false
    @ObservationIgnored private var held: [(off: Int?, data: [UInt8])] = []
    @ObservationIgnored private var renderQueued = false

    init(id: Int) { self.id = id }

    func start(_ model: AppModel) {
        guard token == nil else { return }
        self.model = model
        token = model.subscribe { [weak self] name, payload in
            MainActor.assumeIsolated { self?.event(name, payload) }
        }
        Task { await attach() }
    }

    func stop() { model?.unsubscribe(token); token = nil }

    private var client: BurrowClient? { model?.client }

    private func event(_ name: String, _ payload: Any?) {
        switch name {
        case "pty-data-\(id)":
            guard let d = payload as? [String: Any] else { return }
            let bytes = ptyBytes(d["data"])
            push((d["off"] as? NSNumber)?.intValue, bytes)
        case "open": Task { if next == nil { await attach() } else { await catchUp() } }
        case "pty-resync-\(id)": Task { await attach() }
        case "pty-exit-\(id)": exited = true
        default: break
        }
    }

    private func push(_ off: Int?, _ data: [UInt8]) {
        if catching { held.append((off, data)) } else { apply(off, data) }
    }

    private func apply(_ off: Int?, _ data: [UInt8]) {
        guard let off else { write(data); return } // old daemon: no offsets
        if next == nil { next = off }
        if off > next! { held.append((off, data)); Task { await catchUp() }; return }
        let skip = next! - off
        guard skip < data.count else { return }
        write(skip > 0 ? Array(data[skip...]) : data)
        next = off + data.count
    }

    private func drain() {
        catching = false
        let h = held; held = []
        for c in h { push(c.off, c.data) }
    }

    private func catchUp() async {
        guard !catching, let from = next, let client else { return }
        catching = true
        if let got = try? await client.call("pty_since", ["id": String(id), "since": from]) as? [String: Any],
           let off = (got["off"] as? NSNumber)?.intValue {
            let data = ptyBytes(got["data"])
            if off > from || (off < from && data.isEmpty) { next = off }
            apply(off, data)
        } else {
            next = nil
        }
        drain()
    }

    /// Screen + history from the daemon's emulator, else whatever the byte ring
    /// still holds. Live chunks are held meanwhile and continue at the offset.
    private func attach() async {
        guard !catching, let client else { return }
        catching = true
        if let g = try? await client.call("pty_size", ["id": String(id)]) as? [String: Any] {
            term.resize(cols: int(g["cols"]), rows: int(g["rows"]))
        }
        if let snap = try? await client.call("pty_attach", ["id": String(id)]) as? [String: Any],
           let off = (snap["off"] as? NSNumber)?.intValue {
            term.resize(cols: int(snap["cols"]), rows: int(snap["rows"]))
            term.reset()
            if let b64 = snap["vt"] as? String, let vt = Data(base64Encoded: b64) { write([UInt8](vt)) }
            next = off
        } else if let ring = try? await client.call("pty_since", ["id": String(id), "since": 0]) as? [String: Any],
                  let off = (ring["off"] as? NSNumber)?.intValue {
            let data = ptyBytes(ring["data"])
            term.reset()
            write(data)
            next = off + data.count
        }
        drain()
    }

    private func write(_ bytes: [UInt8]) {
        term.feed(bytes)
        guard !renderQueued else { return }
        renderQueued = true
        // ponytail: coalesce redraws to ~10/s; a streaming TUI writes far more often.
        Task {
            try? await Task.sleep(for: .milliseconds(100))
            renderQueued = false
            lines = term.render()
            version += 1
        }
    }

    // MARK: input

    func send(_ bytes: [UInt8]) {
        Task { _ = try? await client?.call("write_pty", ["id": String(id), "data": bytes.map(Int.init)]) }
    }

    /// Typed text as send_to_tab does: a bracketed paste when it has newlines,
    /// Enter as its own write so it submits once.
    func submit(_ text: String) {
        Task {
            if !text.isEmpty {
                let body = text.contains("\n") ? "\u{1b}[200~" + text + "\u{1b}[201~" : text
                _ = try? await client?.call("write_pty", ["id": String(id), "data": Array(body.utf8).map(Int.init)])
                try? await Task.sleep(for: .milliseconds(150))
            }
            _ = try? await client?.call("write_pty", ["id": String(id), "data": [13]])
        }
    }
}

struct TerminalScreen: View {
    let tab: TermTab
    let feed: TerminalFeed
    @Environment(AppModel.self) private var model
    @State private var session: TermSession
    @State private var input = ""
    @State private var follow = true
    @AppStorage("term.wrap") private var wrap = false
    @FocusState private var focused: Bool

    init(tab: TermTab, feed: TerminalFeed) {
        self.tab = tab; self.feed = feed
        _session = State(initialValue: TermSession(id: tab.id))
    }

    var body: some View {
        VStack(spacing: 0) {
            if let p = feed.perms(for: tab.id).first {
                PermBanner(perm: p, more: feed.perms(for: tab.id).count - 1) { feed.answer(p, $0) }
            }
            if session.exited {
                Label("This terminal has exited.", systemImage: "xmark.octagon")
                    .font(.footnote).foregroundStyle(.secondary).padding(8)
            }
            screen
            Divider()
            keys
            composer
        }
        .navigationTitle(tab.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                VStack(spacing: 0) {
                    Text(tab.title).font(.headline).lineLimit(1)
                    let p = feed.phase(tab.id)
                    Text(p.detail.isEmpty ? p.state.replacingOccurrences(of: "_", with: " ") : p.detail)
                        .font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Toggle("Follow output", systemImage: "arrow.down.to.line", isOn: $follow)
                    Toggle("Wrap lines", systemImage: "text.word.spacing", isOn: $wrap)
                } label: { Image(systemName: "ellipsis.circle") }
            }
        }
        .onAppear { session.start(model); Task { await feed.loadPerms() } }
        .onDisappear { session.stop() }
        .onChange(of: feed.phase(tab.id)) { feed.markSeen(tab.id) }
        .task { feed.markSeen(tab.id) }
    }

    private var screen: some View {
        ScrollViewReader { proxy in
            ScrollView(wrap ? .vertical : [.vertical, .horizontal]) {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(0..<session.lines.count, id: \.self) { i in
                        let line = session.lines[i]
                        Text(line.isEmpty ? " " : line)
                            .fixedSize(horizontal: !wrap, vertical: true)
                    }
                    Color.clear.frame(height: 1).id("end")
                }
                .font(.system(.caption, design: .monospaced))
                .textSelection(.enabled)
                .padding(8)
            }
            .defaultScrollAnchor(.bottom)
            .background(Color(.secondarySystemBackground))
            .onChange(of: session.version) { if follow { proxy.scrollTo("end", anchor: .bottom) } }
            .onChange(of: follow) { if follow { proxy.scrollTo("end", anchor: .bottom) } }
            .overlay { if session.lines.isEmpty && !session.exited { ProgressView() } }
        }
    }

    private var keys: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                key("esc", "Escape", [0x1b])
                key("^C", "Control C", [0x03])
                key("tab", "Tab", [0x09])
                key("⇧tab", "Shift Tab", Array("\u{1b}[Z".utf8))
                key(icon: "arrow.up", "Up", Array("\u{1b}[A".utf8))
                key(icon: "arrow.down", "Down", Array("\u{1b}[B".utf8))
                key(icon: "arrow.left", "Left", Array("\u{1b}[D".utf8))
                key(icon: "arrow.right", "Right", Array("\u{1b}[C".utf8))
                key(icon: "return", "Return", [0x0d])
            }
            .padding(.horizontal, 8).padding(.vertical, 6)
        }
        .disabled(session.exited)
    }

    private func key(_ title: String? = nil, icon: String? = nil, _ label: String, _ bytes: [UInt8]) -> some View {
        Button { session.send(bytes) } label: {
            Group {
                if let icon { Image(systemName: icon) } else { Text(title ?? "") }
            }
            .font(.system(.footnote, design: .monospaced).weight(.medium))
            .frame(minWidth: 28)
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.roundedRectangle)
        .accessibilityLabel(label)
    }

    private var composer: some View {
        HStack(spacing: 8) {
            TextField("Type into the terminal", text: $input, axis: .vertical)
                .lineLimit(1...5)
                .font(.system(.body, design: .monospaced))
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .focused($focused)
                .padding(8)
                .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
                .onSubmit(submit)
            Button(action: submit) { Image(systemName: "arrow.up.circle.fill").font(.title2) }
                .accessibilityLabel("Send with Enter")
        }
        .padding(.horizontal, 8).padding(.bottom, 8)
        .disabled(session.exited)
    }

    private func submit() {
        session.submit(input)
        input = ""
    }
}

private struct PermBanner: View {
    let perm: PtyPerm
    let more: Int
    let answer: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Label("Allow \(perm.tool)?", systemImage: "lock.fill").font(.subheadline.weight(.semibold))
                Spacer()
                if more > 0 { Text("+\(more)").font(.caption.bold()).foregroundStyle(.secondary) }
            }
            if !perm.target.isEmpty {
                Text(perm.target).font(.system(.caption, design: .monospaced)).lineLimit(4)
            }
            HStack {
                Button("Deny", role: .destructive) { answer("deny") }.buttonStyle(.bordered)
                Spacer()
                Button("Always") { answer("always") }.buttonStyle(.bordered)
                Button("Allow") { answer("allow") }.buttonStyle(.borderedProminent)
            }
            .buttonBorderShape(.roundedRectangle)
            .controlSize(.small)
        }
        .padding(12)
        .background(Color.orange.opacity(0.15))
    }
}

// MARK: text terminal

/// A tiny VT text model: enough cursor movement and erasing that a redrawing
/// TUI (Claude Code's input box, spinners) reads as its screen, not as every
/// frame appended. Colours/attributes are dropped.
/// ponytail: no scroll regions, no wide-char widths, no insert/delete line;
/// a TUI using those mis-renders a line or two. Swap for a real emulator then.
final class TextTerm {
    private(set) var cols = 200, rows = 50
    private var lines: [[Character]] = [[]]
    private var top = 0, row = 0, col = 0
    private var savedCursor = (0, 0)
    private var mainScreen: (lines: [[Character]], top: Int, row: Int, col: Int)?
    private var st = 0          // 0 ground, 1 esc, 2 csi, 3 osc/dcs string, 4 string-esc, 5 charset
    private var params = ""
    private var pending: [UInt8] = []
    private var rendered: [String] = []
    private var dirtyFrom = 0
    private static let maxLines = 4000

    func resize(cols c: Int, rows r: Int) { if c > 0 { cols = c }; if r > 0 { rows = r } }

    func reset() {
        lines = [[]]; top = 0; row = 0; col = 0; mainScreen = nil; st = 0; pending = []; rendered = []; dirtyFrom = 0
    }

    func render() -> [String] {
        let from = min(dirtyFrom, rendered.count, lines.count)
        rendered = Array(rendered.prefix(from)) + lines[from...].map { String($0) }
        dirtyFrom = top
        return rendered
    }

    func feed(_ bytes: [UInt8]) {
        pending += bytes
        let n = Self.completeUTF8(pending)
        let s = String(decoding: pending[..<n], as: UTF8.self)
        pending = Array(pending[n...])
        dirtyFrom = min(dirtyFrom, top)
        for ch in s { step(ch) }
        if lines.count > Self.maxLines {
            let cut = lines.count - Self.maxLines
            lines.removeFirst(cut); top = max(0, top - cut); row = max(0, row - cut); dirtyFrom = 0
        }
    }

    /// Length of the prefix that ends on a whole UTF-8 sequence.
    static func completeUTF8(_ b: [UInt8]) -> Int {
        var i = b.count - 1
        while i >= 0 && b.count - i <= 4 {
            let c = b[i]
            if c & 0xC0 != 0x80 {
                let need = c >= 0xF0 ? 4 : c >= 0xE0 ? 3 : c >= 0xC0 ? 2 : 1
                return b.count - i >= need ? b.count : i
            }
            i -= 1
        }
        return b.count
    }

    private func step(_ ch: Character) {
        switch st {
        case 1: escape(ch)
        case 2:
            guard let a = ch.asciiValue else { st = 0; return }
            if a >= 0x40 && a <= 0x7E { st = 0; csi(ch, params) } else { params.append(ch) }
        case 3: if ch == "\u{07}" { st = 0 } else if ch == "\u{1b}" { st = 4 }
        case 4: st = ch == "\\" ? 0 : 3
        case 5: st = 0
        default: ground(ch)
        }
    }

    private func ground(_ ch: Character) {
        switch ch {
        case "\u{1b}": st = 1
        case "\r": col = 0
        case "\n", "\u{0b}", "\u{0c}": lineFeed()
        case "\r\n": col = 0; lineFeed()
        case "\u{08}": col = max(0, col - 1)
        case "\t": col = min(cols - 1, (col / 8 + 1) * 8)
        case "\u{07}", "\u{0e}", "\u{0f}", "\u{00}", "\u{7f}": break
        default:
            if let a = ch.asciiValue, a < 0x20 { return }
            if col >= cols { col = 0; lineFeed() }
            put(ch); col += 1
        }
    }

    private func escape(_ ch: Character) {
        st = 0
        switch ch {
        case "[": st = 2; params = ""
        case "]", "P", "_", "^", "X": st = 3
        case "(", ")", "*", "+", "#", "%": st = 5
        case "7": savedCursor = (row - top, col)
        case "8": row = top + savedCursor.0; col = savedCursor.1; ensure(row)
        case "D": lineFeed()
        case "E": col = 0; lineFeed()
        case "M": if row > top { row -= 1 }
        case "c": reset()
        default: break
        }
    }

    private func csi(_ final: Character, _ p: String) {
        let priv = p.hasPrefix("?")
        let nums = p.trimmingCharacters(in: CharacterSet(charactersIn: "?>=<")).split(separator: ";", omittingEmptySubsequences: false).map { Int($0) ?? 0 }
        func n(_ i: Int, _ def: Int = 1) -> Int { i < nums.count && nums[i] > 0 ? nums[i] : def }
        switch final {
        case "A": row = max(top, row - n(0))
        case "B", "e": row = min(top + rows - 1, row + n(0)); ensure(row)
        case "C", "a": col = min(cols - 1, col + n(0))
        case "D": col = max(0, col - n(0))
        case "E": row = min(top + rows - 1, row + n(0)); col = 0; ensure(row)
        case "F": row = max(top, row - n(0)); col = 0
        case "G", "`": col = min(cols - 1, n(0) - 1)
        case "d": row = top + min(rows, n(0)) - 1; ensure(row)
        case "H", "f": row = top + min(rows, n(0)) - 1; col = min(cols - 1, n(1) - 1); ensure(row)
        case "J":
            switch nums.first ?? 0 {
            case 0: truncate(row, at: col); for i in (row + 1)..<max(row + 1, lines.count) { lines[i] = [] }
            case 1: for i in top..<row { lines[i] = [] }; blank(row, upTo: col)
            case 2: for i in top..<lines.count { lines[i] = [] }
            default: // 3: scrollback gone
                lines.removeFirst(top); row -= top; top = 0; dirtyFrom = 0
            }
        case "K":
            switch nums.first ?? 0 {
            case 0: truncate(row, at: col)
            case 1: blank(row, upTo: col)
            default: lines[row] = []
            }
        case "X": // erase n chars
            if col < lines[row].count { for c in col..<min(col + n(0), lines[row].count) { lines[row][c] = " " } }
        case "P": // delete n chars
            if col < lines[row].count { lines[row].removeSubrange(col..<min(lines[row].count, col + n(0))) }
        case "@": // insert n blanks
            if col < lines[row].count { lines[row].insert(contentsOf: repeatElement(" ", count: n(0)), at: col) }
        case "s": savedCursor = (row - top, col)
        case "u": row = top + savedCursor.0; col = savedCursor.1; ensure(row)
        case "h", "l":
            guard priv, nums.contains(where: { [47, 1047, 1049].contains($0) }) else { return }
            if final == "h", mainScreen == nil {
                mainScreen = (lines, top, row, col)
                lines = [[]]; top = 0; row = 0; col = 0; dirtyFrom = 0
            } else if final == "l", let m = mainScreen {
                (lines, top, row, col) = m; mainScreen = nil; dirtyFrom = 0
            }
        default: break // m (colours), r (scroll region), t, n, …
        }
    }

    private func lineFeed() {
        row += 1
        ensure(row)
        if row - top >= rows { top = row - rows + 1 }
    }

    private func ensure(_ r: Int) { while lines.count <= r { lines.append([]) } }

    private func put(_ ch: Character) {
        ensure(row)
        if lines[row].count < col { lines[row] += repeatElement(" ", count: col - lines[row].count) }
        if col < lines[row].count { lines[row][col] = ch } else { lines[row].append(ch) }
    }

    private func truncate(_ r: Int, at c: Int) { if c < lines[r].count { lines[r].removeSubrange(c...) } }

    private func blank(_ r: Int, upTo c: Int) {
        for i in 0..<min(c + 1, lines[r].count) { lines[r][i] = " " }
    }
}

/// pty-data / pty_since bytes: base64 since protocol rev 2, a number array
/// from an older core.
func ptyBytes(_ v: Any?) -> [UInt8] {
    if let s = v as? String { return [UInt8](Data(base64Encoded: s) ?? Data()) }
    return (v as? [NSNumber] ?? []).map { UInt8(truncatingIfNeeded: $0.intValue) }
}
