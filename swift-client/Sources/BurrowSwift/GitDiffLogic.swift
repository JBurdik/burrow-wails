import Foundation

// OWNER: gitdiff (wave 2). Pure logic for the Diff surface + transcript review comments — no SwiftUI,
// checkable with gitDiffSelfCheck() at the bottom.

// MARK: unified diff → files (what @pierre/diffs' parsePatchFiles gives DiffTab.vue)

struct DiffLine: Hashable {
    enum Kind { case hunk, ctx, add, del, meta }
    let kind: Kind
    let text: String // without the +/-/space prefix (hunk/meta: whole line)
    let old: Int     // 0 = none
    let new: Int
    /// Review-comment anchor, as @pierre/diffs' AnnotationSide + line number. nil = not commentable.
    var anchor: (side: String, line: Int)? {
        switch kind {
        case .del: ("deletions", old)
        case .add, .ctx: ("additions", new)
        default: nil
        }
    }
    static func == (a: DiffLine, b: DiffLine) -> Bool { a.kind == b.kind && a.text == b.text && a.old == b.old && a.new == b.new }
    func hash(into h: inout Hasher) { h.combine(text); h.combine(old); h.combine(new) }
}

struct DiffFile: Identifiable {
    var id: String { name }
    var name = ""
    var lines: [DiffLine] = []
    var raw = ""
    var adds: Int { lines.filter { $0.kind == .add }.count }
    var dels: Int { lines.filter { $0.kind == .del }.count }
    /// Content identity (reviewReceipts.ts diffRevision): a new edit clears the "seen" receipt.
    var revision: String { diffRevision(raw) }
}

func parseDiffFiles(_ diff: String) -> [DiffFile] {
    var out: [DiffFile] = []
    var cur: DiffFile?
    var rawLines: [String] = []
    var inHunk = false, old = 0, new = 0
    func flush() {
        guard var f = cur else { return }
        f.raw = rawLines.joined(separator: "\n")
        out.append(f)
    }
    var lines = diff.components(separatedBy: "\n")
    if lines.last == "" { lines.removeLast() }
    for l in lines {
        if l.hasPrefix("diff --git ") || cur == nil {
            flush()
            cur = DiffFile()
            rawLines = []
            inHunk = false
            if let r = l.range(of: " b/", options: .backwards) { cur!.name = String(l[r.upperBound...]) }
        }
        rawLines.append(l)
        if l.hasPrefix("diff --git ") { continue }
        if l.hasPrefix("@@") {
            inHunk = true
            (old, new) = hunkStarts(l)
            cur!.lines.append(DiffLine(kind: .hunk, text: l, old: 0, new: 0))
            continue
        }
        if !inHunk {
            if l.hasPrefix("+++ "), l != "+++ /dev/null" { cur!.name = stripPrefix(String(l.dropFirst(4)), "b/") }
            else if l.hasPrefix("--- "), l != "--- /dev/null", cur!.name.isEmpty { cur!.name = stripPrefix(String(l.dropFirst(4)), "a/") }
            else if l.hasPrefix("Binary files") || l.hasPrefix("new file") || l.hasPrefix("deleted file") || l.hasPrefix("rename ") {
                cur!.lines.append(DiffLine(kind: .meta, text: l, old: 0, new: 0))
            }
            continue
        }
        let body = String(l.dropFirst())
        switch l.first {
        case "+": new += 1; cur!.lines.append(DiffLine(kind: .add, text: body, old: 0, new: new - 1))
        case "-": old += 1; cur!.lines.append(DiffLine(kind: .del, text: body, old: old - 1, new: 0))
        case "\\": cur!.lines.append(DiffLine(kind: .meta, text: l, old: 0, new: 0))
        default: old += 1; new += 1; cur!.lines.append(DiffLine(kind: .ctx, text: body, old: old - 1, new: new - 1))
        }
    }
    flush()
    return out.filter { !$0.name.isEmpty || !$0.lines.isEmpty }
}

private func stripPrefix(_ s: String, _ p: String) -> String { s.hasPrefix(p) ? String(s.dropFirst(p.count)) : s }

