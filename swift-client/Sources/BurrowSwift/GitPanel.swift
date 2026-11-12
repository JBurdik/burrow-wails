import AppKit
import SwiftUI

// MARK: - git plumbing

struct GitOut {
    let stdout: String
    let stderr: String
    let code: Int64
    var message: String { (stderr.isEmpty ? stdout : stderr).trimmingCharacters(in: .whitespacesAndNewlines) }
}

extension Burrow {
    /// run_git (no stdin) / run_git_input (stdin; forces LC_ALL=C). Never throws on a non-zero exit — check `.code`.
    func git(_ cwd: String, _ args: [String], stdin: String? = nil) async throws -> GitOut {
        let raw = try await stdin == nil
            ? call("run_git", ["cwd": cwd, "args": args])
            : call("run_git_input", ["cwd": cwd, "args": args, "stdin": stdin!])
        let o = raw as? JSON ?? [:]
        return GitOut(stdout: o.s("stdout"), stderr: o.s("stderr"), code: o.i("code"))
    }

    /// config.json (read_config returns it as a string). Read fresh each time, like the Vue stores' getConfig at use time.
    func readConfig() async -> JSON {
        guard let s = try? await call("read_config") as? String,
              let o = (try? JSONSerialization.jsonObject(with: Data(s.utf8))) as? JSON else { return [:] }
        return o
    }

    /// `textGenPrefs()` in src/stores/git.ts.
    func textGenPrefs() async -> JSON {
        let ui = await readConfig()["uiPrefs"] as? JSON ?? [:]
        return ["model": ui.s("textGenerationModel"), "policy": ui.s("textGenerationPolicy"), "rules": ui.s("textGenerationRules")]
    }
}

// MARK: - model

@MainActor
final class GitModel: ObservableObject {
    let cwd: String
    let b: Burrow
    @Published var status = GitStatus()
    @Published var opState: String? // merge | rebase | cherry-pick | revert
    @Published var log: [(hash: String, subject: String, author: String, rel: String)] = []
    @Published var notRepo = false
    @Published var commitMsg = ""
    @Published var busy: String? // "Committing…", "Pushing…" — one at a time
    @Published var error: String?
    @Published var notice: String?
    // git window state (GitWindow*.swift / GitBranch*.swift); loaded by refresh() only when `full`
    @Published var refs: [GitRef] = []
    @Published var worktrees: [GitWorktree] = []
    @Published var stashes: [GitStash] = []
    /// Bumped on every refresh — the "history may have moved" signal the graph reloads on.
    @Published var rev = 0
    /// One confirm / name prompt at a time, rendered by whoever hosts the model (ChangesPane, GitWindow).
    @Published var ask: GitAsk?
    var full = false

    init(cwd: String, b: Burrow, full: Bool = false) { self.cwd = cwd; self.b = b; self.full = full }

    var hasUpstream: Bool { !status.upstream.isEmpty }
    var hasChanges: Bool { !status.isEmpty }
    var localBranches: Set<String> { Set(refs.filter { $0.kind == .local }.map(\.name)) }

    func refresh() async {
        guard !cwd.isEmpty, let st = try? await b.git(cwd, ["status", "--porcelain=v1", "-b", "-uall"]) else { return }
        notRepo = st.code != 0 && st.stderr.contains("not a git repository")
        guard st.code == 0 else { return }
        status = parseGitStatus(st.stdout)
        opState = await readOpState()
        let l = (try? await b.git(cwd, ["log", "-15", "--pretty=format:%H%x1f%s%x1f%an%x1f%cr"]))?.stdout ?? ""
        log = l.split(separator: "\n").compactMap {
            let p = $0.components(separatedBy: "\u{1f}")
            return p.count == 4 ? (p[0], p[1], p[2], p[3]) : nil
        }
        if full {
            async let r = b.git(cwd, ["for-each-ref", "--sort=refname",
                                      "--format=%(refname)%1f%(refname:short)%1f%(objectname)%1f%(HEAD)%1f%(upstream:track)",
                                      "refs/heads", "refs/remotes", "refs/tags"])
            async let w = b.git(cwd, ["worktree", "list", "--porcelain"])
            async let st = b.git(cwd, ["stash", "list", "--format=%gd%x1f%s%x1f%cr"])
            refs = parseRefs((try? await r)?.stdout ?? "")
            worktrees = parseWorktrees((try? await w)?.stdout ?? "")
            stashes = parseStashes((try? await st)?.stdout ?? "")
        }
        rev += 1
    }

