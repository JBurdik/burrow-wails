import AppKit
import CGhosttyVT
import SwiftUI

// MARK: - Split tree (TerminalSplitView.vue)

/// A tab's panes. Like the Vue desktop the tree lives in memory only: every leaf is its own terminal_tabs
/// row, so after a restart (or in the desktop window) a split comes back as separate tabs.
indirect enum SplitNode: Equatable {
    case leaf(Int64)
    /// `h` = side by side (⌘D), else stacked (⌘⇧D); `ratio` = share of `a`.
    case split(h: Bool, ratio: Double, a: SplitNode, b: SplitNode)

    var leaves: [Int64] {
        switch self {
        case .leaf(let id): [id]
        case .split(_, _, let a, let b): a.leaves + b.leaves
        }
    }

    /// `target` becomes a split of itself and `new` (new goes second, like insertSplit).
    func inserting(_ new: Int64, at target: Int64, h: Bool) -> SplitNode {
        switch self {
        case .leaf(let id): id == target ? .split(h: h, ratio: 0.5, a: self, b: .leaf(new)) : self
        case .split(let d, let r, let a, let b): .split(h: d, ratio: r, a: a.inserting(new, at: target, h: h), b: b.inserting(new, at: target, h: h))
        }
    }

    /// nil when the last leaf goes; a split left with one child collapses into it.
    func removing(_ id: Int64) -> SplitNode? {
        switch self {
        case .leaf(let l): return l == id ? nil : self
        case .split(let d, let r, let a, let b):
            switch (a.removing(id), b.removing(id)) {
            case (nil, nil): return nil
            case (let x?, nil): return x
            case (nil, let y?): return y
            case (let x?, let y?): return .split(h: d, ratio: r, a: x, b: y)
            }
        }
    }

    /// path: false = `a`, true = `b`, down to the split whose ratio changes.
    func withRatio(_ ratio: Double, at path: [Bool]) -> SplitNode {
        guard case .split(let d, let r, let a, let b) = self else { return self }
        guard let first = path.first else { return .split(h: d, ratio: min(0.9, max(0.1, ratio)), a: a, b: b) }
        let rest = Array(path.dropFirst())
        return first ? .split(h: d, ratio: r, a: a, b: b.withRatio(ratio, at: rest))
            : .split(h: d, ratio: r, a: a.withRatio(ratio, at: rest), b: b)
    }

    static func selfCheck() {
        let t = SplitNode.leaf(1).inserting(2, at: 1, h: true).inserting(3, at: 2, h: false)
        assert(t.leaves == [1, 2, 3])
        assert(t.removing(2)?.leaves == [1, 3])
        assert(t.removing(1) == .split(h: false, ratio: 0.5, a: .leaf(2), b: .leaf(3)))
        assert(SplitNode.leaf(1).removing(1) == nil)
        if case .split(_, _, _, .split(_, let r, _, _)) = t.withRatio(0.3, at: [true]) { assert(r == 0.3) } else { assert(false) }
    }
}

/// Foreground-poll state per PTY (XTerm.vue's lastProcess / isAgentSession / agentTitled).
struct TitleState {
    var lastProc = ""
    var agentSession = false
    var agentTitled = false
    var lastOsc = ""

    static let shell = try! NSRegularExpression(pattern: "^-?(zsh|bash|sh|fish|csh|tcsh|dash)$")
    static func isShell(_ p: String) -> Bool { shell.firstMatch(in: p, range: NSRange(p.startIndex..., in: p)) != nil }
    static func isAgent(_ p: String) -> Bool { ["claude", "codex", "copilot"].contains(p.lowercased()) }

