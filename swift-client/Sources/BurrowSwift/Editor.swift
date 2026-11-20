import AppKit
import SwiftUI

// OWNER: editor agent (wave 2). Port of CodeEditor.vue / FileViewer.vue (single file; the RP owns which file is open).

/// RP surface: an editable file (path absolute). `line` 1-based, 0 = none; a changed `line` re-reveals.
struct EditorSurfaceSlot: View {
    @EnvironmentObject var b: Burrow
    let path: String
    var line: Int = 0
    @StateObject private var doc = EditorDoc()

    var body: some View {
        VStack(spacing: 0) {
            header
            switch doc.mode {
            case .loading:
                ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
            case .text:
                CodeTextView(doc: doc, path: path, save: { Task { await save() } })
            case .image(let img):
                ScrollView([.vertical, .horizontal]) {
                    Image(nsImage: img).padding(12)
                }
            case .placeholder(let msg):
                VStack(spacing: 10) {
                    Image(systemName: "doc.badge.ellipsis").font(.system(size: 30, weight: .thin)).foregroundStyle(Theme.muted)
                    Text(msg).font(.brand(-3)).foregroundStyle(Theme.muted)
                    Button("Open in default editor") { open("editor") }
                        .buttonStyle(.plain).font(.brand(-4)).foregroundStyle(Theme.accent)
                }.frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(Theme.terminal)
        .task(id: path) { await load() }
        .onChange(of: line) { _, l in doc.reveal(line: l) }
        // CodeEditor.vue saveOnUnmount: the RP unmounts the editor on tab switch — write unsaved text out, don't drop it.
        .onDisappear { if doc.dirty, let text = doc.tv?.string { let p = path; Task { try? await b.call("write_text_file", ["path": p, "content": text]) } } }
    }

    var header: some View {
        HStack(spacing: 6) {
            Text((path as NSString).lastPathComponent).font(.brand(-3, weight: .semibold)).foregroundStyle(Theme.text).lineLimit(1)
            if doc.dirty { Circle().fill(Theme.accent).frame(width: 6, height: 6).help("Unsaved changes") }
            if !doc.error.isEmpty { Text(doc.error).font(.brand(-5)).foregroundStyle(Theme.red).lineLimit(1) }
            Spacer(minLength: 4)
            if doc.dirty {
                Button("Save") { Task { await save() } }.buttonStyle(.plain).font(.brand(-4)).foregroundStyle(Theme.accent)
                    .help("⌘S")
            }
            Menu {
                Button("Reload from disk") { Task { await load(force: true) } }
                Button("Open in default editor") { open("editor") }
                Button("Reveal in Finder") { open("finder") }
                Button("Copy path") { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(path, forType: .string) }
            } label: { Image(systemName: "ellipsis") }
                .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
        }
        .padding(.horizontal, 12).padding(.vertical, 7)
        .background(Theme.sidebar)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
        .help(path)
    }

    func open(_ target: String) {
        Task { try? await b.call("open_path_in", ["path": path, "target": target, "line": doc.currentLine]) }
    }

    func load(force: Bool = false) async {
        if force, doc.dirty, !confirm("Discard unsaved changes to \((path as NSString).lastPathComponent)?") { return }
        doc.error = ""
        let ext = (path as NSString).pathExtension.lowercased()
        if EditorDoc.imageExts.contains(ext) {
            let b64 = (try? await b.call("read_file_base64", ["path": path]) as? String) ?? ""
            if let data = Data(base64Encoded: b64), let img = NSImage(data: data) { doc.mode = .image(img) }
            else { doc.mode = .placeholder("Could not open image") }
            return
        }
        // ponytail: no stat on the wire — known binary extensions are refused unread; others are read whole, then sniffed.
        if EditorDoc.binaryExts.contains(ext) { doc.mode = .placeholder("Binary file — cannot edit"); return }
        do {
            let t = try await b.call("read_text_file_checked", ["path": path]) as? String ?? ""
            if let why = EditorDoc.refusal(t) { doc.mode = .placeholder(why); return }
            doc.saved = t
            doc.language = Highlighter.language(for: path)
            doc.pendingLine = line
            doc.dirty = false
            if case .text = doc.mode, let tv = doc.tv { doc.setText(tv, t) } else { doc.mode = .text }
        } catch {
            doc.mode = .placeholder("Could not open file")
            doc.error = error.localizedDescription
        }
    }

    func save() async {
        guard let text = doc.tv?.string else { return }
        do {
            try await b.call("write_text_file", ["path": path, "content": text])
            doc.saved = text
            doc.dirty = false
            doc.error = ""
        } catch { doc.error = "Save failed: \(error.localizedDescription)" }
    }

    func confirm(_ q: String) -> Bool {
        let a = NSAlert(); a.messageText = q; a.addButton(withTitle: "Discard"); a.addButton(withTitle: "Cancel")
        return a.runModal() == .alertFirstButtonReturn
    }
}

@MainActor final class EditorDoc: ObservableObject {
    enum Mode { case loading, text, image(NSImage), placeholder(String) }
    @Published var mode = Mode.loading
    @Published var dirty = false
    @Published var error = ""
    var saved = ""
    var pendingLine = 0
    var language: NSRegularExpression?
    weak var tv: NSTextView?

    static let imageExts: Set<String> = ["png", "jpg", "jpeg", "gif", "webp", "ico", "bmp", "tif", "tiff", "heic"]
    static let binaryExts: Set<String> = ["zip", "gz", "tgz", "tar", "xz", "7z", "pdf", "exe", "dll", "dylib", "so", "o", "a",
        "class", "jar", "wasm", "woff", "woff2", "ttf", "otf", "mp3", "mp4", "mov", "wav", "sqlite", "db", "bin", "dmg", "icns"]
    nonisolated static let maxBytes = 2_000_000

    /// Why a read file can't be edited (CodeEditor.vue's "binary" / "too-large" placeholders), nil = editable.
    /// Go hands invalid UTF-8 over as U+FFFD, NUL survives as-is.
    nonisolated static func refusal(_ t: String) -> String? {
        if t.utf8.count > maxBytes { return "File too large to open" }
        if t.contains("\u{0}") || t.unicodeScalars.contains("\u{FFFD}") { return "Binary file — cannot edit" }
        return nil
    }

    var currentLine: Int {
        guard let tv else { return 0 }
        let s = tv.string as NSString
        return s.substring(to: min(tv.selectedRange().location, s.length)).reduce(1) { $1 == "\n" ? $0 + 1 : $0 }
    }

    func setText(_ tv: NSTextView, _ t: String) {
        tv.string = t
        tv.undoManager?.removeAllActions()
        Highlighter.apply(tv, language)
        if pendingLine > 0 { reveal(line: pendingLine); pendingLine = 0 }
    }

    /// Caret on a 1-based line, the line selected and scrolled into view.
    func reveal(line: Int) {
        guard line > 0, let tv else { return }
        let r = EditorDoc.lineRange(tv.string as NSString, line)
        tv.setSelectedRange(r)
        tv.scrollRangeToVisible(r)
        tv.window?.makeFirstResponder(tv)
    }

    nonisolated static func lineRange(_ s: NSString, _ line: Int) -> NSRange {
        var loc = 0
        for _ in 1..<max(1, line) {
            let r = s.range(of: "\n", range: NSRange(location: loc, length: s.length - loc))
            if r.location == NSNotFound { break }
            loc = r.location + 1
        }
        let lr = s.lineRange(for: NSRange(location: loc, length: 0))
        // Select the text, not its newline.
        return NSRange(location: lr.location, length: max(0, lr.length - (s.substring(with: lr).hasSuffix("\n") ? 1 : 0)))
    }
}

// MARK: - NSTextView

final class EditorTextView: NSTextView {
    var onSave: () -> Void = {}

    override func performKeyEquivalent(with e: NSEvent) -> Bool {
        guard window?.firstResponder === self, e.modifierFlags.intersection(.deviceIndependentFlagsMask) == .command else {
            return super.performKeyEquivalent(with: e)
        }
        switch e.charactersIgnoringModifiers {
        case "s": onSave(); return true
        case "f": finder(.showFindInterface); return true
        case "g": finder(.nextMatch); return true
        default: return super.performKeyEquivalent(with: e)
        }
    }

    private func finder(_ a: NSTextFinder.Action) {
        let item = NSMenuItem(); item.tag = a.rawValue
        performTextFinderAction(item)
    }
}

struct CodeTextView: NSViewRepresentable {
    @ObservedObject var doc: EditorDoc
    let path: String
    let save: () -> Void

    static var font: NSFont {
        let v = UserDefaults.standard.double(forKey: "fontSize")
        let size = (v > 0 ? v : 15) - 2
        return NSFont(name: "0xProtoNerdFont-Regular", size: size) ?? .monospacedSystemFont(ofSize: size, weight: .regular)
    }

    func makeCoordinator() -> Coordinator { Coordinator(doc: doc) }

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSScrollView()
        scroll.hasVerticalScroller = true
        scroll.hasHorizontalScroller = true
        scroll.autohidesScrollers = true
        scroll.drawsBackground = false

        let tv = EditorTextView()
        tv.onSave = save
        tv.font = Self.font
        tv.textColor = NSColor(Theme.text)
        tv.backgroundColor = NSColor(Theme.terminal)
        tv.insertionPointColor = NSColor(Theme.accent)
        tv.selectedTextAttributes = [.backgroundColor: NSColor(Theme.selected)]
        tv.isRichText = false
        tv.allowsUndo = true
        tv.usesFindBar = true
        tv.isIncrementalSearchingEnabled = true
        tv.isAutomaticQuoteSubstitutionEnabled = false
        tv.isAutomaticDashSubstitutionEnabled = false
        tv.isAutomaticTextReplacementEnabled = false
        tv.isAutomaticSpellingCorrectionEnabled = false
        tv.isContinuousSpellCheckingEnabled = false
        tv.smartInsertDeleteEnabled = false
        tv.textContainerInset = NSSize(width: 4, height: 6)
        // No wrap, like CodeMirror's default: grow sideways, scroll horizontally.
        tv.isHorizontallyResizable = true
        tv.isVerticallyResizable = true
        tv.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        tv.autoresizingMask = [.width]
        tv.textContainer?.widthTracksTextView = false
        tv.textContainer?.containerSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        tv.delegate = context.coordinator
        scroll.documentView = tv

        let ruler = LineNumberRuler(textView: tv, scroll: scroll)
        scroll.verticalRulerView = ruler
        scroll.hasVerticalRuler = true
        scroll.rulersVisible = true
        scroll.contentView.postsBoundsChangedNotifications = true
        NotificationCenter.default.addObserver(forName: NSView.boundsDidChangeNotification, object: scroll.contentView, queue: .main) { [weak ruler] _ in
            MainActor.assumeIsolated { ruler?.needsDisplay = true }
        }

        doc.tv = tv
        doc.setText(tv, doc.saved)
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        (scroll.documentView as? EditorTextView)?.onSave = save
    }

    final class Coordinator: NSObject, NSTextViewDelegate {
        let doc: EditorDoc
        init(doc: EditorDoc) { self.doc = doc }

        func textDidChange(_ n: Notification) {
            guard let tv = n.object as? NSTextView else { return }
            MainActor.assumeIsolated {
                Highlighter.apply(tv, doc.language)
                let d = tv.string != doc.saved
                if doc.dirty != d { doc.dirty = d }
                tv.enclosingScrollView?.verticalRulerView?.needsDisplay = true
            }
        }
    }
}

final class LineNumberRuler: NSRulerView {
    weak var tv: NSTextView?