    /// MERGE/CHERRY_PICK/REVERT_HEAD vanish when the op ends; a rebase is read from `git status` prose.
    private func readOpState() async -> String? {
        func has(_ ref: String) async -> Bool { (try? await b.git(cwd, ["rev-parse", "-q", "--verify", ref]))?.code == 0 }
        if await has("MERGE_HEAD") { return "merge" }
        if (try? await b.git(cwd, ["status"], stdin: ""))?.stdout.contains("rebase in progress") == true { return "rebase" }
        if await has("CHERRY_PICK_HEAD") { return "cherry-pick" }
        if await has("REVERT_HEAD") { return "revert" }
        return nil
    }

    /// One door for every write: error surfaced, status refreshed. Returns success.
    @discardableResult
    func op(_ title: String, _ args: [String], stdin: String = "", done: String? = nil) async -> Bool {
        defer { Task { await refresh() } }
        do {
            let out = try await b.git(cwd, args, stdin: stdin)
            guard out.code == 0 else { error = "\(title) failed: \(out.message)"; return false }
            error = nil
            if let done { notice = done }
            return true
        } catch {
            self.error = "\(title) failed: \(error.localizedDescription)"
            return false
        }
    }

    // hunks — `--unidiff-zero` lets a zero-context hunk apply.
    private let apply = ["apply", "--unidiff-zero", "--whitespace=nowarn"]
    func stageHunk(_ h: DiffHunk) async { await op("Stage hunk", apply + ["--cached", "-"], stdin: h.patch) }
    func unstageHunk(_ h: DiffHunk) async { await op("Unstage hunk", apply + ["--cached", "--reverse", "-"], stdin: h.patch) }
    func discardHunk(_ h: DiffHunk) async { await op("Discard hunk", apply + ["--reverse", "-"], stdin: h.patch) }

    func stage(_ path: String) async { await op("Stage", ["add", "--", path]) }
    func unstage(_ path: String) async { await op("Unstage", ["reset", "-q", "HEAD", "--", path]) }
    func stageAll() async { await op("Stage all", ["add", "-A"]) }
    func unstageAll() async { await op("Unstage all", ["reset", "-q", "HEAD"]) }
    func discard(_ f: GitFile, untracked: Bool) async {
        await op("Discard", untracked ? ["clean", "-fd", "-q", "--", f.path] : ["checkout", "--", f.path])
    }

    // conflicts
    func resolveWith(_ path: String, _ side: String) async {
        if await op("Use \(side)", ["checkout", "--\(side)", "--", path]) { await op("Mark resolved", ["add", "--", path]) }
    }
    func markResolved(_ path: String) async { await op("Mark resolved", ["add", "--", path]) }
    func continueOp() async {
        guard let s = opState else { return }
        // a merge "continues" by committing; --no-edit keeps the default message
        await op("Continue \(s)", s == "merge" ? ["commit", "--no-edit"] : [s, "--continue"], done: "\(s.capitalized) continued")
    }
    func abortOp() async {
        guard let s = opState else { return }
        await op("Abort \(s)", [s, "--abort"], done: "\(s.capitalized) aborted")
    }

    /// Commit/generate act on the whole tree when nothing is staged (`git add -A` first), like the Vue store.
    private func stageAllIfNeeded() async {
        if status.staged.isEmpty && !(status.unstaged.isEmpty && status.untracked.isEmpty) {
            _ = try? await b.git(cwd, ["add", "-A"])
            await refresh()
        }
    }

    func generateMessage() async {
        guard busy == nil, !status.isEmpty else { return }
        busy = "Generating message…"
        defer { busy = nil }
        await stageAllIfNeeded()
        var args = await b.textGenPrefs()
        args["cwd"] = cwd
        do {
            let o = try await b.call("generate_commit_message", args) as? JSON ?? [:]
            guard o.i("code") == 0, !o.s("stdout").isEmpty else { error = "Generate failed: \(o.s("stderr"))"; return }
            commitMsg = o.s("stdout").trimmingCharacters(in: .whitespacesAndNewlines)
            error = nil
        } catch { self.error = "Generate failed: \(error.localizedDescription)" }
    }

    /// Empty box → generated message first (t3code: canCommit = hasChanges).
    @discardableResult
    func commit() async -> Bool {
        guard busy == nil, !status.staged.isEmpty || !status.unstaged.isEmpty || !status.untracked.isEmpty else { return false }
        if commitMsg.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { await generateMessage() }
        let msg = commitMsg.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !msg.isEmpty else { error = error ?? "Commit failed: no commit message"; return false }
        busy = "Committing…"
        defer { busy = nil }
        await stageAllIfNeeded()
        guard await op("Commit", ["commit", "-m", msg], done: "Committed: \(msg.split(separator: "\n").first ?? "")") else { return false }
        commitMsg = ""
        return true
    }

    func push() async {
        guard busy == nil else { return }
        busy = "Pushing…"
        defer { busy = nil }
        let args = hasUpstream ? ["push"] : ["push", "-u", "origin", status.branch]
        if await op("Push", args, done: "Pushed") {
            // The push may have moved a linked PR's checks/state (chatpr.go).
            _ = try? await b.call("chat_prs_refresh", ["cwd": cwd])
        }
    }

