import SwiftUI

/// Scheduled prompts (Settings → Scheduled on the desktop, ScheduledTasksPanel.vue) as a sheet.
struct ScheduleSheet: View {
    @EnvironmentObject var b: Burrow
    @Environment(\.dismiss) private var dismiss
    @State private var items: [JSON] = []
    @State private var draft: Draft?
    @State private var error = ""

    struct Draft {
        var id: Int64 = 0
        var name = ""
        var text = ""
        var kind = "weekly"
        var due = Date().addingTimeInterval(3600)
        var everyMin = 60
        var weekdays: Set<Int> = [1, 2, 3, 4, 5]
        var time = "09:00"
        var newChat = false
        var chatID: Int64 = 0
        var workspaceID: Int64 = 0
        var agent = ""
        var paused = false
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Text("Scheduled").font(.brand(1, weight: .bold)).foregroundStyle(Theme.text)
                Spacer()
                if draft == nil {
                    Button("New task") { draft = Draft(chatID: b.chats.first?.i("id") ?? 0, workspaceID: b.projects.first?.i("id") ?? 0) }
                }
                Button("Done") { dismiss() }.keyboardShortcut(.cancelAction)
            }
            Text("Prompts sent later, once or on a schedule. A run into a chat waits until its turn has finished.")
                .font(.ui(-2)).foregroundStyle(Theme.muted)
            if !error.isEmpty { Text(error).font(.ui(-2)).foregroundStyle(Theme.red) }
            if draft != nil { editor } else { list }
        }
        .padding(20)
        .frame(width: 560, height: 520, alignment: .top)
        .background(Theme.base)
        .task { await load() }
    }

    var list: some View {
        ScrollView {
            if items.isEmpty {
                VStack(spacing: 8) { Pet(state: "idle", size: 48); Text("Nothing scheduled").font(.ui(-1)).foregroundStyle(Theme.muted) }
                    .frame(maxWidth: .infinity).padding(.top, 40)
            }
            VStack(spacing: 8) {
                ForEach(items, id: \.id) { s in
                    HStack(alignment: .top) {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(s.s("name").isEmpty ? s.s("text") : s.s("name")).font(.ui(-1, weight: .semibold)).foregroundStyle(Theme.text).lineLimit(1)
                            Text(subtitle(s)).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(2)
                            if !s.s("last_error").isEmpty { Text(s.s("last_error")).font(.brand(-5)).foregroundStyle(Theme.red).lineLimit(1) }
                        }
                        Spacer()
                        Button(s["paused"] as? Bool == true ? "Resume" : "Pause") {
                            act("schedule_set_paused", ["id": s.i("id"), "paused": !(s["paused"] as? Bool ?? false)])
                        }
                        Button("Run now") { act("schedule_run_now", ["id": s.i("id")]) }.disabled(s["paused"] as? Bool == true)
                        Button("Edit") { draft = Self.draft(s) }
                        Button("Delete") { act("chat_schedule_cancel", ["id": s.i("id")]) }
                    }
                    .buttonStyle(.borderless).font(.brand(-4))
                    .padding(12)
                    .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                    .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
                }
            }
        }
    }

    @ViewBuilder var editor: some View {
        let d = Binding(get: { draft ?? Draft() }, set: { draft = $0 })
        Form {
            TextField("Name", text: d.name)
            TextField("Prompt", text: d.text, axis: .vertical).lineLimit(3...8)
            Picker("Repeat", selection: d.kind) {
                Text("Once").tag("once"); Text("Every N minutes").tag("interval"); Text("Weekly").tag("weekly")
            }
            switch d.wrappedValue.kind {
            case "once": DatePicker("At", selection: d.due)
            case "interval": Stepper("Every \(d.wrappedValue.everyMin) min", value: d.everyMin, in: 15...10080, step: 15)
            default:
                HStack {
                    ForEach(Array(["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"].enumerated()), id: \.offset) { i, n in
                        Toggle(n, isOn: Binding(get: { d.wrappedValue.weekdays.contains(i) },
                                                set: { on in if on { draft?.weekdays.insert(i) } else { draft?.weekdays.remove(i) } }))
                            .toggleStyle(.button)
                    }
                }
                TextField("Time (HH:MM)", text: d.time)
            }
            Toggle("Start a new chat each run", isOn: d.newChat)
            if d.wrappedValue.newChat {
                Picker("Project", selection: d.workspaceID) {
                    ForEach(b.projects, id: \.id) { Text($0.s("name")).tag($0.i("id")) }
                }
                Picker("Agent", selection: d.agent) {
                    Text("Default").tag("")
                    ForEach(Launch.agents, id: \.0) { Text($0.1).tag($0.0) }
                }
            } else {
                Picker("Chat", selection: d.chatID) {
                    ForEach(b.chats, id: \.id) { Text($0.s("title").isEmpty ? "Untitled" : $0.s("title")).lineLimit(1).tag($0.i("id")) }
                }
            }
        }
        .formStyle(.grouped).scrollContentBackground(.hidden)
        HStack {
            Spacer()
            Button("Cancel") { draft = nil }
            Button("Save") { Task { await save() } }.keyboardShortcut(.defaultAction)
                .disabled(d.wrappedValue.text.trimmingCharacters(in: .whitespaces).isEmpty)
        }
    }

    func subtitle(_ s: JSON) -> String {
        let target = s["new_chat"] as? Bool == true
            ? "New chat in \(b.workspaces[s.i("workspace_id")]?.s("name") ?? "?")"
            : (b.chat(s.i("chat_id"))?.s("title") ?? "Chat \(s.i("chat_id"))")
        var parts = [target, s.s("recurs")]
        parts.append(s["paused"] as? Bool == true ? "Paused" : "next \(Self.when(s.s("due_at")))")
        if !s.s("last_run_at").isEmpty { parts.append("last \(Self.when(s.s("last_run_at")))") }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    static let iso: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter(); f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]; return f
    }()
    static func date(_ s: String) -> Date? { iso.date(from: s) ?? ISO8601DateFormatter().date(from: s) }
    static func when(_ s: String) -> String {
        guard let d = date(s) else { return "—" }
        return d.formatted(date: .abbreviated, time: .shortened)
    }

    static func draft(_ s: JSON) -> Draft {
        let r = s["rule"] as? JSON ?? [:]
        return Draft(
            id: s.i("id"), name: s.s("name"), text: s.s("text"), kind: r.s("kind").isEmpty ? "once" : r.s("kind"),
            due: date(s.s("due_at")) ?? Date().addingTimeInterval(3600),
            everyMin: max(15, Int(r.i("every_min"))), weekdays: Set((r["weekdays"] as? [Int]) ?? []),
            time: r.s("time_of_day").isEmpty ? "09:00" : r.s("time_of_day"),
            newChat: s["new_chat"] as? Bool ?? false, chatID: s.i("chat_id"), workspaceID: s.i("workspace_id"),
            agent: s.s("agent"), paused: s["paused"] as? Bool ?? false)
    }

    /// Same shape ScheduledTasksPanel.save() sends; a recurring item's next run is Go's (NextDue).
    static func payload(_ d: Draft) -> JSON {
        let rule: JSON = switch d.kind {
        case "interval": ["kind": "interval", "every_min": d.everyMin]
        case "weekly": ["kind": "weekly", "weekdays": d.weekdays.sorted(), "time_of_day": d.time]
        default: ["kind": "once"]
        }
        return [
            "id": d.id, "chat_id": d.newChat ? 0 : d.chatID, "name": d.name, "text": d.text, "rule": rule,
            "new_chat": d.newChat, "workspace_id": d.newChat ? d.workspaceID : 0, "agent": d.newChat ? d.agent : "",
            "paused": d.paused,
            "due_at": d.kind == "once" ? ISO8601DateFormatter().string(from: d.due) : "0001-01-01T00:00:00Z",
        ]
    }

    func save() async {
        guard let d = draft else { return }
        do {
            try await b.call("schedule_save", ["schedule": Self.payload(d)])
            draft = nil; error = ""
        } catch { self.error = error.localizedDescription }
        await load()
    }

    func act(_ cmd: String, _ args: JSON) {
        Task {
            do { try await b.call(cmd, args); error = "" } catch { self.error = error.localizedDescription }
            await load()
        }
    }

    func load() async {
        items = (try? await b.call("schedule_list_all")) as? [JSON] ?? []
    }
}
