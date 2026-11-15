import XCTest
@testable import Burrow

private func row(_ id: Int, _ role: String = "assistant", _ text: String = "", partial: Bool = false) -> [String: Any] {
    var d: [String: Any] = ["id": id, "role": role, "text": text]
    if partial { d["partial"] = true }
    return d
}

private func splice(_ base: Int64, _ rev: Int64, from: Int, _ rows: [[String: Any]], auth: Bool = false) -> [String: Any] {
    var d: [String: Any] = ["baseRev": base, "rev": rev, "fromIndex": from, "messages": rows]
    if auth { d["authoritative"] = true }
    return d
}

private func tail(_ rev: Int64, index: Int, offset: Int, _ append: String) -> [String: Any] {
    ["rev": rev, "index": index, "offset": offset, "append": append, "kind": "text"]
}

private func window(start: Int = 0, rev: Int64 = 10, _ texts: [String]) -> TranscriptWindow {
    TranscriptWindow(page: ["start": start, "rev": rev,
                            "messages": texts.enumerated().map { row(start + $0, "assistant", $1) }])
}

final class TranscriptWindowTests: XCTestCase {
    func testPageSetsWindow() {
        let w = window(start: 5, rev: 7, ["a", "b"])
        XCTAssertEqual(w.start, 5)
        XCTAssertEqual(w.rev, 7)
        XCTAssertEqual(w.rows.map(\.text), ["a", "b"])
    }

    func testSpliceChain() {
        var w = window(["a", "b"])
        XCTAssertEqual(w.apply(splice: splice(10, 11, from: 1, [row(1, "assistant", "b2"), row(2, "tool", "Bash")])), .applied)
        XCTAssertEqual(w.apply(splice: splice(11, 12, from: 3, [row(3, "assistant", "c")])), .applied)
        XCTAssertEqual(w.rows.map(\.text), ["a", "b2", "Bash", "c"])
        XCTAssertEqual(w.rev, 12)
    }

    func testSpliceGapLeavesWindowAlone() {
        var w = window(["a", "b"])
        let before = w
        XCTAssertEqual(w.apply(splice: splice(11, 12, from: 1, [row(1, "assistant", "x")])), .gap)
        XCTAssertEqual(w, before)
        // A chained splice past the window's end is a gap too.
        XCTAssertEqual(w.apply(splice: splice(10, 11, from: 5, [row(5)])), .gap)
        XCTAssertEqual(w, before)
    }

    func testSpliceReachingBackPastWindowKeepsInsidePart() {
        var w = window(start: 2, ["c", "d"])
        XCTAssertEqual(w.apply(splice: splice(10, 11, from: 1, [row(1, "assistant", "b"), row(2, "assistant", "C"), row(3, "assistant", "D")])), .applied)
        XCTAssertEqual(w.start, 2)
        XCTAssertEqual(w.rows.map(\.text), ["C", "D"])
    }

    func testAuthoritativeRewindAndClear() {
        var w = window(["a", "b", "c", "d"])
        // Rewind: cut at 2, no messages, rev doesn't chain.
        XCTAssertEqual(w.apply(splice: splice(99, 20, from: 2, [], auth: true)), .applied)
        XCTAssertEqual(w.rows.map(\.text), ["a", "b"])
        XCTAssertEqual(w.rev, 20)
        // Clear.
        XCTAssertEqual(w.apply(splice: splice(5, 21, from: 0, [], auth: true)), .applied)
        XCTAssertEqual(w.rows, [])
        XCTAssertEqual(w.start, 0)
    }

    func testAuthoritativeRewindBeforeWindowMovesStart() {
        var w = window(start: 50, ["x", "y"])
        XCTAssertEqual(w.apply(splice: splice(0, 30, from: 20, [], auth: true)), .applied)
        XCTAssertEqual(w.start, 20)
        XCTAssertEqual(w.rows, [])
    }

    func testAuthoritativePastGapJumpsWindow() {
        var w = window(start: 0, ["a"])
        XCTAssertEqual(w.apply(splice: splice(0, 40, from: 100, [row(100, "assistant", "new")], auth: true)), .applied)
        XCTAssertEqual(w.start, 100)
        XCTAssertEqual(w.rows.map(\.text), ["new"])
    }

    func testTailAppendsAndIsIdempotent() {
        var w = window(["hel"])
        let t = tail(10, index: 0, offset: 3, "lo")
        XCTAssertEqual(w.apply(tail: t), .applied)
        XCTAssertEqual(w.apply(tail: t), .applied) // re-applied: same result
        XCTAssertEqual(w.rows[0].text, "hello")
        XCTAssertEqual(w.rev, 10) // never bumps rev
    }

