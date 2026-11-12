import Foundation

// Pure git / PR / surface helpers — Foundation only, so `swiftc GitLogic.swift` + an assert script checks them
// without the app (see gitSelfCheck at the bottom).

// MARK: status (src/stores/git.ts parseStatus)

struct GitFile: Identifiable, Hashable {
    let path: String
    let x: Character
    let y: Character
    var id: String { path }
}

struct GitStatus {
    var branch = ""
    var upstream = ""
    var ahead = 0
    var behind = 0
    var staged: [GitFile] = []
    var unstaged: [GitFile] = []
    var untracked: [GitFile] = []
    var conflicted: [GitFile] = []
    var isEmpty: Bool { staged.isEmpty && unstaged.isEmpty && untracked.isEmpty && conflicted.isEmpty }
}

/// Unmerged XY pairs (git-status(1), "Short Format").
private let conflictCodes: Set<String> = ["DD", "AU", "UD", "UA", "DU", "AA", "UU"]

/// `git status --porcelain=v1 -b -uall` → files by section + the `## branch...upstream [ahead N, behind M]` header.
func parseGitStatus(_ raw: String) -> GitStatus {
    var s = GitStatus()
    for line in raw.components(separatedBy: "\n") {
        if line.hasPrefix("## ") {
            parseBranchHeader(String(line.dropFirst(3)), into: &s)
            continue
        }
        let chars = Array(line)
        guard chars.count > 3 else { continue }
        let x = chars[0], y = chars[1]
        var path = String(chars[3...])
        if let r = path.range(of: " -> ") { path = String(path[r.upperBound...]) }
        let f = GitFile(path: path, x: x, y: y)
        if x == "?" && y == "?" { s.untracked.append(f) }
        else if conflictCodes.contains(String([x, y])) { s.conflicted.append(f) }
        else {
            if x != " " && x != "?" { s.staged.append(f) }
            if y != " " && y != "?" { s.unstaged.append(f) }
        }
    }
    return s
}

private func parseBranchHeader(_ h: String, into s: inout GitStatus) {
    var head = h
    if let br = head.range(of: " [") {
        let info = head[br.upperBound...].dropLast() // "ahead 1, behind 2"
        for part in info.components(separatedBy: ", ") {
            let kv = part.split(separator: " ")
            if kv.count == 2, let n = Int(kv[1]) {
                if kv[0] == "ahead" { s.ahead = n } else if kv[0] == "behind" { s.behind = n }
            }
        }
        head = String(head[..<br.lowerBound])
    }
    if head.hasPrefix("No commits yet on ") { s.branch = String(head.dropFirst("No commits yet on ".count)); return }
    if let dots = head.range(of: "...") {
        s.branch = String(head[..<dots.lowerBound])
        s.upstream = String(head[dots.upperBound...])
    } else {
        s.branch = head
    }
}

// MARK: hunks (src/lib/gitHunks.ts)

struct DiffHunk: Identifiable {
    let id: Int
    let header: String
    /// File header + this hunk only — input for `git apply [--cached] [--reverse] -`.
    let patch: String
    let lines: [String]
    let adds: Int
    let dels: Int
}

/// Splits a one-file `git diff` into standalone one-hunk patches. Binary / mode-only diffs → [].
func splitHunks(_ diff: String) -> [DiffHunk] {
    var lines = diff.components(separatedBy: "\n")
    if lines.last == "" { lines.removeLast() }
    guard let first = lines.firstIndex(where: { $0.hasPrefix("@@") }) else { return [] }
    let fileHeader = lines[..<first].joined(separator: "\n") + "\n"
    var out: [DiffHunk] = []
    var cur: [String] = []
    func flush() {
        guard !cur.isEmpty else { return }
        let body = cur.dropFirst()
        out.append(DiffHunk(id: out.count, header: cur[0], patch: fileHeader + cur.joined(separator: "\n") + "\n",
                            lines: Array(body), adds: body.filter { $0.hasPrefix("+") }.count,
                            dels: body.filter { $0.hasPrefix("-") }.count))
    }
    for line in lines[first...] {
        if line.hasPrefix("diff --git ") { break } // callers pass one file
        if line.hasPrefix("@@") { flush(); cur = [line] } else { cur.append(line) }
    }
    flush()
    return out
}

// MARK: conflict markers (src/lib/conflictMarkers.ts)

enum ConflictSegment {
    case text([String])
    case conflict(ours: [String], theirs: [String], base: [String]?, oursLabel: String, theirsLabel: String)
}

enum Resolution { case ours, theirs, both, bothReversed }

