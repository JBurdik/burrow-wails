import AppKit
import SwiftUI

// Pull requests (src/components/PullRequestsPanel.vue + src/composables/usePullRequests.ts + src/lib/prCheckout.ts).
// Optional fields of the forge's normalized PullRequest ARE the capability model: no checks → no checks section.

struct PRPanel: View {
    @EnvironmentObject var b: Burrow
    let cwd: String
    let workspaceID: Int64
    @AppStorage("rp.pr.state") private var state = "open"
    @AppStorage("rp.pr.scope") private var scope = ""
    @State private var forge: JSON?
    @State private var items: [JSON] = []
    @State private var query = ""
    @State private var loading = false
    @State private var error = ""
    @State private var selected: JSON?
    @State private var creating = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let pr = selected {
                PRDetail(cwd: cwd, workspaceID: workspaceID, forge: forge ?? [:], pr: pr) { selected = nil; Task { await refresh() } }
                    .id(pr.i("number"))
            } else if creating {
                PRCreate(cwd: cwd) { created in
                    creating = false
                    Task { await refresh(); if let created { selected = created } }
                }
            } else {
                list
            }
        }
        .task(id: cwd) { await loadForge(); await refresh() }
        .onChange(of: state) { Task { await refresh() } }
        .onChange(of: scope) { Task { await refresh() } }
    }

    var list: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                Picker("", selection: $state) {
                    Text("Open").tag("open"); Text("Merged").tag("merged"); Text("Closed").tag("closed")
                }
                .pickerStyle(.segmented).labelsHidden().frame(maxWidth: 200)
                Picker("", selection: $scope) {
                    Text("All").tag(""); Text("Mine").tag("created"); Text("Assigned").tag("assigned")
                }
                .labelsHidden().frame(width: 92)
                Spacer()
                Button { Task { await refresh() } } label: { Image(systemName: "arrow.clockwise") }
                    .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Refresh")
                Button { creating = true } label: { Image(systemName: "plus") }
                    .buttonStyle(.plain).foregroundStyle(Theme.accent).help("Create pull request")
                    .disabled(forge?["authed"] as? Bool != true)
            }
            .font(.brand(-4)).padding(.horizontal, 10).padding(.vertical, 8)
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass").foregroundStyle(Theme.muted)
                TextField("Search pull requests", text: $query).textFieldStyle(.plain).foregroundStyle(Theme.text)
            }
            .font(.brand(-4)).padding(8)
            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
            .padding(.horizontal, 10).padding(.bottom, 6)

            if !error.isEmpty { Text(error).font(.brand(-5)).foregroundStyle(Theme.red).padding(.horizontal, 12).textSelection(.enabled) }
            if loading || forge == nil {
                Spacer(); HStack { Spacer(); ProgressView().controlSize(.small); Spacer() }; Spacer()
            } else if visible.isEmpty {
                emptyState
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 6) {
                        ForEach(visible, id: \.prURL) { pr in
                            Button { selected = pr } label: { PRRow(pr: pr) }.buttonStyle(.plain)
                        }
                    }.padding(10)
                }
            }
        }
    }

    var visible: [JSON] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return q.isEmpty ? items : items.filter { "\($0.i("number")) \($0.s("title")) \($0.s("headRefName"))".lowercased().contains(q) }
    }

    @ViewBuilder var emptyState: some View {
        let f = forge ?? [:]
        VStack(spacing: 10) {
            Spacer()
            Pet(state: "idle", size: 44)
            if f.s("provider").isEmpty {
                Text("Unknown git host for this repository.").font(.brand(-4)).foregroundStyle(Theme.muted)
                Menu("Choose provider…") {
                    ForEach([("github", "GitHub"), ("gitlab", "GitLab"), ("azure", "Azure DevOps"), ("gitea", "Gitea / Forgejo")], id: \.0) { p in
                        Button(p.1) { Task { await setProvider(p.0) } }
                    }
                }
                .frame(width: 180)
            } else if f["installed"] as? Bool != true {
                Text("\(f.s("bin")) is missing — \(f.s("installCmd"))").font(.brand(-4)).foregroundStyle(Theme.muted).textSelection(.enabled)
            } else if f["authed"] as? Bool != true {
                Text("Sign in with \(f.s("authCmd"))").font(.brand(-4)).foregroundStyle(Theme.muted).textSelection(.enabled)
            } else {
                Text("No \(state) pull requests").font(.brand(-3)).foregroundStyle(Theme.muted)
            }
            Spacer()
        }
        .frame(maxWidth: .infinity).padding(.horizontal, 12)
    }

    func loadForge() async {
        guard !cwd.isEmpty else { return }
        forge = (try? await b.call("forge_info", ["cwd": cwd])) as? JSON ?? [:]
    }

    func refresh() async {
        guard !cwd.isEmpty else { return }
        loading = true
        defer { loading = false }
        do {
            items = try await b.call("forge_pr_list", ["cwd": cwd, "scope": scope, "state": state]) as? [JSON] ?? []
            error = ""
        } catch {
            items = []
            self.error = error.localizedDescription
        }
    }

    func setProvider(_ p: String) async {
        let root = b.rootWorkspace(workspaceID)
        _ = try? await b.call("set_forge_provider", ["wsId": root.i("id"), "provider": p])
        await loadForge(); await refresh()
    }
}

