import Foundation

// Pure transcript derivations, ported from src/lib: visitWatermark.ts, chatTurns.ts, threadFind.ts,
// chatLinks.ts, localLink.ts, transcriptFeedback.ts (insertQuote). Only Foundation + [String: Any], so
// `transcriptSelfCheck()` can be compiled and run standalone (see the bottom of this file).

private func str(_ m: [String: Any], _ k: String) -> String { m[k] as? String ?? "" }
private func int(_ m: [String: Any], _ k: String) -> Int64 { (m[k] as? NSNumber)?.int64Value ?? Int64(m[k] as? String ?? "") ?? 0 }

// MARK: - visit watermark

/// Index of the first message the user has not seen (the "New" divider), or nil. `seenAt` is the chat's
/// read receipt as it was BEFORE this visit; fewer than 2 new agent rows is one reply — no divider.
func visitWatermark(_ msgs: [[String: Any]], seenAt: Int64?) -> Int? {
    guard let seenAt else { return nil }
    guard let first = msgs.firstIndex(where: { str($0, "role") != "queued" && int($0, "at") > seenAt }), first > 0 else { return nil }
    let agent = msgs[first...].filter { ["assistant", "tool"].contains(str($0, "role")) && int($0, "at") > seenAt }
    return agent.count >= 2 ? first : nil
}

// MARK: - turns

struct FileEdit: Equatable {
    var path: String
    var added: Int
    var removed: Int
    var hunks: [Hunk]
    struct Hunk: Equatable { var old: String; var new: String }
}

func countLines(_ s: String) -> Int { s.isEmpty ? 0 : s.components(separatedBy: "\n").count }

/// A tool call counts as a file edit only if it carries edit content (keeps Read/Grep out).
func editOf(_ input: [String: Any]?) -> FileEdit? {
    guard let input else { return nil }
    let path = (input["file_path"] as? String) ?? (input["path"] as? String) ?? ""
    if path.isEmpty { return nil }
    if let edits = input["edits"] as? [[String: Any]] {
        let hunks = edits.map { FileEdit.Hunk(old: str($0, "old_string"), new: str($0, "new_string")) }
        return FileEdit(path: path, added: hunks.reduce(0) { $0 + countLines($1.new) },
                        removed: hunks.reduce(0) { $0 + countLines($1.old) }, hunks: hunks)
    }
    if input["old_string"] is String || input["new_string"] is String {
        let h = FileEdit.Hunk(old: str(input, "old_string"), new: str(input, "new_string"))
        return FileEdit(path: path, added: countLines(h.new), removed: countLines(h.old), hunks: [h])
    }
    if let c = input["content"] as? String { return FileEdit(path: path, added: countLines(c), removed: 0, hunks: [.init(old: "", new: c)]) }
    return nil
}

/// Repeated edits of one file sum into one row, in first-touch order.
func mergeEdits(_ edits: [FileEdit]) -> [FileEdit] {
    var out: [FileEdit] = []
    for e in edits {
        if let i = out.firstIndex(where: { $0.path == e.path }) {
            out[i].added += e.added; out[i].removed += e.removed; out[i].hunks += e.hunks
        } else { out.append(e) }
    }
    return out
}

func fmtDuration(_ ms: Int64) -> String {
    let s = max(0, Int((Double(ms) / 1000).rounded()))
    return s < 60 ? "\(s)s" : "\(s / 60)m \(s % 60)s"
}

/// Hunks as display lines ("sep" between hunks), capped at `max`.
func hunkLines(_ hunks: [FileEdit.Hunk], max: Int = 40) -> (lines: [(kind: String, text: String)], hidden: Int) {
    var all: [(kind: String, text: String)] = []
    for (i, h) in hunks.enumerated() {
        if i > 0 { all.append(("sep", "")) }
        if !h.old.isEmpty { all += h.old.components(separatedBy: "\n").map { ("del", $0) } }
        if !h.new.isEmpty { all += h.new.components(separatedBy: "\n").map { ("add", $0) } }
    }
    return (Array(all.prefix(max)), Swift.max(0, all.count - max))
}

