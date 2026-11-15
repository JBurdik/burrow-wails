import PhotosUI
import SwiftUI
import UIKit

/// An attached image: the data URI sent as `images` (what AgentChat's
/// pendingImages holds) plus a decoded thumbnail for the strip.
struct ComposerImage: Identifiable {
    let id = UUID()
    let dataURI: String
    let thumb: UIImage

    /// Downscaled to 1568 px on the long edge (Claude's own resize threshold —
    /// anything bigger is shrunk server-side anyway) and JPEG'd, so a 12 MP photo
    /// is ~300 KB on the socket instead of ~6 MB of base64.
    init?(_ image: UIImage) {
        let maxEdge: CGFloat = 1568
        let size = image.size
        let scale = min(1, maxEdge / max(size.width, size.height))
        let target = CGSize(width: (size.width * scale).rounded(), height: (size.height * scale).rounded())
        let fmt = UIGraphicsImageRendererFormat.default(); fmt.scale = 1
        let resized = UIGraphicsImageRenderer(size: target, format: fmt).image { _ in image.draw(in: CGRect(origin: .zero, size: target)) }
        guard let jpeg = resized.jpegData(compressionQuality: 0.85) else { return nil }
        dataURI = "data:image/jpeg;base64," + jpeg.base64EncodedString()
        thumb = resized
    }
}

/// Drafts outlive the chat screen (ChatView is torn down on back), per chat.
/// ponytail: in memory only — survives navigation, not an app kill.
private enum Drafts {
    static var text: [Int: String] = [:]
    static var images: [Int: [ComposerImage]] = [:]
}

private struct Suggestion: Identifiable {
    var id: String { insert }
    let label: String
    let hint: String
    let insert: String
    let isSkill: Bool
}

/// The chat composer: attachments, `/` completion, send / queue / steer / stop,
/// restart / clear, compact launch settings.
struct Composer: View {
    let chatId: Int
    @Environment(AppModel.self) private var model
    @State private var draft = ""
    @State private var images: [ComposerImage] = []
    @State private var error: String?
    @State private var notice: String?
    @State private var steer = false
    @State private var sending = false
    @State private var sent = 0
    @State private var pickerOpen = false
    @State private var picked: [PhotosPickerItem] = []
    @State private var confirmClear = false
    @State private var skills: [(name: String, description: String)] = []
    @FocusState private var focused: Bool

