import SwiftUI

// Diff viewer for the Git surface: one section per file (collapsible, big files start folded
// and render in chunks), light syntax tinting. Parsing is pure and unit-tested (GitDiffTests).

struct DiffFile: Identifiable, Hashable {
    var path: String
    var lines: [String]   // everything after the `diff --git` line (headers, hunks)
    var id: String { path + "#" + String(lines.count) }
    var additions: Int { lines.lazy.filter { $0.hasPrefix("+") && !$0.hasPrefix("+++") }.count }
    var deletions: Int { lines.lazy.filter { $0.hasPrefix("-") && !$0.hasPrefix("---") }.count }
    var isBinary: Bool { lines.contains { $0.hasPrefix("Binary files ") || $0.hasPrefix("GIT binary patch") } }
}

enum DiffParser {
    /// Splits `git diff` / `git show` output into files. Text before the first `diff --git`
    /// (a commit's header + stat) comes back as a file with an empty path.
    static func parse(_ text: String) -> [DiffFile] {
        var files: [DiffFile] = []
        var cur: DiffFile?
        for sub in text.split(separator: "\n", omittingEmptySubsequences: false) {
            let l = String(sub)
            if l.hasPrefix("diff --git ") {
                if let c = cur { files.append(c) }
                cur = DiffFile(path: pathOf(l), lines: [])
            } else if cur == nil {
                if cur == nil { cur = DiffFile(path: "", lines: []) }
                cur?.lines.append(l)
            } else {
                cur?.lines.append(l)
            }
        }
        if let c = cur { files.append(c) }
        // A `--no-index` diff and a trailing newline both leave empty tail lines.
        return files.map { f in
            var f = f
            while f.lines.last == "" { f.lines.removeLast() }
            return f
        }.filter { !($0.path.isEmpty && $0.lines.isEmpty) }
    }

    /// `diff --git a/x b/y` → `y` (the new name; quoted names and spaces are best-effort).
    static func pathOf(_ header: String) -> String {
        let rest = header.dropFirst("diff --git ".count)
        if let r = rest.range(of: " b/", options: .backwards) { return String(rest[r.upperBound...]) }
        return String(rest)
    }
}

/// Keyword-level tinting, language-agnostic: comments, strings, numbers, common keywords.
enum DiffSyntax {
    private static let keywords: Set<String> = [
        "func", "function", "const", "let", "var", "if", "else", "for", "while", "return", "import", "from", "export",
        "class", "struct", "enum", "interface", "type", "def", "fn", "pub", "use", "package", "switch", "case", "break",
        "continue", "async", "await", "try", "catch", "throw", "throws", "guard", "in", "is", "as", "new", "nil", "null",
        "true", "false", "self", "this", "go", "defer", "chan", "range", "static", "private", "public", "final", "override",
    ]

    static func highlight(_ code: String) -> AttributedString {
        var out = AttributedString()
        let chars = Array(code)
        var i = 0
        func put(_ s: String, _ c: Color?) {
            var a = AttributedString(s)
            if let c { a.foregroundColor = c }
            out += a
        }
        while i < chars.count {
            let c = chars[i]
            if (c == "/" && i + 1 < chars.count && chars[i + 1] == "/") || c == "#" && i == chars.prefix(i + 1).firstIndex(where: { !$0.isWhitespace }) {
                put(String(chars[i...]), .secondary); break
            }
            if c == "\"" || c == "'" || c == "`" {
                var j = i + 1
                while j < chars.count, chars[j] != c { j += chars[j] == "\\" ? 2 : 1 }
                j = min(j + 1, chars.count)
                put(String(chars[i..<j]), .orange); i = j; continue
            }
            if c.isNumber {
                var j = i
                while j < chars.count, chars[j].isNumber || chars[j] == "." { j += 1 }
                put(String(chars[i..<j]), .cyan); i = j; continue
            }
            if c.isLetter || c == "_" {
                var j = i
                while j < chars.count, chars[j].isLetter || chars[j].isNumber || chars[j] == "_" { j += 1 }
                let w = String(chars[i..<j])
                put(w, keywords.contains(w) ? .pink : nil); i = j; continue
            }
            put(String(c), nil); i += 1
        }
        return out
    }
}

struct GitDiffView: View {
    let cwd: String
    let title: String
    let args: [String]
    @Environment(AppModel.self) private var model
    @State private var files: [DiffFile]?
    @State private var error: String?
    @AppStorage("diff.wrap") private var wrap = false