    func pull() async {
        guard busy == nil, hasUpstream else { return }
        busy = "Pulling…"
        defer { busy = nil }
        await op("Pull", ["pull", "--ff-only"], done: "Pulled")
    }

    func fetch() async {
        guard busy == nil else { return }
        busy = "Fetching…"
        defer { busy = nil }
        await op("Fetch", ["fetch", "--prune"], done: "Fetched")
    }

    /// Amends HEAD with what is staged; an empty box keeps the old message.
    func amend() async {
        guard busy == nil else { return }
        let msg = commitMsg.trimmingCharacters(in: .whitespacesAndNewlines)
        busy = "Amending…"
        defer { busy = nil }
        if await op("Amend", ["commit", "--amend"] + (msg.isEmpty ? ["--no-edit"] : ["-m", msg]), done: "Amended") { commitMsg = "" }
    }

    /// CommitDialog.vue "Commit on new branch": generated branch name (fallback wip/<time>), then commit.
    func commitOnNewBranch() async {
        guard busy == nil, hasChanges else { return }
        if commitMsg.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { await generateMessage() }
        let msg = commitMsg.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !msg.isEmpty else { return }
        var args = await b.textGenPrefs()
        args["cwd"] = cwd
        args["message"] = msg
        args.removeValue(forKey: "rules")
        var name = ((try? await b.call("generate_branch_name", args)) as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if name.isEmpty {
            let f = DateFormatter()
            f.dateFormat = "yyyy-MM-dd'T'HH-mm-ss"
            name = "wip/" + f.string(from: Date())
        }
        guard await op("Create \(name)", ["checkout", "-b", name]) else { return }
        await commit()
    }

    // stash (stores/git.ts)
    func stashPush() async { await op("Stash", ["stash", "push", "--include-untracked"], done: "Stashed") }
    func stashApply(_ ref: String) async { await op("Apply stash", ["stash", "apply", ref], done: "Applied \(ref)") }
    func stashPop(_ ref: String) async { await op("Pop stash", ["stash", "pop", ref], done: "Popped \(ref)") }
    func stashDrop(_ ref: String) async { await op("Drop stash", ["stash", "drop", ref], done: "Dropped \(ref)") }

    // history / branches
    func switchBranch(_ name: String) async { await op("Checkout \(name)", ["checkout", name]) }
    /// `git checkout origin/x` would detach; `switch` DWIMs a tracking branch for the remote's name.
    func checkoutRemote(_ name: String) async {
        await op("Checkout \(name)", ["switch", String(name.drop { $0 != "/" }.dropFirst())])
    }
    func createBranch(_ name: String, at: String = "") async {
        await op("Create \(name)", at.isEmpty ? ["checkout", "-b", name] : ["switch", "-c", name, at])
    }
    func deleteBranch(_ name: String, force: Bool) async { await op("Delete \(name)", ["branch", force ? "-D" : "-d", name]) }
    func deleteTag(_ name: String) async { await op("Delete tag \(name)", ["tag", "-d", name]) }
    func mergeBranch(_ name: String) async { await op("Merge \(name)", ["merge", "--no-edit", name], done: "Merged \(name)") }
    func rebaseOnto(_ name: String) async { await op("Rebase onto \(name)", ["rebase", name], done: "Rebased onto \(name)") }
    func cherryPick(_ hash: String) async { await op("Cherry-pick", ["cherry-pick", hash], done: "Cherry-picked") }
    func revertCommit(_ hash: String) async { await op("Revert", ["revert", "--no-edit", hash], done: "Reverted") }
    func checkoutCommit(_ hash: String) async { await op("Checkout", ["switch", "--detach", hash]) }
    func tagAt(_ name: String, _ hash: String) async { await op("Tag \(name)", ["tag", name, hash], done: "Tagged \(name)") }
    func resetTo(_ hash: String, _ mode: String) async { await op("Reset (\(mode))", ["reset", "--\(mode)", hash], done: "Reset (\(mode))") }
    func gitInit() async { await op("git init", ["init"]) }
}

/// A confirm or name prompt. `input` → the alert shows a text field and `run` gets its value.
struct GitAsk {
    var title: String
    var body = ""
    var input = false
    var confirm = "OK"
    var danger = false
    var run: (String) async -> Void
}

/// Renders `git.ask` as an alert (with a text field for name prompts).
struct GitAskAlert: ViewModifier {
    @ObservedObject var git: GitModel
    @State private var value = ""

