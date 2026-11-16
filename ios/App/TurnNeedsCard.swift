import SwiftUI

/// end_turn's card: Needs you = one ask at a time with options; Done = follow-ups.
struct TurnNeedsCard: View {
    @Environment(AppModel.self) private var model
    let needs: TurnNeeds
    let chatId: Int
    @State private var answers: [Int: Set<String>] = [:]
    /// "Something else": the user's own words for an ask (replaces its options).
    @State private var other: [Int: String] = [:]
    @State private var otherOpen: Set<Int> = []
    @State private var confirmRun: Int?
    @State private var starting: Int?
    @State private var runError: String?
    @State private var sending = false
    @State private var sendError: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if needs.needsYou {
                Label("Needs you", systemImage: "questionmark.bubble.fill").font(.caption.weight(.semibold)).foregroundStyle(.orange)
                ForEach(Array(needs.asks.enumerated()), id: \.offset) { i, ask in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(ask.question).font(.subheadline.weight(.medium))
                        if let run = ask.run { runBlock(i, run) }
                        ForEach(Array(ask.options.enumerated()), id: \.offset) { j, opt in
                            option(i, ask, j, opt)
                        }
                        somethingElse(i, ask)
                    }
                }
                Button("Send answers") { Task { await answer(picks) } }
                    .buttonStyle(.borderedProminent)
                    .buttonBorderShape(.roundedRectangle(radius: 10))
                    .disabled(sending || picks.isEmpty && !needs.asks.contains { !($0.run?.reply ?? "").isEmpty })
            } else {
                Label("Done", systemImage: "checkmark.circle.fill").font(.caption.weight(.semibold)).foregroundStyle(.green)
                ForEach(needs.next, id: \.self) { n in
                    Button { Task { await answer([TurnPickItem(index: 0, options: [n])]) } } label: {
                        HStack { Text(n).multilineTextAlignment(.leading); Spacer(); Image(systemName: "arrow.up.right") }
                            .font(.subheadline)
                    }
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.roundedRectangle(radius: 10))
                    .disabled(sending)
                }
            }
            if let sendError { Text(sendError).font(.caption).foregroundStyle(.red) }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
        .confirmationDialog("Run this command on the Mac?", isPresented: Binding(get: { confirmRun != nil }, set: { if !$0 { confirmRun = nil } }),
                            titleVisibility: .visible, presenting: confirmRun) { i in
            Button("Run") { Task { await run(i) } }
            Button("Cancel", role: .cancel) {}
        } message: { i in
            Text(needs.asks.indices.contains(i) ? needs.asks[i].run?.command ?? "" : "")
        }
    }

    /// The command, where it runs, and Run / its result (TurnNeedsCard.vue's needs-run).
    private func runBlock(_ i: Int, _ run: TurnRun) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            ScrollView(.horizontal, showsIndicators: false) {
                Text(run.command).font(.caption.monospaced()).textSelection(.enabled).fixedSize()
            }
            .padding(8)
            .background(Color(.tertiarySystemBackground), in: RoundedRectangle(cornerRadius: 8))
            HStack(spacing: 8) {
                if run.state == "done" {
                    Label("exit \(run.exit) · \(run.reply.isEmpty ? "result sent" : "goes with your answers")",
                          systemImage: run.exit == 0 ? "checkmark.circle.fill" : "xmark.octagon.fill")
                        .font(.caption).foregroundStyle(run.exit == 0 ? .green : .red)
                } else {
                    Button { confirmRun = i } label: {
                        if run.state == "running" || starting == i {
                            HStack(spacing: 6) { ProgressView().controlSize(.mini); Text("Running…") }
                        } else {
                            Label("Run", systemImage: "play.fill")
                        }
                    }
                    .buttonStyle(.borderedProminent).controlSize(.small)
                    .buttonBorderShape(.roundedRectangle(radius: 8))
                    .disabled(run.state == "running" || starting != nil)
                    if run.interrupted { Text("Interrupted by a restart").font(.caption).foregroundStyle(.orange) }
                }
                Spacer(minLength: 0)
            }
            if let runError { Text(runError).font(.caption).foregroundStyle(.red) }
        }
    }

    /// chat_run_ask: Go runs the stored command in the chat's folder and either
    /// sends the output as the reply or keeps it on the card (state comes back
    /// through chats-changed).
    private func run(_ i: Int) async {
        guard let client = model.client else { runError = BurrowError.closed.localizedDescription; return }
        starting = i; runError = nil
        defer { starting = nil }
        do { try await client.call("chat_run_ask", ["chatId": chatId, "index": i]) }
        catch { runError = error.localizedDescription }
    }

    private func option(_ i: Int, _ ask: TurnAsk, _ j: Int, _ opt: String) -> some View {
        let picked = answers[i, default: []].contains(opt)
        return Button {
            var s = answers[i, default: []]
            if ask.multi { if picked { s.remove(opt) } else { s.insert(opt) } } else { s = picked ? [] : [opt] }
            answers[i] = s
            if !s.isEmpty { otherOpen.remove(i) }
        } label: {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: picked ? (ask.multi ? "checkmark.square.fill" : "largecircle.fill.circle") : (ask.multi ? "square" : "circle"))
                    .foregroundStyle(picked ? Color.accentColor : .secondary)
                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text(opt).font(.subheadline)
                        if ask.suggested == j { Text("suggested").font(.caption2).foregroundStyle(.secondary) }
                    }
                    if j < ask.details.count, !ask.details[j].isEmpty {
                        Text(ask.details[j]).font(.caption).foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 0)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// The desktop card's Something else: a free-text answer instead of the
    /// options (an ask with no options gets the field straight away).
    @ViewBuilder private func somethingElse(_ i: Int, _ ask: TurnAsk) -> some View {
        if ask.options.isEmpty && ask.run == nil || otherOpen.contains(i) {
            TextField("Your answer", text: Binding(get: { other[i] ?? "" }, set: { other[i] = $0 }), axis: .vertical)
                .textFieldStyle(.roundedBorder)
                .font(.subheadline)
                .lineLimit(1...4)
        } else if !ask.options.isEmpty {
            Button {
                otherOpen.insert(i)
                answers[i] = []
            } label: {
                Label("Something else", systemImage: "pencil").font(.subheadline)
            }
            .buttonStyle(.plain)
            .foregroundStyle(.secondary)
        }
    }

    /// Per ask: the typed answer, else the picked options (Go joins them, in card order).
    private var picks: [TurnPickItem] {
        needs.asks.indices.compactMap { i in
            let typed = (other[i] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            if (otherOpen.contains(i) || needs.asks[i].options.isEmpty), !typed.isEmpty { return TurnPickItem(index: i, text: typed) }
            let picked = needs.asks[i].options.filter { answers[i, default: []].contains($0) }
            return picked.isEmpty ? nil : TurnPickItem(index: i, options: picked)
        }
    }

    /// chat_answer_turn: Go composes the message from the stored card (kept run
    /// replies included) and submits it; the composer's draft text and images go
    /// with it. Echo + offline hold like a composer send (AppModel.answerTurn).
    private func answer(_ picks: [TurnPickItem]) async {
        sending = true; sendError = nil
        defer { sending = false }
        let extra = Drafts.text[chatId] ?? ""
        let images = (Drafts.images[chatId] ?? []).map(\.dataURI)
        do {
            _ = try await model.answerTurn(chatId, picks, extra: extra, images: images)
            Drafts.text[chatId] = nil; Drafts.images[chatId] = nil
            NotificationCenter.default.post(name: Drafts.taken, object: chatId)
        } catch { sendError = error.localizedDescription }
    }
}
