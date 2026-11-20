import SwiftUI

// A chat's blocking requests, answered from the phone. Mirrors chatSession.ts's
// PendingRequest union + respond(), but holds no reducer of its own: Go's
// load_pending_chat_requests (gated on the chat's waiting_* phase, skipping
// cancelled / answered ids) is re-read whenever a line or phase says something
// changed. That covers reconnect, "answered on the Mac" (the synthetic answered
// line + the phase leaving waiting_*) and a request that arrived while no view
// was open, with one code path.

private let diffTools: Set<String> = ["Edit", "Write", "MultiEdit", "NotebookEdit"]

/// One Claude `control_request` / `can_use_tool`.
struct ClaudeRequest: Identifiable {
    var id: String
    var toolName: String
    var input: [String: Any]
    var description: String

    var detail: String {
        ["command", "file_path", "path", "url", "pattern"].lazy.compactMap { self.input[$0] as? String }.first ?? description
    }
}

struct AcpOption: Identifiable { var id: String; var name: String; var kind: String }

/// session/request_permission (ACP) or a Codex approval, in acpParser.ts's shape.
struct AcpPermission {
    var rpcId: Int
    var title: String
    var options: [AcpOption]
    var rawInput: [String: Any]
    var plan: String? { rawInput["plan"] as? String }
}

struct CodexQuestion: Identifiable {
    var id: String
    var header: String
    var question: String
    var isOther: Bool
    var isSecret: Bool
    var options: [(label: String, description: String)]
}

struct CodexInput { var rpcId: Int; var questions: [CodexQuestion] }

struct SecretRequest: Identifiable {
    var id: String
    var owner: String
    var name: String
    var reason: String
    var repoId: Int
    var hasSaved: Bool
}

@Observable
final class ChatRequests {
    var claude: [ClaudeRequest] = []
    var acp: AcpPermission?
    var codex: CodexInput?
    var secrets: [SecretRequest] = []
    var error: String?
    var busy = false
    /// config.json `chatPermissionRules` — the desktop's "Allow always" list.
    var rules: [String] = []
    @ObservationIgnored var autoAllowed: Set<String> = []

    var isEmpty: Bool { claude.isEmpty && acp == nil && codex == nil && secrets.isEmpty }

    func parse(_ lines: [[String: Any]]) {
        var cl: [ClaudeRequest] = []
        var acp: AcpPermission?
        var codex: CodexInput?
        for l in lines {
            guard let raw = l["line"] as? String,
                  let m = try? JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [String: Any] else { continue }
            switch l["kind"] as? String {
            case "claude-data":
                let req = m["request"] as? [String: Any] ?? [:]
                guard let rid = m["request_id"] as? String, req["subtype"] as? String == "can_use_tool" else { continue }
                cl.append(ClaudeRequest(id: rid, toolName: req["tool_name"] as? String ?? "",
                                        input: req["input"] as? [String: Any] ?? [:], description: req["description"] as? String ?? ""))
            case "acp-req":
                if let input = Self.codexInput(m) { codex = input } else if let p = Self.acpPermission(m) { acp = p }
            default: break
            }
        }
        claude = cl; self.acp = acp; self.codex = codex
    }

    static func codexInput(_ m: [String: Any]) -> CodexInput? {
        guard m["method"] as? String == "item/tool/requestUserInput", let rpc = m["id"] as? NSNumber else { return nil }
        let qs = ((m["params"] as? [String: Any])?["questions"] as? [[String: Any]] ?? []).compactMap { q -> CodexQuestion? in
            guard let id = q["id"] as? String, let text = q["question"] as? String else { return nil }
            return CodexQuestion(id: id, header: q["header"] as? String ?? "Question", question: text,
                                 isOther: q["isOther"] as? Bool ?? false, isSecret: q["isSecret"] as? Bool ?? false,
                                 options: (q["options"] as? [[String: Any]] ?? []).compactMap { o in
                                     (o["label"] as? String).map { ($0, o["description"] as? String ?? "") }
                                 })
        }
        return qs.isEmpty ? nil : CodexInput(rpcId: rpc.intValue, questions: qs)
    }