    func body(content: Content) -> some View {
        content.alert(git.ask?.title ?? "", isPresented: Binding(get: { git.ask != nil }, set: { if !$0 { git.ask = nil } })) {
            if git.ask?.input == true { TextField("Name", text: $value) }
            Button("Cancel", role: .cancel) { git.ask = nil; value = "" }
            Button(git.ask?.confirm ?? "OK", role: git.ask?.danger == true ? .destructive : nil) {
                let a = git.ask, v = value.trimmingCharacters(in: .whitespacesAndNewlines)
                git.ask = nil
                value = ""
                if a?.input == true && v.isEmpty { return }
                Task { await a?.run(v) }
            }
        } message: { Text(git.ask?.body ?? "") }
    }
}

// MARK: - auto refresh (AutoRefreshButton.vue + useAutoRefresh)

/// Interval menu; the timer runs while the view is on screen. 0 = off.
struct GitAutoRefresh: View {
    let refresh: () async -> Void
    @AppStorage("git.autoRefresh") private var interval = 30
    private static let choices = [5, 10, 30, 60, 0]

    var body: some View {
        Menu {
            ForEach(Self.choices, id: \.self) { n in
                Button { interval = n } label: {
                    if n == interval { Label(n == 0 ? "Off" : "Every \(n)s", systemImage: "checkmark") }
                    else { Text(n == 0 ? "Off" : "Every \(n)s") }
                }
            }
        } label: {
            Image(systemName: interval == 0 ? "clock.badge.xmark" : "clock.arrow.2.circlepath")
        }
        .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
        .foregroundStyle(interval == 0 ? Theme.muted : Theme.text2)
        .help(interval == 0 ? "Auto-refresh off" : "Auto-refresh every \(interval)s")
        .task(id: interval) {
            guard interval > 0 else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(interval))
                if Task.isCancelled { break }
                await refresh()
            }
        }
    }
}

// MARK: - Changes tab

struct ChangesView: View {
    @EnvironmentObject var b: Burrow
    let cwd: String
    let chatID: Int64

    var body: some View { ChangesHost(chatID: chatID, git: GitModel(cwd: cwd, b: b)) }
}

/// The model must outlive body re-evaluation, so it lives in a @StateObject here (ChangesView only has the Burrow).
private struct ChangesHost: View {
    let chatID: Int64
    @StateObject var git: GitModel

    init(chatID: Int64, git: @autoclosure @escaping () -> GitModel) {
        self.chatID = chatID
        _git = StateObject(wrappedValue: git())
    }

    var body: some View { ChangesPane(git: git, chatID: chatID) }
}

