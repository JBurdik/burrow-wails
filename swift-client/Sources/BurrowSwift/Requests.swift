import AppKit
import SwiftUI

// MARK: model (chatSession.ts PendingRequest / PendingAnswer)

struct PendingRequest: Identifiable {
    enum Kind {
        /// Claude can_use_tool (incl. Edit/Write diffs).
        case permission(requestID: String, tool: String, input: JSON)
        /// Claude AskUserQuestion.
        case question(requestID: String, input: JSON)
        /// Claude ExitPlanMode.
        case plan(requestID: String, input: JSON)
        /// Generic ACP session/request_permission or a Codex approval — the adapter's own options.
        case acp(rpcID: Int64, options: [JSON], plan: String)
        /// Codex item/tool/requestUserInput.
        case codexInput(rpcID: Int64, questions: [JSON])
        /// secret_request owned by this chat / tab (secretrequests.go).
        case secret(JSON)
        case other
    }
    let id: String
    let kind: Kind
    let title: String
    let detail: String

    init(id: String, kind: Kind, title: String, detail: String) {
        self.id = id; self.kind = kind; self.title = title; self.detail = detail
    }

    init(secret s: JSON) {
        self.init(id: "secret-\(s.s("id"))", kind: .secret(s), title: s.s("name"), detail: s.s("reason"))
    }

    /// One raw line from load_pending_chat_requests (chatstream.go): a claude-data control_request or an acp-req line.
    init?(line: String) {
        guard let o = (try? JSONSerialization.jsonObject(with: Data(line.utf8))) as? JSON else { return nil }
        let method = o.s("method")
        let params = o["params"] as? JSON ?? [:]
        if o.s("type") == "control_request", let req = o["request"] as? JSON {
            guard req.s("subtype") == "can_use_tool" else { return nil }
            let input = req["input"] as? JSON ?? [:]
            let tool = req.s("tool_name")
            let rid = o.s("request_id")
            id = rid
            title = tool
            detail = (input["command"] as? String) ?? (input["file_path"] as? String) ?? (input["path"] as? String)
                ?? req.s("description")
            switch tool {
            case "AskUserQuestion": kind = .question(requestID: rid, input: input)
            case "ExitPlanMode": kind = .plan(requestID: rid, input: input)
            default: kind = .permission(requestID: rid, tool: tool, input: input)
            }
        } else if method == "item/tool/requestUserInput" {
            let qs = (params["questions"] as? [JSON] ?? []).filter { $0["id"] is String && $0["question"] is String }
            guard !qs.isEmpty else { return nil }
            id = "rpc-\(o.i("id"))"; title = "Codex needs input"; detail = ""
            kind = .codexInput(rpcID: o.i("id"), questions: qs)
        } else if method.hasPrefix("item/"), method.hasSuffix("/requestApproval") {
            // acpParser.ts: Codex approvals mapped to protocol-specific option ids Go translates back.
            let command = params["command"] as? String
            id = "rpc-\(o.i("id"))"
            title = method.contains("commandExecution") ? (command.map { "Run: \($0)" } ?? "Run command")
                : method.contains("fileChange") ? "Apply file changes" : "Grant additional permission"
            detail = params.s("reason")
            kind = .acp(rpcID: o.i("id"), options: [
                ["optionId": "codex:accept", "name": "Allow once", "kind": "allow_once"],
                ["optionId": "codex:acceptForSession", "name": "Always allow", "kind": "allow_always"],
                ["optionId": "codex:decline", "name": "Deny", "kind": "reject_once"],
            ], plan: "")
        } else if method == "session/request_permission" {
            let call = params["toolCall"] as? JSON ?? [:]
            let raw = call["rawInput"] as? JSON ?? [:]
            id = "rpc-\(o.i("id"))"
            title = call.s("title").isEmpty ? "Permission" : call.s("title")
            detail = (raw["command"] as? String) ?? (raw["file_path"] as? String) ?? ""
            kind = .acp(rpcID: o.i("id"), options: params["options"] as? [JSON] ?? [], plan: raw.s("plan"))
        } else if !method.isEmpty {
            id = "rpc-\(o.i("id"))"; title = method; detail = ""; kind = .other
        } else {
            return nil
        }
    }
}

