import XCTest
@testable import Burrow

final class ClientTests: XCTestCase {
    func testTransportFailuresReadAsClosed() {
        // A write on a socket that just died must be held + retried by callers
        // (they catch BurrowError.closed), not shown as a refusal.
        guard case BurrowError.closed = wireError(URLError(.networkConnectionLost)) else { return XCTFail() }
        guard case BurrowError.server("no") = wireError(BurrowError.server("no")) else { return XCTFail() }
    }

    func testCallWithoutSocketIsClosed() async {
        let c = BurrowClient(Credentials(baseURL: "https://x.invalid", token: "t"))
        do { _ = try await c.call("x"); XCTFail() } catch BurrowError.closed {} catch { XCTFail("\(error)") }
    }

    func testAuthRequestsTimeOut() {
        XCTAssertEqual(httpRequest(URL(string: "https://x.invalid")!).timeoutInterval, 10)
    }
}
