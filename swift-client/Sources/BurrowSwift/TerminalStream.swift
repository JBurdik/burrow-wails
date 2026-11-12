import Foundation

// Exactly-once PTY output for one terminal, across a dropped /v2/ws — a port of src/runtime/ptyStream.ts.
// Foundation only, so it compiles standalone for its self-check (see TerminalStream.check()).

/// The pty-data-<id> / pty_since payload. `off` is nil from a daemon that predates offsets.
struct PtyChunk {
    var off: Int64?
    var data: [UInt8]

    init(off: Int64?, data: [UInt8]) { self.off = off; self.data = data }

    /// `{off, data: [int]}` as it comes off the wire.
    init?(_ payload: Any?) {
        guard let p = payload as? [String: Any] else { return nil }
        off = (p["off"] as? NSNumber)?.int64Value
        data = (p["data"] as? [Any] ?? []).map { UInt8(truncatingIfNeeded: ($0 as? NSNumber)?.intValue ?? 0) }
    }
}

@MainActor
final class PtyStream {
    /// Offset of the next byte to render; nil = no position yet (the first chunk sets it).
    private(set) var next: Int64?
    private var catching = false
    private var held: [PtyChunk] = []

    let write: ([UInt8]) -> Void
    let fetchSince: (Int64) async throws -> PtyChunk
    /// The daemon's ring no longer held `bytes` of the gap — they are gone.
    var onLost: ((Int64) -> Void)?

    init(write: @escaping ([UInt8]) -> Void, fetchSince: @escaping (Int64) async throws -> PtyChunk) {
        self.write = write
        self.fetchSince = fetchSince
    }

    private func apply(_ c: PtyChunk) {
        guard let off = c.off else { write(c.data); return } // old daemon: nothing to dedupe on
        if next == nil { next = off }
        guard let n = next else { return }
        if off > n {
            // A hole: hold this chunk behind a catch-up.
            held.append(c)
            Task { await catchUp() }
            return
        }
        let skip = Int(n - off)
        if skip >= c.data.count { return } // already rendered
        write(skip > 0 ? Array(c.data[skip...]) : c.data)
        next = off + Int64(c.data.count)
    }

    func push(_ c: PtyChunk) {
        if catching { held.append(c) } else { apply(c) }
    }

    /// Fetch whatever was missed since the last rendered byte (call on reconnect).
    func catchUp() async {
        guard !catching, let from = next else { return }
        catching = true
        do {
            let got = try await fetchSince(from)
            if let off = got.off, let n = next {
                if off > n {
                    onLost?(off - n)
                    next = off
                } else if off < n, got.data.isEmpty {
                    next = off // the PTY behind this id is behind us: a new one
                }
                apply(got)
            }
        } catch {
            next = nil // no catch-up possible: accept the hole, restart from the next chunk
        }
        drain()
    }

    /// Start from a snapshot (pty_attach) instead of mid-stream; live chunks are held meanwhile.
    /// A nil / failed fetch changes nothing. Returns whether a snapshot was painted.
    @discardableResult
    func attach(fetch: () async throws -> (off: Int64, paint: () -> Void)?) async -> Bool {
        guard !catching else { return false }
        catching = true
        var painted = false
        if let snap = try? await fetch() {
            snap.paint()
            next = snap.off
            painted = true
        }
        drain()
        return painted
    }

    /// Forget the position: the PTY behind this id exited / was replaced.
    func reset() {
        next = nil
        held = []
    }

    private func drain() {
        catching = false
        let h = held
        held = []
        for c in h { push(c) }
    }
}

extension PtyStream {
    /// assert-based check of the offset logic; `swiftc TerminalStream.swift <main calling it>` runs it standalone.
    static func check() async {
        var out: [UInt8] = []
        var ring: [UInt8] = Array(0..<100)
        let s = PtyStream(write: { out += $0 }, fetchSince: { from in
            PtyChunk(off: from, data: Array(ring[Int(from)...]))
        })
        s.push(PtyChunk(off: 0, data: [0, 1, 2]))
        s.push(PtyChunk(off: 1, data: [1, 2, 3])) // overlap trimmed
        s.push(PtyChunk(off: 0, data: [0, 1])) // fully seen: dropped
        assert(out == [0, 1, 2, 3], "dedupe \(out)")
        ring = Array(0..<10)
        s.push(PtyChunk(off: 8, data: [8, 9])) // hole 4..<8 → catch-up fills it
        try? await Task.sleep(nanoseconds: 50_000_000)
        assert(out == Array(0..<10), "gap fill \(out)")
        assert(s.next == 10)

        // A gap past the ring: onLost reports the bytes gone, stream continues from the ring start.
        var lost: Int64 = 0
        var out2: [UInt8] = []
        let s2 = PtyStream(write: { out2 += $0 }, fetchSince: { _ in PtyChunk(off: 50, data: [50, 51]) })
        s2.onLost = { lost = $0 }
        s2.push(PtyChunk(off: 10, data: [10]))
        await s2.catchUp()
        assert(lost == 39 && out2 == [10, 50, 51] && s2.next == 52, "lost \(lost) \(out2)")

        // attach paints, then continues exactly at its offset; chunks held during fetch are trimmed.
        var out3: [UInt8] = []
        var painted = false
        let s3 = PtyStream(write: { out3 += $0 }, fetchSince: { _ in PtyChunk(off: nil, data: []) })
        let ok = await s3.attach(fetch: {
            s3.push(PtyChunk(off: 18, data: [18, 19, 20, 21])) // arrives mid-fetch → held
            return (off: 20, paint: { painted = true })
        })
        assert(ok && painted && out3 == [20, 21] && s3.next == 22, "attach \(out3)")
        // reset: a new PTY under the same id numbers from 0 again.
        s3.reset()
        s3.push(PtyChunk(off: 0, data: [7]))
        assert(out3 == [20, 21, 7])
        // wire parsing
        let c = PtyChunk(["off": NSNumber(value: 5), "data": [NSNumber(value: 104), NSNumber(value: 105)]])
        assert(c?.off == 5 && c?.data == [104, 105])
        assert(PtyChunk(["off": NSNull(), "data": []])?.off == nil)
        print("PtyStream ok")
    }
}
