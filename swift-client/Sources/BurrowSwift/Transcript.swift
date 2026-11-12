import AppKit
import SwiftUI

struct MessageView: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.transcript) private var t
    let m: JSON
    @State private var open = false
    @State private var hover = false

    /// The chat this transcript belongs to (a sub-agent's in the RP), else the main pane's.
    var chatID: Int64? { t.chatID > 0 ? t.chatID : b.selectedChat }

    var body: some View {
        switch m.s("role") {
        case "user":
            HStack(alignment: .bottom, spacing: 6) {
                Spacer(minLength: 80)
                HStack(spacing: 8) {
                    CopyButton(text: m.s("text"))
                    HoverAction(icon: "pencil", help: "Edit — put this text back in the composer") { edit() }
                }
                .opacity(hover ? 1 : 0)
                VStack(alignment: .trailing, spacing: 6) {
                    if let imgs = m["images"] as? [String], !imgs.isEmpty {
                        HStack(spacing: 6) { ForEach(imgs.indices, id: \.self) { TranscriptImage(src: imgs[$0], index: $0) } }
                    }
                    if !m.s("text").isEmpty {
                        Text(highlighted(m.s("text"), t.find))
                            .textSelection(.enabled)
                            .padding(.horizontal, 14).padding(.vertical, 10)
                            .background(Theme.accent.opacity(0.16), in: RoundedRectangle(cornerRadius: 16))
                            .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Theme.accent.opacity(0.22)))
                    }
                }
            }
            .onHover { hover = $0 }
        case "assistant":
            VStack(alignment: .leading, spacing: 4) {
                Markdown(text: m.s("text"))
                if m["partial"] as? Bool == true { Caret() }
                else {
                    HStack(spacing: 10) {
                        CopyButton(text: m.s("text"))
                        HoverAction(icon: "text.quote", help: "Quote into the composer") { quote() }
                        if let chatID { CommentAction(chatID: chatID, m: m) }
                    }
                    .opacity(hover ? 1 : 0)
                    if let chatID { ReviewNoteList(chatID: chatID, messageID: m.i("id"), source: m.s("text")) }
                }
            }
            .onHover { hover = $0 }
        case "thinking":
            DisclosureGroup(isExpanded: $open) {
                Text(highlighted(m.s("text"), t.find)).font(.ui(-1)).foregroundStyle(Theme.text2).textSelection(.enabled)
                    .padding(.leading, 12)
                    .overlay(alignment: .leading) { Rectangle().fill(Theme.accent.opacity(0.35)).frame(width: 2) }
            } label: {
                HStack(spacing: 6) {
                    Label("Thinking", systemImage: "sparkle").font(.brand(-3)).foregroundStyle(Theme.accent.opacity(0.8))
                    if m["partial"] as? Bool == true { Caret() }
                }
            }
        case "permission":
            Label(m.s("text").isEmpty ? "Permission request" : m.s("text"), systemImage: "hand.raised")
                .font(.brand(-3)).foregroundStyle(Theme.yellow)
        case "system-info":
            NoteRow(m: m)
        default:
            if !m.s("text").isEmpty {
                Text(m.s("text")).font(.brand(-3)).foregroundStyle(Theme.muted)
            }
        }
    }

    /// AgentChat.editMessage: the text (and inline images) back into this chat's composer, nothing sent.
    func edit() {
        guard let id = chatID else { return }
        ComposerStore.shared.setDraft(id, m.s("text"))
        let imgs = (m["images"] as? [String] ?? []).filter { $0.hasPrefix("data:") }
        if !imgs.isEmpty { ComposerStore.shared.images[id] = imgs }
    }

    /// The whole reply as a `> quote` in the draft, with a line left for the question (transcriptFeedback.insertQuote).
    // ponytail: quotes the whole message — SwiftUI's Text selection isn't readable; an NSTextView row would allow a selection quote.
    func quote() {
        guard let id = chatID else { return }
        ComposerStore.shared.setDraft(id, appendQuote(ComposerStore.shared.drafts[id] ?? "", m.s("text"), ask: true))
    }
}