    /// One poll: the title to show, or nil to keep the current one. Names are sticky — an empty read or
    /// the shell coming back never resets them.
    mutating func next(proc: String, osc rawOsc: String, cwd: String) -> String? {
        let osc = String(rawOsc.unicodeScalars.filter { $0.value >= 0x20 && $0.value != 0x7F }).trimmingCharacters(in: .whitespaces).prefix(80)
        let oscChanged = osc != lastOsc
        lastOsc = String(osc)
        guard !proc.isEmpty else { return nil }
        var out: String?
        if Self.isShell(proc) {
            agentSession = false
            return nil // shell precmd/preexec titles are cwd noise
        } else if Self.isAgent(proc) {
            agentSession = true
            if proc != lastProc, !agentTitled { out = proc.lowercased() == "claude" ? "Claude" : proc }
        } else if !agentSession, proc != lastProc {
            out = proc // plain command: vim, python3, node
        }
        let first = proc != lastProc
        lastProc = proc
        // The program's own OSC 0/2 title, when it changed. On the first sight of a program a bare word / path
        // is still the shell's preexec title, not the program's.
        if !osc.isEmpty, oscChanged {
            let base = (cwd as NSString).lastPathComponent
            let noise = osc == base || osc.hasPrefix("/") || osc.hasPrefix("~") || osc.range(of: #"^[\w.-]+$"#, options: .regularExpression) != nil
            let idle = osc.range(of: #"^✳?\s*Claude\s+Code$"#, options: [.regularExpression, .caseInsensitive]) != nil
            if !(first && noise), !(Self.isAgent(proc) && agentTitled && idle) {
                if Self.isAgent(proc) { agentTitled = true }
                out = String(osc)
            }
        }
        return out
    }

    static func selfCheck() {
        var s = TitleState()
        assert(s.next(proc: "zsh", osc: "~/code", cwd: "/x/code") == nil)
        assert(s.next(proc: "claude", osc: "claude", cwd: "/x/code") == "Claude")
        assert(s.next(proc: "claude", osc: "✳ Fix the bug", cwd: "/x/code") == "✳ Fix the bug")
        assert(s.next(proc: "claude", osc: "✳ Claude Code", cwd: "/x/code") == nil)
        assert(s.next(proc: "git", osc: "✳ Claude Code", cwd: "/x/code") == nil) // child of the agent keeps its name
        assert(s.next(proc: "", osc: "", cwd: "/x/code") == nil)
        assert(s.next(proc: "zsh", osc: "~", cwd: "/x/code") == nil)
        assert(s.next(proc: "vim", osc: "~", cwd: "/x/code") == "vim")
    }
}

/// A terminal leaf someone closed — ⌘⇧T relaunches it as a new tab (closedViews.ts).
struct ClosedTerm {
    let wsID: Int64
    let cmd: String?
    let cwd: String
    let at: Int64
}

// MARK: - Layout store

@MainActor
final class TerminalLayout: ObservableObject {
    static let shared = TerminalLayout()

    /// Split tree per tab, keyed by the tab's pty id (the row the sidebar lists).
    @Published var trees: [Int64: SplitNode] = [:]
    /// Focused leaf per tab.
    @Published var focused: [Int64: Int64] = [:]
    /// Leaves shown in their own window; the main window shows a placeholder (the `frozen` rule: one view per grid).
    @Published var popped: Set<Int64> = []
    /// Last `burrow log` entries per pty (Terminal.vue log strip).
    @Published var logs: [Int64: [(level: String, message: String)]] = [:]
    var closed: [ClosedTerm] = []
    private var windows: [Int64: NSWindow] = [:]
    var titles: [Int64: TitleState] = [:]

    /// Leaves inside some tab other than its key — hidden from the sidebar's tab list.
    var hiddenLeaves: Set<Int64> {
        Set(trees.flatMap { key, t in t.leaves.filter { $0 != key } })
    }

    func tree(_ key: Int64) -> SplitNode { trees[key] ?? .leaf(key) }

    /// The tab a leaf belongs to (itself when unsplit).
    func tabKey(of leaf: Int64) -> Int64 {
        trees.first { $0.value.leaves.contains(leaf) }?.key ?? leaf
    }

    func focusedLeaf(_ key: Int64) -> Int64 {
        let t = tree(key)
        if let f = focused[key], t.leaves.contains(f) { return f }
        return t.leaves.first ?? key
    }

    /// The terminal view that should take keyboard focus when it lands in a window.
    func wantsFocus(_ leaf: Int64) -> Bool {
        let key = tabKey(of: leaf)
        return trees[key] == nil || focusedLeaf(key) == leaf
    }

    func didFocus(_ leaf: Int64) {
        let key = tabKey(of: leaf)
        if trees[key] != nil, focused[key] != leaf { focused[key] = leaf }
    }

    /// A leaf left its tab. Returns the tab key afterwards (changes when the key leaf itself went).
    @discardableResult
    func remove(_ leaf: Int64) -> Int64? {
        closeWindow(leaf)
        titles[leaf] = nil
        logs[leaf] = nil
        let key = tabKey(of: leaf)
        guard let t = trees[key] else { return nil }
        trees[key] = nil
        focused[key] = nil
        guard let rest = t.removing(leaf), let newKey = rest.leaves.first else { return nil }
        if rest.leaves.count > 1 { trees[newKey] = rest }
        focused[newKey] = rest.leaves.first
        return newKey
    }

    func log(_ pty: Int64, _ p: JSON) {
        logs[pty, default: []].append((p.s("level").isEmpty ? "info" : p.s("level"), p.s("message")))
        if logs[pty]!.count > 5 { logs[pty]!.removeFirst() }
    }

    // MARK: pop-out (stores/popouts.ts)

    func popOut(_ leaf: Int64, b: Burrow, title: String) {
        if let w = windows[leaf] { w.makeKeyAndOrderFront(nil); return }
        popped.insert(leaf)
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 760, height: 480), styleMask: [.titled, .closable, .resizable, .miniaturizable], backing: .buffered, defer: false)
        w.isReleasedWhenClosed = false
        w.title = title
        w.contentView = NSHostingView(rootView: TerminalPopoutView(ptyID: leaf).environmentObject(b))
        // uiPrefs.floatPopouts: no zoom button, so tiling WMs float it.
        if ConfigStore.shared.ui["floatPopouts"] as? Bool == true { w.standardWindowButton(.zoomButton)?.isEnabled = false }
        w.center()
        windows[leaf] = w
        NotificationCenter.default.addObserver(forName: NSWindow.willCloseNotification, object: w, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.windows[leaf] = nil
                self?.popped.remove(leaf)
            }
        }
        w.makeKeyAndOrderFront(nil)
    }

    func closeWindow(_ leaf: Int64) { windows[leaf]?.close() }
}

