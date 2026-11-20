import Foundation

// Pure git-window helpers — Foundation only. Ports src/lib/gitGraph.ts, src/lib/branchTree.ts and the
// parsers from GitHistory.vue / GitSidebar.vue / stores/git.ts. Checked by gitGraphSelfCheck() (bottom).

// MARK: graph (lib/gitGraph.ts — lane layout from GitPaw)

struct GraphCommit: Identifiable, Hashable {
    let hash: String
    var shortHash = ""
    var parents: [String] = []
    var author = ""
    var relTime = ""
    var refs: [String] = []
    var subject = ""
    var id: String { hash }
}

struct GraphEdge: Hashable { let from: Int; let to: Int; let color: Int }

struct GraphRow {
    let lane: Int
    let color: Int
    /// top half: lanes converging into the node, or straight pass-through
    let up: [GraphEdge]
    /// bottom half: node/lane to lane at row bottom
    let down: [GraphEdge]
    let width: Int
    let isMerge: Bool
}

/// Lanes keep their column while alive so pass-through lines stay straight.
func layoutGraph(_ commits: [GraphCommit]) -> (rows: [GraphRow], maxLanes: Int) {
    var lanes: [String?] = []
    var colors: [Int] = []
    var nextColor = 0
    var maxLanes = 1
    var rows: [GraphRow] = []
    let known = Set(commits.map(\.hash))

    func alloc(_ hash: String, _ color: Int) -> Int {
        let i = lanes.firstIndex(where: { $0 == nil }) ?? lanes.count
        if i == lanes.count { lanes.append(hash) } else { lanes[i] = hash }
        if i < colors.count { colors[i] = color } else { colors.append(contentsOf: Array(repeating: 0, count: i - colors.count + 1)); colors[i] = color }
        return i
    }

    for c in commits {
        let before = lanes
        let beforeColors = colors
        var lane = lanes.firstIndex(where: { $0 == c.hash }) ?? -1
        if lane == -1 { lane = alloc(c.hash, nextColor); nextColor += 1 }
        let color = colors[lane]
        var up: [GraphEdge] = []
        var down: [GraphEdge] = []

        for (i, h) in before.enumerated() {
            if h == c.hash { up.append(GraphEdge(from: i, to: lane, color: beforeColors[i])) }
            else if h != nil { up.append(GraphEdge(from: i, to: i, color: beforeColors[i])) }
        }
        lanes = lanes.enumerated().map { i, h in h == c.hash && i != lane ? nil : h }

        // parents outside the loaded window are dropped
        let parents = c.parents.filter { known.contains($0) }
        if parents.isEmpty { lanes[lane] = nil }
        for (idx, p) in parents.enumerated() {
            if idx == 0 {
                if let existing = lanes.firstIndex(where: { $0 == p }), existing != lane {
                    // first parent already tracked elsewhere: bend into that lane
                    lanes[lane] = nil
                    down.append(GraphEdge(from: lane, to: existing, color: color))
                } else {
                    lanes[lane] = p
                    down.append(GraphEdge(from: lane, to: lane, color: color))
                }
            } else {
                var j = lanes.firstIndex(where: { $0 == p }) ?? -1
                if j == -1 { j = alloc(p, nextColor); nextColor += 1 }
                down.append(GraphEdge(from: lane, to: j, color: colors[j]))
            }
        }
        for (i, h) in lanes.enumerated() where h != nil && i != lane && i < before.count && before[i] == h {
            down.append(GraphEdge(from: i, to: i, color: colors[i]))
        }
        while let last = lanes.last, last == nil { lanes.removeLast() }
        let width = max(before.count, lanes.count, lane + 1)
        maxLanes = max(maxLanes, width)
        rows.append(GraphRow(lane: lane, color: color, up: up, down: down, width: width, isMerge: c.parents.count > 1))
    }
    return (rows, maxLanes)
}

/// `git log … --format=%H%x1f%h%x1f%P%x1f%an%x1f%cr%x1f%D%x1f%s%x1e` (stores/git.ts fetchGraph).
func parseGraphLog(_ out: String) -> [GraphCommit] {
    out.components(separatedBy: "\u{1e}").compactMap { rec in
        let r = rec.hasPrefix("\n") ? String(rec.dropFirst()) : rec
        let p = r.components(separatedBy: "\u{1f}")
        guard p.count >= 7, !p[0].isEmpty else { return nil }
        return GraphCommit(hash: p[0], shortHash: p[1], parents: p[2].split(separator: " ").map(String.init),
                           author: p[3], relTime: p[4], refs: p[5].isEmpty ? [] : p[5].components(separatedBy: ", "),
                           subject: p[6...].joined(separator: "\u{1f}"))
    }
}

