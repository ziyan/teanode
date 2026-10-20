import Foundation

public struct RecordingSpeakerDraft: Codable, Hashable, Sendable {
    public let accountKey: String
    public let recordingId: String
    public let segment: RecordingTranscriptSegment
    public let clientCorrectionId: String
    public var expectedSourceGeneration: Int64
    public var confirmedName: String
    public var isWholeGroup: Bool
    public var passageStartMS: Int64
    public var passageEndMS: Int64
    public var hasSubmittedRequest: Bool
    /// True when the person said the speaker is them; the name may then be empty and the server uses the account's own.
    public var isAccountOwner: Bool? = nil
    /// The contact the name was chosen from, which the naming ties the speaker to. Nil for a name that was only typed, and for a
    /// draft kept before names could be tied.
    public var contactAddressBookId: String? = nil
    public var contactId: String? = nil

    public init(accountKey: String, recordingId: String, segment: RecordingTranscriptSegment, expectedSourceGeneration: Int64) {
        self.accountKey = accountKey
        self.recordingId = recordingId
        self.segment = segment
        self.expectedSourceGeneration = expectedSourceGeneration
        clientCorrectionId = UUID().uuidString
        confirmedName = ""
        isWholeGroup = false
        passageStartMS = segment.segmentStartMS
        passageEndMS = segment.segmentEndMS
        hasSubmittedRequest = false
    }
}

@MainActor
public enum RecordingSpeakerDrafts {
    public static func read(accountKey: String, segmentId: String, in defaults: UserDefaults = .standard) -> RecordingSpeakerDraft? {
        guard let encoded = defaults.data(forKey: key(accountKey: accountKey, segmentId: segmentId)),
            let draft = try? JSONDecoder().decode(RecordingSpeakerDraft.self, from: encoded), draft.accountKey == accountKey,
            draft.segment.id == segmentId
        else { return nil }
        return draft
    }
    public static func save(_ draft: RecordingSpeakerDraft, in defaults: UserDefaults = .standard) throws {
        defaults.set(try JSONEncoder().encode(draft), forKey: key(accountKey: draft.accountKey, segmentId: draft.segment.id))
    }
    public static func finish(_ draft: RecordingSpeakerDraft, in defaults: UserDefaults = .standard) {
        if read(accountKey: draft.accountKey, segmentId: draft.segment.id, in: defaults)?.clientCorrectionId == draft.clientCorrectionId {
            defaults.removeObject(forKey: key(accountKey: draft.accountKey, segmentId: draft.segment.id))
        }
    }
    private static func key(accountKey: String, segmentId: String) -> String {
        "recording.speaker-correction." + Data(accountKey.utf8).base64EncodedString() + "." + Data(segmentId.utf8).base64EncodedString()
    }
}