/// "@@ -12,3 +14,5 @@" → (12, 14)
private func hunkStarts(_ h: String) -> (Int, Int) {
    let parts = h.split(separator: " ")
    func start(_ prefix: Character) -> Int {
        guard let p = parts.first(where: { $0.first == prefix }) else { return 1 }
        return Int(p.dropFirst().split(separator: ",").first ?? "") ?? 1
    }
    return (start("-"), start("+"))
}

// MARK: review receipts (src/lib/reviewReceipts.ts)

func diffRevision(_ text: String) -> String {
    var a: UInt32 = 0x811c9dc5, b: UInt32 = 0x9e3779b9, n = 0
    for u in text.utf16 {
        a = (a ^ UInt32(u)) &* 0x01000193
        b = (b ^ UInt32(u)) &* 0x85ebca6b
        n += 1
    }
    return "\(n):\(a):\(b)"
}

/// New edits clear a receipt; returning to an earlier diff cannot revive it.
func currentReceipts(_ receipts: [String: String], _ revisions: [String: String]) -> [String: String] {
    receipts.filter { revisions[$0.key] == $0.value }
}

// MARK: diff review notes (DiffTab.vue quoteFromRange / addNote)

/// Lines `a...b` of one file → the note's quote (only lines on the start line's side, like Vue).
func diffQuote(_ lines: [DiffLine], _ a: Int, _ b: Int) -> String {
    let lo = min(a, b), hi = max(a, b)
    guard lines.indices.contains(lo), let side = lines[lo].anchor?.side else { return "" }
    return lines[lo...min(hi, lines.count - 1)].filter { $0.anchor?.side == side }.map(\.text).joined(separator: "\n")
}

func diffNoteBody(quote: String, comment: String) -> String {
    quote.isEmpty ? comment : "```\n\(quote)\n```\n\n\(comment)"
}

// MARK: transcript feedback (src/lib/transcriptFeedback.ts)

struct TranscriptQuote: Codable, Hashable {
    var quote: String
    var before: String
    var after: String
}

struct TranscriptNote: Codable, Identifiable, Hashable {
    var id: String
    var messageId: Int64
    var source: String
    var quote: String
    var before: String
    var after: String
    var body: String
    var delivery: String?
    var resolvedAt: Int64?
}

func composeTranscriptFeedback(_ notes: [TranscriptNote], request: String = "") -> String {
    if notes.isEmpty { return request }
    let feedback = notes.enumerated().map { i, n in
        [
            "### Comment \(i + 1)",
            "Quoted passage from your earlier response:",
            n.quote.components(separatedBy: "\n").map { "> \($0)" }.joined(separator: "\n"),
            n.before.isEmpty ? "" : "Context before: \(n.before)",
            n.after.isEmpty ? "" : "Context after: \(n.after)",
            "Feedback: \(n.body)",
        ].filter { !$0.isEmpty }.joined(separator: "\n\n")
    }.joined(separator: "\n\n")
    let extra = request.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "" : "\n\nAdditional request:\n\(request)"
    return "Please address these review comments on your earlier responses. Answer each comment in order and make any requested changes.\n\n\(feedback)\(extra)"
}

/// A selection (UTF-16 `range`, i.e. NSTextView's selectedRange) of a rendered message → quote + 100 chars of context.
func transcriptQuote(_ text: String, _ range: NSRange) -> TranscriptQuote? {
    let ns = text as NSString
    guard range.length > 0, NSMaxRange(range) <= ns.length else { return nil }
    let quote = ns.substring(with: range)
    guard !quote.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
    let before = ns.substring(to: range.location), after = ns.substring(from: NSMaxRange(range))
    return TranscriptQuote(quote: quote, before: String(before.suffix(100)), after: String(after.prefix(100)))
}