struct HoverAction: View {
    let icon: String
    let help: String
    let action: () -> Void
    var body: some View {
        Button(action: action) { Image(systemName: icon).font(.system(size: 11)).foregroundStyle(Theme.muted) }
            .buttonStyle(.plain).help(help)
    }
}

/// Go-written chat-note rows: a spawned sub-agent (clickable) or a Right Panel surface (plan, outputs, tasks).
struct NoteRow: View {
    @EnvironmentObject var b: Burrow
    let m: JSON

    var body: some View {
        let sub = m.i("subagentChatId")
        HStack(spacing: 8) {
            Image(systemName: sub > 0 ? "person.2" : surfaceIcon(m.s("surface"))).foregroundStyle(sub > 0 ? Theme.accent : Theme.muted)
            Text(m.s("text")).lineLimit(2).foregroundStyle(Theme.text2)
            if sub > 0 {
                Circle().fill(Theme.status(b.state(sub))).frame(width: 7, height: 7)
                if !m.s("subagentAgent").isEmpty { Text(m.s("subagentAgent")).foregroundStyle(Theme.muted) }
                Spacer()
                Button("Open") { openSubagent(sub) }.buttonStyle(.plain).foregroundStyle(Theme.accent)
            }
        }
        .font(.brand(-3))
        .padding(.horizontal, sub > 0 ? 12 : 0).padding(.vertical, sub > 0 ? 8 : 0)
        .background(sub > 0 ? Theme.panel.opacity(0.6) : .clear, in: RoundedRectangle(cornerRadius: 10))
        .contentShape(Rectangle())
        .onTapGesture { if sub == 0, !m.s("surface").isEmpty { openRightPanelSurface(m.s("surface")) } }
    }

    func surfaceIcon(_ s: String) -> String {
        switch s {
        case "plan": "list.bullet.clipboard"
        case "outputs", "changes": "doc.on.doc"
        case "tasks": "gearshape.2"
        default: "info.circle"
        }
    }
}

/// Data URL inline, or a page stub (`burrow-image://<index>/<n>`) fetched with load_chat_image.
struct TranscriptImage: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.transcript) private var t
    let src: String
    let index: Int
    @State private var img: NSImage?

    var body: some View {
        Group {
            if let img {
                Image(nsImage: img).resizable().scaledToFit().frame(maxWidth: 220, maxHeight: 160)
                    .clipShape(RoundedRectangle(cornerRadius: 10))
                    .onTapGesture { openImageLightbox(img) }
            } else {
                RoundedRectangle(cornerRadius: 10).fill(Theme.panel).frame(width: 80, height: 60)
            }
        }
        .task(id: src) {
            if let i = nsImage(dataURL: src) { img = i; return }
            guard let (idx, n) = imageStub(src), let chat = t.chatID > 0 ? t.chatID : b.selectedChat,
                  let s = try? await b.call("load_chat_image", ["chatId": chat, "index": idx, "image": n]) as? String else { return }
            img = nsImage(dataURL: s)
        }
    }

}

struct Caret: View {
    var body: some View {
        TimelineView(.periodic(from: .now, by: 0.5)) { ctx in
            RoundedRectangle(cornerRadius: 1).fill(Theme.accent)
                .frame(width: 7, height: 14)
                .opacity(Int(ctx.date.timeIntervalSinceReferenceDate * 2) % 2 == 0 ? 0.9 : 0.25)
        }
    }
}

struct CopyButton: View {
    let text: String
    @State private var done = false

    var body: some View {
        Button {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(text, forType: .string)
            done = true
            Task { try? await Task.sleep(for: .seconds(1.2)); done = false }
        } label: {
            Image(systemName: done ? "checkmark" : "doc.on.doc").font(.system(size: 11)).foregroundStyle(done ? Theme.green : Theme.muted)
        }
        .buttonStyle(.plain).help("Copy")
    }
}