    /// parseAcpPermRequest in src/lib/acpParser.ts.
    static func acpPermission(_ m: [String: Any]) -> AcpPermission? {
        let method = m["method"] as? String ?? ""
        guard let rpc = m["id"] as? NSNumber else { return nil }
        let p = m["params"] as? [String: Any] ?? [:]
        if ["item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval"].contains(method) {
            let command = p["command"] as? String
            let title = method.contains("commandExecution") ? (command.map { "Run: \($0)" } ?? "Run command")
                : method.contains("fileChange") ? "Apply file changes" : "Grant additional permission"
            return AcpPermission(rpcId: rpc.intValue, title: title, options: [
                AcpOption(id: "codex:accept", name: "Allow once", kind: "allow_once"),
                AcpOption(id: "codex:acceptForSession", name: "Always allow", kind: "allow_always"),
                AcpOption(id: "codex:decline", name: "Deny", kind: "reject_once"),
            ], rawInput: p)
        }
        guard method == "session/request_permission" else { return nil }
        let tc = p["toolCall"] as? [String: Any] ?? [:]
        return AcpPermission(rpcId: rpc.intValue, title: tc["title"] as? String ?? "Tool",
                             options: (p["options"] as? [[String: Any]] ?? []).map {
                                 AcpOption(id: $0["optionId"] as? String ?? "", name: $0["name"] as? String ?? "", kind: $0["kind"] as? String ?? "")
                             },
                             rawInput: tc["rawInput"] as? [String: Any] ?? [:])
    }

    static func secrets(_ v: Any?) -> [SecretRequest] {
        (v as? [[String: Any]] ?? []).map {
            SecretRequest(id: $0["id"] as? String ?? "", owner: $0["owner"] as? String ?? "", name: $0["name"] as? String ?? "",
                          reason: $0["reason"] as? String ?? "", repoId: int($0["repo_id"]), hasSaved: $0["hasSaved"] as? Bool ?? false)
        }
    }
}

/// ruleKeys in chatSession.ts: Bash gets a command-prefix key so allowing `git`
/// once doesn't blanket-allow every Bash call.
func permissionRuleKeys(_ tool: String, _ input: [String: Any]) -> [String] {
    var keys = [tool]
    if tool == "Bash", let first = (input["command"] as? String)?.split(whereSeparator: \.isWhitespace).first {
        keys.append("Bash:\(first)")
    }
    return keys
}

extension AppModel {
    @MainActor
    func loadPendingRequests(_ chatId: Int, into r: ChatRequests) async {
        if let lines = try? await client?.call("load_pending_chat_requests", ["chatId": chatId]) as? [[String: Any]] {
            r.parse(lines)
        }
        let all = ChatRequests.secrets(try? await client?.call("list_secret_requests"))
        r.secrets = all.filter { $0.owner == "chat:\(chatId)" }
        // Same auto-allow as the desktop session: a request an "Always" rule
        // covers is answered without a card (the desktop may not have this chat live).
        for req in r.claude where !r.autoAllowed.contains(req.id)
            && !["AskUserQuestion", "ExitPlanMode"].contains(req.toolName)
            && permissionRuleKeys(req.toolName, req.input).contains(where: r.rules.contains) {
            r.autoAllowed.insert(req.id)
            r.claude.removeAll { $0.id == req.id }
            try? await respondControl(chatId, req.id, ["behavior": "allow", "updatedInput": req.input])
        }
    }

    @MainActor
    func loadPermissionRules(into r: ChatRequests) async {
        guard let raw = try? await client?.call("read_config") as? String,
              let cfg = try? JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [String: Any] else { return }
        r.rules = cfg["chatPermissionRules"] as? [String] ?? []
    }

    func respondControl(_ chatId: Int, _ requestId: String, _ response: [String: Any]) async throws {
        try await client?.call("claude_respond_control", ["id": chatId, "requestId": requestId, "response": response])
    }

    /// addPermissionRule in claudeChats.ts: read-modify-write of one config key.
    @MainActor
    func addPermissionRule(_ key: String, into r: ChatRequests) async {
        await loadPermissionRules(into: r)
        guard !r.rules.contains(key) else { return }
        r.rules.append(key)
        _ = try? await client?.call("config_set", ["key": "chatPermissionRules", "value": r.rules])
    }
}

