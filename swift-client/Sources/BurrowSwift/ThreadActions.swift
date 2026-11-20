import Foundation
import SwiftUI

/// Thread lifecycle state that isn't on the chat row: pins (config `sidebarPinnedChats`, shared with the desktop)
/// and which dialog is open. Dialogs are presented once, by the Sidebar (`threadDialogs()`).
@MainActor
final class ThreadStore: ObservableObject {
    static let shared = ThreadStore()
    @Published var pinned: [Int64] = []
    @Published var renaming: JSON?
    @Published var linkingPR: JSON?
    @Published var moving: JSON?
    @Published var schedulesOpen = false
    /// Rewind puts the cut message back in the composer (ThreadView reads and clears it).
    @Published var composerRefill: String?
    private var subscribed = false

    func start(_ b: Burrow) {
        guard !subscribed else { return }
        subscribed = true
        b.subscribe("config-changed") { [weak self] _, p in
            guard let p = p as? JSON, p.s("key") == "sidebarPinnedChats" else { return }
            self?.pinned = Self.ids(p["value"])
        }
        Task { await loadPins(b) }
    }

    func loadPins(_ b: Burrow) async {
        guard let raw = try? await b.call("read_config") as? String,
              let cfg = (try? JSONSerialization.jsonObject(with: Data(raw.utf8))) as? JSON else { return }
        pinned = Self.ids(cfg["sidebarPinnedChats"])
    }

    static func ids(_ v: Any?) -> [Int64] { (v as? [NSNumber])?.map(\.int64Value) ?? [] }

    func togglePin(_ id: Int64, _ b: Burrow) {
        pinned = pinned.contains(id) ? pinned.filter { $0 != id } : pinned + [id]
        let value = pinned
        Task { try? await b.call("config_set", ["key": "sidebarPinnedChats", "value": value]) }
    }
}

/// Snooze presets — port of src/lib/snoozePresets.ts (local time).
func snoozePresets(now: Date = Date()) -> [(label: String, until: Int64)] {
    let cal = Calendar.current
    func at(_ dayOffset: Int, _ hour: Int) -> Int64 {
        let day = cal.date(byAdding: .day, value: dayOffset, to: cal.startOfDay(for: now))!
        return Int64(cal.date(bySettingHour: hour, minute: 0, second: 0, of: day)!.timeIntervalSince1970 * 1000)
    }
    let ms = Int64(now.timeIntervalSince1970 * 1000)
    var out: [(String, Int64)] = [("1 hour", ms + 3_600_000)]
    if at(0, 18) > ms { out.append(("This evening", at(0, 18))) }
    out.append(("Tomorrow 9:00", at(1, 9)))
    let weekday = cal.component(.weekday, from: now) - 1 // 0 = Sunday, like JS getDay()
    let toMonday = (8 - weekday) % 7
    out.append(("Next Monday 9:00", at(toMonday == 0 ? 7 : toMonday, 9)))
    return out
}

