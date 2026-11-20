import SwiftUI

/// What the transcript draws: AgentChat.vue's grouping + turn fold, as a pure
/// projection of the loaded rows (`displayRows`), so streamed splices only
/// recompute it.
enum TranscriptRow: Identifiable, Equatable {
    case message(Message)
    /// 2+ finished tool calls in a row ("Ran 3 commands").
    case tools(id: String, items: [Message])
    /// A settled turn's work (thinking + tools) folded behind one row.
    case worked(turn: Int, tools: Int, folded: Bool)
    /// A prompt Go sent on its own (sub-agent finished, schedule, resume):
    /// a divider inside the turn, not a new turn ("6 min later · Fix CI finished").
    case gap(Message, label: String)

    var id: String {
        switch self {
        case .message(let m): "m\(m.id)"
        case .tools(let id, _): "g\(id)"
        case .worked(let t, _, _): "w\(t)"
        case .gap(let m, _): "m\(m.id)"
        }
    }
}

/// The one rule, mirrored from src/lib/transcriptRows.ts and pinned for both by
/// fixtures/transcript-rows.json (TranscriptRowsTests).
enum Transcript {
    static func isWork(_ m: Message) -> Bool { m.role == "tool" || m.role == "thinking" }
    /// A user row that opens a turn: typed by the user, not sent by Go on its own.
    static func opensTurn(_ m: Message) -> Bool { m.role == "user" && m.autoLabel == nil }

    static func fmtWait(_ ms: Int) -> String {
        let s = Int((Double(ms) / 1000).rounded())
        if s < 60 { return "\(s)s" }
        let m = Int((Double(s) / 60).rounded())
        if m < 60 { return "\(m) min" }
        return m % 60 != 0 ? "\(m / 60) h \(m % 60) min" : "\(m / 60) h"
    }

