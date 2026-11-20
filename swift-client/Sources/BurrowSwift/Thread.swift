import AppKit
import SwiftUI

// MARK: - Thread

/// Consecutive tool calls collapse into one row — the transcript is mostly tools.
enum Item: Identifiable {
    case message(Int, JSON)
    case tools(Int, [JSON])
    var id: Int {
        switch self { case .message(let i, _), .tools(let i, _): i }
    }
}

func items(_ msgs: [JSON]) -> [Item] {
    var out: [Item] = []
    for (i, m) in msgs.enumerated() where m.s("role") != "system-info" || m.i("subagentChatId") > 0 || !m.s("surface").isEmpty {
        if m.s("role") == "tool" {
            if case .tools(let start, var run) = out.last {
                run.append(m)
                out[out.count - 1] = .tools(start, run)
                continue
            }
            out.append(.tools(i, [m]))
        } else {
            out.append(.message(i, m))
        }
    }
    return out
}

struct ThreadView: View {
    @EnvironmentObject var b: Burrow
    let chat: JSON
    @State private var draft = ""
    @State private var olderDone = false
    @ObservedObject private var store = ThreadStore.shared

    var body: some View {
        let id = chat.i("id")
        VStack(spacing: 0) {
            ThreadHeader(chat: chat)
            // Transcript rendering, autoscroll, find, folds, links: TranscriptScroll.swift (transcript agent).
            TranscriptList(chatID: id, messages: b.messages, start: b.pageStart, busy: b.inFlight(id), loadOlder: { await b.loadOlder() }) { g, m in
                if rewindable { messageMenu(g - b.pageStart, m) }
            } footer: {
                if b.inFlight(id) {
                    HStack(spacing: 10) {
                        Pet(state: b.state(id), size: 34)
                        Text(b.phase(id).s("detail").isEmpty ? label(b.state(id)) : b.phase(id).s("detail"))
                            .font(.brand(-2))
                            .foregroundStyle(Theme.status(b.state(id)))
                    }
                    .padding(.top, 4)
                }
            }
            UsageLimitBanner(chat: chat)
            ForEach(b.requests) { RequestCard(r: $0) }
            ReviewFeedbackBar(chatID: id)
            Composer(draft: $draft, running: b.inFlight(id), chatID: id, send: submit, stop: { Task { await b.interrupt() } }) {
                LaunchBar(
                    agent: .constant(chat.s("agent_kind").isEmpty ? "claude" : chat.s("agent_kind")), agentLocked: true,
                    model: live("model"), effort: live("effort"), mode: live("permission_mode"))
            }
        }
        .background(Theme.base)
        .navigationTitle(chat.s("title").isEmpty ? "Untitled" : chat.s("title"))
        .onReceive(store.$composerRefill) { t in
            guard let t, b.selectedChat == id else { return }
            draft = t; store.composerRefill = nil
        }
    }

    /// Rewind / fork cut the Claude session at a user message (chatrewind.go); Codex / ACP refuse.
    var rewindable: Bool { chat.s("transport") == "claude-cli" && !b.inFlight(chat.i("id")) }

    @ViewBuilder func messageMenu(_ local: Int, _ m: JSON) -> some View {
        let id = chat.i("id"), index = b.pageStart + local, text = m.s("text")
        Button("Rewind to here") {
            Task { if await b.rewind(id, index: index, text: text) { draft = text } }
        }
        Button("Fork from here") { Task { await b.fork(id, index: index, text: text) } }
    }

    func submit() {
        let t = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return }
        draft = ""
        Task { await b.send(t) }
    }

    /// Applies to the running process (chat_set_launch_settings), else on the next start.
    func live(_ key: String) -> Binding<String> {
        Binding(get: { chat.s(key) }, set: { v in Task { await b.setLaunch(chat.i("id"), key, v) } })
    }
}

struct ThreadHeader: View {
    @EnvironmentObject var b: Burrow
    let chat: JSON

    var body: some View {
        let id = chat.i("id")
        HStack(spacing: 10) {
            StatusIcon(state: b.state(id), unread: false)
            Text(chat.s("title").isEmpty ? "Untitled" : chat.s("title"))
                .font(.brand(-1, weight: .bold)).foregroundStyle(Theme.text).lineLimit(1)
            HStack(spacing: 5) {
                ProjectIcon(ws: b.rootWorkspace(chat.i("workspace_id")), size: 14)
                Text(b.rootWorkspace(chat.i("workspace_id")).s("name")).font(.brand(-5)).foregroundStyle(Theme.text2)
            }
            .padding(.horizontal, 6).padding(.vertical, 3)
            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 6))
            .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(Theme.border))
            if !chat.s("branch").isEmpty { Chip(text: chat.s("branch"), icon: "arrow.triangle.branch") }
            if chat.i("pr_number") > 0 { Chip(text: "#\(chat.i("pr_number")) \(chat.s("pr_state"))", icon: "arrow.triangle.pull") }
            Spacer()
            if b.inFlight(id) {
                Text(label(b.state(id))).font(.brand(-4)).foregroundStyle(Theme.status(b.state(id)))
            }
            ThreadDetailsButton(chat: chat)
            Menu { ThreadMenuItems(chat: chat) } label: { Image(systemName: "ellipsis.circle") }
                .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
                .foregroundStyle(Theme.text2).help("Thread actions")
        }
        .padding(.horizontal, 20).padding(.vertical, 10)
        .background(Theme.base)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }
    }
}

struct Chip: View {
    let text: String
    let icon: String
    var body: some View {
        if !text.isEmpty {
            Label(text, systemImage: icon)
                .font(.brand(-5)).foregroundStyle(Theme.text2).lineLimit(1)
                .padding(.horizontal, 7).padding(.vertical, 3)
                .background(Theme.panel, in: RoundedRectangle(cornerRadius: 6))
                .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(Theme.border))
        }
    }
}

func label(_ state: String) -> String {
    switch state {
    case "running": "Working…"
    case "waiting_approval": "Waiting for approval"
    case "waiting_input": "Waiting for you"
    default: state
    }
}

/// Go recorded a usage-limit reset (chats.limit_reset_at): offer to resume automatically then (AgentChat's banner).
struct UsageLimitBanner: View {
    @EnvironmentObject var b: Burrow
    let chat: JSON

    var body: some View {
        let at = chat.i("limit_reset_at"), id = chat.i("id")
        if at > 0 {
            let d = Date(timeIntervalSince1970: Double(at) / 1000)
            let when = Calendar.current.isDateInToday(d) ? d.formatted(date: .omitted, time: .shortened) : d.formatted(date: .abbreviated, time: .shortened)
            let armed = chat.i("limit_resume_at") > 0
            HStack(spacing: 10) {
                Image(systemName: "hourglass").foregroundStyle(Theme.yellow)
                Text(armed ? "Usage limit reached — resuming automatically at \(when)." : "Usage limit reached — resets at \(when).")
                    .font(.ui(-1)).foregroundStyle(Theme.text)
                Spacer()
                if !armed { Button("Resume automatically") { Task { await b.limitAction("chat_limit_arm", id) } } }
                Button(armed ? "Cancel" : "Dismiss") { Task { await b.limitAction("chat_limit_dismiss", id) } }
            }
            .buttonStyle(.borderless).font(.brand(-3))
            .padding(.horizontal, 14).padding(.vertical, 10)
            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.yellow.opacity(0.4)))
            .frame(maxWidth: 860).padding(.horizontal, 24).padding(.bottom, 6)
        }
    }
}