    init(textView: NSTextView, scroll: NSScrollView) {
        tv = textView
        super.init(scrollView: scroll, orientation: .verticalRuler)
        clientView = textView
        ruleThickness = 40
    }
    required init(coder: NSCoder) { fatalError() }

    override func drawHashMarksAndLabels(in rect: NSRect) {
        guard let tv, let lm = tv.layoutManager, let tc = tv.textContainer else { return }
        NSColor(Theme.terminal).setFill(); bounds.fill()
        let s = tv.string as NSString
        let font = NSFont.monospacedDigitSystemFont(ofSize: max(9, (tv.font?.pointSize ?? 13) - 2), weight: .regular)
        let caretLine = EditorDoc.lineNumber(s, at: tv.selectedRange().location)
        let visible = tv.visibleRect
        let chars = lm.characterRange(forGlyphRange: lm.glyphRange(forBoundingRect: visible, in: tc), actualGlyphRange: nil)
        // ponytail: counts newlines from the top on every redraw — O(file) per scroll, fine under the 2 MB cap; cache line starts if it shows.
        var line = EditorDoc.lineNumber(s, at: chars.location)
        var i = chars.location
        func draw(_ n: Int, _ frag: NSRect) {
            let y = convert(NSPoint(x: 0, y: frag.minY + tv.textContainerOrigin.y), from: tv).y
            let attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: NSColor(n == caretLine ? Theme.text2 : Theme.muted)]
            let str = "\(n)" as NSString
            let w = str.size(withAttributes: attrs).width
            str.draw(at: NSPoint(x: ruleThickness - w - 8, y: y + (frag.height - font.ascender + font.descender) / 2 - 1), withAttributes: attrs)
        }
        while i < NSMaxRange(chars) {
            let lr = s.lineRange(for: NSRange(location: i, length: 0))
            let g = lm.glyphIndexForCharacter(at: lr.location)
            draw(line, lm.lineFragmentRect(forGlyphAt: g, effectiveRange: nil))
            line += 1
            i = NSMaxRange(lr)
        }
        // The empty last line (file ends with "\n", or an empty file).
        if NSMaxRange(chars) >= s.length, !lm.extraLineFragmentRect.isEmpty { draw(line, lm.extraLineFragmentRect) }
    }
}