enum RequestAnswer {
    case permission(allow: Bool, always: Bool)
    /// AskUserQuestion — answers keyed by question text (String or [String]); [:] dismisses.
    case question(JSON)
    case plan(approve: Bool, feedback: String)
    case acpOption(String)
    /// Codex input — answers keyed by question id; all-empty dismisses.
    case codexInput([String: [String]])
    case secret(value: String, useSaved: Bool, remember: Bool)
    case decline
}

/// "Always allow" match keys; Bash gets a command-prefix key (chatSession.ts ruleKeys).
func ruleKeys(_ tool: String, _ input: JSON) -> [String] {
    var keys = [tool]
    if tool == "Bash", let first = (input["command"] as? String)?.split(whereSeparator: \.isWhitespace).first {
        keys.append("Bash:\(first)")
    }
    return keys
}

extension Burrow {
    /// Throws on a failed write so the card stays on screen and can be retried.
    func respond(chatID id: Int64, _ r: PendingRequest, _ a: RequestAnswer) async throws {
        let cid = "\(id)"
        switch (r.kind, a) {
        case let (.permission(rid, tool, input), .permission(allow, always)):
            let response: JSON = allow ? ["behavior": "allow", "updatedInput": input]
                                       : ["behavior": "deny", "message": "User denied this action."]
            try await call("claude_respond_control", ["id": cid, "requestId": rid, "response": response])
            if allow, always, let key = ruleKeys(tool, input).last { await AttentionStore.shared.addRule(key, self) }
        case let (.question(rid, input), .question(answers)):
            var u = input; u["answers"] = answers
            try await call("claude_respond_control", ["id": cid, "requestId": rid,
                                                      "response": ["behavior": "allow", "updatedInput": u] as JSON])
        case let (.plan(rid, input), .plan(approve, feedback)):
            let f = feedback.trimmingCharacters(in: .whitespacesAndNewlines)
            let response: JSON = approve ? ["behavior": "allow", "updatedInput": input]
                : ["behavior": "deny", "message": f.isEmpty ? "Keep planning — do not exit plan mode yet." : f]
            try await call("claude_respond_control", ["id": cid, "requestId": rid, "response": response])
            // ExitPlanMode leaves plan mode in the live session; record it on the row only (AgentChat respondPlan).
            if approve, let c = chat(id), c.s("permission_mode") == "plan", c.s("transport").isEmpty || c.s("transport") == "claude-cli" {
                _ = try? await call("chat_set_launch_settings", ["chatId": id, "patch": ["permission_mode": "auto", "row_only": true] as JSON])
                try? await refresh()
            }
        case let (.acp(rpc, _, _), .acpOption(opt)):
            try await call("acp_respond_permission", ["id": cid, "rpcId": rpc, "optionId": opt])
        case let (.codexInput(rpc, _), .codexInput(answers)):
            try await call("acp_respond_user_input", ["id": cid, "rpcId": rpc, "answers": answers])
        case let (.secret(s), .secret(value, useSaved, remember)):
            try await call("answer_secret_request", ["id": s.s("id"), "value": value, "useSaved": useSaved, "remember": remember])
        case let (.secret(s), .decline):
            try await call("decline_secret_request", ["id": s.s("id")])
        default:
            return
        }
    }
}

// MARK: views

/// Card wrapper shared by every request kind.
private struct Card<Content: View>: View {
    var tint: Color = Theme.yellow
    var pet = "waiting_approval"
    @ViewBuilder var content: Content

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Pet(state: pet, size: 40)
            VStack(alignment: .leading, spacing: 8) { content }
                .controlSize(.large)
        }
        .padding(14)
        .frame(maxWidth: 820, alignment: .leading)
        .background(tint.opacity(0.08), in: RoundedRectangle(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(tint.opacity(0.35)))
        .padding(.horizontal, 20)
    }
}

private func heading(_ s: String, _ c: Color = Theme.yellow) -> some View {
    Text(s).font(.brand(-1, weight: .bold)).foregroundStyle(c)
}