/// Insert `quote` as a markdown blockquote into `text` at `caret` (UTF-16 offset), on its own lines.
/// `ask` leaves an empty line after it for the question. Returns the new text + caret (UTF-16).
func insertQuote(_ text: String, caret: Int, quote: String, ask: Bool = false) -> (text: String, caret: Int) {
    let ns = text as NSString
    let at = max(0, min(caret, ns.length))
    let before = ns.substring(to: at), after = ns.substring(from: at)
    let block = quote.trimmingCharacters(in: .whitespacesAndNewlines).components(separatedBy: "\n")
        .map { $0.trimmingCharacters(in: .whitespaces).isEmpty ? ">" : "> \($0)" }.joined(separator: "\n")
    let head = !before.isEmpty && !before.hasSuffix("\n") ? before + "\n" : before
    let insert = block + "\n" + (ask ? "\n" : "")
    let tail = !after.isEmpty && !after.hasPrefix("\n") ? "\n" + after : after
    return (head + insert + tail, (head as NSString).length + (insert as NSString).length)
}

// MARK: self-check

/// Run with: swiftc -o /tmp/gdcheck GitDiffLogic.swift <(echo 'gitDiffSelfCheck()') && /tmp/gdcheck
func gitDiffSelfCheck() {
    let d = """
    diff --git a/a.txt b/a.txt
    index 1..2 100644
    --- a/a.txt
    +++ b/a.txt
    @@ -3,3 +3,4 @@ ctx
     keep
    --- gone
    +added
    +more
     tail
    diff --git a/new.bin b/new.bin
    new file mode 100644
    Binary files /dev/null and b/new.bin differ
    diff --git a/old.txt b/old.txt
    deleted file mode 100644
    --- a/old.txt
    +++ /dev/null
    @@ -1 +0,0 @@
    -bye
    \\ No newline at end of file

    """
    let fs = parseDiffFiles(d)
    assert(fs.map(\.name) == ["a.txt", "new.bin", "old.txt"], "\(fs.map(\.name))")
    let a = fs[0]
    assert(a.adds == 2 && a.dels == 1)
    assert(a.lines[1] == DiffLine(kind: .ctx, text: "keep", old: 3, new: 3))
    assert(a.lines[2] == DiffLine(kind: .del, text: "-- gone", old: 4, new: 0), "a deleted '--- ' line is a del, not a header")
    assert(a.lines[3] == DiffLine(kind: .add, text: "added", old: 0, new: 4))
    assert(a.lines[5] == DiffLine(kind: .ctx, text: "tail", old: 5, new: 6))
    assert(a.lines[2].anchor! == ("deletions", 4) && a.lines[3].anchor! == ("additions", 4))
    assert(fs[1].lines.first?.kind == .meta && fs[2].dels == 1)
    assert(diffQuote(a.lines, 3, 5) == "added\nmore\ntail")
    assert(diffQuote(a.lines, 2, 3) == "-- gone", "only the start line's side")
    assert(diffNoteBody(quote: "x", comment: "fix") == "```\nx\n```\n\nfix")
    assert(fs[0].revision != fs[2].revision && fs[0].revision == parseDiffFiles(d)[0].revision)
    assert(currentReceipts(["a": "1", "b": "2"], ["a": "1", "b": "3"]) == ["a": "1"])
    assert(parseDiffFiles("").isEmpty)

    let q = transcriptQuote("hello brave world", NSRange(location: 6, length: 5))!
    assert(q == TranscriptQuote(quote: "brave", before: "hello ", after: " world"))
    assert(transcriptQuote("x", NSRange(location: 0, length: 0)) == nil)
    let r = insertQuote("ab", caret: 1, quote: "q1\n\nq2", ask: true)
    assert(r.text == "a\n> q1\n>\n> q2\n\n\nb" && r.caret == 15, "\(r)")
    assert(insertQuote("", caret: 0, quote: "x").text == "> x\n")
    let n = TranscriptNote(id: "1", messageId: 1, source: "s", quote: "a\nb", before: "", after: "z", body: "why")
    assert(composeTranscriptFeedback([n]).contains("> a\n> b\n\nContext after: z\n\nFeedback: why"))
    assert(composeTranscriptFeedback([], request: "r") == "r")
    print("gitDiffSelfCheck ok")
}
