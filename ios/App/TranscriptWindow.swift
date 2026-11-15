import Foundation

/// The newest window of a chat's transcript as Go's revisioned splices build it
/// (ADR 0001 phase B, src-wails/chatsplice.go): rows[0] is transcript index
/// `start`, and the window is current as of splice revision `rev`.
/// Pure: ChatView feeds it wire payloads and asks Go to catch up on `.gap`.
struct TranscriptWindow: Equatable {
    var rows: [Message] = []
    var start = 0
    var rev: Int64 = 0

    enum Outcome: Equatable { case applied, ignored, gap }

    /// A `load_chat_messages_page` reply (or the launch cache's copy of one).
    init(page d: [String: Any]) {
        start = Self.int(d["start"])
        rev = Self.i64(d["rev"])
        rows = (d["messages"] as? [[String: Any]] ?? []).enumerated().map { Message($1, index: start + $0) }
    }
    init() {}

    /// `chat-messages-changed-<id>` {baseRev, rev, fromIndex, messages, authoritative?}:
    /// rows[fromIndex...] = messages, applied on top of baseRev only (or always
    /// when authoritative: rewind, clear, a subscribe ring miss).
    mutating func apply(splice d: [String: Any]) -> Outcome {
        let auth = d["authoritative"] as? Bool ?? false
        guard auth || Self.i64(d["baseRev"]) == rev else { return .gap }
        let from = Self.int(d["fromIndex"])
        let new = (d["messages"] as? [[String: Any]] ?? []).enumerated().map { Message($1, index: from + $0) }
        let end = start + rows.count
        if auth, from < start || from > end {
            // Authoritative from outside the window: it is the window now
            // (rewind / clear before it, or the newest page past a gap).
            start = from; rows = new
        } else if from > end {
            return .gap // chained but past the end: can't happen with Go's fold; catch up
        } else if from < start {
            rows = Array(new.dropFirst(start - from)) // reaches back past the window
        } else {
            rows = Array(rows.prefix(from - start)) + new
        }
        rev = Self.i64(d["rev"])
        return .applied
    }

    /// `chat-tail-<id>` {rev, index, offset, append}: row.text = its first
    /// `offset` UTF-16 units + append. Never bumps rev; a repeat is harmless.
    mutating func apply(tail d: [String: Any]) -> Outcome {
        let tailRev = Self.i64(d["rev"])
        if tailRev < rev { return .ignored } // the splice it predates already carries it
        if tailRev > rev { return .gap }      // a splice went missing
        let i = Self.int(d["index"]) - start
        guard rows.indices.contains(i) else { return .ignored }
        let text = rows[i].text as NSString
        let offset = Self.int(d["offset"])
        guard offset <= text.length else { return .gap } // a tail went missing
        rows[i].text = text.substring(to: offset) + (d["append"] as? String ?? "")
        return .applied
    }

    /// `chat_subscribe(chatId, sinceRev: rev)` → {rev, splices, inProgress}.
    /// Splices this window already has (rev ≤ ours — live events can overtake
    /// the reply) are skipped.
    mutating func apply(subscribe d: [String: Any]) -> Outcome {
        for s in d["splices"] as? [[String: Any]] ?? [] where Self.i64(s["rev"]) > rev {
            if apply(splice: s) == .gap { return .gap }
        }
        let replyRev = Self.i64(d["rev"])
        if replyRev > rev { return .gap }
        // Behind us (live splices overtook the reply): its inProgress is stale too.
        if replyRev == rev, let ip = d["inProgress"] as? [String: Any], let text = ip["text"] as? String {
            let i = Self.int(ip["index"]) - start
            // Tails applied before this reply arrived may already be further on.
            if rows.indices.contains(i), !rows[i].text.hasPrefix(text) { rows[i].text = text }
        }
        return .applied
    }

    /// The launch cache's shape of this window (Offline.swift cachePage).
    var page: [String: Any] { ["start": start, "rev": rev, "messages": rows.map(\.json)] }

    static func int(_ v: Any?) -> Int { (v as? NSNumber)?.intValue ?? 0 }
    static func i64(_ v: Any?) -> Int64 { (v as? NSNumber)?.int64Value ?? 0 }
}

extension Message {
    /// Back to the wire shape, as far as Message reads it (launch cache).
    var json: [String: Any] {
        var d: [String: Any] = ["id": id, "role": role, "text": text]
        if partial { d["partial"] = true }
        if failed { d["toolFailed"] = true }
        if role == "tool" ? !running : !toolOutput.isEmpty { d["toolOutput"] = toolOutput }
        if rawName { d["toolRawName"] = true }
        if plumbing { d["plumbing"] = true }
        if !toolTarget.isEmpty { d["toolInput"] = ["command": toolTarget] }
        if !images.isEmpty { d["images"] = images }
        if !clientId.isEmpty { d["clientId"] = clientId }
        return d
    }
}