extension EditorDoc {
    nonisolated static func lineNumber(_ s: NSString, at loc: Int) -> Int {
        var n = 1, i = 0
        let end = min(loc, s.length)
        while i < end {
            let r = s.range(of: "\n", range: NSRange(location: i, length: end - i))
            if r.location == NSNotFound { break }
            n += 1; i = r.location + 1
        }
        return n
    }
}

// MARK: - Syntax highlighting

/// One alternation regex per language; the leftmost match wins, so `//` inside a string stays a string.
/// ponytail: regex highlighter, whole document re-scanned per keystroke (skipped over 300 KB); no nested
/// grammars (template literals, heredocs, embedded languages). Upgrade: tree-sitter, re-highlight only the edited paragraphs.
enum Highlighter {
    // Group order: 1 comment, 2 string, 3 keyword, 4 number, 5 type.
    static func language(for path: String) -> NSRegularExpression? {
        let name = (path as NSString).lastPathComponent.lowercased()
        let ext = (path as NSString).pathExtension.lowercased()
        let cStyle = #"//[^\n]*|/\*[\s\S]*?\*/"#
        let hash = #"#[^\n]*"#
        let dq = #""(?:\\.|[^"\\\n])*""#, sq = #"'(?:\\.|[^'\\\n])*'"#, bt = #"`(?:\\.|[^`\\])*`"#
        let triple = #""""[\s\S]*?"""|'''[\s\S]*?'''"#
        func kw(_ s: String) -> String { #"\b(?:"# + s.split(separator: " ").joined(separator: "|") + #")\b"# }
        let js = "const let var function return if else for while do switch case default break continue new class extends import export from as async await try catch finally throw typeof instanceof in of this super null undefined true false void yield static get set interface type enum implements public private protected readonly declare namespace keyof satisfies"
        let spec: (comment: String, string: String, keywords: String)?
        switch ext {
        case "js", "mjs", "cjs", "jsx", "ts", "mts", "cts", "tsx", "vue", "svelte":
            spec = (cStyle + "|<!--[\\s\\S]*?-->", [dq, sq, bt].joined(separator: "|"), js)
        case "go":
            spec = (cStyle, [dq, bt, sq].joined(separator: "|"), "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false iota")
        case "swift":
            spec = (cStyle, [triple, dq].joined(separator: "|"), "import let var func return if else guard for while repeat switch case default break continue struct class enum protocol extension init deinit self Self super nil true false throw throws rethrows try catch async await in where as is some any static private fileprivate public internal open final override mutating weak unowned lazy defer inout typealias associatedtype")
        case "rs":
            spec = (cStyle, dq, "as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while")
        case "c", "h", "cpp", "cc", "hpp", "m", "mm", "java", "kt", "kts", "cs", "scala", "dart":
            spec = (cStyle, [dq, sq].joined(separator: "|"), "auto break case char const continue default do double else enum extern float for goto if int long register return short signed sizeof static struct switch typedef union unsigned void volatile while class public private protected new this true false null nullptr namespace using template typename virtual override final import package fun val var when object interface extends implements throws try catch finally throw")
        case "py":
            spec = (hash, [triple, dq, sq].joined(separator: "|"), "and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield self")
        case "rb":
            spec = (hash, [dq, sq].joined(separator: "|"), "alias and begin break case class def defined do else elsif end ensure false for if in module next nil not or redo rescue retry return self super then true undef unless until when while yield")
        case "sh", "bash", "zsh", "fish":
            spec = (hash, [dq, sq].joined(separator: "|"), "if then else elif fi for while until do done case esac function in return local export source exit set unset echo")
        case "yaml", "yml", "toml", "ini", "conf", "env":
            spec = (hash, [dq, sq].joined(separator: "|"), "true false null yes no on off")
        case "json", "jsonc":
            spec = (cStyle, dq, "true false null")
        case "sql":
            spec = (#"--[^\n]*|/\*[\s\S]*?\*/"#, sq, "(?i:select from where insert into values update set delete create table index view drop alter add primary key foreign references join left right inner outer on group by order having limit offset as and or not null is in exists distinct union all case when then else end begin commit rollback integer text real blob default unique autoincrement)")
        case "css", "scss", "sass", "less":
            spec = (cStyle, [dq, sq].joined(separator: "|"), "important media import from to and not only keyframes")
        case "html", "htm", "xml", "svg", "plist":
            spec = ("<!--[\\s\\S]*?-->", [dq, sq].joined(separator: "|"), "")
        case "md", "mdx", "markdown":
            spec = (#"(?m)^#{1,6} [^\n]*"#, #"```[\s\S]*?```|`[^`\n]*`"#, "")
        default:
            spec = name == "makefile" || name == "justfile" || name == "dockerfile" ? (hash, [dq, sq].joined(separator: "|"), "") : nil
        }
        guard let spec else { return nil }
        let keyword = spec.keywords.isEmpty ? "(?!)" : (spec.keywords.hasPrefix("(?i:") ? #"\b"# + spec.keywords.replacingOccurrences(of: " ", with: "|") + #"\b"# : kw(spec.keywords))
        let pattern = "(\(spec.comment))|(\(spec.string))|(\(keyword))|(\\b\\d[\\w.]*\\b)|(\\b[A-Z][A-Za-z0-9_]*\\b)"
        return try? NSRegularExpression(pattern: pattern)
    }

    @MainActor static func apply(_ tv: NSTextView, _ language: NSRegularExpression?) {
        guard let ts = tv.textStorage else { return }
        let full = NSRange(location: 0, length: ts.length)
        ts.beginEditing()
        ts.addAttribute(.foregroundColor, value: NSColor(Theme.text), range: full)
        if let re = language, ts.length < 300_000 {
            let colors = [NSColor(Theme.muted), NSColor(Theme.green), NSColor(Theme.accent), NSColor(Theme.yellow), NSColor(Theme.text2)]
            re.enumerateMatches(in: ts.string, range: full) { m, _, _ in
                guard let m else { return }
                for g in 1...5 where m.range(at: g).location != NSNotFound {
                    ts.addAttribute(.foregroundColor, value: colors[g - 1], range: m.range(at: g)); break
                }
            }
        }
        ts.endEditing()
    }
}

/// Open a file in the Right Panel editor (transcript file links, Files tab, Spotlight…). `line` 1-based, 0 = none.
@MainActor func openFileInRP(_ path: String, line: Int = 0) {
    RPOpenRequests.shared.file = (path, line)
}

/// Requests other parts of the app make to the Right Panel; the RP agent observes and acts on them.
@MainActor final class RPOpenRequests: ObservableObject {
    static let shared = RPOpenRequests()
    @Published var file: (String, Int)?
    /// A named surface: "diff", "browser:<url>", "doc:<path>", "devservers", "sidechat:<chatId>", plus the existing tab names.
    @Published var surface: String?
}