struct RequestCard: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.requestChatID) private var reqChat
    @ObservedObject private var attention = AttentionStore.shared
    let r: PendingRequest

    var body: some View {
        if !attention.isAnswered(reqChat ?? b.selectedChat, r) { card }
    }

    @ViewBuilder private var card: some View {
        switch r.kind {
        case .permission: PermissionCard(r: r)
        case .question(_, let input): QuestionCard(r: r, questions: input["questions"] as? [JSON] ?? [])
        case .plan(_, let input): PlanCard(r: r, plan: input.s("plan"))
        case .acp(_, let options, let plan): AcpCard(r: r, options: options, plan: plan)
        case .codexInput(_, let qs): CodexInputCard(r: r, questions: qs)
        case .secret(let s): SecretCard(req: s) { a in await b.answerIn(reqChat, r, a) }
        case .other:
            Card { heading(r.title); Text("Answer this one in Burrow.").foregroundStyle(Theme.muted) }
        }
    }
}

private let diffTools: Set<String> = ["Edit", "Write", "MultiEdit", "NotebookEdit"]

/// Claude can_use_tool. Plain Y / N / A answer the focused card (AgentChat onKeydown); the main pane's card takes
/// focus when it appears, a sub-agent's card in the RP on click. Edit/Write show the diff they'd apply.
private struct PermissionCard: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.requestChatID) private var reqChat
    let r: PendingRequest
    @FocusState private var focused: Bool

    var body: some View {
        let (tool, input) = parts
        let diff = diffTools.contains(tool)
        Card(tint: diff ? Theme.accent : Theme.yellow) {
            heading(diff ? "\(tool) \(r.detail)" : "Wants to run \(r.title)", diff ? Theme.accent : Theme.yellow)
            if diff {
                EditDiffPreview(tool: tool, input: input)
            } else if !r.detail.isEmpty {
                Text(r.detail).font(.ui(-2, .monospaced)).lineLimit(6).textSelection(.enabled)
            }
            HStack {
                Button { answer(true, false) } label: { Text("\(diff ? "Accept" : "Allow")  Y").padding(.horizontal, 6) }
                    .keyboardShortcut(reqChat == nil ? KeyboardShortcut("y", modifiers: .command) : nil)
                    .tint(Theme.green).buttonStyle(.borderedProminent)
                Button { answer(true, true) } label: { Text("Always  A").padding(.horizontal, 6) }
                    .help("Allow and remember for every chat (chatPermissionRules)")
                    .buttonStyle(.bordered)
                Button { answer(false, false) } label: { Text("\(diff ? "Reject" : "Deny")  N").padding(.horizontal, 6) }
                    .tint(Theme.red).buttonStyle(.bordered)
                if focused { Text("Y / N / A").font(.brand(-5)).foregroundStyle(Theme.muted) }
            }
        }
        .focusable()
        .focused($focused)
        .focusEffectDisabled()
        .onTapGesture { focused = true }
        .onKeyPress(characters: CharacterSet(charactersIn: "ynaYNA")) { press in
            guard press.modifiers.isDisjoint(with: [.command, .control, .option]) else { return .ignored }
            switch press.characters.lowercased() {
            case "y": answer(true, false)
            case "n": answer(false, false)
            default: answer(true, true)
            }
            return .handled
        }
        .onAppear { if reqChat == nil { DispatchQueue.main.async { focused = true } } }
    }

    private var parts: (String, JSON) {
        if case let .permission(_, tool, input) = r.kind { return (tool, input) }
        return (r.title, [:])
    }

    private func answer(_ allow: Bool, _ always: Bool) {
        Task { await b.answerIn(reqChat, r, .permission(allow: allow, always: always)) }
    }
}

/// AgentChat diffPreview: Write = all added, Edit = old_string removed + new_string added, MultiEdit = each edit.
struct EditDiffPreview: View {
    let tool: String
    let input: JSON

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(lines.enumerated()), id: \.offset) { _, l in
                    Text(l.text.isEmpty ? " " : l.text)
                        .font(.ui(-3, .monospaced))
                        .foregroundStyle(l.add ? Theme.green : Theme.red)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 6)
                        .background((l.add ? Theme.green : Theme.red).opacity(0.08))
                }
            }
            .textSelection(.enabled)
        }
        .frame(maxHeight: 220)
        .background(Theme.terminal, in: RoundedRectangle(cornerRadius: 7))
    }

    var lines: [(add: Bool, text: String)] { editDiffLines(tool, input) }
}

