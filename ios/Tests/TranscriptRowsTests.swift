import XCTest
@testable import Burrow

/// fixtures/transcript-rows.json, shared with src/lib/transcriptRows.test.ts:
/// the desktop and iOS must draw a transcript the same way.
final class TranscriptRowsTests: XCTestCase {
    private func normalize(_ r: TranscriptRow) -> NSDictionary {
        switch r {
        case .message(let m): ["message": m.id]
        case .tools(_, let items): ["tools": items.map(\.id), "label": Transcript.groupLabel(items), "failed": items.contains(where: \.failed)]
        case .worked(let turn, let tools, let folded): ["worked": turn, "tools": tools, "folded": folded]
        case .gap(let m, let label): ["gap": m.id, "label": label]
        }
    }

    func testSharedFixtures() throws {
        let url = try XCTUnwrap(Bundle(for: Self.self).url(forResource: "transcript-rows", withExtension: "json"))
        let cases = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [[String: Any]])
        XCTAssertFalse(cases.isEmpty)
        for c in cases {
            let rows = (c["rows"] as? [[String: Any]] ?? []).enumerated().map { Message($1, index: $0) }
            let override = (c["foldOverride"] as? [String: Bool] ?? [:]).reduce(into: [Int: Bool]()) { $0[Int($1.key)!] = $1.value }
            let got = Transcript.rows(rows, foldOverride: override, settledLast: c["settledLast"] as? Bool ?? false).map(normalize)
            let want = (c["expect"] as? [[String: Any]] ?? []).map { $0 as NSDictionary }
            XCTAssertEqual(got, want, c["name"] as? String ?? "")
        }
    }
}