// MARK: - Views

struct PendingRequestsView: View {
    let chatId: Int
    @Environment(AppModel.self) private var model
    @State private var r = ChatRequests()
    @State private var token: UUID?
    @State private var reload: Task<Void, Never>?

    var body: some View {
        Group {
            if !r.isEmpty {
                ScrollView {
                    VStack(spacing: 10) {
                        if let e = r.error { Text(e).font(.caption).foregroundStyle(.red).frame(maxWidth: .infinity, alignment: .leading) }
                        if let s = r.secrets.first { SecretCard(req: s, more: r.secrets.count - 1, r: r).id(s.id) }
                        if let c = r.claude.first {
                            Group {
                                switch c.toolName {
                                case "AskUserQuestion": QuestionCard(chatId: chatId, req: c, r: r)
                                case "ExitPlanMode": PlanCard(chatId: chatId, req: c, r: r)
                                default: PermissionCard(chatId: chatId, req: c, more: r.claude.count - 1, r: r)
                                }
                            }
                            .id(c.id)
                        }
                        if let a = r.acp { AcpCard(chatId: chatId, req: a, r: r).id(a.rpcId) }
                        if let c = r.codex { CodexInputCard(chatId: chatId, req: c, r: r).id(c.rpcId) }
                    }
                    .padding(.horizontal).padding(.vertical, 8)
                }
                .scrollBounceBehavior(.basedOnSize)
                .frame(maxHeight: 420)
                .fixedSize(horizontal: false, vertical: true)
                .glassSurface(RoundedRectangle(cornerRadius: 18, style: .continuous))
                .padding(.horizontal, 8).padding(.top, 4)
            }
        }
        .disabled(r.busy)
        .task {
            await model.loadPermissionRules(into: r)
            await model.loadPendingRequests(chatId, into: r)
        }
        .onAppear {
            token = model.subscribe { name, payload in
                let id = String(chatId)
                switch name {
                case "open", "resync", "phase-chat:\(id)", "claude-data-\(id)", "acp-req-\(id)", "acp-data-\(id)":
                    if name == "claude-data-\(id)" || name == "acp-data-\(id)" {
                        // Only control / resolve lines matter; skip the stream's text deltas.
                        let line = (payload as? [String: Any])?["line"] as? String ?? ""
                        guard line.contains("control_") || line.contains("serverRequest/resolved") else { return }
                    }
                    scheduleReload()
                case "secret-requests":
                    r.secrets = ChatRequests.secrets((payload as? [String: Any])?["requests"]).filter { $0.owner == "chat:\(chatId)" }
                case "config-changed":
                    if let d = payload as? [String: Any], d["key"] as? String == "chatPermissionRules" {
                        r.rules = d["value"] as? [String] ?? []
                    }
                default: break
                }
            }
        }
        .onDisappear { model.unsubscribe(token); token = nil; reload?.cancel() }
    }

    private func scheduleReload() {
        reload?.cancel()
        reload = Task { @MainActor in
            try? await Task.sleep(nanoseconds: 150_000_000)
            if !Task.isCancelled { await model.loadPendingRequests(chatId, into: r) }
        }
    }
}

/// Shared card chrome: icon + title, tinted like the desktop's status banners.
private struct Card<Content: View>: View {
    let icon: String
    let title: String
    var tint: Color = .orange
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label(title, systemImage: icon).font(.subheadline.weight(.semibold)).foregroundStyle(tint)
            content
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .glassSurface(RoundedRectangle(cornerRadius: 14, style: .continuous), tint: tint.opacity(0.3))
    }
}

/// Runs one answer with the busy flag and error line; the reload that follows
/// (answered line / phase event) removes the card.
@MainActor
private func answer(_ r: ChatRequests, _ work: @escaping () async throws -> Void) {
    guard !r.busy else { return }
    r.busy = true; r.error = nil
    Task { @MainActor in
        defer { r.busy = false }
        do { try await work() } catch { r.error = error.localizedDescription }
    }
}

private struct PermissionCard: View {
    let chatId: Int
    let req: ClaudeRequest
    let more: Int
    let r: ChatRequests
    @Environment(AppModel.self) private var model
    @State private var denying = false
    @State private var message = ""