// MARK: - markdown

enum MDBlock {
    case code(lang: String, body: String)
    case heading(Int, String)
    case bullet(CGFloat, String)
    case ordered(CGFloat, String, String)
    case quote(String)
    case table([[String]])
    case para(String)
}

/// Line-based block parser: fences, headings, lists, quotes, tables, paragraphs.
func mdBlocks(_ text: String) -> [MDBlock] {
    var out: [MDBlock] = [], para: [String] = [], table: [[String]] = []
    var fence: (lang: String, lines: [String])?
    func flush() {
        if !para.isEmpty { out.append(.para(para.joined(separator: "\n"))); para = [] }
        if !table.isEmpty { out.append(.table(table)); table = [] }
    }
    for raw in text.components(separatedBy: "\n") {
        let l = raw.trimmingCharacters(in: .whitespaces)
        if var f = fence {
            if l.hasPrefix("```") { out.append(.code(lang: f.lang, body: f.lines.joined(separator: "\n"))); fence = nil }
            else { f.lines.append(raw); fence = f }
            continue
        }
        if l.hasPrefix("```") { flush(); fence = (String(l.dropFirst(3)).trimmingCharacters(in: .whitespaces), []); continue }
        if l.hasPrefix("|") {
            if !para.isEmpty { flush() }
            if let cells = tableCells(l) { table.append(cells) }
            continue
        }
        if !table.isEmpty { flush() }
        let indent = CGFloat(raw.prefix { $0 == " " }.count) * 6
        if l.isEmpty { flush() }
        else if l.hasPrefix("#"), l.drop(while: { $0 == "#" }).first == " " {
            flush(); let n = l.prefix { $0 == "#" }.count
            out.append(.heading(n, String(l.dropFirst(n)).trimmingCharacters(in: .whitespaces)))
        } else if l.hasPrefix("- ") || l.hasPrefix("* ") || l.hasPrefix("+ ") { flush(); out.append(.bullet(indent, String(l.dropFirst(2)))) }
        else if let (n, rest) = orderedItem(l) { flush(); out.append(.ordered(indent, n, rest)) }
        else if l.hasPrefix(">") { flush(); out.append(.quote(String(l.drop { $0 == ">" || $0 == " " }))) }
        else { para.append(l) }
    }
    if let f = fence { out.append(.code(lang: f.lang, body: f.lines.joined(separator: "\n"))) } // still streaming
    flush()
    return out
}

struct Markdown: View {
    @Environment(\.transcript) private var t
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(mdBlocks(text).enumerated()), id: \.offset) { _, blk in block(blk) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    func inline(_ s: String) -> AttributedString { BurrowSwift.inline(s, find: t.find) }

    @ViewBuilder func block(_ blk: MDBlock) -> some View {
        switch blk {
        case .code(let lang, let body):
            switch lang {
            case "mermaid": MermaidView(source: body)
            case "visual": if let spec = parseJSON(body) { VisualView(spec: spec) } else { CodeBlock(lang: lang, code: body) }
            case "questions": if let spec = parseJSON(body) { QuestionsForm(spec: spec) } else { CodeBlock(lang: lang, code: body) }
            default: CodeBlock(lang: lang, code: body)
            }
        case .heading(let level, let s):
            Text(inline(s))
                .font(level == 1 ? .ui(7, weight: .bold) : level == 2 ? .ui(4, weight: .bold) : .ui(1, weight: .semibold))
                .padding(.top, 4)
        case .bullet(let indent, let s):
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text("•").foregroundStyle(Theme.accent)
                Text(inline(s)).textSelection(.enabled)
            }.padding(.leading, indent)
        case .ordered(let indent, let n, let s):
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text("\(n).").font(.brand(-1)).foregroundStyle(Theme.accent).frame(minWidth: 18, alignment: .trailing)
                Text(inline(s)).textSelection(.enabled)
            }.padding(.leading, indent)
        case .quote(let s):
            Text(inline(s))
                .foregroundStyle(Theme.text2)
                .padding(.leading, 12)
                .overlay(alignment: .leading) { Rectangle().fill(Theme.accent.opacity(0.5)).frame(width: 3) }
        case .table(let rows):
            MDTable(rows: rows)
        case .para(let s):
            Text(inline(s)).textSelection(.enabled).lineSpacing(3)
        }
    }
}