func editDiffLines(_ tool: String, _ input: JSON) -> [(add: Bool, text: String)] {
    func split(_ s: String?) -> [String] { (s ?? "").components(separatedBy: "\n") }
    if tool == "Write" { return split(input["content"] as? String).map { (true, "+ " + $0) } }
    if tool == "NotebookEdit" { return split(input["new_source"] as? String).map { (true, "+ " + $0) } }
    let edits = tool == "MultiEdit" ? input["edits"] as? [JSON] ?? [] : [input]
    return edits.flatMap { e in
        split(e["old_string"] as? String).map { (false, "- " + $0) } + split(e["new_string"] as? String).map { (true, "+ " + $0) }
    }
}

private struct QuestionCard: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.requestChatID) private var reqChat
    let r: PendingRequest
    let questions: [JSON]
    @State private var picked: [String: [String]] = [:]
    @State private var custom: [String: String] = [:]

    var body: some View {
        Card(tint: Theme.accent, pet: "waiting_input") {
            ForEach(questions.indices, id: \.self) { i in
                let q = questions[i], text = q.s("question"), multi = q["multiSelect"] as? Bool ?? false
                VStack(alignment: .leading, spacing: 6) {
                    if !q.s("header").isEmpty { Text(q.s("header").uppercased()).font(.brand(-4, weight: .bold)).foregroundStyle(Theme.muted) }
                    Text(text).font(.ui(0)).foregroundStyle(Theme.text)
                    if multi { Text("Select one or more.").font(.ui(-3)).foregroundStyle(Theme.muted) }
                    ForEach((q["options"] as? [JSON] ?? []).indices, id: \.self) { j in
                        let o = (q["options"] as? [JSON] ?? [])[j], label = o.s("label")
                        let on = picked[text, default: []].contains(label)
                        Button { toggle(text, label, multi) } label: {
                            HStack(alignment: .top) {
                                Image(systemName: on ? (multi ? "checkmark.square.fill" : "largecircle.fill.circle") : (multi ? "square" : "circle"))
                                    .foregroundStyle(on ? Theme.accent : Theme.muted)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(label).font(.ui(-1)).foregroundStyle(Theme.text)
                                    if !o.s("description").isEmpty { Text(o.s("description")).font(.ui(-3)).foregroundStyle(Theme.text2) }
                                }
                                Spacer(minLength: 0)
                            }
                            .padding(8)
                            .background(on ? Theme.selected : Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                        }
                        .buttonStyle(.plain)
                    }
                    TextField("Other…", text: Binding(get: { custom[text] ?? "" }, set: { v in
                        custom[text] = v
                        if !v.trimmingCharacters(in: .whitespaces).isEmpty { picked[text] = [] } // mutually exclusive
                    }))
                    .textFieldStyle(.roundedBorder)
                }
            }
            HStack {
                Button("Submit") { Task { await b.answerIn(reqChat, r, .question(answers())) } }
                    .keyboardShortcut(reqChat == nil ? KeyboardShortcut(.return, modifiers: .command) : nil).buttonStyle(.borderedProminent).tint(Theme.accent)
                    .disabled(answers().isEmpty)
                Button("Dismiss") { Task { await b.answerIn(reqChat, r, .question([:])) } }.buttonStyle(.bordered)
            }
        }
    }

    func toggle(_ q: String, _ label: String, _ multi: Bool) {
        custom[q] = ""
        var cur = picked[q, default: []]
        if multi { if let i = cur.firstIndex(of: label) { cur.remove(at: i) } else { cur.append(label) } }
        else { cur = [label] }
        picked[q] = cur
    }

    /// Multi-select stays an array — a joined string left the CLI stuck (AgentChat submitQuestion).
    func answers() -> JSON {
        var out: JSON = [:]
        for q in questions {
            let text = q.s("question")
            let c = (custom[text] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            if !c.isEmpty { out[text] = c; continue }
            let labels = picked[text] ?? []
            if labels.isEmpty { continue }
            out[text] = (q["multiSelect"] as? Bool ?? false) ? labels : labels[0]
        }
        return out
    }
}

