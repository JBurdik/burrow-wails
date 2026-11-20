import SwiftUI
import WebKit

/// The Right Panel's file browser: the thread's workspace folder (read_dir_shallow),
/// a Changed shortcut list (git status), push navigation into folders and FileViewer.
struct FilesSurface: View {
    let chatId: Int
    @Environment(AppModel.self) private var model
    @State private var root = ""
    @State private var failed = false

    var body: some View {
        Group {
            if !root.isEmpty {
                DirList(root: root, path: root, showChanged: true)
            } else if failed {
                ContentUnavailableView("No folder", systemImage: "folder.badge.questionmark",
                                       description: Text("This thread's workspace has no folder on the Mac."))
            } else {
                ProgressView()
            }
        }
        .task { await loadRoot() }
    }

    /// `Workspace` carries no path; the list_workspaces row does (as ThreadDetails reads it).
    @MainActor private func loadRoot() async {
        guard let wsId = model.chat(chatId)?.workspaceId,
              let list = try? await model.client?.call("list_workspaces") as? [[String: Any]],
              let path = list.first(where: { int($0["id"]) == wsId })?["path"] as? String, !path.isEmpty
        else { failed = true; return }
        root = path.hasSuffix("/") && path.count > 1 ? String(path.dropLast()) : path
    }
}

private struct DirEntry: Identifiable, Hashable {
    var name: String
    var isDir: Bool
    var id: String { name }
}

private struct ChangedEntry: Identifiable, Hashable {
    var path: String      // absolute
    var rel: String
    var code: String      // porcelain XY, trimmed
    var deleted: Bool
    var id: String { path }
}

private struct DirList: View {
    let root: String
    let path: String
    var showChanged = false
    @Environment(AppModel.self) private var model
    @State private var entries: [DirEntry]?
    @State private var changed: [ChangedEntry] = []
    @State private var error: String?

