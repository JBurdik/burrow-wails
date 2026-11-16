import XCTest
@testable import Burrow

/// Held end_turn answers: delivered via chat_answer_turn under the held id,
/// and outbox files written before the `answer` field still decode.
final class OutboxTests: XCTestCase {
    func testAnswerGoesThroughChatAnswerTurn() {
        let id = UUID()
        let answer = HeldAnswer(picks: [TurnPickItem(index: 0, options: ["A", "B"]), TurnPickItem(index: 1, text: " mine ")], extra: " note ")
        XCTAssertEqual(answer.preview, "A, B\n\nmine\n\nnote")
        let item = OutboxItem(id: id, chatId: 3, text: answer.preview, images: ["data:x"], answer: answer)
        let (cmd, args) = item.call
        XCTAssertEqual(cmd, "chat_answer_turn")
        XCTAssertEqual(args["clientId"] as? String, id.uuidString)
        XCTAssertEqual(args["extra"] as? String, " note ")
        XCTAssertEqual((args["picks"] as? [[String: Any]])?.count, 2)
        XCTAssertEqual(OutboxItem(id: id, chatId: 3, text: "hi", images: []).call.0, "chat_submit")
    }

    func testOldOutboxDecodes() throws {
        let old = #"[{"id":"\#(UUID().uuidString)","chatId":1,"text":"hi","images":[]}]"#
        let items = try JSONDecoder().decode([OutboxItem].self, from: Data(old.utf8))
        XCTAssertNil(items[0].answer)
    }
}
