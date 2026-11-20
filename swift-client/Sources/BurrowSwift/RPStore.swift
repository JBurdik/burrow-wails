import Combine
import SwiftUI

/// What a Right Panel surface is called and how the picker describes it (RightPanel.vue `tabs`).
struct RPSurface: Identifiable {
    let id: String, label: String, icon: String, description: String

    static let all: [RPSurface] = [
        .init(id: "changes", label: "Changes", icon: "plusminus", description: "Inspect, stage and commit workspace changes."),
        .init(id: "prs", label: "PRs", icon: "arrow.triangle.pull", description: "Check pull requests for this workspace."),
        .init(id: "files", label: "Files", icon: "folder", description: "Browse the current workspace."),
        .init(id: "diff", label: "Diff", icon: "doc.text.magnifyingglass", description: "Review changes by turn, branch or worktree; restore a checkpoint."),
        .init(id: "agents", label: "Agents", icon: "person.2", description: "Sub-agents spawned by this chat."),
        .init(id: "progress", label: "Progress", icon: "checklist", description: "The active chat's live todo list."),
        .init(id: "sidechat", label: "Side chat", icon: "bubble.left.and.bubble.right", description: "Ask a quick question about the active chat without disturbing it."),
        .init(id: "browser", label: "Browser", icon: "globe", description: "Preview a dev server without leaving Burrow."),
        .init(id: "devservers", label: "Dev servers", icon: "powerplug", description: "See and kill dev servers running in this workspace."),
        .init(id: "background", label: "Background", icon: "gearshape.2", description: "Background shell tasks the active chat launched."),
        .init(id: "terminal", label: "Terminal", icon: "terminal", description: "Run shell commands next to your changes."),
    ]

    /// Fixed surfaces by id; `file:<path>` / `doc:<path>` carry their path in the id itself.
    static func of(_ id: String) -> RPSurface? {
        if let s = all.first(where: { $0.id == id }) { return s }
        for (prefix, icon) in [("file:", "doc.text"), ("doc:", "doc.richtext")] where id.hasPrefix(prefix) {
            let path = String(id.dropFirst(prefix.count))
            return .init(id: id, label: (path as NSString).lastPathComponent, icon: icon, description: path)
        }
        return nil
    }
}

/// Right Panel layouts (one per workspace, persisted in `rp.layout`) + the doors other code uses to open surfaces.
@MainActor final class RPStore: ObservableObject {
    static let shared = RPStore()
    @Published private var layouts: [Int64: RPLayout] = [:]
    /// The workspace the panel shows (set by RightPanel); open requests land in its layout.
    @Published private(set) var ws: Int64 = 0
    /// Browser surface URL per workspace; `nav` bumps so a repeat open of the same URL reloads.
    @Published var browserURL: [Int64: String] = [:]
    @Published var browserNav = 0
    /// Side chat the Side chat surface shows (nil = the parent's newest). Not persisted (Vue: sideChatSelection).
    @Published var sidePick: Int64?
    /// Line to reveal per open file (openFileInRP's `line`; 0 = none). Reopening with a new line jumps there.
    @Published var fileLine: [String: Int] = [:]
    /// The pane a tab drag started in (drop overlay: "Move here" only on the other pane).
    var dragFrom: Int?
    weak var b: Burrow?
    private var bag: Set<AnyCancellable> = []

    private init() {
        #if DEBUG
        RPLayout.selfCheck()
        #endif
        if let d = UserDefaults.standard.data(forKey: "rp.layout"),
           let raw = try? JSONDecoder().decode([String: RPLayout].self, from: d) {
            for (k, v) in raw { if let id = Int64(k) { layouts[id] = v.normalized() } }
        }
    }

    /// RightPanel calls this on show / workspace switch. The first call starts serving RPOpenRequests
    /// (a request made before the panel ever showed is replayed by @Published on subscribe).
    func attach(_ ws: Int64, _ b: Burrow) {
        self.ws = ws
        self.b = b
        guard bag.isEmpty else { return }
        let req = RPOpenRequests.shared
        req.$file.compactMap { $0 }.receive(on: DispatchQueue.main).sink { [weak self] f in
            req.file = nil
            self?.openFile(f.0, line: f.1)
        }.store(in: &bag)
        req.$surface.compactMap { $0 }.receive(on: DispatchQueue.main).sink { [weak self] s in
            req.surface = nil
            self?.openNamed(s)
        }.store(in: &bag)
    }

    var layout: RPLayout {
        get { layouts[ws] ?? { var l = RPLayout(); l.openIn("changes"); return l }() }
        set {
            layouts[ws] = newValue
            // ponytail: one UserDefaults write per change (also per split-drag frame); debounce if it ever shows.
            let raw = Dictionary(uniqueKeysWithValues: layouts.map { ("\($0.key)", $0.value) })
            UserDefaults.standard.set(try? JSONEncoder().encode(raw), forKey: "rp.layout")
        }
    }

    /// Open (or focus) a surface; shows the panel.
    func open(_ id: String, pane: Int = 0) {
        layout.openIn(id, pane)
        UserDefaults.standard.set(true, forKey: "rp.open")
    }

    /// From a pane's picker: an already-open surface moves here rather than being focused in the other pane.
    func pick(_ id: String, pane: Int) {
        if let at = layout.paneOf(id), at != pane { layout.moveTo(id, pane) } else { open(id, pane: pane) }
    }

    func close(_ id: String) {
        if id == "terminal" { // Vue: closing the surface kills its shells
            let ws = self.ws
            Task { for t in TerminalStore.shared.rp["\(ws)"]?.ids ?? [] { await TerminalStore.shared.closeRp(ws, t) } }
        }
        if id == "browser" { browserURL[ws] = nil }
        layout.close(id)
    }

    /// A file joins the pane that already holds file tabs, so the tree can stay on top.
    func openFile(_ path: String, line: Int = 0) {
        fileLine[path] = line
        let l = layout
        let pane = [0, 1].first { p in l.panes[p].tabs.contains { $0.hasPrefix("file:") } } ?? 0
        open("file:" + path, pane: pane)
    }

    func openBrowser(_ url: String) {
        browserURL[ws] = url
        browserNav += 1
        open("browser")
    }

    /// The names chat notes / RPOpenRequests use (incl. "browser:<url>", "doc:<path>", "sidechat:<id>").
    func openNamed(_ name: String) {
        if name.hasPrefix("browser:") { openBrowser(String(name.dropFirst(8))); return }
        if name.hasPrefix("doc:") { open(name); return }
        if name.hasPrefix("sidechat:") { sidePick = Int64(name.dropFirst(9)); open("sidechat"); return }
        let id: String? = switch name {
        case "progress", "plan": "progress"
        case "background": "background"
        case "outputs", "diff": "diff" // Vue folded "outputs" into Diff
        case "changes", "git": "changes"
        case "prs", "pr", "pull-requests": "prs"
        case "agents", "subagents": "agents"
        case "files", "explorer": "files"
        case "terminal": "terminal"
        case "devservers", "dev-servers": "devservers"
        case "browser": "browser"
        case "sidechat", "side-chat": "sidechat"
        default: nil
        }
        if let id { open(id) }
    }
}
