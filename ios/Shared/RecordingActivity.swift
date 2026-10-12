import ActivityKit
import AppIntents
import Foundation

struct RecordingActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var recordingCaptureState: String
        var elapsedMS: Int64
        var updatedAt: Date
    }
    var localRecordingId: String
}

struct ToggleRecordingPauseIntent: LiveActivityIntent {
    static var title: LocalizedStringResource { "Pause or resume recording" }
    static var isDiscoverable: Bool { false }
    @Parameter(title: "Recording") var localRecordingId: String
    @Parameter(title: "Resume recording") var shouldResume: Bool
    init() { shouldResume = false }
    init(localRecordingId: String, shouldResume: Bool) {
        self.localRecordingId = localRecordingId
        self.shouldResume = shouldResume
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if TEANODE_APP
            let recorder = MeetingRecorder.shared
            if await recorder.restoreForActivity(localRecordingId: localRecordingId) {
                if shouldResume { await recorder.resume(localRecordingId: localRecordingId) } else { await recorder.pause() }
            }
        #endif
        return .result()
    }
}

struct StopRecordingIntent: LiveActivityIntent {
    static var title: LocalizedStringResource { "Stop recording" }
    static var isDiscoverable: Bool { false }
    @Parameter(title: "Recording") var localRecordingId: String
    init() {}
    init(localRecordingId: String) { self.localRecordingId = localRecordingId }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if TEANODE_APP
            let recorder = MeetingRecorder.shared
            if await recorder.restoreForActivity(localRecordingId: localRecordingId) { await recorder.stop(localRecordingId: localRecordingId) }
        #endif
        return .result()
    }
}

enum RecordingActivities {
    static func update(_ activityId: String, captureState: String, elapsedMS: Int64) async {
        for activity in Activity<RecordingActivityAttributes>.activities where activity.id == activityId {
            await activity.update(
                ActivityContent(state: .init(recordingCaptureState: captureState, elapsedMS: elapsedMS, updatedAt: Date()), staleDate: nil))
        }
    }
    static func end(_ activityId: String) async {
        for activity in Activity<RecordingActivityAttributes>.activities where activity.id == activityId {
            await activity.end(nil, dismissalPolicy: .immediate)
        }
    }
}
