import SwiftUI

/// The desktop's Right Panel, on the phone: one sheet per thread with its surfaces.
struct RightPanelView: View {
    let chatId: Int
    @Environment(\.dismiss) private var dismiss
    @AppStorage("panel.surface") private var surface: Surface = .thread

    enum Surface: String, CaseIterable, Identifiable {
        case thread = "Thread", files = "Files", plan = "Plan", servers = "Processes", git = "Git"
        var id: String { rawValue }
        var icon: String {
            switch self {
            case .thread: "sparkles.rectangle.stack"
            case .files: "folder"
            case .plan: "checklist"
            case .servers: "server.rack"
            case .git: "arrow.triangle.branch"
            }
        }
    }

    var body: some View {
        NavigationStack {
            Group {
                switch surface {
                case .thread: ThreadDetailsView(chatId: chatId, embedded: true)
                case .files: FilesSurface(chatId: chatId)
                case .plan: PlanSurface(chatId: chatId)
                case .servers: ProcessesSurface(chatId: chatId)
                case .git: GitSurface(chatId: chatId)
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Picker("Surface", selection: $surface) {
                        ForEach(Surface.allCases) { Image(systemName: $0.icon).tag($0).accessibilityLabel($0.rawValue) }
                    }
                    .pickerStyle(.segmented).frame(width: 260)
                }
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
    }
}