private struct PlanCard: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.requestChatID) private var reqChat
    let r: PendingRequest
    let plan: String
    @State private var feedback = ""

    var body: some View {
        Card(tint: Theme.green, pet: "waiting_input") {
            heading("Proposed a plan", Theme.green)
            ScrollView { Markdown(text: plan).frame(maxWidth: .infinity, alignment: .leading) }
                .frame(maxHeight: 320)
            TextField("Optional feedback if you keep planning…", text: $feedback, axis: .vertical)
                .lineLimit(1...4).textFieldStyle(.roundedBorder)
            HStack {
                Button("Approve plan") { Task { await b.answerIn(reqChat, r, .plan(approve: true, feedback: "")) } }
                    .keyboardShortcut(reqChat == nil ? KeyboardShortcut("y", modifiers: .command) : nil)
                    .buttonStyle(.borderedProminent).tint(Theme.green)
                Button("Keep planning") { Task { await b.answerIn(reqChat, r, .plan(approve: false, feedback: feedback)) } }
                    .keyboardShortcut(reqChat == nil ? KeyboardShortcut(.escape, modifiers: []) : nil).buttonStyle(.bordered)
            }
        }
    }
}

private struct AcpCard: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.requestChatID) private var reqChat
    let r: PendingRequest
    let options: [JSON]
    let plan: String

    var body: some View {
        Card(tint: plan.isEmpty ? Theme.yellow : Theme.green) {
            heading(plan.isEmpty ? r.title : "Review plan", plan.isEmpty ? Theme.yellow : Theme.green)
            if !plan.isEmpty {
                ScrollView { Markdown(text: plan).frame(maxWidth: .infinity, alignment: .leading) }.frame(maxHeight: 320)
            } else if !r.detail.isEmpty {
                Text(r.detail).font(.ui(-2, .monospaced)).lineLimit(6).textSelection(.enabled)
            }
            HStack {
                ForEach(options.indices, id: \.self) { i in
                    let o = options[i], reject = o.s("kind").hasPrefix("reject")
                    Button(o.s("name")) { Task { await b.answerIn(reqChat, r, .acpOption(o.s("optionId"))) } }
                        .buttonStyle(.bordered).tint(reject ? Theme.red : i == 0 ? Theme.green : Theme.text2)
                }
            }
        }
    }
}

private struct CodexInputCard: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.requestChatID) private var reqChat
    let r: PendingRequest
    let questions: [JSON]
    @State private var values: [String: String] = [:]

    var body: some View {
        Card(tint: Theme.accent, pet: "waiting_input") {
            ForEach(questions.indices, id: \.self) { i in
                let q = questions[i], qid = q.s("id")
                let v = Binding(get: { values[qid] ?? "" }, set: { values[qid] = $0 })
                VStack(alignment: .leading, spacing: 6) {
                    Text(q.s("header").isEmpty ? "Question" : q.s("header")).font(.brand(-4, weight: .bold)).foregroundStyle(Theme.muted)
                    Text(q.s("question")).font(.ui(0)).foregroundStyle(Theme.text)
                    let opts = q["options"] as? [JSON] ?? []
                    if !opts.isEmpty {
                        HStack {
                            ForEach(opts.indices, id: \.self) { j in
                                let label = opts[j].s("label")
                                Button(label) { values[qid] = label }
                                    .buttonStyle(.bordered).tint(values[qid] == label ? Theme.accent : Theme.text2)
                                    .help(opts[j].s("description"))
                            }
                        }
                    }
                    if opts.isEmpty || q["isOther"] as? Bool == true {
                        if q["isSecret"] as? Bool == true { SecureField("Answer", text: v).textFieldStyle(.roundedBorder) }
                        else { TextField("Answer", text: v).textFieldStyle(.roundedBorder) }
                    }
                }
            }
            HStack {
                Button("Submit") { Task { await b.answerIn(reqChat, r, .codexInput(answers(empty: false))) } }
                    .buttonStyle(.borderedProminent).tint(Theme.accent)
                Button("Dismiss") { Task { await b.answerIn(reqChat, r, .codexInput(answers(empty: true))) } }.buttonStyle(.bordered)
            }
        }
    }

    func answers(empty: Bool) -> [String: [String]] {
        Dictionary(uniqueKeysWithValues: questions.map { q in
            let v = (values[q.s("id")] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return (q.s("id"), empty || v.isEmpty ? [] : [v])
        })
    }
}

