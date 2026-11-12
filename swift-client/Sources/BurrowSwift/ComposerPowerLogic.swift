import Foundation

// Pure composer helpers (Foundation only, assert-checked by composerPowerSelfCheck):
// prompt history (composerPromptHistory.ts), prompt stash (promptStash.ts), paste markers (composerDom.ts),
// plan usage (planUsage.ts), /context report (contextReport.ts), ACP option lookup (chatTypes.ts acpOption).
// Run: swiftc -o /tmp/cp ComposerPowerLogic.swift <(echo 'composerPowerSelfCheck(); print("ok")') && /tmp/cp

// MARK: prompt history

/// Go-initiated sends that land as user rows but were never typed (sub-agent notices, usage-limit resume).
private let notTyped = try! NSRegularExpression(pattern: #"^(?:Sub-agent .+ \(#\d+\) |Continue where you left off\.$)"#)

/// What the user typed in this thread, oldest first, consecutive repeats folded.
func promptHistory(_ messages: [[String: Any]]) -> [String] {
    var out: [String] = []
    for m in messages where m["role"] as? String == "user" {
        let raw = (m["displayText"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? m["text"] as? String ?? ""
        let p = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if p.isEmpty || out.last == p { continue }
        if notTyped.firstMatch(in: p, range: NSRange(location: 0, length: (p as NSString).length)) != nil { continue }
        out.append(p)
    }
    return out
}

/// Where recall stands: the entry on screen and the draft it replaced.
struct PromptHistoryPos: Equatable { var index: Int; var recalled: String; var draft: String }

/// One step through history; nil = not ours, let the key through. Forward past the newest gives the draft back.
func stepPromptHistory(_ entries: [String], _ pos: PromptHistoryPos?, _ current: String, back: Bool) -> (pos: PromptHistoryPos?, text: String)? {
    let active = pos.flatMap { p in p.recalled == current && entries.indices.contains(p.index) && entries[p.index] == current ? p : nil }
    if back {
        let i = (active?.index ?? entries.count) - 1
        if i < 0 { return nil }
        return (PromptHistoryPos(index: i, recalled: entries[i], draft: active?.draft ?? current), entries[i])
    }
    guard let a = active else { return nil }
    let i = a.index + 1
    if i >= entries.count { return (nil, a.draft) }
    return (PromptHistoryPos(index: i, recalled: entries[i], draft: a.draft), entries[i])
}

func recalling(_ pos: PromptHistoryPos?, _ current: String) -> Bool { pos?.recalled == current }

// MARK: prompt stash

struct StashEntry: Codable, Equatable, Identifiable {
    var id: String
    var createdAt: Double // unix ms, like Vue
    var text: String
    var images: [String]
    var droppedImages: [String]
}

enum Stash {
    static let maxEntries = 20
    static let maxImageChars = 2_700_000

    static func isEmpty(_ text: String, _ images: [String]) -> Bool {
        text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && images.isEmpty
    }

    static func make(_ text: String, _ images: [String], now: Double = Date().timeIntervalSince1970 * 1000) -> StashEntry {
        var kept: [String] = [], dropped: [String] = [], used = 0
        for (i, uri) in images.enumerated() {
            if used + uri.count > maxImageChars { dropped.append("image \(i + 1)"); continue }
            used += uri.count; kept.append(uri)
        }
        return StashEntry(id: "\(Int64(now))-\(UUID().uuidString.prefix(6).lowercased())", createdAt: now, text: text, images: kept, droppedImages: dropped)
    }

    /// Newest first; past the cap the oldest falls off.
    static func push(_ entries: [StashEntry], _ e: StashEntry) -> [StashEntry] { Array(([e] + entries).prefix(maxEntries)) }

    /// Take `id` out; a non-empty composer is stashed first so restoring never loses it.
    static func restore(_ entries: [StashEntry], _ id: String, text: String, images: [String]) -> (entries: [StashEntry], restored: StashEntry?) {
        guard let r = entries.first(where: { $0.id == id }) else { return (entries, nil) }
        let rest = entries.filter { $0.id != id }
        return (isEmpty(text, images) ? rest : push(rest, make(text, images)), r)
    }
}

// MARK: paste markers

/// Invisible markers around a collapsed paste inside the model string (composerDom.ts PASTE_START / PASTE_END).
let pasteStart = "\u{2063}", pasteEnd = "\u{2064}"
/// A paste collapses into a chip past this many lines (`split("\n").length > 4`).
let pasteCollapseLines = 4

func wrapPaste(_ s: String) -> String { pasteStart + s + pasteEnd }
func shouldCollapsePaste(_ s: String) -> Bool { s.components(separatedBy: "\n").count > pasteCollapseLines }
func stripPasteMarkers(_ s: String) -> String { s.replacingOccurrences(of: pasteStart, with: "").replacingOccurrences(of: pasteEnd, with: "") }

/// Plain runs and pasted blocks, in order.
func pasteSegments(_ s: String) -> [(paste: Bool, text: String)] {
    var out: [(Bool, String)] = []
    var rest = Substring(s)
    while let a = rest.range(of: pasteStart), let b = rest[a.upperBound...].range(of: pasteEnd) {
        if a.lowerBound > rest.startIndex { out.append((false, String(rest[..<a.lowerBound]))) }
        out.append((true, String(rest[a.upperBound..<b.lowerBound])))
        rest = rest[b.upperBound...]
    }
    if !rest.isEmpty { out.append((false, String(rest))) }
    return out
}

/// `@path ` appended with sensible spacing; a path with whitespace is quoted (useFileDrop.ts).
func appendRef(_ text: String, path: String) -> String {
    let ref = path.contains(where: \.isWhitespace) ? "@\"\(path)\"" : "@\(path)"
    let sep = text.isEmpty || text.last!.isWhitespace ? "" : " "
    return text + sep + ref + " "
}

// MARK: plan usage

/// The binding constraint: highest percent of any row of any working provider.
func planPeak(_ usage: [String: Any]?) -> Double? {
    var peak: Double?
    for p in usage?["providers"] as? [[String: Any]] ?? [] where p["status"] as? String == "ok" {
        for r in p["rows"] as? [[String: Any]] ?? [] { peak = max(peak ?? 0, (r["percent"] as? NSNumber)?.doubleValue ?? 0) }
    }
    return peak
}

enum UsageLevel { case ok, warning, exceeded }
func usageLevel(_ pct: Double) -> UsageLevel { pct >= 90 ? .exceeded : pct >= 70 ? .warning : .ok }

/// "in 2h 14m" under a day, else "Mon 15:00".
func formatReset(_ iso: String?, now: Date = Date()) -> String {
    guard let iso, !iso.isEmpty else { return "" }
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    guard let t = f.date(from: iso) ?? ISO8601DateFormatter().date(from: iso) else { return "" }
    let ms = t.timeIntervalSince(now)
    if ms <= 0 { return "now" }
    let mins = Int((ms / 60).rounded())
    if mins < 60 { return "in \(mins)m" }
    if mins < 24 * 60 { return "in \(mins / 60)h \(mins % 60)m" }
    let d = DateFormatter()
    d.dateFormat = "EEE HH:mm"
    return d.string(from: t)
}

// MARK: /context report

struct CtxRow: Equatable { let label: String, tokens: String, pct: String }

/// The category table out of Claude Code's `/context` answer; nil when the text is not one.
func parseContextReport(_ text: String) -> [CtxRow]? {
    guard let start = text.range(of: "Estimated usage by category") else { return nil }
    var rows: [CtxRow] = []
    let num = try! NSRegularExpression(pattern: #"^[\d.]+k?$"#)
    for line in text[start.lowerBound...].components(separatedBy: "\n") {
        if line.hasPrefix("###"), !rows.isEmpty { break }
        let c = line.components(separatedBy: "|").map { $0.trimmingCharacters(in: .whitespaces) }
        guard c.count >= 4, num.firstMatch(in: c[2], range: NSRange(location: 0, length: (c[2] as NSString).length)) != nil else { continue }
        rows.append(CtxRow(label: c[1], tokens: c[2], pct: c[3]))
    }
    return rows.isEmpty ? nil : rows
}

// MARK: ACP selectors

/// Adapters name the effort option differently (Codex "effort", ACP category "thought_level", Vibe "thinking").
func acpOption(_ options: [[String: Any]], _ key: String) -> [String: Any]? {
    let efforts: Set<String> = ["thought_level", "thinking"]
    return options.first { $0["id"] as? String == key }
        ?? (key == "effort" ? options.first { efforts.contains($0["category"] as? String ?? "") } : nil)
}

/// (value, label) pairs of one ACP config option.
func acpChoices(_ option: [String: Any]?) -> [(String, String)] {
    (option?["options"] as? [[String: Any]] ?? []).map {
        let v = $0["value"] as? String ?? ""
        let n = $0["name"] as? String ?? ""
        return (v, n.isEmpty ? v : n)
    }
}

func composerPowerSelfCheck() {
    let msgs: [[String: Any]] = [["role": "user", "text": "a"], ["role": "assistant", "text": "x"], ["role": "user", "text": "a"],
                                 ["role": "user", "text": "Continue where you left off."], ["role": "user", "text": "b", "displayText": "B"]]
    let h = promptHistory(msgs)
    assert(h == ["a", "B"])
    let s1 = stepPromptHistory(h, nil, "draft", back: true)!
    assert(s1.text == "B" && s1.pos?.draft == "draft")
    let s2 = stepPromptHistory(h, s1.pos, "B", back: true)!
    assert(s2.text == "a")
    assert(stepPromptHistory(h, s2.pos, "a", back: true) == nil)
    let f1 = stepPromptHistory(h, s2.pos, "a", back: false)!
    assert(f1.text == "B")
    let f2 = stepPromptHistory(h, f1.pos, "B", back: false)!
    assert(f2.text == "draft" && f2.pos == nil)
    assert(stepPromptHistory(h, s1.pos, "edited", back: false) == nil) // an edit makes it a new draft
    assert(stepPromptHistory([], nil, "", back: true) == nil)

    let e = Stash.make("hi", [String(repeating: "x", count: 2_000_000), String(repeating: "y", count: 1_000_000)], now: 1)
    assert(e.images.count == 1 && e.droppedImages == ["image 2"])
    var list = (0..<25).reduce([StashEntry]()) { Stash.push($0, Stash.make("\($1)", [], now: Double($1))) }
    assert(list.count == 20 && list[0].text == "24")
    let r = Stash.restore(list, list[3].id, text: "current", images: [])
    assert(r.restored?.text == "21" && r.entries[0].text == "current" && r.entries.count == 20)
    list = Stash.restore(list, list[0].id, text: " ", images: []).entries
    assert(list.count == 19)

    assert(shouldCollapsePaste("1\n2\n3\n4\n5") && !shouldCollapsePaste("1\n2\n3\n4"))
    let m = "pre " + wrapPaste("a\nb") + " post"
    assert(stripPasteMarkers(m) == "pre a\nb post")
    let seg = pasteSegments(m)
    assert(seg.count == 3 && seg[1].paste && seg[1].text == "a\nb" && seg[2].text == " post")
    assert(appendRef("hi", path: "/a b") == "hi @\"/a b\" " && appendRef("", path: "/x") == "@/x ")

    let u: [String: Any] = ["providers": [["status": "ok", "rows": [["percent": 62], ["percent": 7]]], ["status": "unavailable", "rows": [["percent": 99]]]]]
    assert(planPeak(u) == 62 && planPeak(nil) == nil)
    assert(usageLevel(90) == .exceeded && usageLevel(70) == .warning && usageLevel(10) == .ok)
    let now = Date(timeIntervalSince1970: 0)
    assert(formatReset("1970-01-01T02:14:00Z", now: now) == "in 2h 14m")
    assert(formatReset("1970-01-01T00:05:00.123+00:00", now: now) == "in 5m")

    let rep = "## Context\n**Estimated usage by category**\n| Category | Tokens | Percentage |\n|---|---|---|\n| System prompt | 3.1k | 1.5% |\n| Messages | 120 | 0.1% |\n### MCP tools\n| x | 1k | 1% |"
    assert(parseContextReport(rep) == [CtxRow(label: "System prompt", tokens: "3.1k", pct: "1.5%"), CtxRow(label: "Messages", tokens: "120", pct: "0.1%")])
    assert(parseContextReport("hello") == nil)

    let opts: [[String: Any]] = [["id": "model", "options": [["value": "m1", "name": "M1"]]], ["id": "thinking", "category": "thinking", "options": [["value": "low", "name": ""]]]]
    assert(acpChoices(acpOption(opts, "model")).first! == ("m1", "M1"))
    assert(acpChoices(acpOption(opts, "effort")).first! == ("low", "low"))
}