// MARK: patch split (lib/gitGraph.ts splitPatch)

struct PatchFile: Hashable {
    var path: String
    var status: String // A | M | D | R
    var adds: Int
    var dels: Int
    /// this file's part of the patch, starting at its `diff --git` line
    let text: String
}

func splitPatch(_ patch: String) -> [PatchFile] {
    var chunks: [[String]] = []
    for l in patch.components(separatedBy: "\n") {
        if l.hasPrefix("diff --git ") { chunks.append([l]) } else if !chunks.isEmpty { chunks[chunks.count - 1].append(l) }
    }
    return chunks.map { lines in
        let head = lines[0]
        var path = head.range(of: " b/", options: .backwards).map { String(head[$0.upperBound...]) } ?? String(head.dropFirst(11))
        var status = "M", adds = 0, dels = 0, inHunk = false
        for l in lines.dropFirst() {
            if !inHunk {
                if l.hasPrefix("new file mode") { status = "A" }
                else if l.hasPrefix("deleted file mode") { status = "D" }
                else if l.hasPrefix("rename to ") { status = "R"; path = String(l.dropFirst(10)) }
                else if l.hasPrefix("+++ b/") { path = String(l.dropFirst(6)) }
                else if l.hasPrefix("--- a/") && status == "D" { path = String(l.dropFirst(6)) }
                else if l.hasPrefix("@@") { inHunk = true }
                continue
            }
            if l.hasPrefix("+") { adds += 1 } else if l.hasPrefix("-") { dels += 1 }
        }
        return PatchFile(path: path, status: status, adds: adds, dels: dels, text: lines.joined(separator: "\n"))
    }
}

// MARK: ref pills (GitHistory.vue refPills) — local + same-named remote collapse into one pill with a cloud

struct RefPill: Hashable {
    enum Kind { case local, remote, tag }
    let label: String
    let kind: Kind
    var head = false
    var synced = false
}

func refPills(_ refs: [String], localBranches: Set<String>) -> [RefPill] {
    var locals: [RefPill] = []
    var remotes: [String] = []
    var tags: [RefPill] = []
    func setLocal(_ p: RefPill) {
        if let i = locals.firstIndex(where: { $0.label == p.label }) { locals[i] = p } else { locals.append(p) }
    }
    for r in refs {
        if r.hasPrefix("tag: ") { tags.append(RefPill(label: String(r.dropFirst(5)), kind: .tag)) }
        else if r.hasPrefix("HEAD -> ") { setLocal(RefPill(label: String(r.dropFirst(8)), kind: .local, head: true)) }
        else if r == "HEAD" || r.hasSuffix("/HEAD") { continue }
        else if localBranches.contains(r) { setLocal(RefPill(label: r, kind: .local)) }
        else { remotes.append(r) }
    }
    var out: [RefPill] = []
    for r in remotes {
        let name = r.split(separator: "/", omittingEmptySubsequences: false).dropFirst().joined(separator: "/")
        if let i = locals.firstIndex(where: { $0.label == name }) { locals[i].synced = true }
        else { out.append(RefPill(label: r, kind: .remote)) }
    }
    return locals + out + tags
}

// MARK: refs / worktrees / stashes (GitSidebar.vue loadRefs, stores/git.ts readStashes)

struct GitRef: Hashable, Identifiable {
    enum Kind { case local, remote, tag }
    let full: String
    let name: String
    let hash: String
    let isHead: Bool
    let track: String // "[ahead 1, behind 2]" | "[gone]" | ""
    let kind: Kind
    var id: String { full }

    func trackNum(_ k: String) -> Int? {
        guard let r = track.range(of: "\(k) \\d+", options: .regularExpression) else { return nil }
        return Int(track[r].dropFirst(k.count + 1))
    }
}

/// `for-each-ref --format=%(refname)%1f%(refname:short)%1f%(objectname)%1f%(HEAD)%1f%(upstream:track)`
func parseRefs(_ out: String) -> [GitRef] {
    out.split(separator: "\n").compactMap { l in
        let p = l.components(separatedBy: "\u{1f}")
        guard p.count >= 5, !p[0].hasSuffix("/HEAD") else { return nil }
        let kind: GitRef.Kind = p[0].hasPrefix("refs/heads/") ? .local : p[0].hasPrefix("refs/tags/") ? .tag : .remote
        return GitRef(full: p[0], name: p[1], hash: p[2], isHead: p[3] == "*", track: p[4], kind: kind)
    }
}

