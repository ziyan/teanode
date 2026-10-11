import ActivityKit
import Foundation

/// AgentTurnAttributes are a turn left running as a Live Activity: whose
/// agent it is, and one word for where the turn is. The server pushes only
/// that word (working, waiting, done or failed), which crosses Apple and the
/// relay readable, so what the turn says never does.
struct AgentTurnAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var activityState: String
    }

    var agentTitle: String
}

/// TurnActivityWords are what a turn's state shows as.
enum TurnActivityWords {
    static func text(_ activityState: String) -> String {
        switch activityState {
        case "waiting": String(localized: "Waiting for you")
        case "done": String(localized: "Done")
        case "failed": String(localized: "Stopped with a problem")
        default: String(localized: "Working on it")
        }
    }

    static func symbol(_ activityState: String) -> String {
        switch activityState {
        case "waiting": "questionmark.bubble"
        case "done": "checkmark.circle"
        case "failed": "exclamationmark.triangle"
        default: "ellipsis.bubble"
        }
    }
}