/// Only edits inside the chat's project go on the card (not the agent's memory notes / scratch files).
func isProjectPath(_ path: String, root: String) -> Bool {
    if !path.hasPrefix("/") { return true }
    if root.isEmpty { return false }
    let base = root.hasSuffix("/") ? root : root + "/"
    return path == root || path.hasPrefix(base)
}

/// One row of the rendered transcript. Indexes are GLOBAL transcript indexes (pageStart + local), so ids
/// survive an older page being prepended.
enum TRow: Identifiable {
    case message(Int, [String: Any])
    case tools(Int, [[String: Any]])
    /// A settled turn's work rows folded behind "Worked 26s". `turn` = the user message's index.
    case fold(turn: Int, label: String, folded: Bool, tools: Int, files: Int)
    case files(turn: Int, [FileEdit])

    var id: String {
        switch self {
        case .message(let i, _): "m\(i)"
        case .tools(let i, _): "t\(i)"
        case .fold(let t, _, _, _, _): "f\(t)"
        case .files(let t, _): "c\(t)"
        }
    }
    /// Global message indexes this row shows (find / watermark map a message to its row).
    var indexes: [Int] {
        switch self {
        case .message(let i, _): [i]
        case .tools(let i, let run): Array(i..<(i + run.count))
        default: []
        }
    }
}

/// Group consecutive tools, drop plain system-info, then fold every settled turn (chatTurns + AgentChat displayItems).
/// `busy` = the chat's last turn is still running; `folded` = the user's explicit toggles by turn index.
func transcriptRows(_ msgs: [[String: Any]], start: Int, busy: Bool, folded: [Int: Bool], cwd: String) -> [TRow] {
    var base: [TRow] = []
    for (l, m) in msgs.enumerated() {
        let role = str(m, "role"), g = start + l
        if role == "system-info", int(m, "subagentChatId") == 0, str(m, "surface").isEmpty { continue }
        if role == "tool" {
            if case .tools(let s, var run) = base.last, s + run.count == g { run.append(m); base[base.count - 1] = .tools(s, run); continue }
            base.append(.tools(g, [m]))
        } else { base.append(.message(g, m)) }
    }
    func isUser(_ r: TRow) -> Bool { if case .message(_, let m) = r { return str(m, "role") == "user" }; return false }
    func isWork(_ r: TRow) -> Bool {
        if case .tools = r { return true }
        if case .message(_, let m) = r { return str(m, "role") == "thinking" }
        return false
    }
    var out: [TRow] = [], i = 0
    while i < base.count {
        guard isUser(base[i]), case .message(let ui, let user) = base[i] else { out.append(base[i]); i += 1; continue }
        out.append(base[i]); i += 1
        var turn: [TRow] = []
        while i < base.count, !isUser(base[i]) { turn.append(base[i]); i += 1 }
        let later = i < base.count, settled = later || !busy
        let isFolded = settled && (folded[ui] ?? later)
        let tools = turn.flatMap { r -> [[String: Any]] in if case .tools(_, let run) = r { return run }; return [] }
        let edits = tools.compactMap { editOf($0["toolInput"] as? [String: Any]) }.filter { isProjectPath($0.path, root: cwd) }
        // turnMs is stamped by the session on settle; older rows only have `at` — the last row's minus the prompt's.
        var ms = int(user, "turnMs")
        if ms == 0, settled, let last = turn.last, case let lastAt = rowAt(last), lastAt > 0, int(user, "at") > 0 { ms = lastAt - int(user, "at") }
        var emitted = false
        for r in turn {
            if settled, isWork(r) {
                if !emitted {
                    out.append(.fold(turn: ui, label: ms > 0 ? "Worked \(fmtDuration(ms))" : "Worked", folded: isFolded,
                                     tools: tools.count, files: Set(edits.map(\.path)).count))
                    emitted = true
                }
                if isFolded { continue }
            }
            out.append(r)
        }
        if settled, !edits.isEmpty { out.append(.files(turn: ui, mergeEdits(edits))) }
    }
    return out
}

private func rowAt(_ r: TRow) -> Int64 {
    switch r {
    case .message(_, let m): int(m, "at")
    case .tools(_, let run): run.last.map { int($0, "at") } ?? 0
    default: 0
    }
}

// MARK: - find