struct GitWorktree: Hashable, Identifiable {
    let path: String
    let head: String
    let branch: String?
    var id: String { path }
}

/// `git worktree list --porcelain`
func parseWorktrees(_ out: String) -> [GitWorktree] {
    out.components(separatedBy: "\n\n").filter { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }.map { block in
        let lines = block.split(separator: "\n").map(String.init)
        func get(_ k: String) -> String? { lines.first { $0.hasPrefix(k + " ") }.map { String($0.dropFirst(k.count + 1)) } }
        let br = get("branch").map { $0.hasPrefix("refs/heads/") ? String($0.dropFirst(11)) : $0 }
        return GitWorktree(path: get("worktree") ?? "", head: get("HEAD") ?? "", branch: br)
    }
}

struct GitStash: Hashable, Identifiable {
    let ref: String
    let message: String
    let relTime: String
    var id: String { ref }
}

/// `git stash list --format=%gd%x1f%s%x1f%cr`
func parseStashes(_ out: String) -> [GitStash] {
    out.split(separator: "\n").compactMap { l in
        let p = l.components(separatedBy: "\u{1f}")
        return p.count >= 3 ? GitStash(ref: p[0], message: p[1], relTime: p[2]) : nil
    }
}

// MARK: branch tree (lib/branchTree.ts)

enum BranchRow<T> {
    case folder(key: String, label: String, depth: Int, count: Int, open: Bool)
    case leaf(key: String, label: String, depth: Int, item: T)

    var key: String { switch self { case .folder(let k, _, _, _, _), .leaf(let k, _, _, _): k } }
}

private final class BranchNode<T> {
    var folderOrder: [String] = []
    var folders: [String: BranchNode<T>] = [:]
    var leaves: [(String, T)] = []
    var count = 0
}

/// Folders before leaves at each level, otherwise input order kept. `expandAll` ignores `isCollapsed` (search).
func branchTreeRows<T>(_ items: [T], name: (T) -> String, isCollapsed: (String) -> Bool, expandAll: Bool = false) -> [BranchRow<T>] {
    let root = BranchNode<T>()
    for item in items {
        let parts = name(item).components(separatedBy: "/")
        var n = root
        n.count += 1
        for p in parts.dropLast() {
            if n.folders[p] == nil { n.folders[p] = BranchNode<T>(); n.folderOrder.append(p) }
            n = n.folders[p]!
            n.count += 1
        }
        n.leaves.append((parts.last ?? "", item))
    }
    var rows: [BranchRow<T>] = []
    func walk(_ n: BranchNode<T>, _ prefix: String, _ depth: Int) {
        for label in n.folderOrder {
            let child = n.folders[label]!
            let key = prefix + label
            let open = expandAll || !isCollapsed(key)
            rows.append(.folder(key: key, label: label, depth: depth, count: child.count, open: open))
            if open { walk(child, key + "/", depth + 1) }
        }
        for (label, item) in n.leaves { rows.append(.leaf(key: prefix + label, label: label, depth: depth, item: item)) }
    }
    walk(root, "", 0)
    return rows
}

// MARK: self-check

