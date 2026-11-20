import SwiftUI

/// The bell + notification history (TitleBar.vue's notifications dropdown).
struct AttentionBell: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var center = ToastCenter.shared
    @State private var open = false

    var body: some View {
        Button { open.toggle() } label: {
            ZStack(alignment: .topTrailing) {
                Image(systemName: center.unreadCount > 0 ? "bell.badge" : "bell")
                if center.unreadCount > 0 {
                    Circle().fill(worstColor).frame(width: 7, height: 7).offset(x: 2, y: -2)
                }
            }
        }
        .help(center.unreadCount > 0 ? "\(center.unreadCount) unread notifications" : "Notifications")
        .popover(isPresented: $open, arrowEdge: .bottom) { AttentionCenterList(close: { open = false }).environmentObject(b) }
    }

    private var worstColor: Color {
        let unread = center.history.filter { !$0.read }
        if unread.contains(where: { $0.kind == .error }) { return Theme.red }
        if unread.contains(where: { $0.kind == .done }) { return Theme.green }
        return Theme.accent
    }
}

struct AttentionCenterList: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var center = ToastCenter.shared
    let close: () -> Void

    var body: some View {
        let unread = center.history.filter { !$0.read }, read = center.history.filter(\.read)
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Notifications").font(.brand(-2, weight: .bold)).foregroundStyle(Theme.text)
                Spacer()
                Button("Next ⌘⇧U") { jumpToAttention(b); close() }.disabled(nextAttention(b) == nil)
                Button("Mark all read") { center.markAllRead() }.disabled(unread.isEmpty)
                Button("Clear") { center.clear() }.disabled(center.history.isEmpty)
            }
            .buttonStyle(.plain).font(.brand(-4)).foregroundStyle(Theme.accent)
            .padding(12)
            Rectangle().fill(Theme.border).frame(height: 1)
            if center.history.isEmpty {
                VStack(spacing: 8) {
                    Pet(state: "idle", size: 40)
                    Text("Nothing yet").font(.ui(-2)).foregroundStyle(Theme.muted)
                }
                .frame(maxWidth: .infinity).padding(24)
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 2) {
                        if !unread.isEmpty { group("Unread", unread) }
                        if !read.isEmpty { group("Earlier", read) }
                    }
                    .padding(6)
                }
                .frame(maxHeight: 420)
            }
        }
        .frame(width: 360)
        .background(Theme.panel)
    }

    @ViewBuilder private func group(_ title: String, _ items: [ToastItem]) -> some View {
        Text(title.uppercased()).font(.brand(-5, weight: .bold)).foregroundStyle(Theme.muted)
            .padding(.horizontal, 8).padding(.top, 8).padding(.bottom, 2)
        ForEach(items) { t in row(t) }
    }

    private func row(_ t: ToastItem) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Circle().fill(toastColor(t.kind)).frame(width: 6, height: 6).padding(.top, 5).opacity(t.read ? 0.4 : 1)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(t.title).font(.brand(-3, weight: t.read ? .regular : .bold)).foregroundStyle(Theme.text).lineLimit(1)
                    if t.count > 1 { Text("×\(t.count)").font(.brand(-5)).foregroundStyle(Theme.muted) }
                }
                if !t.body.isEmpty { Text(t.body).font(.ui(-3)).foregroundStyle(Theme.text2).lineLimit(2) }
                Text(Date(timeIntervalSince1970: t.ts), style: .relative).font(.brand(-6)).foregroundStyle(Theme.muted)
            }
            Spacer(minLength: 4)
        }
        .padding(8)
        .contentShape(Rectangle())
        .onTapGesture {
            center.setRead(t.id, true)
            if !t.target.isEmpty { openAttentionTarget(t.target, b); close() }
        }
        .contextMenu {
            Button(t.read ? "Mark unread" : "Mark read") { center.setRead(t.id, !t.read) }
            Button("Remove") { center.remove(t.id) }
        }
    }
}

// MARK: ⌘⇧U — next thing that wants you

/// Candidates in priority order: permission > waiting > unread done; chats (incl. sub-agents, which open in the
/// RP under their parent) then agent tabs. Cycles: the first candidate after the current selection.
@MainActor func attentionQueue(_ b: Burrow) -> [String] {
    func rank(_ state: String) -> Int? {
        switch state { case "waiting_approval": 0; case "waiting_input": 1; default: nil }
    }
    var out: [(Int, Int64, String)] = []
    for c in b.allChats where c.i("archived_at") == 0 {
        let id = c.i("id"), parent = c.i("parent_chat_id")
        let target = parent != 0 ? "chat:\(parent)|sub:\(id)" : "chat:\(id)"
        if let r = rank(b.state(id)) { out.append((r, b.phase(id).i("updated_at"), target)) }
        else if b.unread(id), parent == 0 { out.append((2, b.phase(id).i("turn_ended_at"), target)) }
    }
    for (key, p) in b.phases where key.hasPrefix("pty:") {
        // ponytail: PTY "unread done" needs Terminal.vue's per-device receipts; only waiting tabs count here.
        if let r = rank(p.s("state")) { out.append((r, p.i("updated_at"), key)) }
    }
    return out.sorted { ($0.0, $0.1) < ($1.0, $1.1) }.map(\.2)
}

@MainActor func nextAttention(_ b: Burrow) -> String? {
    let q = attentionQueue(b)
    let cur = b.selectedTab.map { "pty:\($0)" } ?? b.selectedChat.map { "chat:\($0)" } ?? ""
    let mine = { (t: String) in t == cur || t.hasPrefix(cur + "|") }
    if let last = q.lastIndex(where: mine), let n = q[(last + 1)...].first(where: { !mine($0) }) { return n }
    return q.first { !mine($0) } ?? q.first // only the current thread's own sub-agent left: open it
}

@MainActor func jumpToAttention(_ b: Burrow) {
    if let t = nextAttention(b) { openAttentionTarget(t, b) } else { toast("Nothing needs you", kind: .info) }
}

// MARK: Settings → Permissions (chatPermissionRules)

/// The "Always allow" rules list; shell mounts it in Settings (`AttentionRulesView()`).
struct AttentionRulesView: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var store = AttentionStore.shared

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Tools and Bash commands chats may run without asking (\"Always\" on a permission card).")
                .font(.ui(-2)).foregroundStyle(Theme.text2)
            if store.rules.isEmpty {
                Text("No rules yet.").font(.ui(-2)).foregroundStyle(Theme.muted)
            }
            ForEach(store.rules, id: \.self) { r in
                HStack {
                    Text(r).font(.brand(-2)).foregroundStyle(Theme.text)
                    Spacer()
                    Button { Task { await store.removeRule(r, b) } } label: { Image(systemName: "trash") }
                        .buttonStyle(.plain).foregroundStyle(Theme.red).help("Remove rule")
                }
                .padding(.horizontal, 10).padding(.vertical, 6)
                .background(Theme.panel, in: RoundedRectangle(cornerRadius: 7))
            }
        }
    }
}