/// SecretRequestCard.vue: the value never comes back to a client; "Use saved" lets Go read the project file itself.
struct SecretCard: View {
    let req: JSON
    let answer: (RequestAnswer) async -> Void
    @State private var value = ""
    @State private var remember: Bool?
    @State private var busy = false

    var body: some View {
        Card(tint: Theme.yellow, pet: "waiting_input") {
            heading("Secret requested: \(req.s("name"))")
            if !req.s("reason").isEmpty { Text(req.s("reason")).font(.ui(-1)).foregroundStyle(Theme.text2) }
            HStack {
                SecureField("Value for \(req.s("name"))", text: $value)
                    .textFieldStyle(.roundedBorder)
                    .onSubmit { run(.secret(value: value, useSaved: false, remember: remembering)) }
                Button("Provide") { run(.secret(value: value, useSaved: false, remember: remembering)) }
                    .buttonStyle(.borderedProminent).tint(Theme.yellow).disabled(value.isEmpty)
                if req["hasSaved"] as? Bool == true {
                    Button("Use saved") { run(.secret(value: "", useSaved: true, remember: false)) }.buttonStyle(.bordered)
                }
                Button("Decline") { run(.decline) }.buttonStyle(.bordered).tint(Theme.red)
            }
            .disabled(busy)
            if req.i("repo_id") != 0 {
                Toggle("Remember for this project", isOn: Binding(get: { remembering }, set: { remember = $0 }))
                    .font(.ui(-2)).toggleStyle(.checkbox)
            }
        }
    }

    var remembering: Bool { remember ?? (req["hasSaved"] as? Bool ?? false) }

    func run(_ a: RequestAnswer) {
        busy = true
        Task { await answer(a); busy = false; value = "" }
    }
}

/// Secret requests owned by a terminal tab ("pty:<id>"); chats get theirs through b.requests.
struct SecretRequestBanner: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var store = AttentionStore.shared
    let ownerKey: String

    var body: some View {
        if let s = store.secrets.first(where: { $0.s("owner") == ownerKey }) {
            SecretCard(req: s) { a in
                do { try await b.respond(chatID: 0, PendingRequest(secret: s), a) }
                catch { b.status = "Answer failed: \(error.localizedDescription)" }
            }
            .id(s.s("id"))
            .padding(.vertical, 8)
        }
    }
}

/// A Claude tab's tool-permission prompt (ptypermissions.go): oldest request, +N, Allow / Always / Deny.
/// The TUI prompt stays up; answering in either place clears both.
struct PtyPermissionBanner: View {
    @EnvironmentObject var b: Burrow
    @ObservedObject var store = AttentionStore.shared
    let ptyID: Int64

    var body: some View {
        let list = store.ptyPerms["\(ptyID)"] ?? []
        if let r = list.first {
            HStack(spacing: 10) {
                Image(systemName: "hand.raised.fill").foregroundStyle(Theme.yellow)
                Text(r.s("tool_name")).font(.brand(-2, weight: .bold)).foregroundStyle(Theme.yellow)
                Text(describePtyPermission(r)).font(.ui(-2, .monospaced)).foregroundStyle(Theme.text).lineLimit(1).truncationMode(.middle)
                if list.count > 1 { Text("+\(list.count - 1)").font(.brand(-3)).foregroundStyle(Theme.muted) }
                Spacer(minLength: 8)
                PtyPermissionActions(request: r)
            }
            .controlSize(.small)
            .padding(.horizontal, 12).padding(.vertical, 8)
            .background(Theme.yellow.opacity(0.1))
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.yellow.opacity(0.35)).frame(height: 1) }
        }
    }
}