    private var chat: Chat? { model.chat(chatId) }
    private var phase: Phase { model.phase(chatId) }
    private var trimmed: String { draft.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var canSend: Bool { (!trimmed.isEmpty || !images.isEmpty) && !sending }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let error { Text(error).font(.caption).foregroundStyle(.red) }
            if let notice { Text(notice).font(.caption).foregroundStyle(.secondary) }
            if !suggestions.isEmpty { suggestionList }
            if !images.isEmpty { imageStrip }
            HStack(spacing: 8) {
                if let chat { launchMenu(chat) }
                Spacer(minLength: 0)
                if phase.inFlight {
                    Picker("Send mode", selection: $steer) {
                        Text("Queue").tag(false)
                        Text("Steer").tag(true)
                    }
                    .pickerStyle(.segmented).fixedSize()
                    .disabled(phase.state != "running")
                    .accessibilityHint("Queue waits for the turn to end; Steer delivers into the running turn")
                }
            }
            HStack(alignment: .bottom, spacing: 8) {
                plusMenu
                TextField(placeholder, text: $draft, axis: .vertical)
                    .lineLimit(1...8).focused($focused)
                    .padding(.horizontal, 12).padding(.vertical, 9)
                    .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
                sendButton
            }
        }
        .padding(.horizontal).padding(.vertical, 10)
        .background(.bar)
        .sensoryFeedback(.impact(weight: .medium), trigger: sent)
        .photosPicker(isPresented: $pickerOpen, selection: $picked, maxSelectionCount: 6, matching: .images)
        .onChange(of: picked) { _, items in
            guard !items.isEmpty else { return }
            picked = []
            Task { await addPicked(items) }
        }
        .confirmationDialog("Clear this chat?", isPresented: $confirmClear, titleVisibility: .visible) {
            Button("Clear chat", role: .destructive) { Task { await run { try await model.composerClear(chatId) } } }
        } message: {
            Text("Wipes the transcript and starts a fresh session. This can't be undone.")
        }
        .onAppear {
            draft = Drafts.text[chatId] ?? ""
            images = Drafts.images[chatId] ?? []
        }
        .onChange(of: draft) { _, v in Drafts.text[chatId] = v.isEmpty ? nil : v }
        .onChange(of: images.map(\.id)) { _, _ in Drafts.images[chatId] = images.isEmpty ? nil : images }
        .onChange(of: phase.state) { _, s in if s != "running" { steer = false } }
        .task(id: chat?.workspaceId) {
            if let ws = chat?.workspaceId { skills = await model.composerSkills(workspace: ws) }
        }
    }

    private var placeholder: String {
        guard phase.inFlight else { return "Reply…" }
        return steer ? "Steer the running turn…" : "Queue a follow-up…"
    }

    // MARK: send

    @ViewBuilder private var sendButton: some View {
        if phase.inFlight && !canSend && !sending {
            Button { Task { await model.interrupt(chatId) } } label: {
                Image(systemName: "stop.fill").font(.body.bold()).frame(width: 36, height: 36)
                    .background(Color.red.opacity(0.15), in: RoundedRectangle(cornerRadius: 10)).foregroundStyle(.red)
            }
            .accessibilityLabel("Stop")
        } else {
            Button { Task { await send(steer: phase.inFlight && steer) } } label: {
                Group {
                    if sending { ProgressView().tint(.white) } else {
                        Image(systemName: phase.inFlight && steer ? "arrow.turn.down.right" : "arrow.up").font(.body.bold())
                    }
                }
                .frame(width: 36, height: 36)
                .background(canSend || sending ? Color.accentColor : Color(.systemGray4), in: RoundedRectangle(cornerRadius: 10))
                .foregroundStyle(.white)
            }
            .disabled(!canSend)
            .accessibilityLabel(phase.inFlight ? (steer ? "Steer" : "Queue") : "Send")
            .contextMenu {
                // Long-press: a one-off choice without flipping the toggle.
                if phase.inFlight {
                    Button { Task { await send(steer: false) } } label: { Label("Queue", systemImage: "text.badge.plus") }
                    Button { Task { await send(steer: true) } } label: { Label("Steer now", systemImage: "arrow.turn.down.right") }
                        .disabled(phase.state != "running")
                }
            }
        }
    }

    private func send(steer: Bool) async {
        guard canSend else { return }
        let text = trimmed, imgs = images
        sending = true; defer { sending = false }
        draft = ""; images = []
        do {
            notice = try await model.composerSend(chatId, text, imgs.map(\.dataURI), steer: steer)
            error = nil
            sent += 1
        } catch {
            draft = text; images = imgs
            self.error = error.localizedDescription
        }
    }

    // MARK: + menu

    private var plusMenu: some View {
        Menu {
            Button { pickerOpen = true } label: { Label("Photo Library", systemImage: "photo.on.rectangle") }
            Button { pasteImages() } label: { Label("Paste Image", systemImage: "doc.on.clipboard") }
                .disabled(!UIPasteboard.general.hasImages)
            Divider()
            if phase.inFlight {
                Button(role: .destructive) { Task { await model.interrupt(chatId) } } label: { Label("Stop", systemImage: "stop.fill") }
            }
            Button { Task { await run { try await model.composerRestart(chatId) } } } label: {
                Label("Restart Session", systemImage: "arrow.clockwise")
            }
            Button(role: .destructive) { confirmClear = true } label: { Label("Clear Chat…", systemImage: "trash") }
        } label: {
            Image(systemName: "plus").font(.body.weight(.semibold)).frame(width: 36, height: 36)
                .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
        }
        .accessibilityLabel("More")
    }

    private func run(_ op: () async throws -> Void) async {
        do { try await op(); error = nil } catch { self.error = error.localizedDescription }
    }

    // MARK: images

    private var imageStrip: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(images) { img in
                    Image(uiImage: img.thumb).resizable().scaledToFill()
                        .frame(width: 56, height: 56).clipShape(RoundedRectangle(cornerRadius: 8))
                        .overlay(alignment: .topTrailing) {
                            Button { images.removeAll { $0.id == img.id } } label: {
                                Image(systemName: "xmark.circle.fill").symbolRenderingMode(.palette)
                                    .foregroundStyle(.white, .black.opacity(0.6)).font(.body)
                            }
                            .offset(x: 6, y: -6)
                            .accessibilityLabel("Remove image")
                        }
                }
            }
            .padding(.top, 6).padding(.trailing, 6)
        }
    }

    private func pasteImages() {
        let new = (UIPasteboard.general.images ?? []).compactMap(ComposerImage.init)
        if new.isEmpty { error = "No image on the clipboard." } else { images += new }
    }

    private func addPicked(_ items: [PhotosPickerItem]) async {
        for item in items {
            guard let data = try? await item.loadTransferable(type: Data.self), let ui = UIImage(data: data),
                  let img = ComposerImage(ui) else { error = "Couldn't load an image."; continue }
            images.append(img)
        }
    }

    // MARK: launch settings

    private func launchMenu(_ chat: Chat) -> some View {
        let perm = chat.permissionMode.isEmpty ? "default" : chat.permissionMode
        let p = Catalog.permission(perm)
        let tint: Color = perm == "bypassPermissions" ? .red : .secondary
        let summary = [chat.isCodex ? nil : Catalog.modelLabel(chat.model), Catalog.effortLabel(chat.effort), p.label]
            .compactMap { $0 }.joined(separator: " · ")
        return Menu {
            if !chat.isCodex {
                Picker(selection: launch(chat.model) { try await model.setLaunch(chatId, model: $0) }) {
                    ForEach(Catalog.claudeModels, id: \.id) { Text($0.label).tag($0.id) }
                } label: { Label("Model", systemImage: "cpu") }
                .pickerStyle(.menu)
            }
            Picker(selection: launch(chat.effort) { try await model.setLaunch(chatId, effort: $0) }) {
                ForEach(chat.isCodex ? Catalog.codexEfforts : Catalog.claudeEfforts, id: \.self) { Text(Catalog.effortLabel($0)).tag($0) }
            } label: { Label("Effort", systemImage: "gauge.with.dots.needle.50percent") }
            .pickerStyle(.menu)
            Picker(selection: launch(perm) { try await model.setLaunch(chatId, permission: $0) }) {
                ForEach(Catalog.permissions, id: \.id) { Label($0.label, systemImage: $0.icon).tag($0.id) }
            } label: { Label("Permissions", systemImage: p.icon) }
            .pickerStyle(.menu)
        } label: {
            Label(summary, systemImage: p.icon)
                .font(.caption.weight(.medium)).lineLimit(1)
                .foregroundStyle(tint == .red ? .red : .primary)
                .padding(.horizontal, 8).padding(.vertical, 5)
                .background(tint.opacity(0.14), in: RoundedRectangle(cornerRadius: 7))
        }
    }

    /// A launch-setting binding that writes through chat_set_launch_settings.
    private func launch(_ value: String, _ write: @escaping (String) async throws -> Void) -> Binding<String> {
        Binding(get: { value }, set: { v in
            Task {
                do { try await write(v); error = nil } catch { self.error = error.localizedDescription }
            }
        })
    }

    // MARK: completion (src/lib/composerCompletion.ts)

    /// `/x` or `$x` at the end of the draft, at start or after whitespace (CMD_RE).
    private var trigger: (kind: Character, q: String)? {
        guard let m = draft.firstMatch(of: /(?:^|\s)([\/$])([^\s\/$]*)$/) else { return nil }
        return (Character(String(m.output.1)), m.output.2.lowercased())
    }

    private var suggestions: [Suggestion] {
        guard focused, let t = trigger else { return [] }
        let skillRows = skills
            .filter { $0.name.lowercased().contains(t.q) }
            .sorted { $0.name.lowercased().hasPrefix(t.q) && !$1.name.lowercased().hasPrefix(t.q) }
            .map { Suggestion(label: "/\($0.name)", hint: $0.description, insert: "/\($0.name)", isSkill: true) }
        if t.kind == "$" { return Array(skillRows.prefix(8)) }
        // The phone has no separate `$` key handy: `/` lists commands, then skills.
        let cmds = Self.commands.filter { $0.name.hasPrefix(t.q) }
            .map { Suggestion(label: "/\($0.name)", hint: $0.description, insert: "/\($0.name)", isSkill: false) }
        var seen = Set<String>()
        return Array((cmds + skillRows).filter { seen.insert($0.insert).inserted }.prefix(8))
    }

    private var suggestionList: some View {
        VStack(spacing: 0) {
            ForEach(suggestions) { s in
                Button { apply(s) } label: {
                    HStack(spacing: 8) {
                        Image(systemName: s.isSkill ? "sparkles" : "command").foregroundStyle(.secondary).frame(width: 18)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(s.label).font(.subheadline.weight(.medium)).foregroundStyle(.primary)
                            if !s.hint.isEmpty { Text(s.hint).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                        }
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 10).padding(.vertical, 7)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                if s.id != suggestions.last?.id { Divider().padding(.leading, 36) }
            }
        }
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
    }

    private func apply(_ s: Suggestion) {
        guard let t = trigger else { return }
        draft = String(draft.dropLast(t.q.count + 1)) + s.insert + " "
    }

    /// BUILTIN_COMMANDS in AgentChat.vue.
    static let commands: [(name: String, description: String)] = [
        ("pr", "Write a PR description from recent git diff"),
        ("clear", "Clear conversation history"),
        ("compact", "Compact conversation with summary"),
        ("help", "Show available commands"),
        ("review", "Review changes in current directory"),
        ("init", "Initialize project with CLAUDE.md"),
        ("cost", "Show token and cost usage for this session"),
    ]
}

