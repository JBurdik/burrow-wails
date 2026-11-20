import SwiftUI

// Pull requests for the Git surface — PullRequestsPanel.vue / usePullRequests.ts. Wire:
// forge_info, forge_pr_list(cwd, scope, state), forge_pr_view(cwd, number), forge_pr_create,
// forge_pr_merge(cwd, number, squash). Fields are Go's normalized forge.PullRequest.

struct PullRequest: Identifiable, Hashable {
    struct Check: Hashable { var name: String, status: String, conclusion: String }
    struct File: Hashable { var path: String, additions: Int, deletions: Int }
    struct Comment: Hashable { var author: String, body: String }
    var number: Int
    var title = "", body = "", url = "", state = "open"
    var isDraft = false
    var head = "", base = "", author = "", review = ""
    var additions = 0, deletions = 0
    var checks: [Check] = [], files: [File] = [], comments: [Comment] = []
    var id: Int { number }

    init(_ d: [String: Any]) {
        number = int(d["number"])
        title = d["title"] as? String ?? ""; body = d["body"] as? String ?? ""; url = d["url"] as? String ?? ""
        state = d["state"] as? String ?? "open"; isDraft = d["isDraft"] as? Bool ?? false
        head = d["headRefName"] as? String ?? ""; base = d["baseRefName"] as? String ?? ""
        author = d["author"] as? String ?? ""; review = d["reviewDecision"] as? String ?? ""
        additions = int(d["additions"]); deletions = int(d["deletions"])
        checks = (d["checks"] as? [[String: Any]] ?? []).map {
            Check(name: $0["name"] as? String ?? "Check", status: $0["status"] as? String ?? "", conclusion: $0["conclusion"] as? String ?? "")
        }
        files = (d["files"] as? [[String: Any]] ?? []).map {
            File(path: $0["path"] as? String ?? "", additions: int($0["additions"]), deletions: int($0["deletions"]))
        }
        comments = (d["comments"] as? [[String: Any]] ?? []).map {
            Comment(author: $0["author"] as? String ?? "", body: $0["body"] as? String ?? "")
        }
    }

    /// The badge PullRequestsPanel prints: Draft, Merged, else the review decision, else Open.
    var stateLabel: String {
        if isDraft { return "Draft" }
        if state == "merged" { return "Merged" }
        if state == "closed" { return "Closed" }
        return review.isEmpty ? "Open" : review.replacingOccurrences(of: "_", with: " ").capitalized
    }
    var stateColor: Color {
        if isDraft { return .orange }
        switch state { case "merged": return .purple; case "closed": return .red; default: return .green }
    }
    var canMerge: Bool { state == "open" && !isDraft }
}

extension PullRequest.Check {
    /// ok / bad / pending from Go's raw status + conclusion (case differs per forge).
    enum Outcome { case ok, bad, pending }
    var outcome: Outcome {
        switch conclusion.uppercased() {
        case "SUCCESS", "NEUTRAL", "SKIPPED": return .ok
        case "": return .pending
        default: return .bad
        }
    }
}

struct PullRequestsView: View {
    let cwd: String
    @Environment(AppModel.self) private var model
    @State private var items: [PullRequest] = []
    @State private var scope = "all"
    @State private var state = "open"
    @State private var loaded = false
    @State private var error: String?
    @State private var forge: [String: Any] = [:]

    var body: some View {
        List {
            Section {
                Picker("Scope", selection: $scope) {
                    Text("All").tag("all"); Text("Assigned").tag("assigned"); Text("Created").tag("created")
                }.pickerStyle(.segmented)
                Picker("State", selection: $state) {
                    Text("Open").tag("open"); Text("Merged").tag("merged"); Text("Closed").tag("closed")
                }.pickerStyle(.segmented)
            }
            if let error { Text(error).font(.footnote.monospaced()).foregroundStyle(.red).textSelection(.enabled) }
            ForEach(items) { pr in
                NavigationLink { PullRequestDetail(cwd: cwd, pr: pr) { Task { await load() } } } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack {
                            Text("#\(pr.number)").font(.caption.monospaced()).foregroundStyle(.tint)
                            Spacer()
                            Text(pr.stateLabel).font(.caption).foregroundStyle(pr.stateColor)
                        }
                        Text(pr.title).fontWeight(.medium).lineLimit(2)
                        if !pr.head.isEmpty { Text(pr.head).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                    }
                }
            }
            if loaded && items.isEmpty && error == nil { Section { Text(emptyText).foregroundStyle(.secondary) } }
        }
        .overlay { if !loaded { ProgressView() } }
        .navigationTitle("Pull requests")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        .task(id: "\(scope)/\(state)") { await load() }
    }

    private var emptyText: String {
        if forge["provider"] == nil || (forge["provider"] as? String ?? "").isEmpty { return "No forge detected for this repo." }
        if forge["installed"] as? Bool == false { return "Install \(forge["bin"] as? String ?? "the forge CLI") on the Mac." }
        if forge["authed"] as? Bool == false { return "Log in on the Mac: \(forge["authCmd"] as? String ?? "")" }
        return "No pull requests."
    }

    private func load() async {
        defer { loaded = true }
        guard let client = model.client else { return }
        forge = (try? await client.call("forge_info", ["cwd": cwd]) as? [String: Any]) ?? [:]
        do {
            let list = try await client.call("forge_pr_list", ["cwd": cwd, "scope": scope == "all" ? "" : scope, "state": state]) as? [[String: Any]] ?? []
            items = list.map(PullRequest.init)
            error = nil
        } catch { self.error = error.localizedDescription; items = [] }
    }
}