/// Inline markdown; `code` spans get Burrow's tinted code look (AgentChat .md-body code). `path:line` mentions
/// and file-path code spans become `burrow-file:` links (TranscriptList's openURL opens them in the RP);
/// `find` paints the ⌘F occurrences.
func inline(_ s: String, find: String = "") -> AttributedString {
    var a = (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
    for run in a.runs where run.inlinePresentationIntent?.contains(.code) == true {
        a[run.range].font = .brand(-2)
        a[run.range].foregroundColor = Theme.accent
        a[run.range].backgroundColor = Theme.accent.opacity(0.12)
        let code = String(a[run.range].characters)
        if run.link == nil, codeSpanIsFileRef(code) { a[run.range].link = fileLink(code) }
    }
    // file:line refs in prose (not inside a link or code span)
    let plain = String(a.characters)
    if plain.contains(where: \.isNumber) {
        for m in fileRefInText.matches(in: plain, range: NSRange(plain.startIndex..., in: plain)) {
            guard let r = Range(m.range, in: plain) else { continue }
            let lo = a.characters.index(a.startIndex, offsetBy: plain.distance(from: plain.startIndex, to: r.lowerBound))
            let hi = a.characters.index(lo, offsetBy: plain.distance(from: r.lowerBound, to: r.upperBound))
            if a[lo..<hi].runs.contains(where: { $0.link != nil || $0.inlinePresentationIntent?.contains(.code) == true }) { continue }
            a[lo..<hi].link = fileLink(String(plain[r]))
        }
    }
    for run in a.runs where run.link != nil { a[run.range].foregroundColor = Theme.accent }
    highlight(&a, find)
    return a
}

func fileLink(_ ref: String) -> URL? {
    URL(string: "burrow-file:" + (ref.trimmingCharacters(in: .whitespaces).addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? ""))
}

func parseJSON(_ s: String) -> JSON? {
    (try? JSONSerialization.jsonObject(with: Data(s.utf8))) as? JSON
}

struct CodeBlock: View {
    let lang: String
    let code: String
    @State private var hover = false

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Text(code.trimmingCharacters(in: .newlines))
                .font(.ui(-2, .monospaced))
                .textSelection(.enabled)
                .padding(10).padding(.trailing, 20)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.terminal, in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
        .overlay(alignment: .topTrailing) {
            HStack(spacing: 6) {
                if !lang.isEmpty { Text(lang).font(.brand(-5)).foregroundStyle(Theme.muted) }
                CopyButton(text: code)
            }
            .padding(6).opacity(hover ? 1 : 0)
        }
        .onHover { hover = $0 }
    }
}

/// Header row + body rows as a real grid.
struct MDTable: View {
    @Environment(\.transcript) private var t
    let rows: [[String]]

    var body: some View {
        let cols = rows.map(\.count).max() ?? 0
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 0, verticalSpacing: 0) {
                ForEach(rows.indices, id: \.self) { r in
                    GridRow {
                        ForEach(0..<cols, id: \.self) { c in
                            Text(inline(c < rows[r].count ? rows[r][c] : "", find: t.find))
                                .font(r == 0 ? .ui(-1, weight: .semibold) : .ui(-1))
                                .textSelection(.enabled)
                                .padding(.horizontal, 10).padding(.vertical, 6)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .background(r == 0 ? Theme.panel : r % 2 == 0 ? Theme.panel.opacity(0.35) : .clear)
                        }
                    }
                    if r < rows.count - 1 { Divider().overlay(Theme.border) }
                }
            }
        }
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
        .clipShape(RoundedRectangle(cornerRadius: 10))
    }
}