// MARK: model

private var workspacePaths: [Int: String] = [:]
private var skillCache: [String: (at: Date, list: [(name: String, description: String)])] = [:]

extension AppModel {
    /// Send / queue / steer. Returns a notice for the user when the request was
    /// changed on the way (a refused steer lands in the queue instead).
    @MainActor
    func composerSend(_ chat: Int, _ text: String, _ images: [String], steer: Bool) async throws -> String? {
        guard let client else { throw BurrowError.closed }
        let args: [String: Any] = ["chatId": chat, "text": text, "images": images]
        let p = phase(chat)
        var notice: String?
        if !p.inFlight {
            try await client.call("chat_send", args)
            if let c = self.chat(chat) { startActivity(c) }
        } else if steer && p.state == "running" {
            do {
                try await client.call("chat_steer", args)
            } catch {
                // chatsteer.go refuses outside a running turn and on generic ACP:
                // the message is not lost, it waits in the queue.
                try await client.call("chat_queue_add", args)
                notice = "Couldn't steer (\(error.localizedDescription)) — queued instead."
            }
        } else {
            try await client.call("chat_queue_add", args)
            if steer { notice = "Waiting on you, not running — queued instead." }
        }
        return notice
    }

    func composerRestart(_ chat: Int) async throws {
        guard let client else { throw BurrowError.closed }
        try await client.call("chat_restart", ["chatId": chat])
    }