    var body: some View {
        List {
            if let error { Text(error).font(.footnote.monospaced()).foregroundStyle(.red) }
            ForEach(files ?? []) { f in DiffFileSection(file: f, wrap: wrap) }
        }
        .listStyle(.plain)
        .overlay {
            if files == nil { ProgressView() }
            else if files?.isEmpty == true { ContentUnavailableView("No changes", systemImage: "doc.text") }
        }
        .navigationTitle((title as NSString).lastPathComponent)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Toggle(isOn: $wrap) { Image(systemName: "text.word.spacing") }.accessibilityLabel("Wrap lines")
            }
        }
        .task {
            // diff --no-index exits 1 when the files differ: read stdout whatever the code.
            let out = try? await model.client?.call("run_git", ["cwd": cwd, "args": args]) as? [String: Any]
            let text = out?["stdout"] as? String ?? ""
            let err = (out?["stderr"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            if text.isEmpty && !err.isEmpty { error = err }
            files = DiffParser.parse(text)
        }
    }
}

private struct DiffFileSection: View {
    let file: DiffFile
    let wrap: Bool
    /// Past this many lines a file starts folded and renders in chunks.
    static let big = 400
    static let chunk = 400
    @State private var open: Bool
    @State private var shown = DiffFileSection.chunk

    init(file: DiffFile, wrap: Bool) {
        self.file = file; self.wrap = wrap
        _open = State(initialValue: file.lines.count <= Self.big)
    }

    var body: some View {
        Section {
            if open {
                if file.isBinary {
                    Text("Binary file").foregroundStyle(.secondary).font(.caption)
                } else {
                    ScrollView(wrap ? .vertical : .horizontal) {
                        LazyVStack(alignment: .leading, spacing: 0) {
                            ForEach(Array(file.lines.prefix(shown).enumerated()), id: \.offset) { _, l in row(l) }
                        }
                        .font(.system(.caption, design: .monospaced))
                    }
                    .listRowInsets(EdgeInsets())
                    if file.lines.count > shown {
                        Button("Show \(min(Self.chunk, file.lines.count - shown)) more of \(file.lines.count - shown) lines") { shown += Self.chunk }
                            .font(.caption)
                    }
                }
            }
        } header: {
            Button { withAnimation { open.toggle() } } label: {
                HStack(spacing: 6) {
                    Image(systemName: open ? "chevron.down" : "chevron.right").font(.caption2).frame(width: 12)
                    Text(file.path.isEmpty ? "Commit" : file.path).font(.caption.monospaced().weight(.semibold))
                        .lineLimit(1).truncationMode(.head)
                    Spacer()
                    if file.additions > 0 { Text("+\(file.additions)").foregroundStyle(.green) }
                    if file.deletions > 0 { Text("−\(file.deletions)").foregroundStyle(.red) }
                }
                .font(.caption.monospacedDigit()).textCase(nil).foregroundStyle(.primary)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(file.path), \(open ? "expanded" : "collapsed")")
        }
    }

    @ViewBuilder
    private func row(_ l: String) -> some View {
        let meta = l.hasPrefix("+++") || l.hasPrefix("---") || l.hasPrefix("index ") || l.hasPrefix("new file") || l.hasPrefix("deleted file") || l.hasPrefix("similarity") || l.hasPrefix("rename ")
        let text: Text = {
            if meta { return Text(l).foregroundStyle(.secondary) }
            if l.hasPrefix("@@") { return Text(l).foregroundStyle(.purple) }
            if l.hasPrefix("+") || l.hasPrefix("-") || l.hasPrefix(" ") {
                return Text(String(l.first!)).foregroundStyle(l.hasPrefix("+") ? .green : l.hasPrefix("-") ? .red : .secondary)
                    + Text(DiffSyntax.highlight(String(l.dropFirst())))
            }
            return Text(l).foregroundStyle(.secondary)
        }()
        text
            .fixedSize(horizontal: !wrap, vertical: true)
            .frame(maxWidth: wrap ? .infinity : nil, alignment: .leading)
            .frame(minWidth: 0, maxWidth: wrap ? .infinity : .none, alignment: .leading)
            .background(!meta && l.hasPrefix("+") ? Color.green.opacity(0.12) : !meta && l.hasPrefix("-") ? Color.red.opacity(0.12) : .clear)
    }
}