struct PullRequestDetail: View {
    let cwd: String
    @State var pr: PullRequest
    var changed: () -> Void = {}
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL
    @State private var tab = "summary"
    @State private var confirm = false
    @State private var squash = true
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        List {
            Section {
                HStack {
                    Text("#\(pr.number)").font(.callout.monospaced()).foregroundStyle(.tint)
                    Spacer()
                    Text(pr.stateLabel).font(.callout.weight(.medium)).foregroundStyle(pr.stateColor)
                }
                Text(pr.title).font(.headline)
                if !pr.head.isEmpty {
                    Text("\(pr.author.isEmpty ? "Unknown" : pr.author) · \(pr.head) → \(pr.base)").font(.caption).foregroundStyle(.secondary)
                }
                if pr.additions + pr.deletions > 0 {
                    HStack { Text("+\(pr.additions)").foregroundStyle(.green); Text("−\(pr.deletions)").foregroundStyle(.red) }
                        .font(.caption.monospacedDigit())
                }
            }
            if let error { Text(error).font(.footnote.monospaced()).foregroundStyle(.red).textSelection(.enabled) }
            Section {
                Picker("", selection: $tab) {
                    Text("Summary").tag("summary"); Text("Timeline (\(pr.comments.count))").tag("timeline"); Text("Files (\(pr.files.count))").tag("files")
                }.pickerStyle(.segmented).labelsHidden()
                switch tab {
                case "timeline":
                    if pr.comments.isEmpty { Text("No comments yet.").foregroundStyle(.secondary) }
                    ForEach(Array(pr.comments.enumerated()), id: \.offset) { _, c in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(c.author.isEmpty ? "Unknown" : c.author).font(.caption.weight(.semibold))
                            Text(c.body).font(.callout).textSelection(.enabled)
                        }
                    }
                case "files":
                    if pr.files.isEmpty { Text("No file list.").foregroundStyle(.secondary) }
                    ForEach(pr.files, id: \.path) { f in
                        HStack {
                            Text(f.path).font(.caption.monospaced()).lineLimit(1).truncationMode(.head)
                            Spacer()
                            Text("+\(f.additions)").foregroundStyle(.green); Text("−\(f.deletions)").foregroundStyle(.red)
                        }.font(.caption.monospacedDigit())
                    }
                default:
                    LabeledContent("Reviewers", value: pr.review.isEmpty ? "None" : pr.review.replacingOccurrences(of: "_", with: " ").capitalized)
                    Text(pr.body.isEmpty ? "No description provided." : pr.body).font(.callout).textSelection(.enabled)
                        .foregroundStyle(pr.body.isEmpty ? .secondary : .primary)
                }
            }
            Section("Checks (\(pr.checks.count))") {
                if pr.checks.isEmpty { Text("No checks reported.").foregroundStyle(.secondary) }
                ForEach(pr.checks, id: \.name) { c in
                    HStack {
                        switch c.outcome {
                        case .ok: Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                        case .bad: Image(systemName: "xmark.circle.fill").foregroundStyle(.red)
                        case .pending: Image(systemName: "clock").foregroundStyle(.orange)
                        }
                        Text(c.name)
                        Spacer()
                        Text(c.conclusion.isEmpty ? c.status : c.conclusion).font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            if pr.canMerge {
                Section {
                    Toggle("Squash", isOn: $squash)
                    Button { confirm = true } label: { Label("Merge…", systemImage: "arrow.triangle.merge").frame(maxWidth: .infinity) }
                        .buttonStyle(.borderedProminent).tint(.green).disabled(busy)
                }
            }
        }
        .navigationTitle("#\(pr.number)")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { if let u = URL(string: pr.url), ["http", "https"].contains(u.scheme) { openURL(u) } } label: {
                    Image(systemName: "safari")
                }.accessibilityLabel("Open in browser").disabled(pr.url.isEmpty)
            }
        }
        .confirmationDialog("Merge #\(pr.number)?", isPresented: $confirm, titleVisibility: .visible) {
            Button(squash ? "Squash and merge" : "Merge", role: .destructive) { Task { await merge() } }
        } message: { Text("\(pr.title)\n\(pr.head) → \(pr.base). This can't be undone from here.") }
        .refreshable { await reload() }
        .task { await reload() }
    }

    private func reload() async {
        // The list rows are stubs without checks/comments/files: view fills them in.
        if let d = try? await model.client?.call("forge_pr_view", ["cwd": cwd, "number": pr.number]) as? [String: Any] {
            pr = PullRequest(d)
        }
    }

    private func merge() async {
        busy = true; defer { busy = false }
        do {
            try await model.client?.call("forge_pr_merge", ["cwd": cwd, "number": pr.number, "squash": squash])
            error = nil
            await reload(); changed()
        } catch { self.error = error.localizedDescription }
    }
}