    /// "6 min later · Fix CI finished"; the wait only when both rows carry `at`.
    static func gapLabel(_ row: Message, prev: Message?) -> String {
        let label = row.autoLabel ?? ""
        guard row.at > 0, let p = prev, p.at > 0, row.at >= p.at else { return label }
        return ["\(fmtWait(row.at - p.at)) later", label].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// Rows without Go's `plumbing` flag were folded before it existed: same rule
    /// as chatfold.go isPlumbingCall.
    static func isLegacyPlumbing(_ m: Message) -> Bool {
        guard m.role == "tool", !m.failed, !m.running else { return false }
        if m.rawName { return ["mcp__burrow__end_turn", "mcp__burrow__end_turn_pending", "ToolSearch"].contains(m.text) }
        return m.text.trimmingCharacters(in: .whitespaces)
            .range(of: #"^burrow[:\s]+end_turn(_pending)?$"#, options: [.regularExpression, .caseInsensitive]) != nil
    }

    /// Never shown: legacy `queued` rows, plumbing calls, empty thinking.
    static func isVisible(_ m: Message) -> Bool {
        m.role != "queued" && !m.plumbing && !isLegacyPlumbing(m) && !(m.role == "thinking" && m.text.isEmpty)
    }

    /// - foldOverride: the user's explicit fold choice per turn (user message id).
    /// - settledLast: the chat is not in flight, so the last turn is settled too.
    static func rows(_ all: [Message], foldOverride: [Int: Bool], settledLast: Bool) -> [TranscriptRow] {
        let msgs = all.filter(isVisible)
        var prev: [Int: Message] = [:]
        for k in msgs.indices.dropFirst() { prev[msgs[k].id] = msgs[k - 1] }
        var out: [TranscriptRow] = []
        var i = 0
        // Rows before the first user message (a window starting mid-turn) are not folded.
        while i < msgs.count, !opensTurn(msgs[i]) { i += 1 }
        out += grouped(Array(msgs[0..<i]), atEnd: i == msgs.count, prev: prev)
        while i < msgs.count {
            let user = msgs[i]; i += 1
            out.append(.message(user))
            var turn: [Message] = []
            while i < msgs.count, !opensTurn(msgs[i]) { turn.append(msgs[i]); i += 1 }
            let hasLater = i < msgs.count
            // A follow-up still running: fold what settled before its gap, draw the rest live.
            var live: [Message] = []
            if !(hasLater || settledLast) {
                let g = turn.lastIndex(where: { $0.role == "user" }) ?? 0
                live = Array(turn[g...]); turn = Array(turn[..<g])
            }
            guard turn.contains(where: isWork) else { out += grouped(turn + live, atEnd: !hasLater, prev: prev); continue }
            let folded = foldOverride[user.id] ?? hasLater
            let toolCount = turn.filter { $0.role == "tool" }.count
            var emitted = false
            var pending: [Message] = []
            for m in turn {
                if isWork(m) {
                    if !emitted {
                        out += grouped(pending, atEnd: false, prev: prev); pending = []
                        out.append(.worked(turn: user.id, tools: toolCount, folded: folded)); emitted = true
                    }
                    if folded { continue }
                }
                pending.append(m)
            }
            out += grouped(pending, atEnd: !hasLater && live.isEmpty, prev: prev) + grouped(live, atEnd: !hasLater, prev: prev)
        }
        return out
    }

    /// Folds runs of 2+ consecutive tool rows into one group; a run that ends
    /// the transcript (`atEnd`) whose last call still runs stays open so the live call shows.
    static func grouped(_ msgs: [Message], atEnd: Bool, prev: [Int: Message]) -> [TranscriptRow] {
        var out: [TranscriptRow] = []
        var i = 0
        while i < msgs.count {
            if msgs[i].role == "user", msgs[i].autoLabel != nil {
                out.append(.gap(msgs[i], label: gapLabel(msgs[i], prev: prev[msgs[i].id]))); i += 1; continue
            }
            guard msgs[i].role == "tool" else { out.append(.message(msgs[i])); i += 1; continue }
            let start = i
            while i < msgs.count, msgs[i].role == "tool" { i += 1 }
            let run = Array(msgs[start..<i])
            let live = atEnd && i == msgs.count && run.last?.running == true
            if run.count < 2 || live { out += run.map(TranscriptRow.message) }
            else { out.append(.tools(id: "\(run[0].id)", items: run)) }
        }
        return out
    }

    /// A shell command, as opposed to any other tool.
    static func isCommand(_ m: Message) -> Bool {
        m.rawName ? m.text == "Bash" : m.text.range(of: #"^(run|execut|bash|\$)"#, options: [.regularExpression, .caseInsensitive]) != nil
    }

    static func groupLabel(_ items: [Message]) -> String {
        let n = items.count, cmds = items.filter(isCommand).count
        let s = { (c: Int) in c == 1 ? "" : "s" }
        if cmds == n { return "Ran \(n) command\(s(n))" }
        if cmds == 0 { return "Used \(n) tool\(s(n))" }
        return "Used \(n) tools and ran \(cmds) command\(s(cmds))"
    }
}

/// "Ran 3 commands ›": opens to the calls themselves; open by default when one failed.
struct ToolGroupRow: View, Equatable {
    let items: [Message]
    static func == (a: Self, b: Self) -> Bool { a.items == b.items }
    @State private var open: Bool?

    var body: some View {
        let failed = items.contains(where: \.failed)
        let isOpen = open ?? failed
        VStack(alignment: .leading, spacing: 8) {
            Button { open = !isOpen } label: {
                HStack(spacing: 6) {
                    Image(systemName: "chevron.right").font(.caption2).rotationEffect(.degrees(isOpen ? 90 : 0))
                    Image(systemName: items.allSatisfy(Transcript.isCommand) ? "terminal" : "wrench.and.screwdriver").font(.caption)
                    Text(Transcript.groupLabel(items)).font(.caption.weight(.medium))
                    if failed { Image(systemName: "exclamationmark.triangle").font(.caption2).foregroundStyle(.red) }
                }
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            if isOpen {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(items) { MessageRow(message: $0).equatable() }
                }
                .padding(.leading, 14)
            }
        }
    }
}

/// "Worked · 5 tool calls ›": a settled turn's thinking and tool rows behind one row.
struct WorkedRow: View {
    let tools: Int
    let folded: Bool
    let toggle: () -> Void

    var body: some View {
        Button(action: toggle) {
            HStack(spacing: 6) {
                Image(systemName: "chevron.right").font(.caption2.weight(.bold)).rotationEffect(.degrees(folded ? 0 : 90))
                Text("Worked").font(.caption.weight(.semibold)).foregroundStyle(.primary)
                if tools > 0 {
                    Text("· \(tools) tool \(tools == 1 ? "call" : "calls")").font(.caption)
                }
                Spacer(minLength: 0)
            }
            .foregroundStyle(.secondary)
            .padding(.vertical, 6).padding(.horizontal, 10)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 8))
        }
        .buttonStyle(.plain)
    }
}

/// "6 min later · Fix CI finished": a thin line between two replies of one turn;
/// tap it to read what Go sent.
struct GapRow: View {
    let message: Message
    let label: String
    @State private var open = false

    var body: some View {
        VStack(spacing: 6) {
            Button { open.toggle() } label: {
                HStack(spacing: 8) {
                    Rectangle().frame(height: 0.5)
                    Text(label).font(.caption).lineLimit(1).fixedSize()
                    Rectangle().frame(height: 0.5)
                }
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            if open {
                Text(message.text).font(.caption).foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

/// The model's thinking: one dim row, open to read it; live while it streams.
struct ThinkingRow: View {
    let message: Message
    @State private var open = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button { open.toggle() } label: {
                HStack(spacing: 6) {
                    Image(systemName: "chevron.right").font(.caption2).rotationEffect(.degrees(open ? 90 : 0))
                    Image(systemName: "brain").font(.caption)
                    Text(message.partial ? "Thinking…" : "Thinking").font(.caption.weight(.medium))
                    if !open, message.partial, let last = message.text.split(separator: "\n").last {
                        Text(last).font(.caption).lineLimit(1)
                    }
                }
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            if open {
                Text(message.text).font(.caption).italic().foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .padding(.leading, 14)
            }
        }
    }
}
