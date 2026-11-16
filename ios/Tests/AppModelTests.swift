import XCTest
@testable import Burrow

final class AppModelTests: XCTestCase {
    func testLatestWinsDropsOlderAnswers() {
        var r = LatestWins()
        let a = r.begin(), b = r.begin()
        XCTAssertTrue(r.accept(b))   // the later read answers first
        XCTAssertFalse(r.accept(a))  // the earlier one must not overwrite it
        let c = r.begin()
        XCTAssertTrue(r.accept(c))
    }

    func testLivePhaseSurvivesSnapshot() {
        var running = Phase(); running.state = "running"
        var done = Phase(); done.state = "done"
        let merged = mergePhases(snapshot: [1: running, 2: running], live: [1: done])
        XCTAssertEqual(merged[1]?.state, "done") // event after the snapshot was cut
        XCTAssertEqual(merged[2]?.state, "running")
    }
}