/// Body of a popped-out terminal window. It owns the PTY's grid while the main window shows a placeholder.
struct TerminalPopoutView: View {
    @EnvironmentObject var b: Burrow
    let ptyID: Int64

    var body: some View {
        let tab = TerminalStore.shared.tab(pty: ptyID) ?? [:]
        VStack(spacing: 0) {
            PtyPermissionBanner(ptyID: ptyID)
            SecretRequestBanner(ownerKey: "pty:\(ptyID)")
            TerminalPane(id: String(ptyID), cwd: tab.s("cwd").isEmpty ? NSHomeDirectory() : tab.s("cwd"))
        }
        .background(Theme.terminal)
        .tint(Theme.accent)
        .preferredColorScheme(Theme.isDark ? .dark : .light)
        .frame(minWidth: 320, minHeight: 200)
    }
}

// MARK: - Split view

/// One tab's panes: the split tree with draggable dividers, focus ring on the focused leaf when split.
struct SplitTreeView: View {
    @ObservedObject private var layout = TerminalLayout.shared
    let tabKey: Int64
    let node: SplitNode
    var path: [Bool] = []

    var body: some View {
        switch node {
        case .leaf(let id):
            LeafPane(tabKey: tabKey, id: id, ring: layout.tree(tabKey).leaves.count > 1 && layout.focusedLeaf(tabKey) == id)
        case .split(let h, let ratio, let a, let bNode):
            GeometryReader { g in
                let total = h ? g.size.width : g.size.height
                let first = max(60, min(total - 60, (total - 1) * ratio))
                let stack = Group {
                    SplitTreeView(tabKey: tabKey, node: a, path: path + [false])
                        .frame(width: h ? first : nil, height: h ? nil : first)
                    Rectangle().fill(Theme.border)
                        .frame(width: h ? 1 : nil, height: h ? nil : 1)
                        .overlay {
                            // A wider invisible grip than the 1 px line.
                            Color.clear.frame(width: h ? 7 : nil, height: h ? nil : 7).contentShape(Rectangle())
                                .onHover { inside in (inside ? (h ? NSCursor.resizeLeftRight : NSCursor.resizeUpDown) : NSCursor.arrow).set() }
                                .gesture(DragGesture(coordinateSpace: .named(path)).onChanged { v in
                                    let pos = h ? v.location.x : v.location.y
                                    guard total > 0 else { return }
                                    layout.trees[tabKey] = layout.tree(tabKey).withRatio(pos / total, at: path)
                                })
                        }
                    SplitTreeView(tabKey: tabKey, node: bNode, path: path + [true])
                }
                Group {
                    if h { HStack(spacing: 0) { stack } } else { VStack(spacing: 0) { stack } }
                }
                .coordinateSpace(name: path)
            }
        }
    }
}