/// The Changes surface over a shared model: RP Changes tab (chatID = its thread) and the git window (chatID 0).
struct ChangesPane: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.openWindow) private var openWindow
    @ObservedObject var git: GitModel
    var chatID: Int64 = 0
    /// The git window has its own toolbar; the RP shows a compact header.
    var compactHeader = true
    @State private var resolving: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if compactHeader { header }
            if let e = git.error { banner(e, Theme.red) { git.error = nil } }
            else if let n = git.notice { banner(n, Theme.green) { git.notice = nil } }
            if let path = resolving {
                ConflictResolverView(git: git, path: path) { resolving = nil }
            } else if git.notRepo {
                empty("Not a git repository")
            } else {
                ScrollView {
                    VStack(alignment: .leading, spacing: 10) {
                        if let s = git.opState { opBanner(s) }
                        if !git.status.conflicted.isEmpty {
                            section("Conflicts", git.status.conflicted.count) {
                                ForEach(git.status.conflicted) { f in
                                    GitFileRow(f: f, kind: .conflicted, git: git, onOpen: { resolving = f.path }, ask: ask)
                                }
                            }
                        }
                        if !git.status.staged.isEmpty {
                            section("Staged", git.status.staged.count, action: ("Unstage all", { await git.unstageAll() })) {
                                ForEach(git.status.staged) { f in GitFileRow(f: f, kind: .staged, git: git, ask: ask) }
                            }
                        }
                        if !git.status.unstaged.isEmpty || !git.status.untracked.isEmpty {
                            section("Changes", git.status.unstaged.count + git.status.untracked.count,
                                    action: ("Stage all", { await git.stageAll() })) {
                                ForEach(git.status.unstaged) { f in GitFileRow(f: f, kind: .unstaged, git: git, ask: ask) }
                                ForEach(git.status.untracked) { f in GitFileRow(f: f, kind: .untracked, git: git, ask: ask) }
                            }
                        }
                        if git.status.isEmpty {
                            VStack(spacing: 8) {
                                Pet(state: "done", size: 44)
                                Text("Working tree clean").font(.brand(-3)).foregroundStyle(Theme.muted)
                            }
                            .frame(maxWidth: .infinity).padding(.vertical, 18)
                        }
                        if !git.log.isEmpty { recent }
                    }
                    .padding(10)
                }
                if !git.status.isEmpty && git.opState == nil { CommitBox(git: git) }
            }
        }
        .task { if !git.full { await git.refresh() } }
        // Re-read when the thread's agent settles or starts a new step.
        .onChange(of: b.phase(chatID).s("state")) { Task { await git.refresh() } }
        .onChange(of: b.phase(chatID).i("updated_at")) { if b.inFlight(chatID) { Task { await git.refresh() } } }
        .modifier(GitAskAlert(git: git))
    }

    func ask(_ title: String, _ body: String, _ run: @escaping () async -> Void) {
        git.ask = GitAsk(title: title, body: body, confirm: "Discard", danger: true) { _ in await run() }
    }

    var header: some View {
        HStack(spacing: 6) {
            Image(systemName: "arrow.triangle.branch").foregroundStyle(Theme.accent)
            Text(git.status.branch.isEmpty ? "—" : git.status.branch).foregroundStyle(Theme.text).lineLimit(1)
            if git.status.ahead > 0 { Text("↑\(git.status.ahead)").foregroundStyle(Theme.green) }
            if git.status.behind > 0 { Text("↓\(git.status.behind)").foregroundStyle(Theme.yellow) }
            if !git.hasUpstream && !git.status.branch.isEmpty { Text("no upstream").foregroundStyle(Theme.muted) }
            Spacer()
            if let busy = git.busy {
                ProgressView().controlSize(.mini)
                Text(busy).foregroundStyle(Theme.muted)
            } else {
                iconButton("arrow.down.circle", "Pull (fast-forward)", disabled: !git.hasUpstream) { await git.pull() }
                iconButton("arrow.up.circle", git.hasUpstream ? "Push" : "Push and set upstream") { await git.push() }
                iconButton("arrow.triangle.2.circlepath", "Fetch") { await git.fetch() }
            }
            if !git.status.branch.isEmpty && git.busy == nil {
                iconButton("archivebox", "Stash all changes, incl. untracked", disabled: !git.hasChanges) { await git.stashPush() }
            }
            GitAutoRefresh { await git.refresh() }
            iconButton("arrow.clockwise", "Refresh") { await git.refresh() }
            Button { if let ws = workspaceID { openWindow(id: "git", value: ws) } } label: { Image(systemName: "arrow.up.forward.app") }
                .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Open Git window (⌘⇧G)").disabled(workspaceID == nil)
        }
        .font(.brand(-4))
        .padding(.horizontal, 12).padding(.vertical, 8)
    }

    var workspaceID: Int64? { b.workspaces.first { $0.value.s("path") == git.cwd }?.key }

    func iconButton(_ icon: String, _ help: String, disabled: Bool = false, _ run: @escaping () async -> Void) -> some View {
        Button { Task { await run() } } label: { Image(systemName: icon) }
            .buttonStyle(.plain).foregroundStyle(disabled ? Theme.muted.opacity(0.4) : Theme.muted)
            .disabled(disabled).help(help)
    }

    func banner(_ text: String, _ color: Color, close: @escaping () -> Void) -> some View {
        HStack(alignment: .top, spacing: 6) {
            Text(text).font(.brand(-5)).foregroundStyle(color).textSelection(.enabled).lineLimit(6)
            Spacer(minLength: 0)
            Button(action: close) { Image(systemName: "xmark") }.buttonStyle(.plain).foregroundStyle(Theme.muted).font(.brand(-6))
        }
        .padding(8)
        .background(color.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        .padding(.horizontal, 10).padding(.bottom, 4)
    }

    func opBanner(_ s: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("\(s.capitalized) in progress").font(.brand(-4, weight: .bold)).foregroundStyle(Theme.yellow)
            Text(git.status.conflicted.isEmpty ? "No conflicts left." : "\(git.status.conflicted.count) file(s) still conflicted.")
                .font(.brand(-5)).foregroundStyle(Theme.text2)
            HStack {
                chip("Continue", Theme.green, disabled: !git.status.conflicted.isEmpty) { await git.continueOp() }
                chip("Abort", Theme.red) { await git.abortOp() }
            }
        }
        .padding(10).frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.yellow.opacity(0.07), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.yellow.opacity(0.3)))
    }

    func section<C: View>(_ title: String, _ n: Int, action: (String, () async -> Void)? = nil, @ViewBuilder _ rows: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack {
                Text(title.uppercased()).font(.brand(-6, weight: .bold)).foregroundStyle(Theme.muted)
                Text("\(n)").font(.brand(-6)).foregroundStyle(Theme.muted)
                Spacer()
                if let action {
                    Button(action.0) { Task { await action.1() } }.buttonStyle(.plain).font(.brand(-6)).foregroundStyle(Theme.accent)
                }
            }
            .padding(.horizontal, 4).padding(.bottom, 3)
            rows()
        }
    }

    var recent: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("RECENT COMMITS").font(.brand(-6, weight: .bold)).foregroundStyle(Theme.muted).padding(.horizontal, 4)
            ForEach(git.log, id: \.hash) { c in
                HStack(spacing: 6) {
                    Text(String(c.hash.prefix(7))).foregroundStyle(Theme.accent)
                    Text(c.subject).foregroundStyle(Theme.text2).lineLimit(1)
                    Spacer(minLength: 4)
                    Text(c.rel).foregroundStyle(Theme.muted).lineLimit(1)
                }
                .font(.brand(-5)).padding(.horizontal, 4)
                .help("\(c.subject)\n\(c.author) · \(c.rel)")
            }
        }
        .padding(.top, 4)
    }

    func empty(_ text: String) -> some View {
        VStack(spacing: 8) {
            Spacer()
            Pet(state: "idle", size: 44)
            Text(text).font(.brand(-3)).foregroundStyle(Theme.muted)
            Spacer()
        }.frame(maxWidth: .infinity)
    }
}

