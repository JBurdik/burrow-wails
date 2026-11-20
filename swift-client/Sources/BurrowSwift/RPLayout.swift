import Foundation

/// Right Panel layout: two stacked panes, each with its own surface tabs (port of src/lib/rpLayout.ts).
/// A surface lives in at most one pane — opening one that is already open just focuses it where it is.
struct RPLayout: Codable, Equatable {
    struct Pane: Codable, Equatable { var tabs: [String] = []; var active: String? }
    var panes: [Pane] = [Pane(), Pane()]
    /// Top pane's share of the height while both are visible.
    var split: Double = 0.5
    var maximized: Int?
    /// The bottom pane shows while it has tabs, or right after "Split" (picker).
    var bottomOpen = false

    static func clampSplit(_ v: Double) -> Double { v.isFinite ? min(0.85, max(0.15, v)) : 0.5 }

    func paneOf(_ id: String) -> Int? { panes[0].tabs.contains(id) ? 0 : panes[1].tabs.contains(id) ? 1 : nil }
    func isShown(_ id: String) -> Bool { panes[0].active == id || (bottomOpen && panes[1].active == id) }
    func paneVisible(_ p: Int) -> Bool { p == 0 ? maximized != 1 : bottomOpen && maximized != 0 }

    private mutating func detach(_ id: String) {
        guard let from = paneOf(id) else { return }
        panes[from].tabs.removeAll { $0 == id }
        if panes[from].active == id { panes[from].active = panes[from].tabs.first }
        if from == 1 && panes[1].tabs.isEmpty { collapseBottom() }
    }

    private mutating func collapseBottom() {
        bottomOpen = false
        panes[1].active = nil
        maximized = nil
    }

    /// Open (or focus) a surface. A new one lands in `pane`. Returns where it is.
    @discardableResult mutating func openIn(_ id: String, _ pane: Int = 0) -> Int {
        let target = paneOf(id) ?? pane
        if !panes[target].tabs.contains(id) { panes[target].tabs.append(id) }
        panes[target].active = id
        if target == 1 { bottomOpen = true }
        return target
    }

    mutating func close(_ id: String) { detach(id) }

    mutating func moveTo(_ id: String, _ to: Int) {
        guard paneOf(id) != to else { return }
        detach(id)
        openIn(id, to)
    }

    /// "Split": show the bottom pane (empty → it shows the surface picker).
    mutating func openBottom() { bottomOpen = true; maximized = nil }
    /// Put the bottom pane away, its surfaces kept (they come back with it).
    mutating func hideBottom() { collapseBottom() }

    /// Closing the bottom pane never closes its surfaces — they go back on top.
    mutating func closeBottom() {
        let moved = panes[1].tabs
        panes[1].tabs = []
        for id in moved where !panes[0].tabs.contains(id) { panes[0].tabs.append(id) }
        if panes[0].active == nil { panes[0].active = moved.first }
        collapseBottom()
    }

    mutating func toggleMaximize(_ p: Int) { maximized = maximized == p ? nil : p }

    /// Rebuild from untrusted storage: a surface listed in both panes keeps its first place,
    /// a dangling `active` drops, split is clamped, maximize needs a visible bottom.
    func normalized() -> RPLayout {
        var l = RPLayout()
        var seen = Set<String>()
        for i in 0..<2 {
            let p = i < panes.count ? panes[i] : Pane()
            let tabs = p.tabs.filter { seen.insert($0).inserted }
            l.panes[i] = Pane(tabs: tabs, active: p.active.flatMap { tabs.contains($0) ? $0 : nil })
        }
        l.split = Self.clampSplit(split)
        l.bottomOpen = bottomOpen || !l.panes[1].tabs.isEmpty
        l.maximized = l.bottomOpen && (maximized == 0 || maximized == 1) ? maximized : nil
        return l
    }

    /// The rpLayout.test.ts rules, asserted (debug builds run this once at RPStore init).
    static func selfCheck() {
        var l = RPLayout()
        assert(l.openIn("git") == 0 && l.panes[0].active == "git")
        l.openIn("terminal", 1)
        assert(l.bottomOpen && l.paneOf("terminal") == 1)
        assert(l.openIn("terminal") == 1, "already open: focus where it is")
        l.moveTo("terminal", 0)
        assert(l.paneOf("terminal") == 0 && !l.bottomOpen && l.panes[1].active == nil, "empty bottom collapses")
        l.openIn("agents", 1); l.openIn("diff", 1); l.toggleMaximize(1)
        l.closeBottom()
        assert(l.panes[0].tabs == ["git", "terminal", "agents", "diff"] && !l.bottomOpen && l.maximized == nil)
        l.close("git")
        assert(l.panes[0].active == "terminal", "closing the active one focuses the first left")
        l.openIn("x", 1); l.hideBottom()
        assert(l.panes[1].tabs == ["x"] && !l.isShown("x"))
        let raw = RPLayout(panes: [Pane(tabs: ["a", "b"], active: "z"), Pane(tabs: ["b", "c"], active: "c")], split: 9, maximized: 1)
        let n = raw.normalized()
        assert(n.panes[0].active == nil && n.panes[1].tabs == ["c"] && n.split == 0.85 && n.bottomOpen && n.maximized == 1)
        assert(RPLayout(panes: [Pane()], maximized: 0).normalized().maximized == nil)
    }
}
