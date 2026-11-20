import AppKit
import SwiftUI
import WebKit

// Extra Right Panel surfaces: Browser (BrowserPane.vue), Dev servers (DevServersSurface.vue),
// markdown Doc (MarkdownDocSurface.vue), Side chat (SideChatSurface.vue).

// MARK: Browser

/// One web view per workspace, kept alive across surface switches (Vue keeps BrowserPane mounted for the same reason:
/// URL, history and page state survive moving panes or switching projects).
@MainActor final class RPBrowserTab: NSObject, ObservableObject, WKNavigationDelegate {
    private static var tabs: [Int64: RPBrowserTab] = [:]
    static func of(_ ws: Int64) -> RPBrowserTab {
        if let t = tabs[ws] { return t }
        let t = RPBrowserTab(); tabs[ws] = t; return t
    }

    let web = WKWebView()
    @Published var url = ""
    @Published var error = ""
    var handledNav = -1

    override init() {
        super.init()
        web.navigationDelegate = self
    }

    /// "localhost:3000" / "3000" / "example.com" → a loadable URL.
    static func normalize(_ raw: String) -> URL? {
        var s = raw.trimmingCharacters(in: .whitespaces)
        if s.isEmpty { return nil }
        if Int(s) != nil { s = "localhost:" + s }
        if !s.contains("://") {
            let local = s.hasPrefix("localhost") || s.hasPrefix("127.") || s.hasPrefix("[::1]") || s.hasPrefix("0.0.0.0")
            s = (local ? "http://" : "https://") + s
        }
        return URL(string: s)
    }

    func go(_ raw: String) {
        guard let u = Self.normalize(raw) else { return }
        url = u.absoluteString
        error = ""
        web.load(URLRequest(url: u))
    }

    func webView(_ w: WKWebView, didCommit _: WKNavigation!) {
        url = w.url?.absoluteString ?? url
        error = ""
    }

    func webView(_: WKWebView, didFailProvisionalNavigation _: WKNavigation!, withError e: Error) { fail(e) }
    func webView(_: WKWebView, didFail _: WKNavigation!, withError e: Error) { fail(e) }
    private func fail(_ e: Error) {
        if (e as NSError).code == NSURLErrorCancelled { return } // a newer navigation replaced this one
        error = e.localizedDescription
    }
}

private struct WebHost: NSViewRepresentable {
    let web: WKWebView
    func makeNSView(context _: Context) -> WKWebView { web }
    func updateNSView(_: WKWebView, context _: Context) {}
}

struct RPBrowser: View {
    @ObservedObject private var store = RPStore.shared
    @ObservedObject private var tab: RPBrowserTab
    let ws: Int64
    @State private var input = ""

