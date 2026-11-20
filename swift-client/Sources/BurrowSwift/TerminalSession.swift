import AppKit
import CGhosttyVT

/// One PTY's emulator + byte stream, owned by TerminalStore so it keeps rendering (and keeps its
/// scrollback) while no view shows it. The view only draws `term` and forwards input.
@MainActor
final class TerminalSession {
    let id: String
    private(set) var vt: OpaquePointer!
    private(set) var cols: UInt16 = 80
    private(set) var rows: UInt16 = 24
    weak var view: TerminalNSView?
    private weak var b: Burrow?
    private(set) var stream: PtyStream!
    private var started = false
    private var attachedInAlt = false
    /// Last time the PTY produced output — "prompt is ready" for an initial command.
    private(set) var lastDataAt: Date?
    private var resizeTask: Task<Void, Never>?
    /// create_pty waits for the view's first layout so the PTY is born at the right size.
    private var pendingStart: (cwd: String, cmd: String?)?

    func startWhenSized(cwd: String, initialCmd: String? = nil) {
        guard !started else { return }
        if view == nil || pendingStart != nil { pendingStart = (cwd, initialCmd); return }
        Task { await start(cwd: cwd, initialCmd: initialCmd) }
    }

    init?(id: String, burrow: Burrow) {
        self.id = id
        b = burrow
        let me = Unmanaged.passUnretained(self).toOpaque()
        // DA / DSR replies the emulator owes the PTY. ponytail: a desktop xterm showing the same PTY answers too.
        guard let t = bvt_new(80, 24, 10_000, { ud, data, len in
            guard let ud, let data else { return }
            let bytes = Array(UnsafeBufferPointer(start: data, count: len))
            let s = Unmanaged<TerminalSession>.fromOpaque(ud).takeUnretainedValue()
            MainActor.assumeIsolated { s.send(bytes) }
        }, me) else { return nil }
        vt = t
        bvt_set_colors(t, 0xECE6E9, 0x1E161B, 0xA895F5) // Theme.text / Theme.terminal / Theme.accent
        stream = PtyStream(
            write: { [weak self] bytes in self?.render(bytes) },
            fetchSince: { [weak self] since in
                guard let self, let b = self.b else { throw Burrow.CallError(message: "gone") }
                return PtyChunk(try await b.call("pty_since", ["id": self.id, "since": since])) ?? PtyChunk(off: nil, data: [])
            })
        stream.onLost = { [weak self] _ in Task { await self?.attach() } }
    }

    deinit { if let vt { bvt_free(vt) } }

    /// create_pty (spawn or reattach) then paint from the daemon's screen if it has one.
    func start(cwd: String, initialCmd: String? = nil) async {
        guard !started, let b else { return }
        started = true
        do {
            try await b.call("create_pty", ["id": id, "cwd": cwd, "cols": Int(cols), "rows": Int(rows)])
        } catch {
            do { try await b.call("create_pty", ["id": id, "cwd": cwd, "cols": Int(cols), "rows": Int(rows)]) }
            catch { render(Array("\r\n\u{1b}[31mburrow: could not start this terminal — \(error.localizedDescription)\u{1b}[0m\r\n".utf8)) }
        }
        let painted = await attach()
        // A core without pty_attach: replay what the daemon's byte ring still holds instead of a blank view.
        if !painted, stream.next == nil,
           let got = PtyChunk(try? await b.call("pty_since", ["id": id, "since": 0])) {
            stream.push(got)
        }
        if let cmd = initialCmd, !cmd.isEmpty { await sendWhenQuiet(cmd + "\n") }
    }

    /// Wait for the shell to print its prompt and fall quiet (XTerm.vue's trySend), max 5 s.
    private func sendWhenQuiet(_ text: String) async {
        let start = Date()
        while true {
            if let last = lastDataAt, Date().timeIntervalSince(last) >= 0.25 { break }
            if Date().timeIntervalSince(start) >= 5 { break }
            try? await Task.sleep(for: .milliseconds(100))
        }
        send(Array(text.utf8))
    }

