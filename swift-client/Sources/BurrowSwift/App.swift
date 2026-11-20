import AppKit
import SwiftUI

// MARK: - App

struct ContentView: View {
    @EnvironmentObject var b: Burrow
    @AppStorage("fontSize") private var fontSize = 15.0
    @AppStorage("rp.open") private var rpOpen = true
    @AppStorage("theme") private var theme = "plum"
    @ObservedObject private var shell = Shell.shared

    var body: some View {
        NavigationSplitView {
            Sidebar()
                .navigationSplitViewColumnWidth(min: 240, ideal: 300)
                .toolbar {
                    Button { b.selectedChat = nil; b.selectedTab = nil } label: { Label("New thread", systemImage: "square.and.pencil") }
                        .keyboardShortcut("n")
                        .disabled(b.projects.isEmpty)
                }
        } detail: {
            Group {
            if let pty = b.selectedTab {
                TerminalDetailSlot(ptyID: pty).id(pty)
            } else if let c = b.chat(b.selectedChat) {
                ThreadView(chat: c).id("\(c.i("id"))-\(fontSize)")
                    .inspector(isPresented: $rpOpen) {
                        RightPanel(chat: c)
                            .inspectorColumnWidth(min: 260, ideal: 340, max: 600)
                    }
                    .toolbar {
                        Button { rpOpen.toggle() } label: { Label("Right panel", systemImage: "sidebar.right") }
                            .keyboardShortcut("b", modifiers: [.command, .option])
                    }
            } else {
                NewThreadView().id(fontSize)
            }
            }
            .toolbarBackground(Theme.base, for: .windowToolbar)
        }
        // Theme members are computed: re-key the tree so a theme switch repaints everything (AttentionSlot stays mounted).
        .id(theme)
        .background { AttentionSlot() }
        .safeAreaInset(edge: .top, spacing: 0) {
            if let why = shell.incompatible {
                Text(why).font(.ui(-2)).foregroundStyle(Theme.text)
                    .frame(maxWidth: .infinity).padding(8)
                    .background(Theme.red.opacity(0.25))
            }
        }
        .overlay { AttentionToasts() }
        // Sub-agents and side chats never take the main pane: their parent does, the child opens in the RP.
        .onChange(of: b.selectedChat) { _, id in
            guard let id, let c = b.chat(id), c.i("parent_chat_id") != 0 else { return }
            b.selectedChat = c.i("parent_chat_id")
            openSubagent(id)
        }
        .tint(Theme.accent)
        .preferredColorScheme(Theme.isDark ? .dark : .light)
        .sheet(isPresented: $shell.spotlight) { Spotlight() }
        .onChange(of: Shell.sel(b)) { old, _ in shell.moved(from: old) }
    }
}

@main
struct BurrowSwiftApp: App {
    @StateObject private var b = Burrow()
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    init() { NSApplication.shared.setActivationPolicy(.regular) } // SPM executable: no bundle → force a Dock app

    var body: some Scene {
        WindowGroup("Burrow", id: "main") {
            ContentView().environmentObject(b)
                .frame(minWidth: 760, minHeight: 480)
                .task { delegate.burrow = b; ShellControl.shared.install(b); Shell.shared.trackKeyWindow(); await b.connect(); await ConfigStore.shared.attach(b) }
        }
        .commands { ShellCommands(b: b) }
        .commands { GitWindowCommands(b: b) }

        WindowGroup("Thread", id: "thread", for: Int64.self) { $id in
            if let id { ThreadWindow(chatID: id) }
        }

        WindowGroup("Git", id: "git", for: Int64.self) { $ws in GitWindow(wsID: ws).environmentObject(b) }

        Settings { SettingsView().environmentObject(b) }
    }
}
