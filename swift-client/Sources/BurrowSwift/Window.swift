import AppKit
import SwiftUI

// MARK: - Pop-out thread window

/// A thread in its own window. ThreadView/Composer read `b.messages` / `b.selectedChat`, so each pop-out owns a
/// second Burrow (own /v2/ws connection, same endpoint) with that chat selected.
// ponytail: one socket per pop-out window; share one transport with per-chat transcript stores if windows multiply.
struct ThreadWindow: View {
    let chatID: Int64
    @StateObject private var b = Burrow()
    @AppStorage("theme") private var theme = "plum"
    @AppStorage("fontSize") private var fontSize = 15.0

    var body: some View {
        Group {
            if let c = b.chat(chatID) {
                ThreadView(chat: c).id("\(chatID)-\(fontSize)")
            } else {
                VStack(spacing: 10) {
                    Pet(state: "idle", size: 48)
                    Text(b.status).font(.brand(-2)).foregroundStyle(Theme.muted)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(Theme.base)
        .navigationTitle(b.chat(chatID)?.s("title") ?? "Thread")
        .environmentObject(b)
        .tint(Theme.accent)
        .preferredColorScheme(Theme.isDark ? .dark : .light)
        .id(theme)
        .frame(minWidth: 420, minHeight: 360)
        .task {
            await b.connect()
            b.selectedChat = chatID
        }
        .onDisappear { b.stop() }
    }
}

// MARK: - Dock menu (recent threads)

final class AppDelegate: NSObject, NSApplicationDelegate {
    weak var burrow: Burrow?

    @MainActor func applicationDockMenu(_ sender: NSApplication) -> NSMenu? {
        guard let b = burrow else { return nil }
        let menu = NSMenu()
        for c in b.chats.sorted(by: { $0.i("last_activity_at") > $1.i("last_activity_at") }).prefix(8) {
            let item = NSMenuItem(title: c.s("title"), action: #selector(openThread(_:)), keyEquivalent: "")
            item.target = self
            item.representedObject = NSNumber(value: c.i("id"))
            menu.addItem(item)
        }
        return menu
    }

    @MainActor @objc func openThread(_ item: NSMenuItem) {
        guard let id = (item.representedObject as? NSNumber)?.int64Value else { return }
        burrow?.selectedChat = id
        NSApp.activate(ignoringOtherApps: true)
        NSApp.windows.first { $0.isVisible && $0.canBecomeMain }?.makeKeyAndOrderFront(nil)
    }
}

// MARK: - Menus

struct ShellCommands: Commands {
    @ObservedObject var b: Burrow
    @ObservedObject var shell = Shell.shared
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        CommandGroup(after: .toolbar) {
            Button("Bigger Text") { zoom(1) }.keyboardShortcut("+")
            Button("Smaller Text") { zoom(-1) }.keyboardShortcut("-")
            Button("Actual Size") { UserDefaults.standard.set(15.0, forKey: "fontSize") }.keyboardShortcut("0")
        }
        CommandGroup(after: .newItem) {
            // In-app pickers (SpotlightPicker.swift): paths come from the core, which may be another machine.
            // ⌘⇧O, not ⌘O — ThreadDetails' ⌘O opens the workspace in an editor.
            Button("Open Project…") { shell.showSpotlight(.openProject) }.keyboardShortcut("o", modifiers: [.command, .shift])
            Button("New Project…") { shell.showSpotlight(.newProject) }.keyboardShortcut("n", modifiers: [.command, .shift])
        }
        CommandMenu("Go") {
            Button("Spotlight…") { shell.spotlight = true }.keyboardShortcut("p")
            Button("Back") { shell.go(b, forward: false) }.keyboardShortcut("[").disabled(!shell.canBack)
            Button("Forward") { shell.go(b, forward: true) }.keyboardShortcut("]").disabled(!shell.canForward)
            Divider()
            // ⌘1…9 = threads, like the desktop's keymap.ts; projects moved to ⌘⌥1…9.
            ForEach(Array((shell.mainKey ? b.jumpThreads : []).prefix(9).enumerated()), id: \.offset) { i, c in
                Button(c.s("title")) { b.selectedChat = c.i("id") }.keyboardShortcut(KeyEquivalent(Character("\(i + 1)")))
            }
            Divider()
            ForEach(Array((shell.mainKey ? b.orderedProjects : []).prefix(9).enumerated()), id: \.offset) { i, w in
                Button(w.s("name")) { b.openProject(w) }.keyboardShortcut(KeyEquivalent(Character("\(i + 1)")), modifiers: [.command, .option])
            }
        }
        CommandGroup(before: .windowArrangement) {
            Button("Open Thread in New Window") {
                if let id = b.selectedChat { openWindow(id: "thread", value: id) }
            }
            .keyboardShortcut("o", modifiers: [.command, .option])
            .disabled(b.selectedChat == nil)
            Divider()
        }
    }

    func zoom(_ d: Double) {
        let cur = UserDefaults.standard.double(forKey: "fontSize")
        UserDefaults.standard.set(min(28, max(11, (cur > 0 ? cur : 15) + d)), forKey: "fontSize")
    }
}
