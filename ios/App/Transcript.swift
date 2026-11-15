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

    var id: String {
        switch self {
        case .message(let m): "m\(m.id)"
        case .tools(let id, _): "g\(id)"
        case .worked(let t, _, _): "w\(t)"
        }
    }
}

enum Transcript {
    static func isWork(_ m: Message) -> Bool { m.role == "tool" || m.role == "thinking" }

    /// - foldOverride: the user's explicit fold choice per turn (user message id).
    /// - settledLast: the chat is not in flight, so the last turn is settled too.
    static func rows(_ msgs: [Message], foldOverride: [Int: Bool], settledLast: Bool) -> [TranscriptRow] {
        var out: [TranscriptRow] = []
        var i = 0
        // Rows before the first user message (a window starting mid-turn) are not folded.
        while i < msgs.count, msgs[i].role != "user" { out += grouped(Array(msgs[i...i])); i += 1 }
        while i < msgs.count {
            let user = msgs[i]; i += 1
            out.append(.message(user))
            var turn: [Message] = []
            while i < msgs.count, msgs[i].role != "user" { turn.append(msgs[i]); i += 1 }
            let hasLater = i < msgs.count
            let settled = hasLater || settledLast
            let toolCount = turn.filter { $0.role == "tool" }.count
            guard settled, turn.contains(where: isWork) else { out += grouped(turn); continue }
            let folded = foldOverride[user.id] ?? hasLater
            var emitted = false
            var pending: [Message] = []
            for m in turn {
                if isWork(m) {
                    if !emitted { out.append(.worked(turn: user.id, tools: toolCount, folded: folded)); emitted = true }
                    if folded { continue }
                }
                pending.append(m)
            }
            out += grouped(pending)
        }
        return out
    }

    /// Folds runs of 2+ consecutive tool rows into one group; a trailing run
    /// whose last call is still running stays open so the live call shows.
    static func grouped(_ msgs: [Message]) -> [TranscriptRow] {
        var out: [TranscriptRow] = []
        var i = 0
        while i < msgs.count {
            guard msgs[i].role == "tool" else { out.append(.message(msgs[i])); i += 1; continue }
            let start = i
            while i < msgs.count, msgs[i].role == "tool" { i += 1 }
            let run = Array(msgs[start..<i])
            let live = i == msgs.count && run.last?.partial == true
            if run.count < 2 || live { out += run.map(TranscriptRow.message) }
            else { out.append(.tools(id: "\(run[0].id)", items: run)) }
        }
        return out
    }

    static func groupLabel(_ items: [Message]) -> String {
        let n = items.count, bash = items.filter { $0.text == "Bash" }.count
        let s = { (c: Int) in c == 1 ? "" : "s" }
        if bash == n { return "Ran \(n) command\(s(n))" }
        if bash == 0 { return "Used \(n) tool\(s(n))" }
        return "Used \(n) tools and ran \(bash) command\(s(bash))"
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
                    Image(systemName: items.allSatisfy { $0.text == "Bash" } ? "terminal" : "wrench.and.screwdriver").font(.caption)
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