// MARK: - tools

/// A run of tool calls. TodoWrite (latest) and Task / Agent render as cards; the rest fold into one row.
struct ToolRun: View {
    let tools: [JSON]
    @State private var open = false

    var body: some View {
        let rest = tools.filter { !isCard($0.s("text")) }
        let todo = tools.last { $0.s("text") == "TodoWrite" }
        let agents = tools.filter { ["Task", "Agent"].contains($0.s("text")) }
        VStack(alignment: .leading, spacing: 8) {
            if !rest.isEmpty {
                DisclosureGroup(isExpanded: $open) {
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(rest.indices, id: \.self) { ToolRow(t: rest[$0]) }
                    }
                    .padding(.top, 4)
                } label: {
                    HStack(spacing: 8) {
                        Image(systemName: icon(rest.last!.s("text"))).foregroundStyle(Theme.accent).frame(width: 16)
                        if rest.count > 1 {
                            Text("\(rest.count)").font(.brand(-4, weight: .bold)).foregroundStyle(Theme.base)
                                .padding(.horizontal, 5).padding(.vertical, 1)
                                .background(Theme.accent.opacity(0.8), in: RoundedRectangle(cornerRadius: 4))
                        }
                        Text(summary(rest.last!)).lineLimit(1).foregroundStyle(Theme.text2)
                        if rest.contains(where: { $0["toolFailed"] as? Bool == true }) {
                            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(Theme.red)
                        }
                    }
                    .font(.brand(-3))
                }
                .padding(.horizontal, 12).padding(.vertical, 8)
                .background(Theme.panel.opacity(0.6), in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border.opacity(0.8)))
            }
            ForEach(agents.indices, id: \.self) { SubagentCard(t: agents[$0]) }
            if let todo { TodoCard(input: todo["toolInput"] as? JSON ?? [:]) }
        }
    }

    func isCard(_ name: String) -> Bool { ["TodoWrite", "Task", "Agent"].contains(name) }
}

struct ToolRow: View {
    let t: JSON
    @State private var open = false

    var body: some View {
        let input = t["toolInput"] as? JSON ?? [:]
        DisclosureGroup(isExpanded: $open) {
            ToolDetail(name: t.s("text"), input: input, output: t.s("toolOutput"))
                .padding(.vertical, 4)
        } label: {
            HStack(spacing: 6) {
                Image(systemName: icon(t.s("text")))
                    .foregroundStyle(t["toolFailed"] as? Bool == true ? Theme.red : Theme.muted).frame(width: 16)
                Text(t.s("text")).foregroundStyle(Theme.text)
                Text(summary(t)).lineLimit(1).foregroundStyle(Theme.muted)
                if let (add, del) = editCounts(t.s("text"), input) {
                    Text("+\(add)").foregroundStyle(Theme.green)
                    Text("−\(del)").foregroundStyle(Theme.red)
                }
            }
            .font(.brand(-3))
        }
    }
}