    var body: some View {
        List {
            if showChanged && !changed.isEmpty {
                Section("Changed · \(changed.count)") {
                    ForEach(changed) { c in
                        let label = HStack {
                            Label(c.rel, systemImage: "doc").lineLimit(1).truncationMode(.head)
                            Spacer()
                            Text(c.code).font(.caption.monospaced()).foregroundStyle(c.deleted ? .red : .orange)
                        }
                        if c.deleted { label.foregroundStyle(.secondary) }
                        else { NavigationLink { FileViewer(path: c.path) } label: { label } }
                    }
                }
            }
            Section {
                if let error {
                    Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(.secondary)
                } else if let entries {
                    if entries.isEmpty { Text("Empty folder").foregroundStyle(.secondary) }
                    ForEach(entries) { e in
                        let full = path + "/" + e.name
                        NavigationLink {
                            if e.isDir { DirList(root: root, path: full) } else { FileViewer(path: full) }
                        } label: {
                            Label(e.name, systemImage: e.isDir ? "folder.fill" : FileKind(path: e.name).icon)
                                .lineLimit(1)
                        }
                    }
                } else {
                    HStack { Spacer(); ProgressView(); Spacer() }
                }
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle(path == root ? (root as NSString).lastPathComponent : (path as NSString).lastPathComponent)
        .refreshable { await load() }
        .task { await load() }
    }

    @MainActor private func load() async {
        guard let client = model.client else { return }
        do {
            let raw = try await client.call("read_dir_shallow", ["path": path]) as? [[String: Any]] ?? []
            entries = raw.map { DirEntry(name: $0["name"] as? String ?? "", isDir: $0["isDir"] as? Bool ?? false) }
                .filter { !$0.name.isEmpty && $0.name != ".git" }
                .sorted { $0.isDir != $1.isDir ? $0.isDir : $0.name.localizedStandardCompare($1.name) == .orderedAscending }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        if showChanged { await loadChanged() }
    }

    /// `git status --porcelain -z -uall`: paths are relative to the repo root, NUL-separated,
    /// a rename/copy carries its original path as the next field.
    @MainActor private func loadChanged() async {
        guard let client = model.client else { return }
        async let topR = try? client.call("run_git", ["cwd": root, "args": ["rev-parse", "--show-toplevel"]]) as? [String: Any]
        async let statR = try? client.call("run_git", ["cwd": root, "args": ["status", "--porcelain", "-z", "-uall"]]) as? [String: Any]
        guard let top = await topR, int(top["code"]) == 0, let st = await statR, int(st["code"]) == 0 else { changed = []; return }
        let repo = (top["stdout"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let fields = (st["stdout"] as? String ?? "").split(separator: "\0", omittingEmptySubsequences: true).map(String.init)
        var out: [ChangedEntry] = []
        var i = 0
        while i < fields.count {
            let f = fields[i]; i += 1
            guard f.count > 3 else { continue }
            let xy = String(f.prefix(2)), file = String(f.dropFirst(3))
            if xy.contains("R") || xy.contains("C") { i += 1 }
            let abs = repo + "/" + file
            let rel = abs.hasPrefix(root + "/") ? String(abs.dropFirst(root.count + 1)) : file
            out.append(ChangedEntry(path: abs, rel: rel, code: xy.trimmingCharacters(in: .whitespaces),
                                    deleted: xy.contains("D")))
        }
        changed = out
    }
}

// MARK: - Viewer

private enum FileKind {
    case markdown, html, image, text

    init(path: String) {
        switch (path as NSString).pathExtension.lowercased() {
        case "md", "markdown", "mdx": self = .markdown
        case "html", "htm", "svg": self = .html
        case "png", "jpg", "jpeg", "gif", "webp", "heic", "bmp", "ico", "tif", "tiff": self = .image
        default: self = .text
        }
    }

    var icon: String {
        switch self {
        case .markdown: "doc.richtext"
        case .html: "globe"
        case .image: "photo"
        case .text: "doc.text"
        }
    }
}

/// One file: markdown rendered, .html drawn with no scripts / no network, images,
/// everything else as monospaced lines. Copy + Share in the toolbar.
struct FileViewer: View {
    let path: String
    @Environment(AppModel.self) private var model
    @State private var text: String?
    @State private var image: UIImage?
    @State private var truncated = false
    @State private var binary = false
    @State private var error: String?
    @State private var source = false
    @State private var shareURL: URL?

    // ponytail: 1 MiB cap on text, read as the file's tail (read_file_tail is the only bounded read
    // on the wire); images are read whole via read_file_base64 — add a stat command if that bites.
    private static let limit = 1 << 20
    private var kind: FileKind { FileKind(path: path) }
    private var name: String { (path as NSString).lastPathComponent }

    var body: some View {
        content
            .navigationTitle(name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if (kind == .markdown || kind == .html) && text != nil && !binary {
                    ToolbarItem(placement: .primaryAction) {
                        Picker("View", selection: $source) {
                            Text("Preview").tag(false)
                            Text("Source").tag(true)
                        }
                        .pickerStyle(.menu)
                    }
                }
                if let text, !binary {
                    ToolbarItem(placement: .primaryAction) {
                        Button { UIPasteboard.general.string = text } label: { Image(systemName: "doc.on.doc") }
                            .accessibilityLabel("Copy")
                    }
                }
                if let shareURL {
                    ToolbarItem(placement: .primaryAction) { ShareLink(item: shareURL) }
                }
            }
            .task { await load() }
    }

    @ViewBuilder private var content: some View {
        if let error {
            ContentUnavailableView("Can't open", systemImage: "exclamationmark.triangle", description: Text(error))
        } else if let image {
            ScrollView([.horizontal, .vertical]) {
                Image(uiImage: image).resizable().scaledToFit().frame(maxWidth: .infinity).padding()
            }
        } else if binary {
            ContentUnavailableView("Binary file", systemImage: "doc.questionmark",
                                   description: Text("This file isn't text."))
        } else if let text {
            VStack(spacing: 0) {
                if truncated {
                    Label("Large file — showing the last 1 MB", systemImage: "scissors")
                        .font(.footnote).foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity).padding(8).background(.bar)
                }
                switch (kind, source) {
                case (.markdown, false):
                    ScrollView { MarkdownText(text: text).textSelection(.enabled).padding().frame(maxWidth: .infinity, alignment: .leading) }
                case (.html, false):
                    SafeHTMLView(html: text).ignoresSafeArea(edges: .bottom)
                default:
                    CodeView(text: text)
                }
            }
        } else {
            ProgressView()
        }
    }

    @MainActor private func load() async {
        guard let client = model.client else { return }
        do {
            if kind == .image {
                let b64 = try await client.call("read_file_base64", ["path": path]) as? String ?? ""
                guard let data = Data(base64Encoded: b64), let img = UIImage(data: data) else {
                    binary = true; return
                }
                image = img
                shareURL = writeTemp(data)
                return
            }
            let s = try await client.call("read_file_tail", ["path": path, "maxBytes": Self.limit + 1]) as? String ?? ""
            // Go marshals invalid UTF-8 as U+FFFD; NULs or lots of those = not text.
            let bad = s.unicodeScalars.prefix(8192).filter { $0 == "\u{0}" || $0 == "\u{FFFD}" }.count
            if bad > 8 { binary = true; return }
            truncated = s.utf8.count > Self.limit
            text = truncated && s.contains("\n") ? String(s.drop(while: { $0 != "\n" }).dropFirst()) : s
            shareURL = writeTemp(Data((text ?? "").utf8))
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func writeTemp(_ data: Data) -> URL? {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("files", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent(name)
        return (try? data.write(to: url)) != nil ? url : nil
    }
}

/// Monospaced lines with a line-number gutter; scrolls both ways, no wrapping.
private struct CodeView: View {
    let lines: [Substring]
    init(text: String) { lines = text.split(separator: "\n", omittingEmptySubsequences: false) }

    var body: some View {
        let gutter = CGFloat(String(lines.count).count) * 8 + 8
        ScrollView([.vertical, .horizontal]) {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(lines.indices, id: \.self) { i in
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text("\(i + 1)").foregroundStyle(.tertiary).frame(width: gutter, alignment: .trailing)
                        Text(lines[i].isEmpty ? " " : String(lines[i])).fixedSize()
                    }
                    .font(.system(size: 12, design: .monospaced))
                }
            }
            .textSelection(.enabled)
            .padding(.vertical, 8).padding(.trailing, 12)
        }
    }
}

/// HtmlFileSurface on the phone: the page with JavaScript off, a no-network CSP
/// (pageSrcdoc's, minus scripts) and an opaque about:blank origin; links don't navigate.
private struct SafeHTMLView: UIViewRepresentable {
    let html: String

    static let csp = #"<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; form-action 'none'">"#

    static func srcdoc(_ html: String) -> String {
        if let r = html.range(of: #"^\s*<!doctype[^>]*>"#, options: [.regularExpression, .caseInsensitive]) {
            return html[r] + csp + html[r.upperBound...]
        }
        return csp + html
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIView(context: Context) -> WKWebView {
        let cfg = WKWebViewConfiguration()
        cfg.defaultWebpagePreferences.allowsContentJavaScript = false
        cfg.websiteDataStore = .nonPersistent()
        let web = WKWebView(frame: .zero, configuration: cfg)
        web.navigationDelegate = context.coordinator
        web.loadHTMLString(Self.srcdoc(html), baseURL: nil)
        return web
    }

    func updateUIView(_ web: WKWebView, context: Context) {}

    final class Coordinator: NSObject, WKNavigationDelegate {
        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            // Only the initial load (about:blank) — nothing the page links to.
            decisionHandler(action.navigationType == .other && action.request.url?.scheme == "about" ? .allow : .cancel)
        }
    }
}