private struct LeafPane: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var layout = TerminalLayout.shared
    let tabKey: Int64
    let id: Int64
    let ring: Bool

    var body: some View {
        let row = TerminalStore.shared.tab(pty: id) ?? [:]
        VStack(spacing: 0) {
            PtyPermissionBanner(ptyID: id)
            SecretRequestBanner(ownerKey: "pty:\(id)")
            if layout.popped.contains(id) {
                VStack(spacing: 10) {
                    Pet(state: "idle", size: 36)
                    Text("Open in its own window").font(.brand(-3)).foregroundStyle(Theme.muted)
                    Button("Bring back") { layout.closeWindow(id) }.buttonStyle(.bordered)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                TerminalPane(id: String(id), cwd: row.s("cwd").isEmpty ? b.workspaces[row.i("workspace_id")]?.s("path") ?? NSHomeDirectory() : row.s("cwd"))
            }
        }
        .overlay { if ring { Rectangle().strokeBorder(Theme.accent.opacity(0.35), lineWidth: 1).allowsHitTesting(false) } }
        .simultaneousGesture(TapGesture().onEnded { layout.focused[tabKey] = id })
    }
}

// MARK: - Store actions (splits, reopen, keys, auto-title)

extension TerminalStore {
    /// The workspace ⌘T / ⌃1-9 act on: the selected tab's, else the selected thread's.
    var contextWorkspace: Int64? {
        guard let b = burrow else { return nil }
        if let t = b.selectedTab, let row = tab(pty: t) { return row.i("workspace_id") }
        if let c = b.chat(b.selectedChat) { return c.i("workspace_id") }
        return nil
    }

    /// ⌘D / ⌘⇧D: a fresh shell beside the focused pane of the selected tab.
    func split(h: Bool) async {
        guard let b = burrow, let key = b.selectedTab, let row = tab(pty: key) else { return }
        let L = TerminalLayout.shared
        let target = L.focusedLeaf(key)
        let ws = row.i("workspace_id")
        let id = await nextPtyID()
        let cwd = tab(pty: target)?.s("cwd") ?? row.s("cwd")
        let n = tabs(root: b.rootWorkspace(ws).i("id")).count + L.hiddenLeaves.count + 1
        _ = session(id, cwd: cwd.isEmpty ? NSHomeDirectory() : cwd)
        L.trees[key] = L.tree(key).inserting(Int64(id)!, at: target, h: h)
        L.focused[key] = Int64(id)!
        await mutateTabs(ws) { $0.append(["title": "Terminal \(n)", "default_title": "Terminal \(n)", "pty_id": Int64(id)!, "cwd": cwd]) }
    }

    /// ⌘W: the focused pane; the last pane closes the tab.
    func closeFocusedPane() async {
        guard let key = burrow?.selectedTab else { return }
        await close(pty: TerminalLayout.shared.focusedLeaf(key))
    }

    func remember(_ row: JSON) {
        let cmd = row.s("initial_cmd")
        TerminalLayout.shared.closed.append(ClosedTerm(wsID: row.i("workspace_id"), cmd: cmd.isEmpty ? nil : cmd, cwd: row.s("cwd"), at: nowMs()))
        if TerminalLayout.shared.closed.count > 20 { TerminalLayout.shared.closed.removeFirst() }
    }

    /// ⌘⇧T: the newer of the last closed terminal and the most recently archived thread (pickReopen).
    func reopenClosed() async {
        guard let b = burrow else { return }
        let L = TerminalLayout.shared
        let archived = b.allChats.filter { $0.i("archived_at") > 0 && $0.i("parent_chat_id") == 0 }.max { $0.i("archived_at") < $1.i("archived_at") }
        if let a = archived, (L.closed.last?.at ?? 0) < a.i("archived_at") {
            await b.unarchiveThread(a.i("id"))
            b.selectedChat = a.i("id")
            return
        }
        guard let c = L.closed.popLast() else { return }
        await newTab(workspaceID: c.wsID, cmd: c.cmd, cwd: c.cwd.isEmpty ? nil : c.cwd)
    }

    /// Agent presets for the New tab menu: enabled provider instances with a terminal command line
    /// (providers.ts commandLine; the `providers` config key).
    var presets: [(name: String, cmd: String, shortcut: String)] {
        let catalog = ["claude": "claude", "codex": "codex", "gemini": "gemini", "opencode": "opencode", "mistral-vibe": "vibe",
                       "copilot": "copilot", "aider": "aider", "cursor": "cursor-agent"]
        return (ConfigStore.shared.values["providers"] as? [JSON] ?? []).compactMap { p in
            guard p["enabled"] as? Bool ?? true, p.s("providerId") != "openai-compatible" else { return nil }
            let bin = p.s("binary").trimmingCharacters(in: .whitespaces).isEmpty ? catalog[p.s("providerId")] ?? "" : p.s("binary")
            let extra = (p["args"] as? [String] ?? []) + p.s("terminalArgs").split(whereSeparator: \.isWhitespace).map(String.init)
            let cmd = ([bin] + extra).joined(separator: " ").trimmingCharacters(in: .whitespaces)
            return cmd.isEmpty ? nil : (p.s("name"), cmd, p.s("terminalShortcut"))
        }
    }