/// Small square-ish action chip used across the git / PR views.
func chip(_ title: String, _ color: Color, disabled: Bool = false, _ run: @escaping () async -> Void) -> some View {
    Button { Task { await run() } } label: {
        Text(title).font(.brand(-5, weight: .bold)).foregroundStyle(disabled ? Theme.muted : color)
            .padding(.horizontal, 9).padding(.vertical, 4)
            .background((disabled ? Theme.muted : color).opacity(0.12), in: RoundedRectangle(cornerRadius: 6))
    }
    .buttonStyle(.plain).disabled(disabled)
}

// MARK: file row + inline diff

enum GitFileKind { case staged, unstaged, untracked, conflicted }

struct GitFileRow: View {
    @EnvironmentObject var b: Burrow
    let f: GitFile
    let kind: GitFileKind
    @ObservedObject var git: GitModel
    var onOpen: (() -> Void)?
    let ask: (String, String, @escaping () async -> Void) -> Void
    @State private var open = false
    @State private var diff = ""
    @State private var hover = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: open ? "chevron.down" : "chevron.right").font(.system(size: 8, weight: .bold))
                    .foregroundStyle(Theme.muted).frame(width: 10)
                Text(letter).font(.brand(-5, weight: .bold)).foregroundStyle(color).frame(width: 14)
                Text((f.path as NSString).lastPathComponent).font(.brand(-4)).foregroundStyle(Theme.text).lineLimit(1)
                Text((f.path as NSString).deletingLastPathComponent).font(.brand(-5)).foregroundStyle(Theme.muted)
                    .lineLimit(1).truncationMode(.head)
                Spacer(minLength: 0)
                if hover { actions }
            }
            .padding(.horizontal, 6).padding(.vertical, 5)
            .background(hover ? Theme.hover : .clear, in: RoundedRectangle(cornerRadius: 7))
            .contentShape(Rectangle())
            .onHover { hover = $0 }
            .onTapGesture {
                if kind == .conflicted { onOpen?(); return }
                open.toggle()
            }
            if open { hunks }
        }
        .task(id: "\(open)\(git.status.staged.count)\(git.status.unstaged.count)\(git.log.first?.hash ?? "")") {
            if open { await loadDiff() }
        }
    }

    @ViewBuilder var actions: some View {
        HStack(spacing: 8) {
            switch kind {
            case .staged:
                act("minus", "Unstage") { await git.unstage(f.path) }
            case .unstaged, .untracked:
                act("arrow.uturn.backward", "Discard changes") {
                    ask("Discard changes in \((f.path as NSString).lastPathComponent)?",
                        kind == .untracked ? "The untracked file is deleted. This cannot be undone."
                                           : "Uncommitted changes are lost and cannot be restored.") {
                        await git.discard(f, untracked: kind == .untracked)
                    }
                }
                act("plus", "Stage") { await git.stage(f.path) }
            case .conflicted:
                act("arrow.left", "Use ours (current)") { await git.resolveWith(f.path, "ours") }
                act("arrow.right", "Use theirs (incoming)") { await git.resolveWith(f.path, "theirs") }
                act("checkmark", "Mark resolved") { await git.markResolved(f.path) }
            }
        }
        .font(.system(size: 10, weight: .semibold))
    }

    func act(_ icon: String, _ help: String, _ run: @escaping () async -> Void) -> some View {
        Button { Task { await run() } } label: { Image(systemName: icon) }
            .buttonStyle(.plain).foregroundStyle(Theme.text2).help(help)
    }

    @ViewBuilder var hunks: some View {
        let hs = splitHunks(diff)
        if hs.isEmpty {
            GitDiffText(text: diff.isEmpty ? "(no textual diff)" : diff).frame(maxHeight: 360)
        } else {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(hs) { h in
                    VStack(alignment: .leading, spacing: 0) {
                        HStack(spacing: 8) {
                            Text(h.header).font(.brand(-6)).foregroundStyle(Theme.accent).lineLimit(1)
                            Spacer()
                            if kind == .staged {
                                chip("Unstage", Theme.text2) { await git.unstageHunk(h) }
                            } else {
                                chip("Discard", Theme.red) {
                                    ask("Discard this hunk?", "The changes in this block are lost.") { await git.discardHunk(h) }
                                }
                                chip("Stage", Theme.green) { await git.stageHunk(h) }
                            }
                        }
                        .padding(.horizontal, 8).padding(.vertical, 4)
                        .background(Theme.accent.opacity(0.06))
                        DiffLines(lines: h.lines)
                    }
                    .background(Theme.terminal)
                    .clipShape(RoundedRectangle(cornerRadius: 10))
                    .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
                }
            }
            .padding(.leading, 18).padding(.vertical, 4)
        }
    }

    func loadDiff() async {
        switch kind {
        case .untracked:
            let body = (try? await b.call("read_text_file", ["path": git.cwd + "/" + f.path]) as? String) ?? ""
            diff = body.components(separatedBy: "\n").prefix(2000).map { "+" + $0 }.joined(separator: "\n")
        case .staged:
            diff = (try? await b.git(git.cwd, ["diff", "--cached", "--", f.path]))?.stdout ?? ""
        default:
            diff = (try? await b.git(git.cwd, ["diff", "--", f.path]))?.stdout ?? ""
        }
    }

    var letter: String {
        switch kind {
        case .untracked: "U"
        case .conflicted: "!"
        case .staged: String(f.x)
        case .unstaged: String(f.y)
        }
    }

    var color: Color {
        switch letter {
        case "A", "U": Theme.green
        case "D", "!": Theme.red
        case "R": Theme.accent
        default: Theme.yellow
        }
    }
}