struct PRRow: View {
    let pr: JSON
    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text("#\(pr.i("number"))").font(.brand(-5)).foregroundStyle(Theme.accent)
                Spacer()
                PRStateLabel(pr: pr)
            }
            Text(pr.s("title")).font(.system(size: 13)).foregroundStyle(Theme.text).lineLimit(2)
            Text([pr.s("author"), pr.s("headRefName")].filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.brand(-6)).foregroundStyle(Theme.muted).lineLimit(1)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
        .contentShape(Rectangle())
    }
}

struct PRStateLabel: View {
    let pr: JSON
    var body: some View {
        let (t, c): (String, Color) = pr["isDraft"] as? Bool == true ? ("Draft", Theme.yellow)
            : pr.s("state") == "merged" ? ("Merged", Theme.accent)
            : pr.s("state") == "closed" ? ("Closed", Theme.red)
            : (pr.s("reviewDecision").isEmpty ? "Open" : review(pr.s("reviewDecision")), Theme.green)
        Text(t).font(.brand(-6, weight: .bold)).foregroundStyle(c)
    }

    func review(_ d: String) -> String {
        switch d.uppercased() {
        case "APPROVED": "Approved"
        case "CHANGES_REQUESTED": "Changes requested"
        case "REVIEW_REQUIRED": "Review required"
        default: d.capitalized
        }
    }
}

// MARK: detail

