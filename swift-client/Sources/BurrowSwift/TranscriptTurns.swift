import SwiftUI

/// A settled turn's thinking / tool rows fold behind this one row (AgentChat's `worked-bar`).
struct TurnFoldRow: View {
    let label: String
    let folded: Bool
    let tools: Int
    let files: Int
    let toggle: () -> Void

    var body: some View {
        Button(action: toggle) {
            HStack(spacing: 8) {
                Image(systemName: "chevron.right").font(.system(size: 10, weight: .bold))
                    .rotationEffect(.degrees(folded ? 0 : 90)).foregroundStyle(Theme.muted)
                Text(label).font(.brand(-3, weight: .bold)).foregroundStyle(Theme.text)
                Spacer()
                if tools > 0 { pill("wrench.and.screwdriver", "\(tools) tool \(tools == 1 ? "call" : "calls")") }
                if files > 0 { pill("pencil", "\(files) \(files == 1 ? "file" : "files")") }
            }
            .padding(.horizontal, 12).padding(.vertical, 7)
            .background(Theme.panel.opacity(0.45), in: RoundedRectangle(cornerRadius: 10))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    func pill(_ icon: String, _ text: String) -> some View {
        Label(text, systemImage: icon).font(.brand(-5)).foregroundStyle(Theme.text2)
            .padding(.horizontal, 6).padding(.vertical, 2)
            .background(Theme.base.opacity(0.6), in: RoundedRectangle(cornerRadius: 6))
    }
}

/// "Edited N files" diffstat card at the end of a settled turn; a row expands into the edit's own lines,
/// the arrow opens the RP diff, the name opens the file.
struct ChangedFilesCard: View {
    let files: [FileEdit]
    @State private var open: Set<String> = []

    var body: some View {
        let added = files.reduce(0) { $0 + $1.added }, removed = files.reduce(0) { $0 + $1.removed }
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "pencil").foregroundStyle(Theme.accent)
                    .frame(width: 26, height: 26).background(Theme.accent.opacity(0.14), in: RoundedRectangle(cornerRadius: 7))
                VStack(alignment: .leading, spacing: 1) {
                    Text("Edited \(files.count) \(files.count == 1 ? "file" : "files")").font(.ui(-1, weight: .semibold))
                    Text(dirs).font(.brand(-5)).foregroundStyle(Theme.muted).lineLimit(1)
                }
                Spacer()
                stat(added, removed)
            }
            .padding(.horizontal, 12).padding(.vertical, 9)
            ForEach(files, id: \.path) { f in
                Divider().overlay(Theme.border)
                fileRow(f)
                if open.contains(f.path) { lines(f) }
            }
        }
        .frame(maxWidth: 720, alignment: .leading)
        .background(Theme.panel.opacity(0.6), in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.border.opacity(0.8)))
        .clipShape(RoundedRectangle(cornerRadius: 12))
    }

    var dirs: String {
        var seen: [String] = []
        for f in files { let d = ((f.path as NSString).deletingLastPathComponent as NSString).lastPathComponent; if !seen.contains(d) { seen.append(d) } }
        return seen.joined(separator: ", ")
    }

    func stat(_ a: Int, _ r: Int) -> some View {
        HStack(spacing: 4) {
            Text("+\(a)").foregroundStyle(Theme.green)
            Text("−\(r)").foregroundStyle(Theme.red)
        }
        .font(.brand(-4))
    }

    func fileRow(_ f: FileEdit) -> some View {
        HStack(spacing: 6) {
            Image(systemName: "chevron.right").font(.system(size: 9, weight: .bold)).foregroundStyle(Theme.muted)
                .rotationEffect(.degrees(open.contains(f.path) ? 90 : 0))
            Image(systemName: "doc.text").foregroundStyle(Theme.muted)
            Button((f.path as NSString).lastPathComponent) { openFileInRP(f.path) }
                .buttonStyle(.plain).font(.ui(-2, weight: .semibold)).foregroundStyle(Theme.text).lineLimit(1)
                .help("Open \(f.path)")
            Text((f.path as NSString).deletingLastPathComponent).font(.brand(-5)).foregroundStyle(Theme.muted)
                .lineLimit(1).truncationMode(.head)
            Spacer()
            stat(f.added, f.removed)
            Button { RPOpenRequests.shared.surface = "diff" } label: { Image(systemName: "arrow.up.forward.square") }
                .buttonStyle(.plain).foregroundStyle(Theme.muted).help("Open the diff")
        }
        .padding(.horizontal, 12).padding(.vertical, 6)
        .contentShape(Rectangle())
        .onTapGesture { if open.contains(f.path) { open.remove(f.path) } else { open.insert(f.path) } }
    }

    func lines(_ f: FileEdit) -> some View {
        let h = hunkLines(f.hunks)
        return VStack(alignment: .leading, spacing: 0) {
            ForEach(h.lines.indices, id: \.self) { i in
                let l = h.lines[i]
                if l.kind == "sep" {
                    Text("⋯").foregroundStyle(Theme.muted).padding(.leading, 18)
                } else {
                    HStack(alignment: .top, spacing: 6) {
                        Text(l.kind == "add" ? "+" : "−").foregroundStyle(l.kind == "add" ? Theme.green : Theme.red).frame(width: 10)
                        Text(l.text.isEmpty ? " " : l.text).foregroundStyle(Theme.text)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background((l.kind == "add" ? Theme.green : Theme.red).opacity(0.1))
                }
            }
            if h.hidden > 0 {
                Button("\(h.hidden) more lines — open diff") { RPOpenRequests.shared.surface = "diff" }
                    .buttonStyle(.plain).font(.brand(-4)).foregroundStyle(Theme.accent).padding(6)
            }
        }
        .font(.ui(-3, .monospaced))
        .textSelection(.enabled)
        .padding(.horizontal, 8).padding(.bottom, 8)
    }
}