/// Hunk body lines, coloured. ponytail: no horizontal scroll inside a hunk; long lines wrap.
struct DiffLines: View {
    let lines: [String]
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(lines.enumerated()), id: \.offset) { _, l in
                let (fg, bg) = GitDiffText.style(Substring(l))
                Text(l.isEmpty ? " " : l).font(.brand(-5)).foregroundStyle(fg)
                    .padding(.horizontal, 8).frame(maxWidth: .infinity, alignment: .leading).background(bg)
            }
        }
        .textSelection(.enabled)
        .padding(.vertical, 3)
    }
}

// MARK: commit box

struct CommitBox: View {
    @ObservedObject var git: GitModel

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            TextField("Commit message (empty = generated)", text: $git.commitMsg, axis: .vertical)
                .textFieldStyle(.plain).font(.brand(-4)).foregroundStyle(Theme.text)
                .lineLimit(1...6)
                .padding(10)
                .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
            HStack(spacing: 6) {
                chip("✦ Generate", Theme.accent, disabled: git.busy != nil) { await git.generateMessage() }
                Spacer()
                chip("Commit", Theme.text, disabled: git.busy != nil) { await git.commit() }
                chip("Commit & Push", Theme.green, disabled: git.busy != nil) {
                    if await git.commit() { await git.push() }
                }
                Menu {
                    Button("Commit on new branch") { Task { await git.commitOnNewBranch() } }
                    Divider()
                    Button(git.status.staged.isEmpty ? "Amend last commit (message only)" : "Amend last commit with staged") { amend() }
                } label: { Image(systemName: "chevron.down") }
                    .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
                    .foregroundStyle(Theme.text2).disabled(git.busy != nil).help("More commit actions")
            }
            Text(git.status.staged.isEmpty ? "Nothing staged — commits every change." : "Commits \(git.status.staged.count) staged file(s).")
                .font(.brand(-6)).foregroundStyle(Theme.muted)
        }
        .padding(10)
        .overlay(alignment: .top) { Rectangle().fill(Theme.border).frame(height: 1) }
    }

    /// Rewriting a commit that is already on the upstream needs a force push afterwards — say so first.
    func amend() {
        let pushed = git.hasUpstream && git.status.ahead == 0
        let what = git.commitMsg.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "keeps its message" : "gets the new message"
        git.ask = GitAsk(title: "Amend the last commit?",
                         body: "It takes the staged changes and \(what)."
                             + (pushed ? " It is already pushed: the next push will need --force." : ""),
                         confirm: "Amend", danger: pushed) { _ in await git.amend() }
    }
}

// MARK: conflict resolver (src/components/git/ConflictResolver.vue)