/// Splits on "\n"; an unterminated block (no `>>>>>>>`) stays plain text.
func parseConflicts(_ text: String) -> [ConflictSegment] {
    let lines = text.components(separatedBy: "\n")
    var out: [ConflictSegment] = []
    func plain(_ ls: [String]) {
        guard !ls.isEmpty else { return }
        if case .text(let prev)? = out.last { out[out.count - 1] = .text(prev + ls) } else { out.append(.text(ls)) }
    }
    var i = 0
    while i < lines.count {
        guard lines[i].hasPrefix("<<<<<<<") else { plain([lines[i]]); i += 1; continue }
        let start = i
        let oursLabel = String(lines[i].dropFirst(7)).trimmingCharacters(in: .whitespaces)
        var ours: [String] = [], theirs: [String] = [], base: [String]?
        var part = 0 // 0 ours, 1 base, 2 theirs
        var end = -1
        i += 1
        while i < lines.count {
            let l = lines[i]
            if part == 0 && l.hasPrefix("|||||||") { base = []; part = 1 }
            else if part != 2 && l.hasPrefix("=======") { part = 2 }
            else if part == 2 && l.hasPrefix(">>>>>>>") { end = i; break }
            else if part == 0 { ours.append(l) } else if part == 1 { base!.append(l) } else { theirs.append(l) }
            i += 1
        }
        if end < 0 { plain(Array(lines[start...])); break }
        out.append(.conflict(ours: ours, theirs: theirs, base: base, oursLabel: oursLabel,
                             theirsLabel: String(lines[end].dropFirst(7)).trimmingCharacters(in: .whitespaces)))
        i = end + 1
    }
    return out
}

/// Re-joins segments, replacing conflict `index` (counted among conflicts only) by the chosen side.
func resolveConflict(_ segs: [ConflictSegment], index: Int, _ how: Resolution) -> String {
    var n = -1
    let lines = segs.flatMap { s -> [String] in
        switch s {
        case .text(let ls): return ls
        case let .conflict(ours, theirs, base, ol, tl):
            n += 1
            guard n == index else {
                return ["<<<<<<< \(ol)".trimmingTrailing()] + ours + (base.map { ["|||||||"] + $0 } ?? [])
                    + ["======="] + theirs + [">>>>>>> \(tl)".trimmingTrailing()]
            }
            switch how {
            case .ours: return ours
            case .theirs: return theirs
            case .both: return ours + theirs
            case .bothReversed: return theirs + ours
            }
        }
    }
    return lines.joined(separator: "\n")
}

private extension String {
    func trimmingTrailing() -> String {
        var s = self
        while s.last == " " { s.removeLast() }
        return s
    }
}

// MARK: PR checks (src/stores/git.ts rollupChecks)

/// "pass" | "fail" | "pending" | "none". GitHub spells conclusions SUCCESS/FAILURE, GitLab success/failed.
func rollupChecks(_ checks: [[String: Any]]) -> String {
    if checks.isEmpty { return "none" }
    var pending = false
    for c in checks {
        let conclusion = (c["conclusion"] as? String ?? "").uppercased()
        if conclusion == "FAILURE" || conclusion == "FAILED" { return "fail" }
        if conclusion != "SUCCESS" { pending = true }
    }
    return pending ? "pending" : "pass"
}

/// src/lib/projectSettings.ts worktreePath: `<root>/<repo>/<branch with / → ->`.
func worktreePath(repoPath: String, branch: String, root: String) -> String {
    let repo = repoPath.split(separator: "/").last.map(String.init) ?? "repo"
    return "\(root)/\(repo)/\(branch.replacingOccurrences(of: "/", with: "-"))"
}

// MARK: surfaces

struct TodoItem: Hashable {
    let content: String
    let status: String // pending | in_progress | completed
    let activeForm: String
}

/// src/lib/chatTodos.ts latestTodos: the most recent Claude TodoWrite call's items.
func latestTodos(_ messages: [[String: Any]]) -> [TodoItem] {
    for m in messages.reversed() {
        guard m["role"] as? String == "tool", m["toolRawName"] as? Bool == true, m["text"] as? String == "TodoWrite" else { continue }
        let todos = (m["toolInput"] as? [String: Any])?["todos"] as? [[String: Any]] ?? []
        return todos.compactMap { t in
            guard let content = t["content"] as? String, !content.isEmpty else { return nil }
            let st = t["status"] as? String ?? ""
            return TodoItem(content: content, status: ["in_progress", "completed"].contains(st) ? st : "pending",
                            activeForm: t["activeForm"] as? String ?? "")
        }
    }
    return []
}

/// A Claude background shell task, folded from the chat's task lines (src/lib/backgroundTasks.ts).
struct BgTask: Identifiable {
    let id: String
    var description = ""
    var command = ""
    var outputFile = ""
    var status = "running" // running | completed | failed | killed | ended
    var exitCode: Int?
    var summary = ""
    var ord: Int
    var backgrounded = false
    var shell = true
}

