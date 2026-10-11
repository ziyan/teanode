import ActivityKit
import TeaNodeKit
import UIKit

/// TurnActivities start a Live Activity when the person leaves the app with
/// a turn running, so the lock screen says when it needs them or is done,
/// and end it when they come back.
@MainActor
enum TurnActivities {
    /// Keeps the app awake long enough to hand the server the activity's
    /// push token after it was left.
    private static var backgroundTask = UIBackgroundTaskIdentifier.invalid

    static func start(runId: String, agentTitle: String, connection: Connection) {
        guard ActivityAuthorizationInfo().areActivitiesEnabled, Activity<AgentTurnAttributes>.activities.isEmpty else { return }
        endBackgroundTask()
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Live Activity") { endBackgroundTask() }
        #if DEBUG
            let isSandbox = true
        #else
            let isSandbox = false
        #endif
        // Made inside the task, which then holds it alone: an activity is
        // not something to hand from one task to another.
        Task {
            defer { endBackgroundTask() }
            let activity: Activity<AgentTurnAttributes>
            do {
                activity = try Activity.request(
                    attributes: AgentTurnAttributes(agentTitle: agentTitle),
                    content: ActivityContent(
                        state: AgentTurnAttributes.ContentState(activityState: "working"), staleDate: Date().addingTimeInterval(30 * 60)),
                    pushType: .token)
            } catch {
                return
            }
            for await token in activity.pushTokenUpdates {
                do {
                    let registration = try await PushRelay(transport: URLSessionTransport()).register(
                        deviceToken: token, isSandbox: isSandbox, pushType: "liveactivity")
                    try await connection.registerLiveActivity(runId: runId, pushRegistration: registration)
                } catch {
                    // Nothing would ever update it: better none than one
                    // that says "working" after the turn is over.
                    await activity.end(nil, dismissalPolicy: .immediate)
                }
                break
            }
        }
    }

    /// Ends every turn's activity: the person is back in the app, where the
    /// turn is on screen.
    static func endAll() {
        Task {
            for activity in Activity<AgentTurnAttributes>.activities {
                await activity.end(nil, dismissalPolicy: .immediate)
            }
        }
    }

    private static func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }
}