struct ConflictResolverView: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var git: GitModel
    let path: String
    let close: () -> Void
    @State private var text: String?
    @State private var error = ""

    var abs: String { git.cwd + "/" + path }

    var body: some View {
        let segs = text.map(parseConflicts) ?? []
        let count = segs.filter { if case .conflict = $0 { true } else { false } }.count
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Button(action: close) { Label("Back", systemImage: "chevron.left") }.buttonStyle(.plain).foregroundStyle(Theme.accent)
                Text(path).foregroundStyle(Theme.text2).lineLimit(1).truncationMode(.head)
                Spacer()
                chip("Ours", Theme.text2) { await git.resolveWith(path, "ours"); close() }
                chip("Theirs", Theme.text2) { await git.resolveWith(path, "theirs"); close() }
            }
            .font(.brand(-4)).padding(.horizontal, 12).padding(.bottom, 6)
            if !error.isEmpty { Text(error).font(.brand(-5)).foregroundStyle(Theme.red).padding(.horizontal, 12) }
            if text != nil && count == 0 {
                HStack {
                    Text("No conflict markers left.").font(.brand(-4)).foregroundStyle(Theme.text2)
                    chip("Mark resolved", Theme.green) { await git.markResolved(path); close() }
                }.padding(.horizontal, 12).padding(.bottom, 6)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(segs.enumerated()), id: \.offset) { i, s in
                        switch s {
                        case .text(let ls):
                            plain(ls.count > 8 ? Array(ls.prefix(3)) + ["⋯ \(ls.count - 6) unchanged lines"] + Array(ls.suffix(3)) : ls)
                        case let .conflict(ours, theirs, base, ol, tl):
                            conflictBlock(index: segs[..<i].filter { if case .conflict = $0 { true } else { false } }.count,
                                          ours: ours, theirs: theirs, base: base, ol: ol, tl: tl, segs: segs)
                        }
                    }
                }
                .padding(10)
            }
            .background(Theme.terminal)
        }
        .task(id: git.status.conflicted.count) { await load() }
    }

    func plain(_ ls: [String]) -> some View {
        Text(ls.joined(separator: "\n")).font(.brand(-5)).foregroundStyle(Theme.text2)
            .frame(maxWidth: .infinity, alignment: .leading).textSelection(.enabled)
    }

    func conflictBlock(index: Int, ours: [String], theirs: [String], base: [String]?, ol: String, tl: String,
                       segs: [ConflictSegment]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                chip("Current", Theme.green) { await pick(segs, index, .ours) }
                chip("Incoming", Theme.accent) { await pick(segs, index, .theirs) }
                chip("Both", Theme.text2) { await pick(segs, index, .both) }
                chip("Both ↕", Theme.text2) { await pick(segs, index, .bothReversed) }
            }
            .padding(6)
            side("Current \(ol)", ours, Theme.green)
            if let base { side("Common ancestor", base, Theme.muted) }
            side("Incoming \(tl)", theirs, Theme.accent)
        }
        .background(Theme.panel)
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
        .padding(.vertical, 4)
    }

    func side(_ label: String, _ ls: [String], _ c: Color) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label).font(.brand(-6, weight: .bold)).foregroundStyle(c)
            Text(ls.isEmpty ? " " : ls.joined(separator: "\n")).font(.brand(-5)).foregroundStyle(Theme.text).textSelection(.enabled)
        }
        .padding(.horizontal, 8).padding(.vertical, 5)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(c.opacity(0.08))
    }

    func load() async {
        do { text = try await b.call("read_text_file", ["path": abs]) as? String ?? ""; error = "" }
        catch { self.error = "Can't read file: \(error.localizedDescription)" }
    }

    func pick(_ segs: [ConflictSegment], _ index: Int, _ how: Resolution) async {
        let next = resolveConflict(segs, index: index, how)
        do {
            try await b.call("write_text_file", ["path": abs, "content": next])
            text = next
        } catch { self.error = "Can't write file: \(error.localizedDescription)" }
    }
}

// MARK: diff text (also used by file previews)

struct GitDiffText: View {
    let text: String

    var body: some View {
        ScrollView([.vertical, .horizontal]) {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(Array(text.split(separator: "\n", omittingEmptySubsequences: false).enumerated()), id: \.offset) { _, line in
                    let (fg, bg) = Self.style(line)
                    Text(line.isEmpty ? " " : String(line))
                        .font(.brand(-5))
                        .foregroundStyle(fg)
                        .padding(.horizontal, 10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(bg)
                }
            }
            .textSelection(.enabled)
            .padding(.vertical, 6)
        }
        .background(Theme.terminal)
    }

    static func style(_ l: Substring) -> (Color, Color) {
        if l.hasPrefix("+++") || l.hasPrefix("---") || l.hasPrefix("diff ") || l.hasPrefix("index ") { return (Theme.muted, .clear) }
        if l.hasPrefix("@@") { return (Theme.accent, Theme.accent.opacity(0.08)) }
        if l.hasPrefix("+") { return (Theme.green, Theme.green.opacity(0.10)) }
        if l.hasPrefix("-") { return (Theme.red, Theme.red.opacity(0.10)) }
        return (Theme.text2, .clear)
    }
}
