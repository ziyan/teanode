import ActivityKit
import AppIntents
import Foundation

/// CallActivityAttributes are a call with the agent on the lock screen and
/// in the Dynamic Island: whose agent, when the call began, and whether the
/// microphone is muted. The app updates it itself; nothing is pushed.
struct CallActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var isMuted: Bool
    }

    var agentTitle: String
    var startedAt: Date
}

/// EndCallIntent hangs up the call from its Live Activity.
struct EndCallIntent: LiveActivityIntent {
    static var title: LocalizedStringResource { "Hang up" }
    static var isDiscoverable: Bool { false }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if TEANODE_APP
            AppModel.shared.currentCall?.hangUp()
        #endif
        return .result()
    }
}

/// ToggleCallMuteIntent mutes or unmutes the microphone from the call's
/// Live Activity.
struct ToggleCallMuteIntent: LiveActivityIntent {
    static var title: LocalizedStringResource { "Mute or unmute" }
    static var isDiscoverable: Bool { false }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if TEANODE_APP
            if let call = AppModel.shared.currentCall {
                call.isMuted.toggle()
            }
        #endif
        return .result()
    }
}

/// CallActivities changes a call's Live Activity, found by its id: the
/// activity is not handed from one task to another.
enum CallActivities {
    static func update(_ activityId: String, isMuted: Bool) async {
        for activity in Activity<CallActivityAttributes>.activities where activity.id == activityId {
            await activity.update(ActivityContent(state: CallActivityAttributes.ContentState(isMuted: isMuted), staleDate: nil))
        }
    }

    static func end(_ activityId: String) async {
        for activity in Activity<CallActivityAttributes>.activities where activity.id == activityId {
            await activity.end(nil, dismissalPolicy: .immediate)
        }
    }
}
