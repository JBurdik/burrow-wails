import SwiftUI

// Selects that are bigger than a Menu: a searchable project sheet, the launch-settings
// sheet (model · effort · permissions, with what each does) and the agent cards.

// MARK: - Project

struct ProjectChoice: Identifiable, Hashable {
    var id: Int
    var name: String
    var path: String
    var depth: Int = 0
}

/// The field that shows the current project and opens `ProjectPickerSheet`.
struct ProjectPickerField: View {
    let choices: [ProjectChoice]
    @Binding var selection: Int
    var title = "Project"
    @Environment(AppModel.self) private var model
    @State private var open = false

    var body: some View {
        let cur = choices.first { $0.id == selection }
        Button { open = true } label: {
            HStack(spacing: 10) {
                if let cur {
                    ProjectBadge(name: cur.name, workspaceId: model.workspace(cur.id)?.repoId ?? cur.id, size: 28)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(cur.name).font(.headline).foregroundStyle(.primary)
                        if !cur.path.isEmpty {
                            Text(cur.path).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.head)
                        }
                    }
                } else {
                    Text("Pick a project").foregroundStyle(.secondary)
                }
                Spacer()
                Image(systemName: "chevron.up.chevron.down").font(.caption).foregroundStyle(.secondary)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .sheet(isPresented: $open) { ProjectPickerSheet(choices: choices, selection: $selection) }
    }
}

struct ProjectPickerSheet: View {
    let choices: [ProjectChoice]
    @Binding var selection: Int
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @AppStorage("recent.projects") private var recentRaw = ""
    @State private var query = ""

    private var recent: [ProjectChoice] {
        recentRaw.split(separator: ",").compactMap { Int($0) }.compactMap { id in choices.first { $0.id == id } }.prefix(3).map { $0 }
    }
    private var shown: [ProjectChoice] {
        query.isEmpty ? choices : choices.filter { $0.name.localizedCaseInsensitiveContains(query) || $0.path.localizedCaseInsensitiveContains(query) }
    }

    var body: some View {
        NavigationStack {
            List {
                if query.isEmpty && !recent.isEmpty {
                    Section("Recent") { ForEach(recent) { row($0) } }
                }
                Section(query.isEmpty ? "All projects" : "Results") { ForEach(shown) { row($0) } }
            }
            .overlay { if shown.isEmpty { ContentUnavailableView.search(text: query) } }
            .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search projects")
            .navigationTitle("Project").navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
        }
        .presentationDetents([.medium, .large])
    }

    private func row(_ c: ProjectChoice) -> some View {
        Button {
            selection = c.id
            recentRaw = ([c.id] + recentRaw.split(separator: ",").compactMap { Int($0) }.filter { $0 != c.id }).prefix(5).map(String.init).joined(separator: ",")
            dismiss()
        } label: {
            HStack(spacing: 10) {
                ProjectBadge(name: c.name, workspaceId: model.workspace(c.id)?.repoId ?? c.id, size: 28)
                    .padding(.leading, CGFloat(c.depth) * 14)
                VStack(alignment: .leading, spacing: 2) {
                    Text((c.depth > 0 ? "↳ " : "") + c.name).foregroundStyle(.primary)
                    if !c.path.isEmpty { Text(c.path).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.head) }
                }
                Spacer()
                if c.id == selection { Image(systemName: "checkmark").foregroundStyle(Color.accentColor) }
            }
            .contentShape(Rectangle())
        }
    }
}

// MARK: - Agent

struct AgentCards: View {
    @Binding var agent: String
    private let agents: [(id: String, name: String, detail: String, icon: String, tint: Color)] = [
        ("claude", "Claude", "Claude Code", "sparkle", .orange),
        ("codex", "Codex", "OpenAI Codex", "chevron.left.forwardslash.chevron.right", .teal),
    ]

    var body: some View {
        HStack(spacing: 10) {
            ForEach(agents, id: \.id) { a in
                let on = agent == a.id
                Button { agent = a.id } label: {
                    VStack(spacing: 6) {
                        Image(systemName: a.icon).font(.title2).foregroundStyle(a.tint)
                        Text(a.name).font(.subheadline.weight(.semibold)).foregroundStyle(.primary)
                        Text(a.detail).font(.caption2).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity).padding(.vertical, 12)
                    .background(on ? a.tint.opacity(0.14) : Color(.secondarySystemFill), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(on ? a.tint : .clear, lineWidth: 1.5))
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .sensoryFeedback(.selection, trigger: agent)
    }
}

// MARK: - Launch settings

extension Catalog {
    static func modelDetail(_ id: String) -> String {
        switch id {
        case "": "The account's default model"
        case let s where s.contains("opus"): "Most capable, slower"
        case let s where s.contains("fable"): "Strongest reasoning"
        case let s where s.contains("sonnet"): "Balanced speed and quality"
        case let s where s.contains("haiku"): "Fastest and cheapest"
        default: ""
        }
    }
    static func effortDetail(_ e: String) -> String {
        switch e {
        case "": "Let the model decide"
        case "minimal", "low": "Quick answers, little thinking"
        case "medium": "Everyday work"
        case "high": "Careful, more thinking"
        case "xhigh": "Hard problems, much slower"
        case "max": "Maximum thinking, slowest"
        default: ""
        }
    }
    static func permissionDetail(_ id: String) -> String {
        switch id {
        case "default": "Asks before edits and commands"
        case "auto": "Decides what is safe to run itself"
        case "acceptEdits": "Edits files freely, asks for commands"
        case "plan": "Plans first, changes nothing"
        case "bypassPermissions": "Runs everything without asking"
        default: ""
        }
    }
}

/// Model · effort · permissions in one sheet, each choice with what it means.
struct LaunchSheet: View {
    var isCodex: Bool
    @Binding var model: String
    @Binding var effort: String
    @Binding var permission: String
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                if !isCodex {
                    Section("Model") {
                        ForEach(Catalog.claudeModels, id: \.id) { m in
                            option("cpu", m.label, Catalog.modelDetail(m.id), model == m.id) { model = m.id }
                        }
                    }
                }
                Section("Effort") {
                    ForEach(isCodex ? Catalog.codexEfforts : Catalog.claudeEfforts, id: \.self) { e in
                        option("gauge.with.dots.needle.50percent", Catalog.effortLabel(e), Catalog.effortDetail(e), effort == e) { effort = e }
                    }
                }
                Section("Permissions") {
                    ForEach(Catalog.permissions, id: \.id) { p in
                        option(p.icon, p.label, Catalog.permissionDetail(p.id), permission == p.id,
                               tint: p.id == "bypassPermissions" ? .red : nil) { permission = p.id }
                    }
                }
            }
            .navigationTitle("Launch settings").navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .sensoryFeedback(.selection, trigger: model + effort + permission)
        }
        .presentationDetents([.medium, .large])
    }

    private func option(_ icon: String, _ title: String, _ detail: String, _ selected: Bool, tint: Color? = nil, pick: @escaping () -> Void) -> some View {
        Button(action: pick) {
            HStack(spacing: 12) {
                Image(systemName: icon).frame(width: 24).foregroundStyle(tint ?? .secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).foregroundStyle(tint ?? .primary)
                    if !detail.isEmpty { Text(detail).font(.caption).foregroundStyle(.secondary) }
                }
                Spacer()
                if selected { Image(systemName: "checkmark").foregroundStyle(Color.accentColor) }
            }
            .contentShape(Rectangle())
        }
    }
}
