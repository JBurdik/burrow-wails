import AppKit
import SwiftUI

struct NewThreadView: View {
    @EnvironmentObject var b: Burrow
    @AppStorage("newThread.workspace") private var workspaceID = 0
    @AppStorage("newThread.agent") private var agent = "claude"
    @AppStorage("newThread.model") private var model = ""
    @AppStorage("newThread.effort") private var effort = ""
    @AppStorage("newThread.mode") private var mode = "default"
    @AppStorage("newThread.worktree") private var newWorktree = false
    @State private var draft = ""
    @State private var busy = false

    var body: some View {
        let project = b.projects.first { $0.i("id") == Int64(workspaceID) } ?? b.projects.first
        VStack(spacing: 18) {
            Spacer()
            Pet(state: "idle", size: 88)
            Text("What are we digging into?").font(.brand(9, weight: .bold)).foregroundStyle(Theme.text)
            Menu {
                // Archived projects sink to the bottom of the picker (archivedWorkspaces.ts) — their only effect.
                let archived = SidebarStore.shared.archivedProjects
                ForEach(b.projects.filter { !archived.contains($0.i("id")) } + b.projects.filter { archived.contains($0.i("id")) }, id: \.id) { p in
                    Button(archived.contains(p.i("id")) ? "\(p.s("name")) (archived)" : p.s("name")) { workspaceID = Int(p.i("id")) }
                }
            } label: {
                Text(project?.s("name") ?? "No project").font(.brand(-2)).foregroundStyle(Theme.text)
            }
            .pillStyle()
            .overlay(alignment: .leading) { if let project { ProjectIcon(ws: project, size: 18).offset(x: -26) } }
            Toggle(isOn: $newWorktree) {
                Label("New worktree", systemImage: "arrow.triangle.branch").font(.brand(-3))
            }
            .toggleStyle(.checkbox).foregroundStyle(Theme.text2)
            .help("Run the thread in a fresh git worktree (burrow/<name>). Paste a PR URL or #123 to check that PR out instead.")
            if busy { ProgressView().controlSize(.small) }
            Composer(draft: $draft, running: false, placeholder: "Describe the task…", send: start, stop: {}) {
                LaunchBar(agent: $agent, model: $model, effort: $effort, mode: $mode)
            }
            .onChange(of: agent) { model = ""; if !Launch.modes(agent).contains(where: { $0.0 == mode }) { mode = "default" } }
            Spacer()
            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background { ZStack { Theme.base; BurrowGlow() } }
        .navigationTitle("New thread")
    }

    func start() {
        let t = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty, let ws = (b.projects.first { $0.i("id") == Int64(workspaceID) } ?? b.projects.first)?.i("id") else { return }
        draft = ""
        let project = b.workspaces[ws] ?? [:]
        busy = true
        Task {
            defer { busy = false }
            // The whole prompt is a PR URL / "#123": check it out into a worktree + chat, prompt left as a draft.
            if let pr = parsePrRef(t, bare: false) {
                if await b.checkoutPR(project: project, number: pr.number, url: pr.url, agent: agent) == nil { draft = t }
            } else if newWorktree {
                await b.createWorktreeThread(project: project, agent: agent, model: model, effort: effort, mode: mode, prompt: t)
            } else {
                await b.createThread(workspaceID: ws, agent: agent, model: model, effort: effort, mode: mode, prompt: t)
            }
        }
    }
}