    var body: some View {
        Card(icon: diffTools.contains(req.toolName) ? "pencil.and.list.clipboard" : "exclamationmark.shield.fill",
             title: "\(req.toolName) wants permission" + (more > 0 ? "  +\(more)" : "")) {
            if !req.detail.isEmpty {
                Text(req.detail).font(.caption.monospaced()).lineLimit(6).textSelection(.enabled)
            }
            if let old = req.input["old_string"] as? String, let new = req.input["new_string"] as? String {
                diff(old, new)
            } else if let content = req.input["content"] as? String {
                diff("", content)
            }
            if denying {
                TextField("Reason for the agent (optional)", text: $message, axis: .vertical)
                    .textFieldStyle(.roundedBorder).lineLimit(1...4)
                HStack {
                    Button("Cancel") { denying = false }.buttonStyle(.bordered)
                    Spacer()
                    Button("Deny", role: .destructive) { respond(allow: false) }.buttonStyle(.borderedProminent)
                }
            } else {
                HStack {
                    Button("Deny", role: .destructive) { denying = true }.buttonStyle(.bordered)
                    Spacer()
                    Button("Always") { respond(allow: true, always: true) }.buttonStyle(.bordered)
                    Button("Allow") { respond(allow: true) }.buttonStyle(.borderedProminent)
                }
            }
        }
    }

    private func diff(_ old: String, _ new: String) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                if !old.isEmpty { Text(old).foregroundStyle(.red) }
                Text(new).foregroundStyle(.green)
            }
            .font(.caption2.monospaced()).frame(maxWidth: .infinity, alignment: .leading)
        }
        .frame(maxHeight: 140)
        .padding(6).background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 8))
    }

    private func respond(allow: Bool, always: Bool = false) {
        let msg = message.trimmingCharacters(in: .whitespacesAndNewlines)
        answer(r) {
            try await model.respondControl(chatId, req.id, allow
                ? ["behavior": "allow", "updatedInput": req.input]
                : ["behavior": "deny", "message": msg.isEmpty ? "User denied this action." : msg])
            if always, let key = permissionRuleKeys(req.toolName, req.input).last {
                await model.addPermissionRule(key, into: r)
            }
        }
    }
}

private struct QuestionCard: View {
    let chatId: Int
    let req: ClaudeRequest
    let r: ChatRequests
    @Environment(AppModel.self) private var model
    @State private var picked: [String: [String]] = [:]
    @State private var custom: [String: String] = [:]

    private var questions: [[String: Any]] { req.input["questions"] as? [[String: Any]] ?? [] }

    var body: some View {
        Card(icon: "questionmark.bubble.fill", title: "Question", tint: .blue) {
            ForEach(Array(questions.enumerated()), id: \.offset) { _, q in
                let text = q["question"] as? String ?? ""
                let multi = q["multiSelect"] as? Bool ?? false
                VStack(alignment: .leading, spacing: 6) {
                    if let h = q["header"] as? String, !h.isEmpty { Text(h).font(.caption).foregroundStyle(.secondary) }
                    Text(text).font(.subheadline.weight(.medium))
                    ForEach(Array((q["options"] as? [[String: Any]] ?? []).enumerated()), id: \.offset) { _, o in
                        let label = o["label"] as? String ?? ""
                        OptionRow(label: label, detail: o["description"] as? String ?? "", multi: multi,
                                  on: picked[text, default: []].contains(label)) {
                            var s = picked[text, default: []]
                            if s.contains(label) { s.removeAll { $0 == label } } else { s = multi ? s + [label] : [label] }
                            picked[text] = s
                            custom[text] = ""
                        }
                    }
                    TextField("Other…", text: Binding(get: { custom[text] ?? "" }, set: {
                        custom[text] = $0
                        if !$0.trimmingCharacters(in: .whitespaces).isEmpty { picked[text] = [] }
                    }))
                    .textFieldStyle(.roundedBorder)
                }
            }
            HStack {
                Button("Dismiss") { submit([:]) }.buttonStyle(.bordered)
                Spacer()
                Button("Submit") { submit(answers) }.buttonStyle(.borderedProminent).disabled(answers.isEmpty)
            }
        }
    }