/// Everything find looks at in one message: text, a tool's output and input.
func findableText(_ m: [String: Any]) -> String {
    if str(m, "role") == "queued" { return "" }
    if str(m, "role") != "tool" { return str(m, "text") }
    var input = ""
    if let i = m["toolInput"] as? [String: Any], !i.isEmpty,
       let d = try? JSONSerialization.data(withJSONObject: i, options: [.prettyPrinted, .sortedKeys]) { input = String(decoding: d, as: UTF8.self) }
    return [str(m, "text"), str(m, "toolOutput"), input].joined(separator: "\n")
}

/// Case-insensitive, non-overlapping occurrences of `q` in `text`.
func occurrences(_ text: String, _ q: String) -> [Range<String.Index>] {
    guard !q.isEmpty else { return [] }
    var out: [Range<String.Index>] = [], from = text.startIndex
    while let r = text.range(of: q, options: .caseInsensitive, range: from..<text.endIndex) { out.append(r); from = r.upperBound }
    return out
}

/// One entry per occurrence: the GLOBAL message index holding it.
func findMatches(_ msgs: [[String: Any]], start: Int, _ q: String) -> [Int] {
    msgs.enumerated().flatMap { l, m in Array(repeating: start + l, count: occurrences(findableText(m), q).count) }
}

/// Wraps both ways.
func stepFind(_ i: Int, total: Int, _ d: Int) -> Int {
    total <= 0 ? 0 : ((min(max(i, 0), total - 1) + d) % total + total) % total
}

// MARK: - links

struct LocalLink: Equatable { var path: String; var line: Int }

/// An href (or inline-code path) from rendered markdown → a local file, or nil (external URL, anchor, empty).
func resolveLocalLink(_ raw: String, baseDir: String) -> LocalLink? {
    var href = raw.trimmingCharacters(in: .whitespaces)
    if href.isEmpty || href.hasPrefix("#") { return nil }
    if href.lowercased().hasPrefix("file:") { href = String(href.dropFirst(5)); while href.hasPrefix("//") { href.removeFirst() }; if !href.hasPrefix("/") { href = "/" + href } }
    else if href.range(of: "^[a-zA-Z][a-zA-Z0-9+.-]*:", options: .regularExpression) != nil,
            href.range(of: "^[^/]+\\.[A-Za-z]\\w*:\\d", options: .regularExpression) == nil { return nil } // `a.ts:12` is not a scheme
    href = href.removingPercentEncoding ?? href
    var line = 0
    if let h = href.range(of: "#L?(\\d+)", options: .regularExpression) {
        line = Int(href[h].filter(\.isNumber)) ?? 0
    }
    if let q = href.firstIndex(where: { $0 == "?" || $0 == "#" }) { href = String(href[..<q]) }
    if let c = href.range(of: ":(\\d+)(-\\d+|:\\d+)?$", options: .regularExpression) {
        let digits = href[c].dropFirst().prefix { $0.isNumber }
        line = Int(digits) ?? line
        href = String(href[..<c.lowerBound])
    }
    if href.isEmpty { return nil }
    if href.hasPrefix("~/") { href = String(href.dropFirst(1)) }
    return LocalLink(path: joinPath(href.hasPrefix("/") ? "" : baseDir, href), line: line)
}

private func joinPath(_ base: String, _ rel: String) -> String {
    var out: [Substring] = []
    for part in "\(base)/\(rel)".split(separator: "/") {
        if part == "." { continue }
        if part == ".." { _ = out.popLast() } else { out.append(part) }
    }
    return "/" + out.joined(separator: "/")
}

