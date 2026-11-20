import AppKit
import Charts
import SwiftUI
import WebKit

// burrow-visuals blocks (src-wails/agentdocs/skills/burrow-visuals/SKILL.md): ```mermaid, ```visual, ```questions.

// MARK: - mermaid

/// ponytail: mermaid from the jsdelivr CDN in a WKWebView; offline the source stays visible as text (the fallback).
struct MermaidView: View {
    let source: String
    @State private var height: CGFloat = 120

    var body: some View {
        MermaidWeb(source: source, height: $height)
            .frame(height: height)
            .frame(maxWidth: .infinity)
            .background(Theme.terminal, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
            .overlay(alignment: .topTrailing) {
                HStack(spacing: 8) {
                    Button { openDiagram(source) } label: { Image(systemName: "arrow.up.left.and.arrow.down.right").font(.system(size: 11)) }
                        .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Zoom")
                    CopyButton(text: source)
                }
                .padding(6)
            }
    }
}

struct MermaidWeb: NSViewRepresentable {
    let source: String
    @Binding var height: CGFloat
    /// The zoom modal: the SVG scales to the whole view instead of its natural size.
    var fill = false

    func makeCoordinator() -> Coordinator { Coordinator(height: $height) }

    func makeNSView(context: Context) -> WKWebView {
        let cfg = WKWebViewConfiguration()
        cfg.userContentController.add(context.coordinator, name: "h")
        let w = WKWebView(frame: .zero, configuration: cfg)
        w.setValue(false, forKey: "drawsBackground")
        load(w)
        return w
    }

    func updateNSView(_ w: WKWebView, context: Context) {
        if context.coordinator.loaded != source { load(w); context.coordinator.loaded = source }
    }

    func load(_ w: WKWebView) {
        let esc = source.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "<", with: "&lt;")
        let html = """
        <html><head><meta name="color-scheme" content="dark"><style>
        body{margin:0;padding:12px;background:transparent;color:#ECE6E9;font:12px -apple-system}
        pre.mermaid{margin:0;white-space:pre-wrap;font:12px ui-monospace}
        \(fill ? "html,body,pre.mermaid{height:100%}svg{width:100%!important;max-width:100%!important;height:100%}" : "")
        </style></head><body><pre class="mermaid">\(esc)</pre>
        <script src="https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
        <script>
        const post=()=>webkit.messageHandlers.h.postMessage(document.body.scrollHeight);
        post();
        if(window.mermaid){mermaid.initialize({startOnLoad:false,theme:'dark',themeVariables:{background:'transparent',primaryColor:'#2C2127',primaryBorderColor:'#A895F5',lineColor:'#A79CA2',primaryTextColor:'#ECE6E9'}});
        mermaid.run().then(post).catch(post);}
        </script></body></html>
        """
        w.loadHTMLString(html, baseURL: URL(string: "https://burrow.local/"))
    }

    final class Coordinator: NSObject, WKScriptMessageHandler {
        var height: Binding<CGFloat>
        var loaded = ""
        init(height: Binding<CGFloat>) { self.height = height }
        func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) {
            if let h = m.body as? Double { height.wrappedValue = min(max(CGFloat(h) + 4, 60), 900) }
        }
    }
}

// MARK: - visual

func num(_ v: Any?) -> Double? { (v as? NSNumber)?.doubleValue ?? (v as? String).flatMap(Double.init) }
func str(_ v: Any?) -> String {
    switch v {
    case let s as String: s
    case let n as NSNumber: n.stringValue
    case nil: ""
    default: "\(v!)"
    }
}

struct VisualView: View {
    let spec: JSON

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if !spec.s("title").isEmpty, spec["mark"] == nil { Text(spec.s("title")).font(.brand(0, weight: .bold)) }
            if spec["mark"] != nil { ChartPart(spec: spec) }
            ForEach(Array((spec["rows"] as? [JSON] ?? []).enumerated()), id: \.offset) { _, p in VisualPart(p: p) }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.panel.opacity(0.5), in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.border))
    }
}