/// "#123", or a GitHub / GitLab / Gitea / Azure PR URL — port of parsePrRef (src/lib/prLinkInput.ts).
func parsePrRef(_ input: String, bare: Bool = true) -> (number: Int, url: String?)? {
    let s = input.trimmingCharacters(in: .whitespacesAndNewlines)
    if let m = s.firstMatch(of: #/^https?:\/\/\S+\/(?:pull|pulls|merge_requests|pullrequest)\/(\d+)(?:[\/?#]\S*)?$/#.ignoresCase()),
       let n = Int(m.1), n > 0 {
        return (n, s)
    }
    guard let m = s.firstMatch(of: #/^#?(\d+)$/#), bare || s.hasPrefix("#"), let n = Int(m.1), n > 0 else { return nil }
    return (n, nil)
}

/// PR state → glyph shown in sidebar rows.
func prGlyph(_ state: String) -> (icon: String, color: Color) {
    switch state {
    case "merged": ("arrow.triangle.merge", Theme.accent)
    case "closed": ("xmark.circle", Theme.red)
    case "draft": ("arrow.triangle.pull", Theme.muted)
    default: ("arrow.triangle.pull", Theme.green)
    }
}

extension Burrow {
    /// SaveChats with the full current row + a patch (SaveChats upserts whole rows).
    func patchChat(_ id: Int64, _ patch: JSON) async {
        do {
            let rows = try await call("list_chats") as? [JSON] ?? []
            guard var row = rows.first(where: { $0.i("id") == id }) else { return }
            for (k, v) in patch { row[k] = v }
            try await call("save_chats", ["chats": [row]])
            try await refresh()
        } catch { status = "Save failed: \(error.localizedDescription)" }
    }

    func act(_ what: String, _ body: () async throws -> Void) async {
        do { try await body(); try? await refresh() }
        catch { status = "\(what) failed: \(error.localizedDescription)" }
    }

    func renameThread(_ id: Int64, _ title: String) async {
        let t = title.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return }
        await patchChat(id, ["title": t, "pinned_title": true])
    }

    func stopProcess(_ c: JSON) async {
        let cmd = switch c.s("transport") {
        case "claude-cli": "claude_stop"
        case "codex-app-server": "codex_stop"
        default: "acp_stop"
        }
        _ = try? await call(cmd, ["id": c.i("id")])
    }

    /// Like claudeChats.archive(): its sub-agents go too, processes stopped, row kept.
    func archiveThread(_ c: JSON) async {
        let id = c.i("id")
        for child in allChats where child.i("parent_chat_id") == id && child.i("archived_at") == 0 {
            await archiveThread(child)
        }
        await stopProcess(c)
        if selectedChat == id { selectedChat = nil }
        await patchChat(id, ["archived_at": nowMs()])
    }

    func unarchiveThread(_ id: Int64) async { await patchChat(id, ["archived_at": 0]) }

    /// Manual settle also sleeps the process now (Go refuses unless the phase is settled).
    func settleThread(_ id: Int64) async {
        await patchChat(id, ["settled_override": "settled"])
        _ = try? await call("chat_sleep", ["chatId": id])
    }

    /// "Move back to active" wins over a snooze.
    func unsettleThread(_ c: JSON) async {
        await patchChat(c.i("id"), ["settled_override": "active"])
        if c.i("snoozed_until") > 0 { await snooze(c.i("id"), 0) }
    }

    func snooze(_ id: Int64, _ until: Int64) async {
        await act("Snooze") { try await call("chat_snooze", ["chatId": id, "untilMs": until]) }
    }

    func markUnread(_ id: Int64) async {
        await act("Mark unread") { try await call("seen_mark_unread", ["key": "chat:\(id)"]) }
        seen["chat:\(id)"] = 0
    }

    func deleteThread(_ c: JSON) async {
        await stopProcess(c)
        if selectedChat == c.i("id") { selectedChat = nil }
        await act("Delete") { try await call("delete_chat", ["id": c.i("id")]) }
        if ThreadStore.shared.pinned.contains(c.i("id")) { ThreadStore.shared.togglePin(c.i("id"), self) }
    }

    func sideChat(_ id: Int64) async {
        await act("Side chat") {
            if let row = try await call("chat_side_create", ["chatId": id]) as? JSON {
                try await refresh(); openSubagent(row.i("id")) // a side chat never takes the main pane
            }
        }
    }

    /// Review in a new chat (Codex native review/start, else a prompt), base auto-detected.
    func reviewChanges(_ c: JSON) async {
        await act("Review") {
            if let r = try await call("review_changes", ["workspaceId": c.i("workspace_id"), "parentChatId": c.i("id"), "base": "auto"]) as? JSON {
                try await refresh(); openSubagent(r.i("chat_id")) // a child of this thread → the RP
            }
        }
    }

    func moveToWorktree(_ id: Int64, branch: String) async {
        await act("Move to worktree") { try await call("chat_move_to_worktree", ["chatId": id, "branch": branch]) }
    }

    func leaveWorktree(_ id: Int64) async {
        await act("Leave worktree") { try await call("chat_leave_worktree", ["chatId": id]) }
    }

    func linkPR(_ id: Int64, _ ref: String) async {
        await act("Link PR") { try await call("chat_link_pr", ["chatId": id, "ref": ref]) }
    }

    func unlinkPR(_ id: Int64) async {
        await act("Unlink PR") { try await call("chat_unlink_pr", ["chatId": id]) }
    }

    func limitAction(_ cmd: String, _ id: Int64) async {
        await act("Usage limit") { try await call(cmd, ["chatId": id]) }
    }

    /// `index` is the transcript index (pageStart + local). Claude only.
    func rewind(_ id: Int64, index: Int, text: String) async -> Bool {
        do {
            try await call("chat_rewind", ["chatId": id, "index": index, "text": text])
            await loadMessages()
            return true
        } catch { status = "Rewind failed: \(error.localizedDescription)"; return false }
    }

    func fork(_ id: Int64, index: Int, text: String) async {
        await act("Fork") {
            if let row = try await call("chat_fork", ["chatId": id, "index": index, "text": text]) as? JSON {
                try await refresh(); selectedChat = row.i("id")
            }
        }
    }

    /// Older history: prepends the page before messages[0]. Returns false when there's nothing older.
    func loadOlder() async -> Bool {
        guard let id = selectedChat, pageStart > 0 else { return false }
        guard let page = try? await call("load_chat_messages_page", ["chatId": id, "before": pageStart, "limit": 120]) as? JSON,
              selectedChat == id, let older = page["messages"] as? [JSON] else { return false }
        // ponytail: assumes no splice landed below pageStart meanwhile (splices are tail-only).
        messages = older + messages
        pageStart = Int(page.i("start"))
        return !older.isEmpty
    }

    /// Sidebar.vue regenerateTitle: rebuild the title from the whole transcript, not just the first prompt.
    func regenerateTitle(_ c: JSON) async {
        let id = c.i("id")
        status = "Regenerating title…"
        var text = c.s("title")
        if let raw = try? await call("load_chat_messages", ["chatId": id]) as? String,
           let msgs = (try? JSONSerialization.jsonObject(with: Data(raw.utf8))) as? [JSON] {
            let t = msgs.filter { !$0.s("text").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
                .map { "\($0.s("role")): \($0.s("text"))" }.joined(separator: "\n")
            if !t.isEmpty { text = t }
        }
        let prefs = await textGenPrefs()
        let cwd = workspaces[c.i("workspace_id")]?.s("path") ?? ""
        guard let title = try? await call("generate_chat_title", ["cwd": cwd, "model": prefs.s("model"), "policy": prefs.s("policy"), "text": text]) as? String,
              !title.isEmpty else { status = "Title regeneration failed"; return }
        await patchChat(id, ["title": title])
        status = "Title regenerated"
    }

    func searchChats(_ q: String) async -> [JSON] {
        (try? await call("search_chats", ["query": q, "limit": 30])) as? [JSON] ?? []
    }

    // MARK: new-thread options

    func uiPrefs() async -> JSON {
        guard let raw = try? await call("read_config") as? String,
              let cfg = (try? JSONSerialization.jsonObject(with: Data(raw.utf8))) as? JSON else { return [:] }
        var prefs = cfg["uiPrefs"] as? JSON ?? [:]
        prefs["_projectSettings"] = cfg["projectSettings"]
        return prefs
    }

    /// worktreePath (src/lib/projectSettings.ts): <project worktreesDir | uiPrefs.worktreesDir>/<repo>/<branch with / → ->.
    func worktreePath(_ root: JSON, branch: String) async -> String {
        let prefs = await uiPrefs()
        let project = (prefs["_projectSettings"] as? JSON)?["\(root.i("id"))"] as? JSON
        let dir = [project?.s("worktreesDir") ?? "", prefs.s("worktreesDir")].first { !$0.isEmpty } ?? "~/burrow-worktrees"
        let repo = root.s("path").split(separator: "/").last.map(String.init) ?? "repo"
        return "\(dir)/\(repo)/\(branch.replacingOccurrences(of: "/", with: "-"))"
    }

    /// Welcome composer's "new worktree": create under a random burrow/<hex> branch, rename to a task name later.
    func createWorktreeThread(project: JSON, agent: String, model: String, effort: String, mode: String, prompt: String) async {
        let branch = "burrow/" + (0..<4).map { _ in String(format: "%02x", UInt8.random(in: 0...255)) }.joined()
        do {
            let base = (try? await call("run_git", ["cwd": project.s("path"), "args": ["branch", "--show-current"]]) as? JSON)?
                .s("stdout").trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            let path = await worktreePath(project, branch: branch)
            guard let wt = try await call("create_worktree", [
                "repoPath": project.s("path"), "name": branch, "path": path, "branch": branch, "baseRef": base,
            ]) as? JSON else { return }
            try await refresh()
            await createThread(workspaceID: wt.i("id"), agent: agent, model: model, effort: effort, mode: mode, prompt: prompt)
            let prefs = await uiPrefs()
            if let slug = try? await call("generate_branch_name", [
                "cwd": wt.s("path"), "model": prefs.s("textGenerationModel"), "policy": prefs.s("textGenerationPolicy"), "message": prompt,
            ]) as? String, !slug.isEmpty, "burrow/\(slug)" != branch {
                _ = try? await call("rename_worktree_branch", ["id": wt.i("id"), "oldBranch": branch, "newBranch": "burrow/\(slug)"])
                try? await refresh()
            }
        } catch { status = "Worktree failed: \(error.localizedDescription)" }
    }

    /// openPrInWorktree (src/lib/prCheckout.ts): PR head → worktree + chat linked to it, prompt left as a draft.
    /// Returns the draft for the caller's composer.
    func checkoutPR(project: JSON, number: Int, url: String?, agent: String) async -> String? {
        status = "Checking out PR #\(number)…"
        do {
            guard let pr = try await call("forge_pr_view", ["cwd": project.s("path"), "number": number]) as? JSON else { return nil }
            if let url, !url.lowercased().hasPrefix(pr.s("url").lowercased()) {
                throw CallError(message: "\(url) is not a pull request of \(project.s("name"))")
            }
            let branch = pr.s("headRefName").isEmpty ? "pr-\(number)" : pr.s("headRefName")
            let path = await worktreePath(project, branch: branch)
            guard let wt = try await call("forge_pr_checkout", ["repoPath": project.s("path"), "number": number, "branch": branch, "path": path]) as? JSON,
                  let row = try await call("remote_create_chat", ["workspaceId": wt.i("id"), "agentKind": agent, "model": "", "effort": "", "permissionMode": "default"]) as? JSON
            else { return nil }
            let draft = "Review / continue PR #\(number): \(pr.s("title"))"
            _ = try? await call("chat_link_pr", ["chatId": row.i("id"), "ref": "#\(number)"])
            await patchChat(row.i("id"), ["title": "PR #\(number): \(pr.s("title"))"])
            ThreadStore.shared.composerRefill = draft
            selectedChat = row.i("id")
            status = "PR #\(number) checked out"
            return draft
        } catch {
            status = "Couldn't check out PR #\(number): \(error.localizedDescription)"
            return nil
        }
    }
}