    func testStaleTailIgnoredFutureTailIsGap() {
        var w = window(["abc"])
        XCTAssertEqual(w.apply(tail: tail(9, index: 0, offset: 0, "zzz")), .ignored)
        XCTAssertEqual(w.apply(tail: tail(11, index: 0, offset: 3, "d")), .gap)
        XCTAssertEqual(w.rows[0].text, "abc")
    }

    func testTailPastTextIsGapOutsideWindowIgnored() {
        var w = window(start: 4, ["ab"])
        XCTAssertEqual(w.apply(tail: tail(10, index: 4, offset: 5, "x")), .gap) // a tail went missing
        XCTAssertEqual(w.apply(tail: tail(10, index: 9, offset: 0, "x")), .ignored)
        XCTAssertEqual(w.apply(tail: tail(10, index: 3, offset: 0, "x")), .ignored)
        XCTAssertEqual(w.rows[0].text, "ab")
    }

    func testTailOffsetsAreUTF16() {
        // "é" is 1 UTF-16 unit, "😀" is 2; Swift counts each as one Character.
        var w = window(["é😀"])
        XCTAssertEqual(w.apply(tail: tail(10, index: 0, offset: 3, " ok")), .applied)
        XCTAssertEqual(w.rows[0].text, "é😀 ok")
        // Re-sent from an earlier offset: replaces after the emoji, not inside it.
        XCTAssertEqual(w.apply(tail: tail(10, index: 0, offset: 1, "😀!")), .applied)
        XCTAssertEqual(w.rows[0].text, "é😀!")
        // Combining sequence: e + U+0301 is 2 units, 1 Character.
        var c = window(["e\u{301}"])
        XCTAssertEqual(c.apply(tail: tail(10, index: 0, offset: 2, "x")), .applied)
        XCTAssertEqual(c.rows[0].text, "e\u{301}x")
    }

    func testSubscribeRingSplices() {
        var w = window(["a"])
        let reply: [String: Any] = ["rev": Int64(12), "inProgress": NSNull(), "splices": [
            splice(10, 11, from: 1, [row(1, "user", "hi")]),
            splice(11, 12, from: 2, [row(2, "assistant", "yo", partial: true)]),
        ]]
        XCTAssertEqual(w.apply(subscribe: reply), .applied)
        XCTAssertEqual(w.rows.map(\.text), ["a", "hi", "yo"])
        XCTAssertEqual(w.rev, 12)
    }

    func testSubscribeSkipsSplicesAlreadyAppliedLive() {
        var w = window(["a"])
        _ = w.apply(splice: splice(10, 11, from: 1, [row(1, "assistant", "b")]))
        let reply: [String: Any] = ["rev": Int64(11), "splices": [splice(10, 11, from: 1, [row(1, "assistant", "b")])]]
        XCTAssertEqual(w.apply(subscribe: reply), .applied)
        XCTAssertEqual(w.rows.map(\.text), ["a", "b"])
    }

    func testSubscribeAuthoritativeWithInProgress() {
        var w = window(["old"])
        let reply: [String: Any] = ["rev": Int64(30),
                                    "inProgress": ["index": 41, "text": "streamed so far"],
                                    "splices": [splice(10, 30, from: 40, [row(40, "user", "q"), row(41, "assistant", "str", partial: true)], auth: true)]]
        XCTAssertEqual(w.apply(subscribe: reply), .applied)
        XCTAssertEqual(w.start, 40)
        XCTAssertEqual(w.rows.map(\.text), ["q", "streamed so far"])
        XCTAssertEqual(w.rev, 30)
    }

    func testSubscribeInProgressDoesNotRewindNewerTail() {
        var w = window(["hello wor"])
        let reply: [String: Any] = ["rev": Int64(10), "splices": [], "inProgress": ["index": 0, "text": "hello"]]
        XCTAssertEqual(w.apply(subscribe: reply), .applied)
        XCTAssertEqual(w.rows[0].text, "hello wor")
    }

    func testSubscribeAheadOfUsIsGap() {
        var w = window(["a"])
        XCTAssertEqual(w.apply(subscribe: ["rev": Int64(15), "splices": []]), .gap)
    }

    func testCachePageRoundTrips() {
        var w = window(start: 3, rev: 8, ["a"])
        w.rows[0].clientId = "c1"
        let back = TranscriptWindow(page: w.page)
        XCTAssertEqual(back, w)
    }
}
