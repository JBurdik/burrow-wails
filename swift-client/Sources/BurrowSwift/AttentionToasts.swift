import AppKit
import SwiftUI

// In-app toasts + their history (ToastStack.vue + stores/notifications.ts).

enum ToastKind: String, Codable { case done, info, error, pending }

struct ToastItem: Identifiable, Codable {
    var id: Int
    var title: String
    var body: String = ""
    var kind: ToastKind
    /// Where a click goes: "chat:<id>", "chat:<parent>|sub:<id>", "pty:<id>" (see openAttentionTarget).
    var target: String = ""
    var ts: Double = Date().timeIntervalSince1970
    var count = 1
    var read = false
}

/// Shows a toast; anyone may call it. Errors and failures should go through here, not `b.status`.
@MainActor func toast(_ text: String, kind: ToastKind = .info) {
    ToastCenter.shared.push(text, kind: kind)
}

@MainActor
final class ToastCenter: ObservableObject {
    static let shared = ToastCenter()

    @Published var toasts: [ToastItem] = []
    @Published var history: [ToastItem] = []
    /// Inline buttons (Undo, Open, …) — toast-only, history never keeps them.
    private(set) var actions: [Int: (label: String, run: () -> Void)] = [:]
    private var nextID = 0
    private let storeKey = "burrow.notifications"

    init() {
        if let d = UserDefaults.standard.data(forKey: storeKey),
           let h = try? JSONDecoder().decode([ToastItem].self, from: d) { history = h }
        nextID = history.map(\.id).max() ?? 0 // restored rows keep their ids
    }

    var unreadCount: Int { history.filter { !$0.read }.count }

    /// `pending` stays until `resolve`; everything else auto-dismisses after `duration` seconds.
    @discardableResult
    func push(_ title: String, body: String = "", kind: ToastKind, target: String = "",
              action: (label: String, run: () -> Void)? = nil, duration: Double = 5) -> Int {
        nextID += 1
        let t = ToastItem(id: nextID, title: title, body: body, kind: kind, target: target)
        withAnimation(.easeOut(duration: 0.2)) {
            toasts.append(t)
            if toasts.count > 5 { toasts.removeFirst() } // ponytail: cap the stack, oldest goes first
        }
        if let action { actions[t.id] = action }
        guard kind != .pending else { return t.id }
        arm(t.id, duration)
        insertHistory(t)
        return t.id
    }

    /// Turns a pending toast into its final state in place.
    func resolve(_ id: Int, _ title: String, body: String = "", kind: ToastKind) {
        guard let i = toasts.firstIndex(where: { $0.id == id }) else { push(title, body: body, kind: kind); return }
        toasts[i].title = title; toasts[i].body = body; toasts[i].kind = kind
        insertHistory(toasts[i])
        arm(id, 5)
    }

    func dismiss(_ id: Int) {
        actions[id] = nil
        withAnimation(.easeIn(duration: 0.18)) { toasts.removeAll { $0.id == id } }
    }

    func runAction(_ id: Int) {
        let a = actions[id]
        dismiss(id)
        a?.run()
    }

    func markAllRead() { for i in history.indices { history[i].read = true }; persist() }
    func setRead(_ id: Int, _ read: Bool) {
        guard let i = history.firstIndex(where: { $0.id == id }) else { return }
        history[i].read = read; persist()
    }
    func remove(_ id: Int) { history.removeAll { $0.id == id }; persist() }
    func clear() { history = []; persist() }

    private func arm(_ id: Int, _ s: Double) {
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(s))
            self?.dismiss(id)
        }
    }

    /// Same kind/title/target within 60 s collapses into the newest row (count bumps) instead of piling up.
    private func insertHistory(_ t: ToastItem) {
        let now = Date().timeIntervalSince1970
        if var newest = history.first, newest.kind == t.kind, newest.title == t.title,
           newest.target == t.target, now - newest.ts < 60 {
            newest.count += 1; newest.ts = now; newest.read = false; newest.body = t.body
            history[0] = newest
        } else {
            history.insert(t, at: 0)
            if history.count > 50 { history.removeLast() }
        }
        persist()
    }

    private func persist() {
        if let d = try? JSONEncoder().encode(Array(history.prefix(50))) { UserDefaults.standard.set(d, forKey: storeKey) }
    }
}