/// Run with: cat GitGraph.swift <(echo 'gitGraphSelfCheck()') > /tmp/g.swift && swiftc -o /tmp/g /tmp/g.swift && /tmp/g
func gitGraphSelfCheck() {
    // linear: one lane, straight lines
    let lin = layoutGraph([GraphCommit(hash: "c", parents: ["b"]), GraphCommit(hash: "b", parents: ["a"]), GraphCommit(hash: "a")])
    assert(lin.maxLanes == 1 && lin.rows.map(\.lane) == [0, 0, 0])
    assert(lin.rows[0].up.isEmpty && lin.rows[0].down == [GraphEdge(from: 0, to: 0, color: 0)])
    assert(lin.rows[2].down.isEmpty)

    // merge: m = merge(b, f); f branches off a; b off a
    let g = layoutGraph([
        GraphCommit(hash: "m", parents: ["b", "f"]),
        GraphCommit(hash: "f", parents: ["a"]),
        GraphCommit(hash: "b", parents: ["a"]),
        GraphCommit(hash: "a"),
    ])
    assert(g.rows[0].isMerge && g.rows[0].lane == 0)
    assert(g.rows[0].down == [GraphEdge(from: 0, to: 0, color: 0), GraphEdge(from: 0, to: 1, color: 1)])
    assert(g.rows[1].lane == 1 && g.rows[1].color == 1)            // f on its own lane
    assert(g.rows[1].up.contains(GraphEdge(from: 0, to: 0, color: 0))) // b's lane passes straight through f
    assert(g.rows[2].lane == 0)                                    // b on lane 0
    // b's first parent a is already tracked in lane 1 (from f) → b bends into it
    assert(g.rows[2].down == [GraphEdge(from: 0, to: 1, color: 0), GraphEdge(from: 1, to: 1, color: 1)])
    assert(g.rows[3].lane == 1 && g.maxLanes == 2)

    // parents outside the window are dropped
    let cut = layoutGraph([GraphCommit(hash: "x", parents: ["gone"])])
    assert(cut.rows[0].down.isEmpty)

    let log = parseGraphLog("abc\u{1f}ab\u{1f}p1 p2\u{1f}Jo\u{1f}2 days ago\u{1f}HEAD -> main, origin/main, tag: v1\u{1f}Merge x\u{1e}\ndef\u{1f}de\u{1f}\u{1f}Jo\u{1f}3 days ago\u{1f}\u{1f}init\u{1e}\n")
    assert(log.count == 2 && log[0].parents == ["p1", "p2"] && log[0].refs.count == 3 && log[1].parents.isEmpty && log[1].subject == "init")

    let pills = refPills(["HEAD -> main", "origin/main", "origin/feat", "tag: v1", "origin/HEAD"], localBranches: ["main"])
    assert(pills == [RefPill(label: "main", kind: .local, head: true, synced: true), RefPill(label: "origin/feat", kind: .remote), RefPill(label: "v1", kind: .tag)])

    let patch = """
    diff --git a/x.txt b/x.txt
    index 1..2 100644
    --- a/x.txt
    +++ b/x.txt
    @@ -1 +1,2 @@
    -a
    +b
    +c
    diff --git a/gone.txt b/gone.txt
    deleted file mode 100644
    --- a/gone.txt
    +++ /dev/null
    @@ -1 +0,0 @@
    -bye
    diff --git a/old b/new
    similarity index 100%
    rename from old
    rename to new
    """
    let files = splitPatch(patch)
    assert(files.count == 3)
    assert(files[0].path == "x.txt" && files[0].status == "M" && files[0].adds == 2 && files[0].dels == 1)
    assert(files[1].path == "gone.txt" && files[1].status == "D" && files[1].dels == 1)
    assert(files[2].path == "new" && files[2].status == "R")
    assert(files[0].text.hasPrefix("diff --git a/x.txt") && !files[0].text.contains("gone.txt"))

    let refs = parseRefs("refs/heads/main\u{1f}main\u{1f}aaa\u{1f}*\u{1f}[ahead 2, behind 1]\nrefs/remotes/origin/HEAD\u{1f}origin\u{1f}aaa\u{1f} \u{1f}\nrefs/tags/v1\u{1f}v1\u{1f}bbb\u{1f} \u{1f}\n")
    assert(refs.count == 2 && refs[0].isHead && refs[0].kind == .local && refs[0].trackNum("ahead") == 2 && refs[0].trackNum("behind") == 1)
    assert(refs[1].kind == .tag && refs[1].trackNum("ahead") == nil)

    let wts = parseWorktrees("worktree /r\nHEAD aaa\nbranch refs/heads/main\n\nworktree /w\nHEAD bbb\ndetached\n")
    assert(wts == [GitWorktree(path: "/r", head: "aaa", branch: "main"), GitWorktree(path: "/w", head: "bbb", branch: nil)])
    assert(parseStashes("stash@{0}\u{1f}WIP on main\u{1f}1 hour ago\n") == [GitStash(ref: "stash@{0}", message: "WIP on main", relTime: "1 hour ago")])

    let tree = branchTreeRows(["main", "JB/fix/a", "JB/fix/b", "JB/x"], name: { $0 }, isCollapsed: { $0 == "JB/fix" })
    assert(tree.map(\.key) == ["JB", "JB/fix", "JB/x", "main"])
    if case let .folder(_, _, _, count, open) = tree[1] { assert(count == 2 && !open) } else { assert(false) }
    assert(branchTreeRows(["JB/fix/a"], name: { $0 }, isCollapsed: { _ in true }, expandAll: true).count == 3)
    print("gitGraphSelfCheck ok")
}