/// The per-tool body: diffs for edits, command + output for Bash, compact rows for read/search tools.
struct ToolDetail: View {
    let name: String
    let input: JSON
    let output: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            switch name {
            case "Edit":
                EditDiff(ops: lineDiff(input.s("old_string"), input.s("new_string")))
            case "MultiEdit":
                ForEach(Array((input["edits"] as? [JSON] ?? []).enumerated()), id: \.offset) { _, e in
                    EditDiff(ops: lineDiff(e.s("old_string"), e.s("new_string")))
                }
            case "Write":
                EditDiff(ops: lineDiff("", input.s("content")))
            case "Bash":
                mono("$ " + input.s("command"), Theme.text)
                out
            case "Read":
                kv("file", input.s("file_path"))
                if input["offset"] != nil || input["limit"] != nil { kv("lines", "\(input.i("offset"))+\(input.i("limit"))") }
            case "Grep", "Glob":
                kv("pattern", input.s("pattern"))
                if !input.s("path").isEmpty { kv("in", input.s("path")) }
                if !input.s("glob").isEmpty { kv("glob", input.s("glob")) }
                out
            case "WebFetch":
                kv("url", input.s("url"))
                if !input.s("prompt").isEmpty { kv("prompt", input.s("prompt")) }
                out
            case "WebSearch":
                kv("query", input.s("query"))
                out
            default:
                if let d = try? JSONSerialization.data(withJSONObject: input, options: [.prettyPrinted, .sortedKeys]),
                   let s = String(data: d, encoding: .utf8), input.count > 0 {
                    mono(String(s.prefix(3000)), Theme.text2)
                }
                out
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.terminal, in: RoundedRectangle(cornerRadius: 8))
    }

    @ViewBuilder var out: some View {
        if !output.isEmpty { mono(String(output.prefix(4000)), Theme.text2) }
    }

    func mono(_ s: String, _ c: Color) -> some View {
        Text(s).font(.ui(-3, .monospaced)).foregroundStyle(c).textSelection(.enabled)
    }

    func kv(_ k: String, _ v: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(k).font(.brand(-4)).foregroundStyle(Theme.muted).frame(width: 52, alignment: .trailing)
            Text(v).font(.ui(-3, .monospaced)).foregroundStyle(Theme.text).textSelection(.enabled)
        }
    }
}

struct EditDiff: View {
    let ops: [DiffOp]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(ops.prefix(400).enumerated()), id: \.offset) { _, op in
                let (sign, line, col): (String, String, Color) = switch op {
                case .same(let l): (" ", l, Theme.text2)
                case .del(let l): ("−", l, Theme.red)
                case .add(let l): ("+", l, Theme.green)
                }
                HStack(alignment: .top, spacing: 6) {
                    Text(sign).foregroundStyle(col).frame(width: 10)
                    Text(line.isEmpty ? " " : line).foregroundStyle(col == Theme.text2 ? Theme.text2 : Theme.text)
                }
                .font(.ui(-3, .monospaced))
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(col == Theme.text2 ? .clear : col.opacity(0.12))
            }
            if ops.count > 400 { Text("… \(ops.count - 400) more lines").font(.brand(-4)).foregroundStyle(Theme.muted) }
        }
        .textSelection(.enabled)
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }
}

func editCounts(_ name: String, _ input: JSON) -> (Int, Int)? {
    let pairs: [(String, String)] = switch name {
    case "Edit": [(input.s("old_string"), input.s("new_string"))]
    case "MultiEdit": (input["edits"] as? [JSON] ?? []).map { ($0.s("old_string"), $0.s("new_string")) }
    case "Write": [("", input.s("content"))]
    default: []
    }
    if pairs.isEmpty { return nil }
    var add = 0, del = 0
    for (o, n) in pairs {
        for op in lineDiff(o, n) {
            if case .add = op { add += 1 } else if case .del = op { del += 1 }
        }
    }
    return (add, del)
}

struct TodoCard: View {
    let input: JSON

    var body: some View {
        let todos = input["todos"] as? [JSON] ?? []
        VStack(alignment: .leading, spacing: 5) {
            Label("Todos · \(todos.filter { $0.s("status") == "completed" }.count)/\(todos.count)", systemImage: "checklist")
                .font(.brand(-3)).foregroundStyle(Theme.text2)
            ForEach(todos.indices, id: \.self) { i in
                let t = todos[i], st = t.s("status")
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Image(systemName: st == "completed" ? "checkmark.circle.fill" : st == "in_progress" ? "circle.dotted.circle" : "circle")
                        .foregroundStyle(st == "completed" ? Theme.green : st == "in_progress" ? Theme.accent : Theme.muted)
                    Text(st == "in_progress" && !t.s("activeForm").isEmpty ? t.s("activeForm") : t.s("content"))
                        .strikethrough(st == "completed", color: Theme.muted)
                        .foregroundStyle(st == "completed" ? Theme.muted : Theme.text)
                }
                .font(.ui(-1))
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.panel.opacity(0.6), in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.border.opacity(0.8)))
    }
}