/// Opens what a notification points at (Notifier clicks, toasts, the bell list).
@MainActor func openAttentionTarget(_ target: String, _ b: Burrow) {
    let parts = target.split(separator: "|")
    guard let head = parts.first else { return }
    if head.hasPrefix("chat:"), let id = Int64(head.dropFirst(5)) {
        b.selectedChat = id
        if parts.count > 1, let sub = Int64(parts[1].dropFirst(4)) { openSubagent(sub) }
    } else if head.hasPrefix("pty:"), let id = Int64(head.dropFirst(4)) {
        b.selectedTab = id
    }
}

func toastColor(_ k: ToastKind) -> Color {
    switch k {
    case .done: Theme.green
    case .info: Theme.accent
    case .error: Theme.red
    case .pending: Theme.muted
    }
}

/// Mounted once as an overlay on ContentView: toasts, PTY permission toasts, the bell, ⌘⇧U, and the
/// `b.status` → toast hook (every `b.status = "… failed: …"` in the app surfaces here without editing callers).
struct AttentionToasts: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var center = ToastCenter.shared
    @ObservedObject private var store = AttentionStore.shared

    var body: some View {
        ZStack {
            stack.frame(maxWidth: .infinity, maxHeight: .infinity, alignment: alignment).padding(20)
            PtyPermissionToasts().frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomTrailing)
                .padding(.trailing, 20).padding(.bottom, 90) // clear of the composer's send button
            Button("") { jumpToAttention(b) }
                .keyboardShortcut("u", modifiers: [.command, .shift])
                .frame(width: 0, height: 0).opacity(0)
        }
        .toolbar { ToolbarItem(placement: .primaryAction) { AttentionBell() } }
        .onReceive(b.$status) { s in statusToast(s) }
    }

    private var stack: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(center.toasts) { t in ToastRow(t: t) }
        }
    }

    /// uiPrefs.toastPosition; "sidebar" (Vue docks it in the sidebar) → bottom-left.
    private var alignment: Alignment {
        switch store.prefs.s("toastPosition") {
        case "top-left": .topLeading
        case "top-center": .top
        case "top-right": .topTrailing
        case "bottom-center": .bottom
        case "bottom-right": .bottomTrailing
        default: .bottomLeading
        }
    }

    /// Connection chatter stays in the sidebar footer; anything else written to b.status becomes a toast.
    private func statusToast(_ s: String) {
        guard b.connected, !s.isEmpty, s != "Connected", s != "Connecting…", s != "Reconnecting…" else { return }
        let l = s.lowercased()
        let err = l.contains("fail") || l.contains("couldn't") || l.contains("error") || l.contains("not connected")
        let parts = s.split(separator: ":", maxSplits: 1)
        if err, parts.count == 2 {
            center.push(String(parts[0]), body: parts[1].trimmingCharacters(in: .whitespaces), kind: .error)
        } else {
            center.push(s, kind: err ? .error : s.hasSuffix("…") ? .info : .done)
        }
    }
}

