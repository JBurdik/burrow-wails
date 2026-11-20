import ActivityKit
import Foundation

/// Live Activity for one thread's turn (lock screen + Dynamic Island).
struct ChatActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        /// agentphase state: running | waiting_input | waiting_approval | done | failed | idle | stale
        var state: String
        var detail: String
        var startedAt: Date
    }
    var chatId: Int
    var title: String
    var project: String
}

extension ChatActivityAttributes.ContentState {
    var label: String {
        switch state {
        case "running": "Working"
        case "waiting_approval": "Needs approval"
        case "waiting_input": "Needs you"
        case "done": "Done"
        case "failed": "Failed"
        default: "Idle"
        }
    }
    var symbol: String {
        switch state {
        case "running": "circle.dotted"
        case "waiting_approval", "waiting_input": "hand.raised.fill"
        case "done": "checkmark.circle.fill"
        case "failed": "xmark.octagon.fill"
        default: "moon.zzz"
        }
    }
    var inFlight: Bool { ["running", "waiting_input", "waiting_approval"].contains(state) }
}