/// ponytail: Claude only (no Codex background terminals), no start/end times — upgrade by porting
/// mergeCodexTerminals + the timestamps from backgroundTasks.ts.
struct BgTaskFold {
    var tasks: [String: BgTask] = [:]
    var calls: [String: (command: String, description: String)] = [:]
    var byToolUse: [String: String] = [:]
    var lastOrd = -1

    var list: [BgTask] { tasks.values.filter { $0.backgrounded && $0.shell }.sorted { $0.ord < $1.ord } }

    private static let terminal: Set<String> = ["completed", "failed", "killed"]

    private mutating func entry(_ id: String, _ ord: Int) {
        if tasks[id] == nil { tasks[id] = BgTask(id: id, ord: ord) }
    }

    private mutating func link(_ id: String, _ toolUseID: Any?) {
        guard let tu = toolUseID as? String, !tu.isEmpty else { return }
        byToolUse[tu] = id
        if let c = calls[tu] {
            if tasks[id]!.command.isEmpty { tasks[id]!.command = c.command }
            if tasks[id]!.description.isEmpty { tasks[id]!.description = c.description }
        }
    }

    private mutating func setStatus(_ id: String, _ st: String) {
        if Self.terminal.contains(tasks[id]!.status) && !Self.terminal.contains(st) { return }
        tasks[id]!.status = st
    }

    mutating func apply(_ line: String, ord: Int) {
        lastOrd = max(lastOrd, ord)
        guard let m = (try? JSONSerialization.jsonObject(with: Data(line.utf8))) as? [String: Any] else { return }
        let content = (m["message"] as? [String: Any])?["content"] as? [[String: Any]] ?? []
        switch m["type"] as? String {
        case "assistant":
            for b in content where b["type"] as? String == "tool_use" && b["name"] as? String == "Bash" {
                guard let id = b["id"] as? String else { continue }
                let input = b["input"] as? [String: Any] ?? [:]
                calls[id] = (input["command"] as? String ?? "", input["description"] as? String ?? "")
                if let known = byToolUse[id] { link(known, id) }
            }
        case "user":
            guard let id = (m["tool_use_result"] as? [String: Any])?["backgroundTaskId"] as? String, !id.isEmpty else { return }
            entry(id, ord)
            tasks[id]!.backgrounded = true
            for b in content where b["type"] as? String == "tool_result" {
                link(id, b["tool_use_id"])
                let text = (b["content"] as? String)
                    ?? (b["content"] as? [[String: Any]] ?? []).compactMap { $0["text"] as? String }.joined(separator: "\n")
                if tasks[id]!.outputFile.isEmpty,
                   let r = text.range(of: #"Output is being written to: (\S+?\.output)\b"#, options: .regularExpression) {
                    tasks[id]!.outputFile = String(text[r].dropFirst("Output is being written to: ".count))
                }
            }
        case "system":
            let tid = m["task_id"] as? String ?? ""
            switch m["subtype"] as? String {
            case "task_started":
                guard !tid.isEmpty else { return }
                entry(tid, ord)
                if let t = m["task_type"] as? String, t != "local_bash" { tasks[tid]!.shell = false }
                if m["is_backgrounded"] as? Bool == true { tasks[tid]!.backgrounded = true }
                if let d = m["description"] as? String, !d.isEmpty { tasks[tid]!.description = d }
                link(tid, m["tool_use_id"])
            case "task_updated":
                guard tasks[tid] != nil else { return }
                let p = m["patch"] as? [String: Any] ?? [:]
                if p["is_backgrounded"] as? Bool == true {
                    tasks[tid]!.backgrounded = true
                    if tasks[tid]!.status == "ended" { tasks[tid]!.status = "running" }
                }
                if let st = p["status"] as? String, Self.terminal.contains(st) { setStatus(tid, st) }
            case "task_notification":
                guard tasks[tid] != nil else { return }
                if let f = m["output_file"] as? String, !f.isEmpty, tasks[tid]!.outputFile.isEmpty { tasks[tid]!.outputFile = f }
                if let sum = m["summary"] as? String {
                    tasks[tid]!.summary = sum
                    if let r = sum.range(of: #"exit code -?\d+"#, options: .regularExpression) {
                        tasks[tid]!.exitCode = Int(sum[r].dropFirst("exit code ".count))
                    }
                }
                let code = tasks[tid]!.exitCode
                switch m["status"] as? String {
                case "completed": setStatus(tid, code != nil && code != 0 ? "failed" : "completed")
                case "failed": setStatus(tid, "failed")
                case "stopped": setStatus(tid, "killed")
                default: setStatus(tid, "ended")
                }
            case "background_tasks_changed":
                let live = Set((m["tasks"] as? [[String: Any]] ?? []).compactMap { $0["task_id"] as? String })
                for (id, t) in tasks where t.backgrounded && t.status == "running" && t.ord < ord && !live.contains(id) {
                    setStatus(id, "ended")
                }
            default: break
            }
        default: break
        }
    }
}