private struct ToastRow: View {
    @EnvironmentObject var b: Burrow
    let t: ToastItem
    @ObservedObject private var center = ToastCenter.shared

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Circle().fill(toastColor(t.kind)).frame(width: 6, height: 6).padding(.top, 5)
                .opacity(t.kind == .pending ? 0.6 : 1)
            VStack(alignment: .leading, spacing: 2) {
                Text(t.title).font(.brand(-3, weight: .bold)).foregroundStyle(Theme.text).lineLimit(1)
                if !t.body.isEmpty { Text(t.body).font(.ui(-3)).foregroundStyle(Theme.text2).lineLimit(2) }
            }
            Spacer(minLength: 4)
            if let a = center.actions[t.id] {
                Button(a.label) { center.runAction(t.id) }
                    .buttonStyle(.plain).font(.brand(-3, weight: .bold)).foregroundStyle(Theme.accent)
            }
        }
        .padding(.horizontal, 14).padding(.vertical, 10)
        .frame(minWidth: 220, maxWidth: 320, alignment: .leading)
        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
        .shadow(color: .black.opacity(0.35), radius: 10, y: 4)
        .contentShape(Rectangle())
        .onTapGesture {
            if !t.target.isEmpty { openAttentionTarget(t.target, b) }
            center.dismiss(t.id)
        }
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }
}

/// PtyPermissionToasts.vue: every pending PTY permission (oldest first), answerable from anywhere.
/// × only hides the toast — the request stays pending in the tab's banner.
struct PtyPermissionToasts: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject private var store = AttentionStore.shared
    @State private var hidden: Set<String> = []

    var body: some View {
        let all = store.ptyPerms.values.flatMap { $0 }.sorted { $0.i("at") < $1.i("at") }
            .filter { !hidden.contains($0.s("id")) && Int64($0.s("pty_id")) != b.selectedTab } // the tab's own banner covers it
        VStack(alignment: .trailing, spacing: 8) {
            ForEach(all.indices, id: \.self) { i in
                let p = all[i]
                VStack(alignment: .leading, spacing: 8) {
                    HStack(alignment: .top, spacing: 10) {
                        Circle().fill(Theme.yellow).frame(width: 6, height: 6).padding(.top, 5)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("\(p.s("tool_name")) needs permission").font(.brand(-3, weight: .bold)).foregroundStyle(Theme.text)
                            Text(describePtyPermission(p)).font(.ui(-3, .monospaced)).foregroundStyle(Theme.text2)
                                .lineLimit(1).truncationMode(.middle)
                        }
                        .contentShape(Rectangle())
                        .onTapGesture { b.selectedTab = Int64(p.s("pty_id")) }
                        Spacer(minLength: 4)
                        Button { hidden.insert(p.s("id")) } label: { Image(systemName: "xmark").font(.system(size: 9, weight: .bold)) }
                            .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Hide (still pending in the tab)")
                    }
                    HStack {
                        Spacer()
                        PtyPermissionActions(request: p)
                    }
                }
                .padding(.horizontal, 14).padding(.vertical, 10)
                .frame(width: 340)
                .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.yellow.opacity(0.4)))
                .shadow(color: .black.opacity(0.35), radius: 10, y: 4)
            }
        }
    }
}

/// Allow / Always / Deny for one PTY request (PtyPermissionActions.vue). Go closes it; the event clears it.
struct PtyPermissionActions: View {
    @EnvironmentObject var b: Burrow
    let request: JSON

    var body: some View {
        HStack(spacing: 6) {
            Button("Allow") { answer("allow") }.buttonStyle(.borderedProminent).tint(Theme.green)
            Button("Always") { answer("always") }.buttonStyle(.bordered).help("Allow for the rest of this session")
            Button("Deny") { answer("deny") }.buttonStyle(.bordered).tint(Theme.red)
        }
        .controlSize(.small)
    }

    func answer(_ behavior: String) {
        Task {
            // Already closed (answered in the TUI, hook gone): the next pty-permissions event clears it.
            try? await b.call("answer_pty_permission", ["id": request.s("id"), "behavior": behavior, "message": ""])
        }
    }
}

func describePtyPermission(_ p: JSON) -> String {
    let i = p["tool_input"] as? JSON ?? [:]
    for k in ["command", "file_path", "url", "pattern", "path"] { if let s = i[k] as? String { return s } }
    return (try? JSONSerialization.data(withJSONObject: i)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
}
