import SwiftUI

@main
struct BurrowApp: App {
    @UIApplicationDelegateAdaptor private var push: PushDelegate
    @State private var model = AppModel()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            Group {
                if model.creds == nil { PairView() } else { ThreadsView() }
            }
            .environment(model)
            // iOS 26 draws bordered buttons as capsules; keep rounded rects app-wide.
            .buttonBorderShape(.roundedRectangle(radius: 10))
            .onAppear { push.model = model }
            .onChange(of: scenePhase) { _, p in if p == .active { model.foreground() } }
            .onOpenURL { url in
                // burrow://chat/<id> from the widget / Live Activity
                if url.host == "chat", let id = Int(url.lastPathComponent) { model.openChat = id }
            }
        }
    }
}

struct PairView: View {
    @Environment(AppModel.self) private var model
    @State private var address = ""
    @State private var code = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    VStack(spacing: 10) {
                        Image("Logo").resizable().frame(width: 88, height: 88)
                            .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                            .shadow(color: .black.opacity(0.15), radius: 8, y: 4)
                        Text("Burrow").font(.title2.bold())
                        Text("Your Mac's threads, on this iPhone.").font(.subheadline).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity).padding(.vertical, 8)
                    .listRowBackground(Color.clear)
                }
                Section {
                    TextField("https://mac.tailnet.ts.net:8443", text: $address)
                        .keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                        .onChange(of: address) { _, v in
                            if let p = parsePairInput(v), let c = p.code { address = p.base; code = c }
                        }
                    TextField("6-digit code", text: $code).keyboardType(.numberPad)
                } header: {
                    Text("Mac")
                } footer: {
                    Text("Burrow on the Mac → Settings → Remote access. Paste the pairing link or type the address and code.")
                }
                if let e = error ?? model.error { Text(e).foregroundStyle(.red) }
                Section {
                    Button { Task { await pair() } } label: {
                        HStack { Spacer(); if busy { ProgressView() } else { Text("Pair").bold() }; Spacer() }
                    }
                    .disabled(busy || address.isEmpty || code.count < 6)
                }
            }
            .navigationBarTitleDisplayMode(.inline)
        }
    }

    private func pair() async {
        guard let p = parsePairInput(address) else { error = BurrowError.badURL.localizedDescription; return }
        busy = true; defer { busy = false }
        do {
            let c = try await BurrowClient.pair(base: p.base, code: code.trimmingCharacters(in: .whitespaces), name: UIDevice.current.name)
            error = nil
            model.error = nil
            model.paired(c)
        } catch { self.error = error.localizedDescription }
    }
}