struct PRDetail: View {
    @EnvironmentObject var b: Burrow
    let cwd: String
    let workspaceID: Int64
    let forge: JSON
    @State var pr: JSON
    let back: () -> Void
    @State private var error = ""
    @State private var working: String?
    @State private var confirmMerge = false
    @State private var squash = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                Button(action: back) { Label("Pull requests", systemImage: "chevron.left") }.buttonStyle(.plain).foregroundStyle(Theme.accent)
                Spacer()
                if let working { ProgressView().controlSize(.mini); Text(working).foregroundStyle(Theme.muted) }
                if forge["canCheckout"] as? Bool == true {
                    Button { Task { await checkout() } } label: { Image(systemName: "arrow.triangle.branch") }
                        .buttonStyle(.plain).foregroundStyle(Theme.text2).help("Open in a worktree + new thread").disabled(working != nil)
                }
                Button { if let u = URL(string: pr.s("url")) { NSWorkspace.shared.open(u) } } label: { Image(systemName: "arrow.up.right.square") }
                    .buttonStyle(.plain).foregroundStyle(Theme.text2).help("Open in browser")
            }
            .font(.brand(-4)).padding(.horizontal, 12).padding(.vertical, 8)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 1) }

            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    VStack(alignment: .leading, spacing: 4) {
                        HStack { Text("#\(pr.i("number"))").foregroundStyle(Theme.accent); Spacer(); PRStateLabel(pr: pr) }.font(.brand(-5))
                        Text(pr.s("title")).font(.system(size: 14, weight: .semibold)).foregroundStyle(Theme.text).textSelection(.enabled)
                        Text("\(pr.s("author").isEmpty ? "Unknown" : pr.s("author")) · \(pr.s("headRefName")) → \(pr.s("baseRefName"))")
                            .font(.brand(-6)).foregroundStyle(Theme.muted)
                        if pr["additions"] != nil {
                            HStack(spacing: 6) {
                                Text("+\(pr.i("additions"))").foregroundStyle(Theme.green)
                                Text("−\(pr.i("deletions"))").foregroundStyle(Theme.red)
                            }.font(.brand(-6))
                        }
                    }
                    if !error.isEmpty { Text(error).font(.brand(-5)).foregroundStyle(Theme.red).textSelection(.enabled) }
                    if let checks = pr["checks"] as? [JSON], !checks.isEmpty { checksSection(checks) }
                    group("Description") {
                        Text(pr.s("body").isEmpty ? "No description provided." : pr.s("body"))
                            .font(.ui(-2)).foregroundStyle(Theme.text2).textSelection(.enabled)
                    }
                    if let comments = pr["comments"] as? [JSON], !comments.isEmpty {
                        group("Comments · \(comments.count)") {
                            ForEach(Array(comments.enumerated()), id: \.offset) { _, c in
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(c.s("author").isEmpty ? "Unknown" : c.s("author")).font(.brand(-5, weight: .bold)).foregroundStyle(Theme.text)
                                    Text(c.s("body")).font(.ui(-2)).foregroundStyle(Theme.text2).textSelection(.enabled)
                                }
                            }
                        }
                    }
                    if let files = pr["files"] as? [JSON], !files.isEmpty {
                        group("Files · \(files.count)") {
                            ForEach(Array(files.prefix(300).enumerated()), id: \.offset) { _, f in
                                HStack {
                                    Text(f.s("path")).foregroundStyle(Theme.text2).lineLimit(1).truncationMode(.head)
                                    Spacer()
                                    Text("+\(f.i("additions"))").foregroundStyle(Theme.green)
                                    Text("−\(f.i("deletions"))").foregroundStyle(Theme.red)
                                }.font(.brand(-6))
                            }
                        }
                    }
                }
                .padding(12)
            }

            if pr.s("state") == "open" {
                HStack {
                    Toggle("Squash", isOn: $squash).toggleStyle(.checkbox).font(.brand(-5)).foregroundStyle(Theme.text2)
                    Spacer()
                    chip("Merge", Theme.green, disabled: working != nil || pr["isDraft"] as? Bool == true) { confirmMerge = true }
                }
                .padding(10)
                .overlay(alignment: .top) { Rectangle().fill(Theme.border).frame(height: 1) }
            }
        }
        .task { await load() }
        .alert("Merge PR #\(pr.i("number"))\(squash ? " (squash)" : "")?", isPresented: $confirmMerge) {
            Button("Cancel", role: .cancel) {}
            Button("Merge", role: .destructive) { Task { await merge() } }
        } message: { Text("\(pr.s("headRefName")) → \(pr.s("baseRefName"))") }
    }

    func checksSection(_ checks: [JSON]) -> some View {
        let roll = rollupChecks(checks)
        let color = roll == "pass" ? Theme.green : roll == "fail" ? Theme.red : Theme.yellow
        return group("Checks · \(roll)", color) {
            ForEach(Array(checks.enumerated()), id: \.offset) { _, c in
                let concl = c.s("conclusion").uppercased()
                HStack(spacing: 6) {
                    Image(systemName: concl == "SUCCESS" ? "checkmark" : ["FAILURE", "FAILED"].contains(concl) ? "xmark" : "clock")
                        .foregroundStyle(concl == "SUCCESS" ? Theme.green : ["FAILURE", "FAILED"].contains(concl) ? Theme.red : Theme.yellow)
                    Text(c.s("name").isEmpty ? "Check" : c.s("name")).foregroundStyle(Theme.text2).lineLimit(1)
                    Spacer()
                    Text(c.s("conclusion").isEmpty ? c.s("status") : c.s("conclusion")).foregroundStyle(Theme.muted)
                }
                .font(.brand(-6))
            }
        }
    }

    func group<C: View>(_ title: String, _ color: Color = Theme.muted, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title.uppercased()).font(.brand(-6, weight: .bold)).foregroundStyle(color)
            content()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    func load() async {
        working = "Loading…"
        defer { working = nil }
        do { if let full = try await b.call("forge_pr_view", ["cwd": cwd, "number": pr.i("number")]) as? JSON { pr = full } }
        catch { self.error = error.localizedDescription }
    }

    func merge() async {
        working = "Merging…"
        defer { working = nil }
        do {
            try await b.call("forge_pr_merge", ["cwd": cwd, "number": pr.i("number"), "squash": squash])
            error = ""
        } catch { self.error = "Merge failed: \(error.localizedDescription)" }
        working = nil
        await load()
    }

    /// prCheckout.ts openPrInWorktree: worktree named after the PR head, a new chat there, linked to the PR,
    /// with an unsent draft prompt.
    func checkout() async {
        let number = pr.i("number")
        working = "Checking out…"
        defer { working = nil }
        do {
            let root = b.rootWorkspace(workspaceID)
            let cfg = await b.readConfig()
            let ui = cfg["uiPrefs"] as? JSON ?? [:]
            let projectRoot = ((cfg["projectSettings"] as? JSON)?["\(root.i("id"))"] as? JSON)?.s("worktreesDir") ?? ""
            let dir = !projectRoot.isEmpty ? projectRoot : ui.s("worktreesDir").isEmpty ? "~/burrow-worktrees" : ui.s("worktreesDir")
            let branch = pr.s("headRefName").isEmpty ? "pr-\(number)" : pr.s("headRefName")
            guard let wt = try await b.call("forge_pr_checkout", [
                "repoPath": root.s("path"), "number": number, "branch": branch,
                "path": worktreePath(repoPath: root.s("path"), branch: branch, root: dir),
            ]) as? JSON else { throw Burrow.CallError(message: "no worktree returned") }
            // remote_create_chat creates claude / codex only.
            let agent = ui.s("defaultChatAgent") == "codex" ? "codex" : "claude"
            guard let row = try await b.call("remote_create_chat", [
                "workspaceId": wt.i("id"), "agentKind": agent, "model": "", "effort": "", "permissionMode": "",
            ]) as? JSON else { throw Burrow.CallError(message: "no chat returned") }
            let chatID = row.i("id")
            _ = try? await b.call("chat_link_pr", ["chatId": chatID, "ref": "#\(number)"])
            // Left as the new chat's composer draft, never sent (prCheckout.ts).
            ComposerStore.shared.setDraft(chatID, "Review / continue PR #\(number): \(pr.s("title"))")
            try await b.refresh()
            b.selectedChat = chatID
            error = ""
        } catch {
            self.error = "Couldn't check out PR #\(number): \(error.localizedDescription)"
        }
    }
}


