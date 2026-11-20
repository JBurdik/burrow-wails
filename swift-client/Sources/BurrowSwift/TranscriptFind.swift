import SwiftUI

/// ⌘F inside one thread (ThreadFindBar.vue + threadFind.ts). Matches are counted over the loaded messages;
/// TranscriptList unfolds and scrolls to the current one, rows highlight every occurrence.
@MainActor final class TranscriptFindState: ObservableObject {
    @Published var open = false
    @Published var query = ""
    @Published var index = 0
    /// Bumped by next/prev even when the index wraps to the same value (one match), so it still re-scrolls.
    @Published var step = 0
    /// Bumped by ⌘F to (re)focus the field.
    @Published var focus = 0

    func move(_ d: Int, total: Int) { index = stepFind(index, total: total, d); step += 1 }
}

struct TranscriptFindBar: View {
    @ObservedObject var find: TranscriptFindState
    let total: Int
    @FocusState private var focused: Bool

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass").foregroundStyle(Theme.muted)
            TextField("Find in chat", text: $find.query)
                .textFieldStyle(.plain).font(.ui(-1)).frame(width: 180)
                .focused($focused)
                .onSubmit { find.move(1, total: total); focused = true }
                .onExitCommand { close() }
            Text(total == 0 ? (find.query.isEmpty ? "" : "0/0") : "\(min(find.index, total - 1) + 1)/\(total)")
                .font(.brand(-4)).foregroundStyle(total == 0 && !find.query.isEmpty ? Theme.red : Theme.muted)
                .frame(minWidth: 36)
            Button { find.move(-1, total: total) } label: { Image(systemName: "chevron.up") }
                .keyboardShortcut("g", modifiers: [.command, .shift]).help("Previous (⇧⌘G)").disabled(total == 0)
            Button { find.move(1, total: total) } label: { Image(systemName: "chevron.down") }
                .keyboardShortcut("g", modifiers: .command).help("Next (⌘G / Return)").disabled(total == 0)
            Button { close() } label: { Image(systemName: "xmark") }.help("Close (Esc)")
        }
        .buttonStyle(.plain).foregroundStyle(Theme.text2)
        .padding(.horizontal, 10).padding(.vertical, 7)
        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.border))
        .shadow(color: .black.opacity(0.25), radius: 8, y: 2)
        .onAppear { focused = true }
        .onChange(of: find.focus) { focused = true }
    }

    func close() { find.open = false; find.query = ""; find.index = 0 }
}

/// Paints every case-insensitive occurrence of `q` in `a` (find highlight).
func highlight(_ a: inout AttributedString, _ q: String) {
    guard !q.isEmpty else { return }
    let plain = String(a.characters)
    for r in occurrences(plain, q) {
        let lo = a.characters.index(a.startIndex, offsetBy: plain.distance(from: plain.startIndex, to: r.lowerBound))
        let hi = a.characters.index(lo, offsetBy: plain.distance(from: r.lowerBound, to: r.upperBound))
        a[lo..<hi].backgroundColor = Theme.yellow.opacity(0.45)
        a[lo..<hi].foregroundColor = Theme.text
    }
}

func highlighted(_ s: String, _ q: String) -> AttributedString {
    var a = AttributedString(s)
    highlight(&a, q)
    return a
}