struct VisualPart: View {
    @EnvironmentObject var b: Burrow
    let p: JSON

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !p.s("title").isEmpty, p["chart"] == nil { Text(p.s("title")).font(.brand(-2, weight: .bold)).foregroundStyle(Theme.text2) }
            content
        }
    }

    @ViewBuilder var content: some View {
        if let c = p["chart"] as? JSON { ChartPart(spec: c) }
        else if let stats = p["stats"] as? [JSON] { StatTiles(stats: stats) }
        else if let t = p["table"] as? JSON { MDTable(rows: tableRows(t)) }
        else if let text = p["text"] as? String { Markdown(text: text) }
        else if let cols = p["columns"] as? [JSON] {
            // AnyView: a part can nest parts, and an opaque type can't contain itself.
            HStack(alignment: .top, spacing: 12) { ForEach(cols.indices, id: \.self) { AnyView(VisualPart(p: cols[$0])) } }
        } else if let page = p["page"] as? JSON { PageCard(page: page) }
        else if let img = p["image"] as? JSON { PathImage(path: img.s("path"), caption: img.s("caption")) }
    }

    func tableRows(_ t: JSON) -> [[String]] {
        let raw = t["rows"] as? [Any] ?? []
        var cols = t["columns"] as? [String] ?? []
        if cols.isEmpty, let first = raw.first as? JSON { cols = first.keys.sorted() }
        let body = raw.map { r -> [String] in
            if let a = r as? [Any] { return a.map(str) }
            let o = r as? JSON ?? [:]
            return cols.map { str(o[$0]) }
        }
        return [cols] + body
    }
}

@MainActor func resolvePath(_ b: Burrow, _ path: String, cwd: String = "") -> String {
    if path.hasPrefix("/") { return path }
    let cwd = cwd.isEmpty ? b.chat(b.selectedChat).flatMap { b.workspaces[$0.i("workspace_id")]?.s("path") } ?? "" : cwd
    return (cwd as NSString).appendingPathComponent(path)
}

struct StatTiles: View {
    let stats: [JSON]

    var body: some View {
        HStack(spacing: 10) {
            ForEach(stats.indices, id: \.self) { i in
                let s = stats[i]
                VStack(alignment: .leading, spacing: 2) {
                    Text(s.s("label")).font(.brand(-4)).foregroundStyle(Theme.muted)
                    Text(str(s["value"])).font(.ui(6, weight: .bold))
                        .foregroundStyle(s.s("tone") == "good" ? Theme.green : s.s("tone") == "bad" ? Theme.red : Theme.text)
                    if !s.s("note").isEmpty { Text(s.s("note")).font(.ui(-3)).foregroundStyle(Theme.text2) }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.base.opacity(0.6), in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
            }
        }
    }
}

/// The SKILL's chart subset: bar / line / area / point / arc, x as categories, optional colour series.
/// ponytail: multi-series bars stack instead of grouping (`.position(by:)` would need a second builder branch).
struct ChartPart: View {
    let spec: JSON

