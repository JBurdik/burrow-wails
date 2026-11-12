import AppKit
import SwiftUI
import WebKit

// In-app modals for transcript content: image lightbox (ImageLightbox.vue), agent HTML pages (HtmlPageModal.vue),
// zoomed diagrams (DiagramModal.vue). A borderless panel over the main window, independent of whichever view
// (main thread, RP sub-agent, popped window) asked for it. Esc or a click outside closes it.

private final class ModalPanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override func cancelOperation(_ sender: Any?) { close() }
    override func resignKey() { super.resignKey(); close() }
}

@MainActor private var modal: ModalPanel?

@MainActor func presentTranscriptModal<V: View>(size: CGSize, _ content: V) {
    modal?.close()
    let screen = (NSApp.keyWindow?.screen ?? NSScreen.main)?.visibleFrame ?? .init(x: 0, y: 0, width: 1200, height: 800)
    let w = min(size.width, screen.width * 0.9), h = min(size.height, screen.height * 0.85)
    let p = ModalPanel(contentRect: .init(x: screen.midX - w / 2, y: screen.midY - h / 2, width: w, height: h),
                       styleMask: [.borderless, .fullSizeContentView], backing: .buffered, defer: false)
    p.isReleasedWhenClosed = false
    p.backgroundColor = .clear
    p.hasShadow = true
    p.level = .floating
    p.contentView = NSHostingView(rootView: content
        .frame(width: w, height: h)
        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.border))
        .clipShape(RoundedRectangle(cornerRadius: 14)))
    modal = p
    p.makeKeyAndOrderFront(nil)
}

@MainActor func closeTranscriptModal() { modal?.close() }

private struct ModalHeader: View {
    let title: String
    var detail = ""
    var body: some View {
        HStack(spacing: 8) {
            Text(title).font(.brand(-2, weight: .bold)).foregroundStyle(Theme.text).lineLimit(1)
            Spacer()
            if !detail.isEmpty { Text(detail).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.head) }
            Button { closeTranscriptModal() } label: { Image(systemName: "xmark") }
                .buttonStyle(.plain).foregroundStyle(Theme.text2).help("Close (Esc)")
        }
        .padding(.horizontal, 12).padding(.vertical, 8)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }
}

@MainActor func openImageLightbox(_ img: NSImage) {
    let s = img.size
    presentTranscriptModal(size: CGSize(width: max(s.width, 200), height: max(s.height, 150) + 36), VStack(spacing: 0) {
        ModalHeader(title: "Image", detail: "\(Int(s.width))×\(Int(s.height))")
        Image(nsImage: img).resizable().scaledToFit().frame(maxWidth: .infinity, maxHeight: .infinity).padding(8)
    })
}

/// Agent-written HTML: scripts run, under the same no-network CSP as the Vue iframe (`pageSrcdoc`); any
/// navigation away from the page is refused.
@MainActor func openHtmlPage(title: String, path: String, html: String) {
    presentTranscriptModal(size: CGSize(width: 1200, height: 900), VStack(spacing: 0) {
        ModalHeader(title: title, detail: path)
        PageWeb(html: html)
    })
}

let pageCSP = #"<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; form-action 'none'">"#

func pageSrcdoc(_ html: String) -> String {
    if let r = html.range(of: #"^\s*<!doctype[^>]*>"#, options: [.regularExpression, .caseInsensitive]) {
        return html.replacingCharacters(in: r, with: html[r] + pageCSP)
    }
    return pageCSP + html
}

struct PageWeb: NSViewRepresentable {
    let html: String
    func makeCoordinator() -> Nav { Nav() }
    func makeNSView(context: Context) -> WKWebView {
        let w = WKWebView()
        w.navigationDelegate = context.coordinator
        w.loadHTMLString(pageSrcdoc(html), baseURL: nil)
        return w
    }
    func updateNSView(_ w: WKWebView, context: Context) {}

    final class Nav: NSObject, WKNavigationDelegate {
        func webView(_ w: WKWebView, decidePolicyFor a: WKNavigationAction, decisionHandler: @escaping @MainActor @Sendable (WKNavigationActionPolicy) -> Void) {
            decisionHandler(a.navigationType == .other && (a.request.url?.scheme ?? "about") == "about" ? .allow : .cancel)
        }
    }
}

/// Mermaid source zoomed to the panel (DiagramModal.vue).
@MainActor func openDiagram(_ source: String) {
    presentTranscriptModal(size: CGSize(width: 1400, height: 1000), VStack(spacing: 0) {
        ModalHeader(title: "Diagram")
        MermaidWeb(source: source, height: .constant(0), fill: true).padding(8)
    })
}