    func composerClear(_ chat: Int) async throws {
        guard let client else { throw BurrowError.closed }
        try await client.call("chat_clear", ["chatId": chat])
    }

    /// list_skills for the chat's folder, repo skills shadowing personal ones of
    /// the same name, cached a minute per cwd (composerCompletion.ts loadSkills).
    @MainActor
    func composerSkills(workspace: Int) async -> [(name: String, description: String)] {
        guard let client else { return [] }
        if workspacePaths[workspace] == nil, let list = try? await client.call("list_workspaces") as? [[String: Any]] {
            for w in list { workspacePaths[int(w["id"])] = w["path"] as? String }
        }
        guard let cwd = workspacePaths[workspace] else { return [] }
        if let hit = skillCache[cwd], hit.at.timeIntervalSinceNow > -60 { return hit.list }
        guard let raw = try? await client.call("list_skills", ["cwd": cwd]) as? [[String: Any]] else { return [] }
        var merged: [String: (name: String, description: String)] = [:]
        for s in raw where s["enabled"] as? Bool ?? true {
            guard let name = s["name"] as? String else { continue }
            if merged[name] != nil && s["source"] as? String != "project" { continue }
            merged[name] = (name, s["description"] as? String ?? "")
        }
        let list = merged.values.sorted { $0.name < $1.name }
        skillCache[cwd] = (Date(), list)
        return list
    }
}