    var body: some View {
        let markObj = spec["mark"] as? JSON
        let mark = markObj?.s("type") ?? spec.s("mark")
        let enc = spec["encoding"] as? JSON ?? [:]
        let xf = (enc["x"] as? JSON)?.s("field") ?? ""
        let yf = (enc["y"] as? JSON)?.s("field") ?? (enc["theta"] as? JSON)?.s("field") ?? ""
        let cf = (enc["color"] as? JSON)?.s("field")
        let rows = ((spec["data"] as? JSON)?["values"] as? [JSON] ?? []).filter { num($0[yf]) != nil }
        let inner = CGFloat(num(markObj?["innerRadius"]) ?? 0)
        let dots = markObj?["point"] as? Bool ?? false
        VStack(alignment: .leading, spacing: 6) {
            if !spec.s("title").isEmpty { Text(spec.s("title")).font(.brand(-2, weight: .bold)).foregroundStyle(Theme.text2) }
            Chart(rows.indices, id: \.self) { i in
                let r = rows[i]
                let x = str(r[xf]), y = num(r[yf]) ?? 0
                let series = cf.map { str(r[$0]) } ?? (mark == "arc" ? x : yf)
                if mark == "line" {
                    LineMark(x: .value(xf, x), y: .value(yf, y)).foregroundStyle(by: .value("series", series))
                        .symbol(dots ? .circle : .asterisk).symbolSize(dots ? 30 : 0)
                } else if mark == "area" {
                    AreaMark(x: .value(xf, x), y: .value(yf, y)).foregroundStyle(by: .value("series", series)).opacity(0.7)
                } else if mark == "point" {
                    PointMark(x: .value(xf, x), y: .value(yf, y)).foregroundStyle(by: .value("series", series))
                } else if mark == "arc" {
                    SectorMark(angle: .value(yf, y), innerRadius: .fixed(inner), angularInset: 1).foregroundStyle(by: .value("series", series))
                } else {
                    BarMark(x: .value(xf, x), y: .value(yf, y)).foregroundStyle(by: .value("series", series)).cornerRadius(3)
                }
            }
            .chartForegroundStyleScale(range: [Theme.accent, Theme.green, Theme.yellow, Theme.red, Theme.accentDim, Theme.text2])
            .chartLegend(cf == nil && mark != "arc" ? .hidden : .automatic)
            .frame(height: CGFloat(num(spec["height"]) ?? 220))
        }
    }
}

struct PageCard: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.transcript) private var t
    let page: JSON

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "doc.richtext").foregroundStyle(Theme.accent).font(.system(size: 20))
            VStack(alignment: .leading, spacing: 2) {
                Text(page.s("title")).font(.brand(-1, weight: .bold))
                if !page.s("description").isEmpty { Text(page.s("description")).font(.ui(-2)).foregroundStyle(Theme.text2) }
            }
            Spacer()
            Button("Open") { Task { await open() } }
        }
        .padding(12)
        .background(Theme.base.opacity(0.6), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
    }

    /// The agent's HTML file in Burrow's no-network page modal (AgentChat.openPage).
    func open() async {
        let abs = resolvePath(b, page.s("path"), cwd: t.cwd)
        let html: String
        do { html = try await b.call("read_text_file_checked", ["path": abs]) as? String ?? "" }
        catch { html = "<p style=\"font:13px system-ui;color:#b91c1c;padding:16px\">Could not read \(abs.replacingOccurrences(of: "<", with: "&lt;")): \(error.localizedDescription.replacingOccurrences(of: "<", with: "&lt;"))</p>" }
        openHtmlPage(title: page.s("title").isEmpty ? abs : page.s("title"), path: abs, html: html)
    }
}

struct PathImage: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.transcript) private var t
    let path: String
    let caption: String
    @State private var img: NSImage?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if let img { Image(nsImage: img).resizable().scaledToFit().frame(maxHeight: 420).clipShape(RoundedRectangle(cornerRadius: 10)).onTapGesture { openImageLightbox(img) } }
            else { Text(path).font(.brand(-4)).foregroundStyle(Theme.muted) }
            if !caption.isEmpty { Text(caption).font(.ui(-2)).foregroundStyle(Theme.text2) }
        }
        .task(id: path) {
            let s = try? await b.call("read_file_base64", ["path": resolvePath(b, path, cwd: t.cwd)]) as? String
            img = s.flatMap { Data(base64Encoded: $0) }.flatMap(NSImage.init(data:))
        }
    }
}

// MARK: - questions

