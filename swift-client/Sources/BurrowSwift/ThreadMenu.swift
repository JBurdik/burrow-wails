import AppKit
import SwiftUI

/// One menu for a thread — the sidebar row's context menu and the thread header's ⋯ menu.
struct ThreadMenuItems: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.openWindow) private var openWindow
    @ObservedObject var store = ThreadStore.shared
    let chat: JSON

    var body: some View {
        let id = chat.i("id")
        let archived = chat.i("archived_at") > 0
        let claude = chat.s("transport") == "claude-cli" || chat.s("agent_kind") == "claude"
        let inWorktree = b.workspaces[chat.i("workspace_id")]?["parent_id"] != nil
        Button("Rename…") { store.renaming = chat }
        Button("Regenerate title") { Task { await b.regenerateTitle(chat) } }
        Button("Open in window") { openWindow(id: "thread", value: id) }
        Button(store.pinned.contains(id) ? "Unpin" : "Pin") { store.togglePin(id, b) }
        if !b.unread(id) && b.state(id) == "done" {
            Button("Mark as unread") { Task { await b.markUnread(id) } }
        }
        Divider()
        if b.settled(chat) {
            Button("Move back to active") { Task { await b.unsettleThread(chat) } }
        } else {
            Button("Settle") { Task { await b.settleThread(id) } }
        }
        Menu("Snooze") {
            ForEach(snoozePresets(), id: \.label) { p in
                Button(p.label) { Task { await b.snooze(id, p.until) } }
            }
            if chat.i("snoozed_until") > nowMs() {
                Divider()
                Button("Unsnooze") { Task { await b.snooze(id, 0) } }
            }
        }
        Divider()
        if claude || chat.s("agent_kind") == "codex" {
            Button("Side chat") { Task { await b.sideChat(id) } }
        }
        Button("Review changes") { Task { await b.reviewChanges(chat) } }
        if inWorktree {
            Button("Leave worktree") { Task { await b.leaveWorktree(id) } }
        } else {
            Button("Move to worktree…") { store.moving = chat }
        }
        if chat.i("pr_number") > 0 {
            Menu("PR #\(chat.i("pr_number")) · \(chat.s("pr_state"))") {
                if let url = URL(string: chat.s("pr_url")), !chat.s("pr_url").isEmpty {
                    Button("Open in browser") { NSWorkspace.shared.open(url) }
                }
                Button("Unlink") { Task { await b.unlinkPR(id) } }
            }
        } else {
            Button("Link pull request…") { store.linkingPR = chat }
        }
        Divider()
        if archived {
            Button("Unarchive") { Task { await b.unarchiveThread(id) } }
        } else {
            Button("Archive") { Task { await b.archiveThread(chat) } }
        }
        Button("Delete", role: .destructive) { SidebarStore.shared.deleteWithUndo([chat], b) }
    }
}

/// Text-field dialogs + delete confirmation. Mounted once (Sidebar), driven by ThreadStore.
struct ThreadDialogs: ViewModifier {
    @EnvironmentObject var b: Burrow
    @ObservedObject var store = ThreadStore.shared
    @State private var text = ""

    func body(content: Content) -> some View {
        content
            .alert("Rename thread", isPresented: shown(\.renaming)) {
                TextField("Title", text: $text)
                Button("Rename") { let c = store.renaming; Task { if let c { await b.renameThread(c.i("id"), text) } } }
                Button("Cancel", role: .cancel) {}
            }
            .alert("Move to a worktree", isPresented: shown(\.moving)) {
                TextField("Branch", text: $text)
                Button("Move") {
                    let c = store.moving, t = text.trimmingCharacters(in: .whitespaces)
                    if let c, !t.isEmpty { Task { await b.moveToWorktree(c.i("id"), branch: t) } }
                }
                Button("Cancel", role: .cancel) {}
            } message: { Text("Takes effect after the current turn settles.") }
            .alert("Link pull request", isPresented: shown(\.linkingPR)) {
                TextField("#123 or PR URL", text: $text)
                Button("Link") {
                    let c = store.linkingPR, t = text.trimmingCharacters(in: .whitespaces)
                    if let c, !t.isEmpty { Task { await b.linkPR(c.i("id"), t) } }
                }
                Button("Cancel", role: .cancel) {}
            }
            .sheet(isPresented: $store.schedulesOpen) { ScheduleSheet().environmentObject(b) }
            .onChange(of: store.renaming?.i("id")) { text = store.renaming?.s("title") ?? "" }
            .onChange(of: store.moving?.i("id")) { text = "" }
            .onChange(of: store.linkingPR?.i("id")) { text = "" }
            .task { store.start(b) }
    }

    func shown(_ kp: ReferenceWritableKeyPath<ThreadStore, JSON?>) -> Binding<Bool> {
        Binding(get: { store[keyPath: kp] != nil }, set: { if !$0 { store[keyPath: kp] = nil } })
    }
}

extension View {
    func threadDialogs() -> some View { modifier(ThreadDialogs()) }
}