    /// ⌘J (keymap toggleTerminal): the Right Panel's Terminal surface in the bottom pane; again hides it.
    func toggleRpTerminal() {
        let rp = RPStore.shared
        if UserDefaults.standard.bool(forKey: "rp.open"), rp.layout.isShown("terminal") {
            if rp.layout.paneOf("terminal") == 1 { rp.layout.hideBottom() } else { UserDefaults.standard.set(false, forKey: "rp.open") }
        } else {
            rp.open("terminal", pane: 1)
        }
    }

    // MARK: keys

    func installKeys() {
        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] e in
            nonisolated(unsafe) let ev = e
            let handled = MainActor.assumeIsolated { self?.handleKey(ev) == true }
            return handled ? nil : e
        }
    }

    private static func glyphs(_ f: NSEvent.ModifierFlags) -> String {
        (f.contains(.control) ? "⌃" : "") + (f.contains(.option) ? "⌥" : "") + (f.contains(.shift) ? "⇧" : "") + (f.contains(.command) ? "⌘" : "")
    }

    private func handleKey(_ e: NSEvent) -> Bool {
        // Main window only (SwiftUI names it "main-AppWindow-N"): pop-outs, thread windows and Settings keep the defaults.
        guard let b = burrow, let win = NSApp.keyWindow, win.identifier?.rawValue.hasPrefix("main") == true || win.title == "Burrow",
              win.attachedSheet == nil else { return false }
        let f = e.modifierFlags.intersection([.command, .shift, .option, .control])
        let key = (e.characters(byApplyingModifiers: []) ?? "").lowercased()
        let onTab = b.selectedTab != nil
        switch (Self.glyphs(f), key) {
        case ("⌘", "t"):
            if let ws = contextWorkspace { Task { await newTab(workspaceID: ws) }; return true }
        case ("⌘", "w") where onTab:
            Task { await closeFocusedPane() }; return true
        case ("⌘", "d") where onTab:
            Task { await split(h: true) }; return true
        case ("⇧⌘", "d") where onTab:
            Task { await split(h: false) }; return true
        case ("⌥⌘", "o") where onTab:
            let leaf = TerminalLayout.shared.focusedLeaf(b.selectedTab!)
            TerminalLayout.shared.popOut(leaf, b: b, title: TabRow.title(tab(pty: leaf) ?? [:])); return true
        case ("⇧⌘", "t"):
            Task { await reopenClosed() }; return true
        case ("⌘", "j"):
            toggleRpTerminal(); return true
        case ("⌃", let k) where Int(k).map { (1...9).contains($0) } == true:
            guard let ws = contextWorkspace else { return false }
            let rows = tabs(root: b.rootWorkspace(ws).i("id"))
            if rows.indices.contains(Int(k)! - 1) { b.selectedTab = rows[Int(k)! - 1].i("pty_id") }
            return true
        default: break
        }
        // Agent preset shortcuts (provider instance terminalShortcut, e.g. ⌘⇧1).
        let pressed = Self.glyphs(f) + key.uppercased()
        if !f.isEmpty, let ws = contextWorkspace,
           let p = presets.first(where: { !$0.shortcut.isEmpty && Self.normalize($0.shortcut) == Self.normalize(pressed) }) {
            Task { await newTab(workspaceID: ws, cmd: p.cmd, name: p.name) }
            return true
        }
        return false
    }

    /// Same modifier set + key regardless of glyph order.
    private static func normalize(_ s: String) -> String { String(s.uppercased().sorted()) }

    // MARK: foreground auto-title (XTerm.vue poll)

    func pollTitles() async {
        guard let b = burrow else { return }
        let L = TerminalLayout.shared
        for (id, s) in sessions {
            guard let pty = Int64(id), let row = tab(pty: pty) else { continue }
            guard let proc = (try? await b.call("get_pty_foreground", ["id": id])) as? String else { continue }
            var st = L.titles[pty] ?? TitleState()
            let title = st.next(proc: proc, osc: s.oscTitle, cwd: row.s("cwd"))
            L.titles[pty] = st
            if let title, !title.isEmpty, title != row.s("title") { await rename(pty: pty, to: title) }
        }
    }
}