    init(ws: Int64) {
        self.ws = ws
        tab = RPBrowserTab.of(ws)
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 4) {
                Button { tab.web.reload() } label: { Image(systemName: "arrow.clockwise") }
                    .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Refresh")
                TextField("Enter URL or localhost:3000…", text: $input)
                    .textFieldStyle(.plain).font(.brand(-4)).foregroundStyle(Theme.text)
                    .padding(.horizontal, 8).padding(.vertical, 5)
                    .background(Theme.base, in: RoundedRectangle(cornerRadius: 6))
                    .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(Theme.border))
                    .onSubmit { tab.go(input) }
                Button { if let u = URL(string: tab.url), !tab.url.isEmpty { NSWorkspace.shared.open(u) } } label: { Image(systemName: "arrow.up.right.square") }
                    .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Open in system browser")
            }
            .padding(6)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }

            ZStack {
                WebHost(web: tab.web).opacity(tab.url.isEmpty ? 0 : 1)
                if tab.url.isEmpty {
                    empty("globe", "Enter a URL above to browse", "Works best with localhost dev servers.")
                } else if !tab.error.isEmpty {
                    empty("powerplug", tab.error, "Start a dev server on this address, or ask the agent to start one.")
                        .background(Theme.sidebar)
                }
            }
        }
        .onAppear { input = tab.url; follow() }
        .onChange(of: store.browserNav) { follow() }
        .onChange(of: tab.url) { _, u in input = u }
    }

    /// A URL pushed from outside (chat link, Dev servers' Preview): navigate once per request.
    func follow() {
        guard tab.handledNav != store.browserNav, let u = store.browserURL[ws] else { return }
        tab.handledNav = store.browserNav
        tab.go(u)
    }

    func empty(_ icon: String, _ title: String, _ hint: String) -> some View {
        VStack(spacing: 8) {
            Image(systemName: icon).font(.system(size: 30)).foregroundStyle(Theme.muted.opacity(0.5))
            Text(title).font(.ui(-2)).foregroundStyle(Theme.text2).multilineTextAlignment(.center)
            Text(hint).font(.ui(-4)).foregroundStyle(Theme.muted).multilineTextAlignment(.center)
            if !tab.url.isEmpty { chip("Retry", Theme.accent) { tab.go(tab.url) } }
        }
        .padding(20).frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: Dev servers

/// Listening processes under this workspace (Go: lsof), polled every 3 s; Preview opens the Browser surface.
struct RPDevServers: View {
    @EnvironmentObject var b: Burrow
    let cwd: String
    @State private var servers: [JSON] = []
    @State private var error = ""

    var body: some View {
        VStack(spacing: 0) {
            if !error.isEmpty { Text(error).font(.brand(-5)).foregroundStyle(Theme.red).padding(8) }
            if servers.isEmpty {
                VStack(spacing: 8) {
                    Spacer()
                    Pet(state: "idle", size: 44)
                    Text(cwd.isEmpty ? "Open a workspace." : "No dev server is running in this workspace.")
                        .font(.brand(-3)).foregroundStyle(Theme.muted)
                    Spacer()
                }.frame(maxWidth: .infinity)
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(servers, id: \.pid) { s in row(s) }
                    }
                }
            }
        }
        .task {
            while !Task.isCancelled {
                await refresh()
                try? await Task.sleep(for: .seconds(3))
            }
        }
    }

    func row(_ s: JSON) -> some View {
        HStack(spacing: 8) {
            Text(":\(s.i("port"))").font(.brand(-4, weight: .bold)).foregroundStyle(Theme.accent)
            Text(s.s("command")).font(.brand(-5)).foregroundStyle(Theme.text2).lineLimit(1).truncationMode(.middle)
                .help(s.s("command"))
            Spacer(minLength: 0)
            Text("pid \(s.i("pid"))").font(.brand(-6)).foregroundStyle(Theme.muted)
            chip("Preview", Theme.accent) { RPStore.shared.openBrowser("http://localhost:\(s.i("port"))") }
            chip("Kill", Theme.red) {
                do { try await b.call("kill_dev_server", ["pid": s.i("pid")]) } catch { self.error = error.localizedDescription }
                await refresh()
            }
        }
        .padding(.horizontal, 10).padding(.vertical, 7)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border.opacity(0.6)).frame(height: 1) }
    }

    func refresh() async {
        guard !cwd.isEmpty else { servers = []; return }
        do {
            servers = try await b.call("list_dev_servers", ["workspacePath": cwd]) as? [JSON] ?? []
            error = ""
        } catch { self.error = error.localizedDescription }
    }
}

private extension Dictionary where Key == String, Value == Any {
    var pid: Int64 { i("pid") }
}

// MARK: Markdown doc

/// A rendered .md file (plan, spec, README) opened from a chat link; re-read every 2 s so a plan the agent is
/// still writing updates live.
struct RPDoc: View {
    @EnvironmentObject var b: Burrow
    let path: String
    let cwd: String
    @State private var text: String?
    @State private var error = ""