let fileRefBody = #"(?:\.{0,2}/|~/)?[\w@.-]+(?:/[\w@.-]+)*\.[A-Za-z]\w{0,7}(?:#L\d+(?:-L?\d+)?|:\d+(?:-\d+)?(?::\d+)?)"#
/// `path:line` / `path#L12` mentions in prose (a line reference is mandatory, so plain filenames stay text).
let fileRefInText = try! NSRegularExpression(pattern: #"(?<![\w/.:@#~-])("# + fileRefBody + #")(?![\w/])"#)

/// Inline `code` that is a link: a path with a line reference, or a markdown file path.
func codeSpanIsFileRef(_ s: String) -> Bool {
    let t = s.trimmingCharacters(in: .whitespaces)
    return t.range(of: "^" + fileRefBody + "$", options: .regularExpression) != nil
        || t.range(of: #"^[\w.~/@-]+\.(md|markdown|mdx)(:\d+)?$"#, options: [.regularExpression, .caseInsensitive]) != nil
}

/// `host/owner/repo` (lowercased, no .git) for a git remote in any usual shape.
func repoKey(_ remote: String) -> String? {
    let s = remote.trimmingCharacters(in: .whitespacesAndNewlines)
    var host = "", path = ""
    if let m = s.range(of: #"^[\w.-]+@[^:/]+:.+$"#, options: .regularExpression), m.lowerBound == s.startIndex {
        let afterAt = s[s.index(after: s.firstIndex(of: "@")!)...]
        let colon = afterAt.firstIndex(of: ":")!
        host = String(afterAt[..<colon]); path = String(afterAt[afterAt.index(after: colon)...])
    } else if let u = URL(string: s), let h = u.host { host = h; path = u.path } else { return nil }
    path = path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    if path.lowercased().hasSuffix(".git") { path = String(path.dropLast(4)) }
    guard !host.isEmpty, path.contains("/") else { return nil }
    return "\(host)/\(path)".lowercased()
}

/// GitHub `/<owner>/<repo>/pull/<n>` or GitLab `/<group…>/<repo>/-/merge_requests/<n>`.
func prRef(_ href: String) -> (repo: String, number: Int)? {
    guard let u = URL(string: href), ["http", "https"].contains(u.scheme ?? ""), let host = u.host else { return nil }
    let p = u.path
    for pat in [#"^/([^/]+/[^/]+)/pull/(\d+)(/|$)"#, #"^/(.+?)/-/merge_requests/(\d+)(/|$)"#] {
        let re = try! NSRegularExpression(pattern: pat)
        if let m = re.firstMatch(in: p, range: NSRange(p.startIndex..., in: p)),
           let r1 = Range(m.range(at: 1), in: p), let r2 = Range(m.range(at: 2), in: p), let n = Int(p[r2]) {
            return ("\(host)/\(p[r1])".lowercased(), n)
        }
    }
    return nil
}

/// A link to a dev server on this machine (0.0.0.0 → localhost), else nil.
func localURL(_ href: String) -> String? {
    guard var c = URLComponents(string: href), ["http", "https"].contains(c.scheme ?? ""),
          let h = c.host, ["localhost", "127.0.0.1", "0.0.0.0", "::1", "[::1]"].contains(h) else { return nil }
    if h == "0.0.0.0" { c.host = "localhost" }
    return c.string
}

// MARK: - quote into composer (transcriptFeedback.insertQuote)

/// `quote` as a markdown blockquote on its own lines appended to `text`; `ask` leaves an empty line after it.
func appendQuote(_ text: String, _ quote: String, ask: Bool = false) -> String {
    let block = quote.trimmingCharacters(in: .whitespacesAndNewlines).components(separatedBy: "\n")
        .map { $0.trimmingCharacters(in: .whitespaces).isEmpty ? ">" : "> \($0)" }.joined(separator: "\n")
    let head = text.isEmpty || text.hasSuffix("\n") ? text : text + "\n"
    return head + block + "\n" + (ask ? "\n" : "")
}

// MARK: - self-check

/// Run standalone: `cat TranscriptLogic.swift > /tmp/t.swift; echo 'transcriptSelfCheck(); print("ok")' >> /tmp/t.swift; swift /tmp/t.swift`
func transcriptSelfCheck() {
    // watermark
    let w: [[String: Any]] = [["role": "user", "at": 1], ["role": "assistant", "at": 5], ["role": "tool", "at": 6], ["role": "assistant", "at": 7]]
    assert(visitWatermark(w, seenAt: 4) == 1)
    assert(visitWatermark(w, seenAt: 6) == nil) // one new agent row
    assert(visitWatermark(w, seenAt: 0) == nil) // everything new
    assert(visitWatermark(w, seenAt: nil) == nil)
    // edits
    assert(editOf(["file_path": "/a", "old_string": "x\ny", "new_string": "z"]) == FileEdit(path: "/a", added: 1, removed: 2, hunks: [.init(old: "x\ny", new: "z")]))
    assert(editOf(["file_path": "/a"]) == nil)
    assert(mergeEdits([FileEdit(path: "a", added: 1, removed: 0, hunks: []), FileEdit(path: "b", added: 1, removed: 0, hunks: []),
                       FileEdit(path: "a", added: 2, removed: 3, hunks: [])]).map { "\($0.path)\($0.added)\($0.removed)" } == ["a33", "b10"])
    assert(fmtDuration(26_400) == "26s" && fmtDuration(125_000) == "2m 5s")
    assert(isProjectPath("src/x", root: "") && isProjectPath("/r/x", root: "/r") && !isProjectPath("/rr/x", root: "/r"))
    assert(hunkLines([.init(old: "a", new: "b"), .init(old: "", new: "c")]).lines.map(\.kind) == ["del", "add", "sep", "add"])
    // turns: turn 1 settled (later turn) folds its tools; last turn still busy stays open
    let t: [[String: Any]] = [
        ["role": "user", "at": 1000, "text": "go"],
        ["role": "tool", "text": "Edit", "at": 2000, "toolInput": ["file_path": "/r/a.swift", "old_string": "", "new_string": "x"]],
        ["role": "tool", "text": "Read", "at": 3000, "toolInput": ["file_path": "/r/b.swift"]],
        ["role": "assistant", "text": "done", "at": 5000],
        ["role": "user", "text": "again", "at": 6000],
        ["role": "tool", "text": "Bash", "at": 7000],
    ]
    let rows = transcriptRows(t, start: 10, busy: true, folded: [:], cwd: "/r")
    assert(rows.map(\.id) == ["m10", "f10", "m13", "c10", "m14", "t15"], "\(rows.map(\.id))")
    if case .fold(_, let label, let folded, let tools, let files) = rows[1] { assert(label == "Worked 4s" && folded && tools == 2 && files == 1) } else { assert(false) }
    let open = transcriptRows(t, start: 10, busy: false, folded: [10: false, 14: false], cwd: "/r")
    assert(open.map(\.id) == ["m10", "f10", "t11", "m13", "c10", "m14", "f14", "t15"], "\(open.map(\.id))")
    // find
    assert(occurrences("Foo foo fOO", "foo").count == 3 && occurrences("aaaa", "aa").count == 2)
    assert(findMatches([["role": "user", "text": "a b a"], ["role": "assistant", "text": "A"]], start: 5, "a") == [5, 5, 6])
    assert(stepFind(2, total: 3, 1) == 0 && stepFind(0, total: 3, -1) == 2 && stepFind(0, total: 0, 1) == 0)
    // links
    assert(resolveLocalLink("src/a.ts:12", baseDir: "/r") == LocalLink(path: "/r/src/a.ts", line: 12))
    assert(resolveLocalLink("file:///x/y.md#L4-L9", baseDir: "/r") == LocalLink(path: "/x/y.md", line: 4))
    assert(resolveLocalLink("../b/c.go", baseDir: "/r/s") == LocalLink(path: "/r/b/c.go", line: 0))
    assert(resolveLocalLink("https://x.dev", baseDir: "/r") == nil && resolveLocalLink("#top", baseDir: "/r") == nil)
    assert(codeSpanIsFileRef("src/a.ts:12") && codeSpanIsFileRef("docs/plan.md") && !codeSpanIsFileRef("a.ts") && !codeSpanIsFileRef("foo bar"))
    let s = "see src/lib/a.ts:12 and http://x/y.ts:3"
    assert(fileRefInText.matches(in: s, range: NSRange(s.startIndex..., in: s)).count == 1)
    assert(repoKey("git@github.com:JB/burrow.git") == "github.com/jb/burrow" && repoKey("https://github.com/JB/burrow.git") == "github.com/jb/burrow")
    assert(prRef("https://github.com/JB/burrow/pull/42/files")! == ("github.com/jb/burrow", 42))
    assert(prRef("https://gitlab.com/g/s/r/-/merge_requests/7")! == ("gitlab.com/g/s/r", 7))
    assert(prRef("https://github.com/JB/burrow/issues/4") == nil)
    assert(localURL("http://0.0.0.0:5173/x") == "http://localhost:5173/x" && localURL("https://github.com") == nil)
    assert(appendQuote("hi", "a\n\nb", ask: true) == "hi\n> a\n>\n> b\n\n")
}