    @discardableResult
    func attach() async -> Bool {
        guard let b else { return false }
        return await stream.attach {
            guard let snap = try await b.call("pty_attach", ["id": self.id]) as? JSON,
                  let vt = Data(base64Encoded: snap.s("vt")) else { return nil }
            return (off: snap.i("off"), paint: {
                let c = UInt16(clamping: snap.i("cols")), r = UInt16(clamping: snap.i("rows"))
                bvt_reset(self.vt)
                // Bytes are laid out for the PTY's grid: write at that size, then reflow to ours.
                if c > 0, r > 0 { bvt_resize(self.vt, c, r, 0, 0) }
                vt.withUnsafeBytes { bvt_write(self.vt, $0.bindMemory(to: UInt8.self).baseAddress, vt.count) }
                bvt_resize(self.vt, self.cols, self.rows, 0, 0)
                bvt_scroll_bottom(self.vt)
                self.attachedInAlt = snap["alt"] as? Bool ?? false
                self.view?.needsDisplay = true
            })
        }
    }

    /// OSC 0/2 title the program set (read by the foreground poll for auto-titles).
    var oscTitle: String {
        var buf = [CChar](repeating: 0, count: 256)
        let n = bvt_title(vt, &buf, buf.count)
        return String(decoding: buf.prefix(n).map { UInt8(bitPattern: $0) }, as: UTF8.self)
    }

    /// ⌘K (XTerm.vue term.clear()): drop the scrollback and everything above the cursor line, locally only —
    /// the program isn't told, exactly like xterm.js. A full-screen TUI's screen is left alone.
    func clear() {
        var f = BvtFrame()
        _ = bvt_snapshot(vt, nil, 0, &f)
        guard !f.alt_screen else { return }
        let y = Int(f.cursor_y), x = Int(f.cursor_x)
        let seq = (y > 0 ? "\u{1b}[\(y)S" : "") + "\u{1b}[1;\(x + 1)H\u{1b}[3J"
        bvt_scroll_bottom(vt)
        seq.utf8CString.withUnsafeBufferPointer { p in
            p.baseAddress!.withMemoryRebound(to: UInt8.self, capacity: p.count) { bvt_write(vt, $0, p.count - 1) }
        }
        view?.needsDisplay = true
    }

    /// ⌘⇧R: repaint from the daemon's own screen (un-scrambles a garbled TUI).
    func repaint() { Task { await attach() } }

    func onResync() {
        guard attachedInAlt else { return }
        attachedInAlt = false
        Task { await attach() }
    }

    private func render(_ bytes: [UInt8]) {
        bytes.withUnsafeBufferPointer { bvt_write(vt, $0.baseAddress, $0.count) }
        lastDataAt = Date()
        view?.dataArrived()
    }

    func send(_ bytes: [UInt8]) {
        guard !bytes.isEmpty, let b else { return }
        Task { try? await b.call("write_pty", ["id": id, "data": bytes.map(Int.init)]) }
    }

    func send(_ text: String) { send(Array(text.utf8)) }

    /// The view's grid changed: reflow locally now, tell the PTY once the drag settles.
    func resize(cols c: UInt16, rows r: UInt16, cellW: UInt32, cellH: UInt32) {
        if let p = pendingStart {
            pendingStart = nil
            cols = c; rows = r
            bvt_resize(vt, c, r, cellW, cellH)
            Task { await start(cwd: p.cwd, initialCmd: p.cmd) }
            return
        }
        guard c != cols || r != rows else { return }
        cols = c; rows = r
        bvt_resize(vt, c, r, cellW, cellH)
        resizeTask?.cancel()
        resizeTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(120))
            guard let self, !Task.isCancelled, let b = self.b else { return }
            _ = try? await b.call("resize_pty", ["id": self.id, "cols": Int(c), "rows": Int(r)])
        }
    }

    func kill() async {
        _ = try? await b?.call("kill_pty", ["id": id])
    }
}