/// The ```questions form: options per question, an Other field, notes, Send / Skip all → one user message.
struct QuestionsForm: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.transcript) private var t
    let spec: JSON
    @State private var picked: [Int: [String]] = [:]
    @State private var other: [Int: String] = [:]
    @State private var notes = ""
    @State private var sent = false

    var body: some View {
        let qs = spec["questions"] as? [JSON] ?? []
        VStack(alignment: .leading, spacing: 14) {
            ForEach(qs.indices, id: \.self) { i in question(i, qs[i]) }
            if qs.count > 1 {
                TextField("Anything else the agent should know (optional)", text: $notes, axis: .vertical)
                    .textFieldStyle(.plain).font(.ui(-1)).lineLimit(1...4).padding(8)
                    .background(Theme.base.opacity(0.6), in: RoundedRectangle(cornerRadius: 8))
            }
            HStack {
                Spacer()
                Button("Skip all") { submit(qs, skip: true) }.buttonStyle(.bordered)
                Button("Send answers") { submit(qs, skip: false) }.buttonStyle(.borderedProminent).tint(Theme.accentDim)
            }
            .disabled(sent)
        }
        .padding(14)
        .background(Theme.panel.opacity(0.6), in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.accent.opacity(0.3)))
        .opacity(sent ? 0.6 : 1)
    }

    @ViewBuilder func question(_ i: Int, _ q: JSON) -> some View {
        let multi = q["multiSelect"] as? Bool ?? false
        let opts: [JSON] = (q["options"] as? [Any] ?? []).map { ($0 as? JSON) ?? ["label": str($0)] }
        VStack(alignment: .leading, spacing: 6) {
            if !q.s("header").isEmpty { Text(q.s("header").uppercased()).font(.brand(-5)).foregroundStyle(Theme.accent) }
            Text(q.s("question")).font(.ui(0, weight: .semibold))
            ForEach(opts.indices, id: \.self) { j in
                let o = opts[j], label = o.s("label"), on = picked[i, default: []].contains(label)
                Button {
                    var cur = picked[i, default: []]
                    if on { cur.removeAll { $0 == label } } else { cur = multi ? cur + [label] : [label] }
                    picked[i] = cur
                } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Image(systemName: on ? (multi ? "checkmark.square.fill" : "largecircle.fill.circle") : (multi ? "square" : "circle"))
                            .foregroundStyle(on ? Theme.accent : Theme.muted)
                        VStack(alignment: .leading, spacing: 1) {
                            HStack(spacing: 6) {
                                Text(label).foregroundStyle(Theme.text)
                                if o["recommended"] as? Bool == true { Text("recommended").font(.brand(-5)).foregroundStyle(Theme.green) }
                            }
                            if !o.s("description").isEmpty { Text(o.s("description")).font(.ui(-2)).foregroundStyle(Theme.text2) }
                        }
                        Spacer()
                    }
                    .padding(.horizontal, 10).padding(.vertical, 6)
                    .background(on ? Theme.selected : Theme.base.opacity(0.4), in: RoundedRectangle(cornerRadius: 8))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
            TextField(opts.isEmpty ? "Your answer" : "Other / note", text: Binding(get: { other[i] ?? "" }, set: { other[i] = $0 }))
                .textFieldStyle(.plain).font(.ui(-1)).padding(8)
                .background(Theme.base.opacity(0.6), in: RoundedRectangle(cornerRadius: 8))
        }
        .disabled(sent)
    }

    func submit(_ qs: [JSON], skip: Bool) {
        let answers = qs.indices.map { i in
            (question: qs[i].s("question"), picked: skip ? [] : picked[i, default: []], other: skip ? "" : other[i] ?? "")
        }
        let text = formatAnswers(answers, notes: skip ? "" : notes)
        guard let id = t.chatID > 0 ? t.chatID : b.selectedChat else { return }
        sent = true
        Task { await ComposerStore.shared.sendText(b, id, text, running: b.inFlight(id)) }
    }
}

extension ComposerStore {
    /// A message from the transcript (form answers): queued while a turn runs, else sent.
    func sendText(_ b: Burrow, _ id: Int64, _ text: String, running: Bool) async {
        let args: JSON = ["chatId": id, "text": text, "images": [String]()]
        do { try await b.call(running ? "chat_queue_add" : "chat_send", args) }
        catch { b.status = "Send failed: \(error.localizedDescription)" }
    }
}