    var rel: String {
        let root = cwd.hasSuffix("/") ? String(cwd.dropLast()) : cwd
        return !root.isEmpty && path.hasPrefix(root + "/") ? String(path.dropFirst(root.count + 1)) : path
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 6) {
                Text(rel).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(1).truncationMode(.head)
                Spacer()
                Button { openFileInRP(path) } label: { Image(systemName: "pencil") }
                    .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Edit")
            }
            .padding(.horizontal, 12).padding(.vertical, 7)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
            ScrollView {
                Group {
                    if !error.isEmpty { Text(error).font(.brand(-4)).foregroundStyle(Theme.red) }
                    else if let text { Markdown(text: text).font(.ui(-1)).foregroundStyle(Theme.text).textSelection(.enabled) }
                    else { ProgressView().controlSize(.small) }
                }
                .padding(14).frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task {
            // ponytail: 2 s poll like Vue; an fs watch if it ever matters.
            while !Task.isCancelled {
                do {
                    let t = try await b.call("read_text_file_checked", ["path": path]) as? String ?? ""
                    if t != text { text = t }
                    error = ""
                } catch { self.error = error.localizedDescription }
                try? await Task.sleep(for: .seconds(2))
            }
        }
    }
}

// MARK: Side chat

/// Side chats of the thread on screen: quick questions on a fork of its current session (Go: chatside.go), so the
/// thread itself — even mid-turn — is never disturbed. The chat view is the RP's sub-agent detail.
struct RPSideChat: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var store = RPStore.shared
    let parent: JSON
    @State private var creating = false
    @State private var error = ""

    var body: some View {
        let pid = parent.i("id")
        let list = b.allChats.filter { $0.i("parent_chat_id") == pid && $0.s("kind") == "side" && $0.i("archived_at") == 0 }
            .sorted { $0.i("id") > $1.i("id") }
        let current = list.first { $0.i("id") == store.sidePick } ?? list.first
        VStack(spacing: 0) {
            HStack(spacing: 6) {
                Image(systemName: "bubble.left.and.bubble.right").foregroundStyle(Theme.text)
                if list.count > 1 {
                    Menu(current.map(title) ?? "Side chat") {
                        ForEach(list, id: \.id) { c in Button(title(c)) { store.sidePick = c.i("id") } }
                    }
                    .menuStyle(.borderlessButton).fixedSize()
                } else {
                    Text(current.map(title) ?? "Side chat").lineLimit(1)
                }
                Spacer()
                if let current {
                    Button { Task { await b.archiveThread(current) } } label: { Image(systemName: "archivebox") }
                        .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Archive this side chat")
                }
                if parent.s("kind") != "side" {
                    Button { Task { await create() } } label: { Image(systemName: "plus") }
                        .buttonStyle(.plain).foregroundStyle(Theme.muted).disabled(creating)
                        .help("New side chat from the chat's current state")
                }
            }
            .font(.brand(-4, weight: .bold)).foregroundStyle(Theme.text)
            .padding(.horizontal, 10).padding(.vertical, 7)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }

            if !error.isEmpty { Text(error).font(.brand(-5)).foregroundStyle(Theme.red).padding(8) }
            if let current {
                SubagentDetailView(chatID: current.i("id"), b: b, nav: false).id(current.i("id"))
            } else {
                VStack(spacing: 12) {
                    Spacer()
                    Text("Ask a quick question about this chat. The side chat starts from everything the chat knows right now and leaves the chat itself alone, even mid-turn.")
                        .font(.ui(-3)).foregroundStyle(Theme.muted).multilineTextAlignment(.center)
                    chip("Start a side chat", Theme.accent, disabled: creating) { await create() }
                    Spacer()
                }
                .padding(20).frame(maxWidth: .infinity)
            }
        }
    }

    func title(_ c: JSON) -> String { c.s("title").isEmpty ? "Side chat \(c.i("id"))" : c.s("title") }

    /// Go creates (or reuses an unused) side chat and forks the parent's session on its first send;
    /// it refuses generic ACP / no session yet — shown as the error.
    func create() async {
        creating = true
        defer { creating = false }
        do {
            let row = try await b.call("chat_side_create", ["chatId": parent.i("id")]) as? JSON
            try? await b.refresh()
            if let id = row?.i("id") { store.sidePick = id }
            error = ""
        } catch { self.error = error.localizedDescription }
    }
}
