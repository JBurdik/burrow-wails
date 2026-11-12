import Foundation

// Pure helpers for the composer and transcript. Foundation only, so they compile into the
// assert check (swiftc ComposerLogic.swift <check>.swift) without the app.

/// The completion token right before the caret: `@path`, `/command` or `$skill` (composerCompletion.ts AT_RE / CMD_RE).
struct ComposerTrigger: Equatable {
    let kind: Character // "@", "/", "$"
    let query: String
    let range: NSRange // the trigger char + query, in UTF-16 offsets of the text
}

func composerTrigger(_ text: String, caret: Int) -> ComposerTrigger? {
    let ns = text as NSString
    guard caret >= 0, caret <= ns.length else { return nil }
    var i = caret
    while i > 0 {
        let c = ns.character(at: i - 1)
        guard let u = Unicode.Scalar(c) else { return nil }
        let ch = Character(u)
        if ch.isWhitespace { return nil }
        if ch == "@" || ch == "/" || ch == "$" {
            // start-of-input or whitespace before the trigger
            if i - 1 > 0, let p = Unicode.Scalar(ns.character(at: i - 2)), !Character(p).isWhitespace {
                // `@` allows `/` inside the path; keep scanning past a slash for @
                if ch == "/" { i -= 1; continue }
                return nil
            }
            let q = ns.substring(with: NSRange(location: i, length: caret - i))
            if ch != "@", q.contains("/") || q.contains("$") { return nil }
            return ComposerTrigger(kind: ch, query: q, range: NSRange(location: i - 1, length: caret - i + 1))
        }
        i -= 1
    }
    return nil
}

/// Subsequence fuzzy score (higher is better), nil = no match. Basename hits and contiguous runs win.
func fuzzyScore(_ query: String, _ candidate: String) -> Int? {
    if query.isEmpty { return 0 }
    let q = Array(query.lowercased()), c = Array(candidate.lowercased())
    let baseStart = (candidate.lastIndex(of: "/").map { candidate.distance(from: candidate.startIndex, to: $0) + 1 }) ?? 0
    var qi = 0, score = 0, prev = -2
    for (ci, ch) in c.enumerated() where qi < q.count && ch == q[qi] {
        score += 1
        if ci == prev + 1 { score += 4 }
        if ci >= baseStart { score += 2 }
        if ci == baseStart || ci == 0 || "/-_.".contains(c[max(ci - 1, 0)]) { score += 3 }
        prev = ci; qi += 1
    }
    return qi == q.count ? score - c.count / 10 : nil
}

