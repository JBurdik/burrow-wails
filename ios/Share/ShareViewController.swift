import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// Share sheet → new Burrow thread with the shared text / link as its prompt.
final class ShareViewController: UIViewController {
    override func viewDidLoad() {
        super.viewDidLoad()
        let items = (extensionContext?.inputItems as? [NSExtensionItem]) ?? []
        let host = UIHostingController(rootView: ShareView(items: items) { [weak self] in
            self?.extensionContext?.completeRequest(returningItems: nil)
        })
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
    }
}

struct ShareView: View {
    let items: [NSExtensionItem]
    let done: () -> Void

    @State private var workspaces = CachedWorkspace.load()
    @State private var workspace: Int?
    @State private var agent = "claude"
    @State private var text = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                if CredentialStore.load() == nil || workspaces.isEmpty {
                    Text("Open Burrow and pair with your Mac first.").foregroundStyle(.secondary)
                } else {
                    Picker("Project", selection: $workspace) {
                        ForEach(workspaces) { Text($0.name).tag(Int?.some($0.id)) }
                    }
                    Picker("Agent", selection: $agent) {
                        Text("Claude").tag("claude")
                        Text("Codex").tag("codex")
                    }.pickerStyle(.segmented)
                    TextField("Prompt", text: $text, axis: .vertical).lineLimit(4...14)
                }
                if let error { Text(error).foregroundStyle(.red) }
            }
            .navigationTitle("New Burrow thread")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel", action: done) }
                ToolbarItem(placement: .confirmationAction) {
                    Button(busy ? "Sending…" : "Send") { Task { await send() } }
                        .disabled(busy || workspace == nil || text.isEmpty)
                }
            }
        }
        .task {
            workspace = SharedStore.get("share.lastWorkspace", Int.self) ?? workspaces.first?.id
            text = await sharedText()
        }
    }

    private func sharedText() async -> String {
        var parts: [String] = []
        for item in items {
            for p in item.attachments ?? [] {
                if p.hasItemConformingToTypeIdentifier(UTType.url.identifier),
                   let url = try? await p.loadItem(forTypeIdentifier: UTType.url.identifier) as? URL {
                    parts.append(url.absoluteString)
                } else if p.hasItemConformingToTypeIdentifier(UTType.plainText.identifier),
                          let s = try? await p.loadItem(forTypeIdentifier: UTType.plainText.identifier) as? String {
                    parts.append(s)
                }
            }
        }
        return parts.joined(separator: "\n\n")
    }

    private func send() async {
        guard let creds = CredentialStore.load(), let workspace else { return }
        busy = true; defer { busy = false }
        let client = BurrowClient(creds)
        client.start()
        defer { client.stop() }
        do {
            try await client.ready()
            let row = try await client.call("remote_create_chat", ["workspaceId": workspace, "agentKind": agent,
                                                                   "model": "", "effort": "", "permissionMode": ""]) as? [String: Any]
            let id = (row?["id"] as? NSNumber)?.intValue ?? 0
            guard id > 0 else { throw BurrowError.server("Could not create the thread.") }
            // ponytail: heuristic title only — the extension is gone before a model
            // title (up to minutes) could come back. Rename on the Mac if it matters.
            await client.nameThread(chatId: id, current: row?["title"] as? String ?? "", cwd: "", text: text, heuristicOnly: true)
            try await client.call("chat_send", ["chatId": id, "text": text, "images": [String](), "clientId": UUID().uuidString])
            SharedStore.put("share.lastWorkspace", workspace)
            done()
        } catch { self.error = error.localizedDescription }
    }
}
