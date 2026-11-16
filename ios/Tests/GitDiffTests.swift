import XCTest
@testable import Burrow

final class GitDiffTests: XCTestCase {
    func testSplitsFilesAndCounts() {
        let d = "commit abc\n\ndiff --git a/x.go b/x.go\nindex 1..2\n--- a/x.go\n+++ b/x.go\n@@ -1 +1,2 @@\n-a\n+b\n+c\ndiff --git a/dir/y b/dir/y\nBinary files differ\n"
        let f = DiffParser.parse(d)
        XCTAssertEqual(f.map(\.path), ["", "x.go", "dir/y"])
        XCTAssertEqual(f[1].additions, 2); XCTAssertEqual(f[1].deletions, 1)
        XCTAssertTrue(f[2].isBinary)
    }
    func testEmptyIsNoFiles() { XCTAssertTrue(DiffParser.parse("").isEmpty) }
    func testCheckOutcome() {
        XCTAssertEqual(PullRequest.Check(name: "a", status: "COMPLETED", conclusion: "SUCCESS").outcome, .ok)
        XCTAssertEqual(PullRequest.Check(name: "a", status: "IN_PROGRESS", conclusion: "").outcome, .pending)
        XCTAssertEqual(PullRequest.Check(name: "a", status: "COMPLETED", conclusion: "FAILURE").outcome, .bad)
    }
}