/// Ranges of `/name`, `$name`, `@path` tokens, for inline highlighting.
func composerTokenRanges(_ text: String) -> [NSRange] {
    guard let re = try? NSRegularExpression(pattern: #"(?:^|(?<=\s))[/$@][^\s]+"#) else { return [] }
    return re.matches(in: text, range: NSRange(location: 0, length: (text as NSString).length)).map(\.range)
}

/// The reply a filled-in ```questions form sends (chatBlocks.ts formatAnswers).
func formatAnswers(_ answers: [(question: String, picked: [String], other: String)], notes: String = "") -> String {
    let lines = answers.enumerated().map { i, a -> String in
        let other = a.other.trimmingCharacters(in: .whitespacesAndNewlines)
        let parts = a.picked + (other.isEmpty ? [] : [other])
        return "\(i + 1). \(a.question)\n   → \(parts.isEmpty ? "(skipped)" : parts.joined(separator: "; "))"
    }
    let n = notes.trimmingCharacters(in: .whitespacesAndNewlines)
    return "Answers:\n" + lines.joined(separator: "\n") + (n.isEmpty ? "" : "\n\nNotes: \(n)")
}

enum DiffOp: Equatable { case same(String), del(String), add(String) }

/// Line diff via LCS. ponytail: O(n·m), capped at 400×400 lines (beyond that: all-del + all-add).
func lineDiff(_ old: String, _ new: String) -> [DiffOp] {
    let a = old.isEmpty ? [] : old.components(separatedBy: "\n")
    let b = new.isEmpty ? [] : new.components(separatedBy: "\n")
    if a.count > 400 || b.count > 400 { return a.map(DiffOp.del) + b.map(DiffOp.add) }
    var t = Array(repeating: Array(repeating: 0, count: b.count + 1), count: a.count + 1)
    for i in stride(from: a.count - 1, through: 0, by: -1) {
        for j in stride(from: b.count - 1, through: 0, by: -1) {
            t[i][j] = a[i] == b[j] ? t[i + 1][j + 1] + 1 : max(t[i + 1][j], t[i][j + 1])
        }
    }
    var out: [DiffOp] = [], i = 0, j = 0
    while i < a.count, j < b.count {
        if a[i] == b[j] { out.append(.same(a[i])); i += 1; j += 1 }
        else if t[i + 1][j] >= t[i][j + 1] { out.append(.del(a[i])); i += 1 }
        else { out.append(.add(b[j])); j += 1 }
    }
    out += a[i...].map(DiffOp.del) + b[j...].map(DiffOp.add)
    return out
}

/// `| a | b |` → ["a", "b"]; nil for the `|---|:--:|` separator row.
func tableCells(_ line: String) -> [String]? {
    var s = line.trimmingCharacters(in: .whitespaces)
    if s.hasPrefix("|") { s.removeFirst() }
    if s.hasSuffix("|") { s.removeLast() }
    let cells = s.components(separatedBy: "|").map { $0.trimmingCharacters(in: .whitespaces) }
    if cells.allSatisfy({ !$0.isEmpty && $0.allSatisfy { "-:".contains($0) } }) { return nil }
    return cells
}

/// "12. text" → ("12", "text").
func orderedItem(_ line: String) -> (String, String)? {
    let digits = line.prefix { $0.isNumber }
    guard !digits.isEmpty, digits.count < 4 else { return nil }
    let rest = line.dropFirst(digits.count)
    guard rest.hasPrefix(". ") || rest.hasPrefix(") ") else { return nil }
    return (String(digits), String(rest.dropFirst(2)))
}

/// "burrow-image://<transcript index>/<n>" → (index, n) for load_chat_image.
func imageStub(_ s: String) -> (Int, Int)? {
    guard s.hasPrefix("burrow-image://") else { return nil }
    let p = s.dropFirst("burrow-image://".count).split(separator: "/")
    guard p.count == 2, let i = Int(p[0]), let n = Int(p[1]) else { return nil }
    return (i, n)
}

func composerLogicSelfCheck() {
    assert(composerTrigger("hi @src/ma", caret: 10) == ComposerTrigger(kind: "@", query: "src/ma", range: NSRange(location: 3, length: 7)))
    assert(composerTrigger("/comp", caret: 5)?.kind == "/")
    assert(composerTrigger("a/b", caret: 3) == nil)
    assert(composerTrigger("x $sk", caret: 5)?.query == "sk")
    assert(composerTrigger("hello", caret: 5) == nil)
    assert(composerTrigger("mail me@x", caret: 9) == nil)
    assert(fuzzyScore("cmp", "src/Composer.swift") != nil)
    assert(fuzzyScore("zz", "src/Composer.swift") == nil)
    assert(fuzzyScore("comp", "Composer.swift")! > fuzzyScore("comp", "src/c/o/m/p.swift")!)
    assert(composerTokenRanges("run /compact and @a/b.go x$y").count == 2)
    assert(formatAnswers([("Q?", ["A"], ""), ("R?", [], " ")]) == "Answers:\n1. Q?\n   → A\n2. R?\n   → (skipped)")
    assert(lineDiff("a\nb\nc", "a\nx\nc") == [.same("a"), .del("b"), .add("x"), .same("c")])
    assert(lineDiff("", "n") == [.add("n")])
    assert(tableCells("| a | b |") == ["a", "b"])
    assert(tableCells("|---|:-:|") == nil)
    assert(orderedItem("12. go")! == ("12", "go"))
    assert(orderedItem("2024 was") == nil)
    assert(imageStub("burrow-image://7/0")! == (7, 0))
}