    /// submitQuestion in AgentChat.vue: keyed by question text; a multi-select
    /// answer stays an array.
    private var answers: [String: Any] {
        var out: [String: Any] = [:]
        for q in questions {
            let text = q["question"] as? String ?? ""
            let c = (custom[text] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            if !c.isEmpty { out[text] = c; continue }
            let labels = picked[text] ?? []
            if labels.isEmpty { continue }
            out[text] = (q["multiSelect"] as? Bool ?? false) ? labels : labels[0]
        }
        return out
    }

    private func submit(_ a: [String: Any]) {
        var input = req.input
        input["answers"] = a
        answer(r) { try await model.respondControl(chatId, req.id, ["behavior": "allow", "updatedInput": input]) }
    }
}

private struct OptionRow: View {
    let label: String
    let detail: String
    let multi: Bool
    let on: Bool
    let toggle: () -> Void

    var body: some View {
        Button(action: toggle) {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: on ? (multi ? "checkmark.square.fill" : "largecircle.fill.circle") : (multi ? "square" : "circle"))
                    .foregroundStyle(on ? Color.accentColor : .secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text(label).font(.subheadline)
                    if !detail.isEmpty { Text(detail).font(.caption).foregroundStyle(.secondary) }
                }
                Spacer(minLength: 0)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

private struct PlanCard: View {
    let chatId: Int
    let req: ClaudeRequest
    let r: ChatRequests
    @Environment(AppModel.self) private var model
    @State private var feedback = ""

    var body: some View {
        Card(icon: "list.bullet.clipboard.fill", title: "Claude proposed a plan", tint: .green) {
            ScrollView { MarkdownText(text: req.input["plan"] as? String ?? "").font(.callout) }
                .frame(maxHeight: 220)
            TextField("Feedback to keep planning (optional)", text: $feedback, axis: .vertical)
                .textFieldStyle(.roundedBorder).lineLimit(1...4)
            HStack {
                Button("Keep planning") { respond(false) }.buttonStyle(.bordered)
                Spacer()
                Button("Approve plan") { respond(true) }.buttonStyle(.borderedProminent)
            }
        }
    }

    private func respond(_ approve: Bool) {
        let fb = feedback.trimmingCharacters(in: .whitespacesAndNewlines)
        answer(r) {
            try await model.respondControl(chatId, req.id, approve
                ? ["behavior": "allow", "updatedInput": req.input]
                : ["behavior": "deny", "message": fb.isEmpty ? "Keep planning — do not exit plan mode yet." : fb])
            // respondPlan: an approved plan leaves plan mode in the live session; record it.
            if approve, model.chat(chatId)?.permissionMode == "plan" {
                _ = try? await model.client?.call("chat_set_launch_settings",
                                                  ["chatId": chatId, "patch": ["permission_mode": "auto", "row_only": true]])
            }
        }
    }
}

private struct AcpCard: View {
    let chatId: Int
    let req: AcpPermission
    let r: ChatRequests
    @Environment(AppModel.self) private var model

    var body: some View {
        Card(icon: req.plan != nil ? "list.bullet.clipboard.fill" : "exclamationmark.shield.fill",
             title: req.plan != nil ? "Review plan" : req.title, tint: req.plan != nil ? .green : .orange) {
            if let plan = req.plan {
                ScrollView { MarkdownText(text: plan).font(.callout) }.frame(maxHeight: 220)
            } else if let cmd = req.rawInput["command"] as? String, !req.title.contains(cmd) {
                Text(cmd).font(.caption.monospaced()).lineLimit(6)
            }
            ViewThatFits {
                HStack { buttons }
                VStack(alignment: .leading) { buttons }
            }
        }
    }

    @ViewBuilder private var buttons: some View {
        ForEach(req.options) { o in
            let reject = o.kind.hasPrefix("reject")
            Button(o.name, role: reject ? .destructive : nil) {
                answer(r) { try await model.client?.call("acp_respond_permission", ["id": chatId, "rpcId": req.rpcId, "optionId": o.id]) }
            }
            .buttonStyle(.bordered)
            .tint(o.kind == "allow_once" ? .accentColor : nil)
        }
    }
}

private struct CodexInputCard: View {
    let chatId: Int
    let req: CodexInput
    let r: ChatRequests
    @Environment(AppModel.self) private var model
    @State private var picked: [String: String] = [:]
    @State private var typed: [String: String] = [:]

    var body: some View {
        Card(icon: "questionmark.bubble.fill", title: "Codex needs input", tint: .blue) {
            ForEach(req.questions) { q in
                VStack(alignment: .leading, spacing: 6) {
                    Text(q.header).font(.caption).foregroundStyle(.secondary)
                    Text(q.question).font(.subheadline.weight(.medium))
                    ForEach(q.options, id: \.label) { o in
                        OptionRow(label: o.label, detail: o.description, multi: false, on: picked[q.id] == o.label) {
                            picked[q.id] = picked[q.id] == o.label ? nil : o.label
                            typed[q.id] = ""
                        }
                    }
                    if q.isOther || q.options.isEmpty {
                        let b = Binding(get: { typed[q.id] ?? "" }, set: { typed[q.id] = $0; if !$0.isEmpty { picked[q.id] = nil } })
                        Group {
                            if q.isSecret { SecureField("Answer", text: b) } else { TextField("Answer", text: b) }
                        }
                        .textFieldStyle(.roundedBorder)
                    }
                }
            }
            HStack {
                Button("Dismiss") { send(Dictionary(uniqueKeysWithValues: req.questions.map { ($0.id, [String]()) })) }
                    .buttonStyle(.bordered)
                Spacer()
                Button("Submit") { send(answers) }.buttonStyle(.borderedProminent)
                    .disabled(answers.values.allSatisfy(\.isEmpty))
            }
        }
    }

    private var answers: [String: [String]] {
        Dictionary(uniqueKeysWithValues: req.questions.map { q in
            let t = (typed[q.id] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return (q.id, !t.isEmpty ? [t] : picked[q.id].map { [$0] } ?? [])
        })
    }

    private func send(_ a: [String: [String]]) {
        answer(r) { try await model.client?.call("acp_respond_user_input", ["id": chatId, "rpcId": req.rpcId, "answers": a]) }
    }
}

/// SecretRequestCard.vue: the value lives only in this view's state until sent.
private struct SecretCard: View {
    let req: SecretRequest
    let more: Int
    let r: ChatRequests
    @Environment(AppModel.self) private var model
    @State private var value = ""
    @State private var remember = false

    var body: some View {
        Card(icon: "key.fill", title: "Agent requests a secret" + (more > 0 ? "  +\(more)" : "")) {
            Text(req.name).font(.callout.monospaced().weight(.semibold))
            if !req.reason.isEmpty { Text(req.reason).font(.callout) }
            Text("The agent only gets a file path; the file is deleted when its turn ends.")
                .font(.caption).foregroundStyle(.secondary)
            if req.hasSaved {
                Button("Use saved value") { send(["id": req.id, "value": "", "useSaved": true, "remember": false]) }
                    .buttonStyle(.borderedProminent)
            }
            SecureField(req.hasSaved ? "Or enter a new value" : "Secret value", text: $value)
                .textContentType(.oneTimeCode) // keeps Passwords' AutoFill from offering to save it
                .textInputAutocapitalization(.never).autocorrectionDisabled()
                .textFieldStyle(.roundedBorder)
                .onSubmit(submit)
            if req.repoId > 0 { Toggle("Remember for this project", isOn: $remember).font(.callout) }
            HStack {
                Button("Decline", role: .destructive) {
                    answer(r) { try await model.client?.call("decline_secret_request", ["id": req.id]) }
                }
                .buttonStyle(.bordered)
                Spacer()
                Button("Submit", action: submit).buttonStyle(.borderedProminent).disabled(value.isEmpty)
            }
        }
    }

    private func submit() {
        guard !value.isEmpty else { return }
        send(["id": req.id, "value": value, "useSaved": false, "remember": remember && req.repoId > 0])
    }

    private func send(_ args: [String: Any]) {
        answer(r) {
            try await model.client?.call("answer_secret_request", args)
            value = ""
        }
    }
}