// MARK: create

struct PRCreate: View {
    @EnvironmentObject var b: Burrow
    let cwd: String
    let done: (JSON?) -> Void
    @State private var title = ""
    @State private var bodyText = ""
    @State private var base = ""
    @State private var head = ""
    @State private var working: String?
    @State private var error = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Button { done(nil) } label: { Label("Cancel", systemImage: "chevron.left") }.buttonStyle(.plain).foregroundStyle(Theme.accent)
                Spacer()
                if let working { ProgressView().controlSize(.mini); Text(working).foregroundStyle(Theme.muted) }
            }
            .font(.brand(-4))
            Text("\(head.isEmpty ? "…" : head) → \(base.isEmpty ? "default branch" : base)").font(.brand(-5)).foregroundStyle(Theme.muted)
            field("Title", $title, lines: 1...2)
            field("Description", $bodyText, lines: 6...16)
            if !error.isEmpty { Text(error).font(.brand(-5)).foregroundStyle(Theme.red).textSelection(.enabled) }
            HStack {
                chip("✦ Generate", Theme.accent, disabled: working != nil) { await generate() }
                Spacer()
                chip("Create PR", Theme.green, disabled: working != nil || title.trimmingCharacters(in: .whitespaces).isEmpty) { await create() }
            }
            Text("Push the branch first — the forge CLI creates the PR from the remote head.").font(.brand(-6)).foregroundStyle(Theme.muted)
            Spacer()
        }
        .padding(12)
        .task { await prepare() }
    }

    func field(_ p: String, _ text: Binding<String>, lines: ClosedRange<Int>) -> some View {
        TextField(p, text: text, axis: .vertical)
            .textFieldStyle(.plain).font(.brand(-4)).foregroundStyle(Theme.text).lineLimit(lines)
            .padding(10)
            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
    }

    func prepare() async {
        head = (try? await b.git(cwd, ["branch", "--show-current"]))?.stdout.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let o = (try? await b.git(cwd, ["symbolic-ref", "--short", "refs/remotes/origin/HEAD"]))?.stdout ?? ""
        base = o.trimmingCharacters(in: .whitespacesAndNewlines).replacingOccurrences(of: "origin/", with: "")
        // Head commit as the starting point; Generate replaces it.
        title = (try? await b.git(cwd, ["log", "-1", "--format=%s"]))?.stdout.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        bodyText = (try? await b.git(cwd, ["log", "-1", "--format=%b"]))?.stdout.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        await generate()
    }

    /// Best-effort, like usePullRequests.generatedContent: a missing model keeps the commit fallback.
    func generate() async {
        guard !head.isEmpty, !base.isEmpty, head != base else { return }
        working = "Generating…"
        defer { working = nil }
        var args = await b.textGenPrefs()
        args["cwd"] = cwd; args["baseBranch"] = base; args["headBranch"] = head
        if let o = try? await b.call("generate_pr_content", args) as? JSON, !o.s("title").isEmpty {
            title = o.s("title"); bodyText = o.s("body")
        }
    }

    func create() async {
        working = "Creating…"
        defer { working = nil }
        do {
            let pr = try await b.call("forge_pr_create", ["cwd": cwd, "title": title, "body": bodyText, "base": "", "head": ""]) as? JSON
            done(pr)
        } catch { self.error = "Create failed: \(error.localizedDescription)" }
    }
}

extension Dictionary where Key == String, Value == Any {
    /// ForEach identity for a forge PullRequest row.
    var prURL: String { s("url") }
}