// MARK: self-check

/// Run with: swiftc -o /tmp/gitcheck GitLogic.swift <(echo 'gitSelfCheck()') — or call from a debugger.
func gitSelfCheck() {
    let st = parseGitStatus("## main...origin/main [ahead 2, behind 1]\nM  a.txt\n M b.txt\nMM c.txt\n?? d.txt\nUU e.txt\nR  old -> new.txt\n")
    assert(st.branch == "main" && st.upstream == "origin/main" && st.ahead == 2 && st.behind == 1)
    assert(st.staged.map(\.path) == ["a.txt", "c.txt", "new.txt"])
    assert(st.unstaged.map(\.path) == ["b.txt", "c.txt"])
    assert(st.untracked.map(\.path) == ["d.txt"] && st.conflicted.map(\.path) == ["e.txt"])
    let fresh = parseGitStatus("## No commits yet on main\n")
    assert(fresh.branch == "main" && fresh.upstream.isEmpty)
    assert(parseGitStatus("## feat\n").branch == "feat")

    let diff = "diff --git a/f b/f\nindex 1..2 100644\n--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n-a\n+A\n b\n@@ -10 +10,2 @@\n x\n+y\n"
    let h = splitHunks(diff)
    assert(h.count == 2)
    assert(h[0].patch == "diff --git a/f b/f\nindex 1..2 100644\n--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n-a\n+A\n b\n")
    assert(h[0].adds == 1 && h[0].dels == 1 && h[1].adds == 1 && h[1].dels == 0)
    assert(splitHunks("Binary files differ\n").isEmpty)

    let text = "top\n<<<<<<< HEAD\nmine\n=======\ntheirs\n>>>>>>> feat\nmid\n<<<<<<< HEAD\nm2\n||||||| base\nb2\n=======\nt2\n>>>>>>> feat\nend"
    let segs = parseConflicts(text)
    assert(segs.count == 5)
    assert(resolveConflict(segs, index: 0, .theirs).hasPrefix("top\ntheirs\nmid\n<<<<<<< HEAD\nm2\n|||||||\nb2\n=======\nt2\n>>>>>>> feat\nend"))
    assert(resolveConflict(segs, index: 1, .both) == "top\n<<<<<<< HEAD\nmine\n=======\ntheirs\n>>>>>>> feat\nmid\nm2\nt2\nend")
    assert(resolveConflict(parseConflicts(resolveConflict(segs, index: 0, .ours)), index: 0, .bothReversed) == "top\nmine\nmid\nt2\nm2\nend")
    assert(parseConflicts("<<<<<<< x\nno end").count == 1)

    assert(rollupChecks([]) == "none")
    assert(rollupChecks([["conclusion": "SUCCESS"], ["conclusion": "failed"]]) == "fail")
    assert(rollupChecks([["conclusion": "SUCCESS"], ["status": "IN_PROGRESS"]]) == "pending")
    assert(rollupChecks([["conclusion": "success"]]) == "pass")
    assert(worktreePath(repoPath: "/x/burrow", branch: "feat/a", root: "~/wt") == "~/wt/burrow/feat-a")

    let todos = latestTodos([
        ["role": "tool", "text": "TodoWrite", "toolRawName": true, "toolInput": ["todos": [["content": "old", "status": "pending"]]]],
        ["role": "tool", "text": "TodoWrite", "toolRawName": true, "toolInput": ["todos": [["content": "A", "status": "in_progress"], ["content": "B", "status": "weird"], ["status": "x"]]]],
    ])
    assert(todos == [TodoItem(content: "A", status: "in_progress", activeForm: ""), TodoItem(content: "B", status: "pending", activeForm: "")])

    var f = BgTaskFold()
    f.apply(#"{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","id":"tu1","input":{"command":"npm run dev","description":"dev"}}]}}"#, ord: 1)
    f.apply(#"{"type":"system","subtype":"task_started","task_id":"t1","task_type":"local_bash","tool_use_id":"tu1"}"#, ord: 2)
    f.apply(#"{"type":"user","tool_use_result":{"backgroundTaskId":"t1"},"message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"Output is being written to: /tmp/x.output now"}]}}"#, ord: 3)
    assert(f.list.count == 1 && f.list[0].command == "npm run dev" && f.list[0].outputFile == "/tmp/x.output" && f.list[0].status == "running")
    f.apply(#"{"type":"system","subtype":"task_notification","task_id":"t1","status":"completed","summary":"exited with exit code 1"}"#, ord: 4)
    assert(f.list[0].status == "failed" && f.list[0].exitCode == 1 && f.lastOrd == 4)
    print("gitSelfCheck ok")
}