struct SubagentCard: View {
    let t: JSON
    @State private var open = false

    var body: some View {
        let input = t["toolInput"] as? JSON ?? [:]
        let done = !t.s("toolOutput").isEmpty
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                if done { Image(systemName: t["toolFailed"] as? Bool == true ? "xmark.circle" : "checkmark.circle").foregroundStyle(t["toolFailed"] as? Bool == true ? Theme.red : Theme.green) }
                else { Pet(state: "running", size: 20) }
                Text(input.s("description").isEmpty ? "Sub-agent" : input.s("description")).font(.brand(-2, weight: .bold)).foregroundStyle(Theme.text)
                if !input.s("subagent_type").isEmpty { Chip(text: input.s("subagent_type"), icon: "person.2") }
                Spacer()
                Button(open ? "Hide" : "Details") { open.toggle() }.buttonStyle(.plain).font(.brand(-4)).foregroundStyle(Theme.accent)
            }
            if open {
                Text(input.s("prompt")).font(.ui(-2)).foregroundStyle(Theme.text2).textSelection(.enabled)
                if done { Divider().overlay(Theme.border); Markdown(text: String(t.s("toolOutput").prefix(6000))).font(.ui(-1)) }
            }
        }
        .padding(12)
        .background(Theme.panel.opacity(0.6), in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.accent.opacity(0.25)))
    }
}

func summary(_ t: JSON) -> String {
    let input = t["toolInput"] as? JSON ?? [:]
    for k in ["description", "file_path", "pattern", "command", "url", "query", "prompt"] {
        if let v = input[k] as? String, !v.isEmpty {
            return k == "file_path" ? (v as NSString).lastPathComponent : v
        }
    }
    return t.s("text")
}

func icon(_ tool: String) -> String {
    switch tool {
    case "Bash": "terminal"
    case "Read": "doc.text"
    case "Edit", "MultiEdit", "Write": "pencil"
    case "Grep", "Glob": "magnifyingglass"
    case "WebFetch", "WebSearch": "globe"
    case "Agent", "Task": "person.2"
    case "TodoWrite": "checklist"
    default: "wrench.and.screwdriver"
    }
}

/// Comment on an assistant reply (TranscriptComments.vue). The note is held in TranscriptFeedback and goes out
/// with the next message (ComposerStore.submit) or via the bar's "Send comments".
/// ponytail: quotes the whole reply — SwiftUI Text exposes no selection; transcriptQuote(text, range) is ready for a selectable view.
private struct CommentAction: View {
    let chatID: Int64
    let m: JSON
    @State private var open = false
    @State private var body_ = ""

    var body: some View {
        HoverAction(icon: "text.bubble", help: "Comment on this reply") { open = true }
            .popover(isPresented: $open) {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Comment").font(.brand(-3, weight: .bold)).foregroundStyle(Theme.text2)
                    TextField("What should change?", text: $body_, axis: .vertical).lineLimit(3...8).textFieldStyle(.roundedBorder)
                    HStack {
                        Spacer()
                        Button("Cancel") { open = false }
                        Button("Add") {
                            let text = m.s("text")
                            TranscriptFeedback.forChat(chatID).add(
                                messageId: m.i("id"), source: text,
                                quote: TranscriptQuote(quote: String(text.prefix(600)), before: "", after: ""), body: body_)
                            body_ = ""; open = false
                        }
                        .keyboardShortcut(.return, modifiers: .command)
                        .disabled(body_.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    }
                }
                .padding(12).frame(width: 340)
            }
    }
}
