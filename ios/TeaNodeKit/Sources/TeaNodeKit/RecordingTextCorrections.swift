import Foundation

public struct RecordingTranscriptTextCorrection: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let recordingId: String
    public let recordingMediaId: String
    public let recordingTranscriptRunId: String
    public let recordingTranscriptSegmentId: String
    public let clientCorrectionId: String
    public let passageStartMS: Int64
    public let passageEndMS: Int64
    public let originalTranscriptText: String
    public let correctedTranscriptText: String
    public let sourceGeneration: Int64
    public let createdAt: String
    public let isRemoved: Bool
}

/// RecordingTextCorrectionDraft retains the exact request through an unknown mutation outcome.
public struct RecordingTextCorrectionDraft: Codable, Hashable, Sendable {
    public let accountKey: String
    public let recordingId: String
    public let recordingTranscriptSegmentId: String
    public let clientCorrectionId: String
    public var correctedTranscriptText: String
    public var expectedSourceGeneration: Int64
    public var hasSubmittedRequest: Bool

    public init(
        accountKey: String, recordingId: String, recordingTranscriptSegmentId: String, correctedTranscriptText: String,
        expectedSourceGeneration: Int64
    ) {
        self.accountKey = accountKey
        self.recordingId = recordingId
        self.recordingTranscriptSegmentId = recordingTranscriptSegmentId
        clientCorrectionId = UUID().uuidString
        self.correctedTranscriptText = correctedTranscriptText
        self.expectedSourceGeneration = expectedSourceGeneration
        hasSubmittedRequest = false
    }
}

@MainActor
public enum RecordingTextCorrectionDrafts {
    public static func read(accountKey: String, segmentId: String, in defaults: UserDefaults = .standard) -> RecordingTextCorrectionDraft? {
        guard let encoded = defaults.data(forKey: key(accountKey: accountKey, segmentId: segmentId)),
            let draft = try? JSONDecoder().decode(RecordingTextCorrectionDraft.self, from: encoded), draft.accountKey == accountKey,
            draft.recordingTranscriptSegmentId == segmentId
        else { return nil }
        return draft
    }
    public static func save(_ draft: RecordingTextCorrectionDraft, in defaults: UserDefaults = .standard) throws {
        defaults.set(try JSONEncoder().encode(draft), forKey: key(accountKey: draft.accountKey, segmentId: draft.recordingTranscriptSegmentId))
    }
    public static func finish(_ draft: RecordingTextCorrectionDraft, in defaults: UserDefaults = .standard) {
        let storageKey = key(accountKey: draft.accountKey, segmentId: draft.recordingTranscriptSegmentId)
        if read(accountKey: draft.accountKey, segmentId: draft.recordingTranscriptSegmentId, in: defaults)?.clientCorrectionId
            == draft.clientCorrectionId
        {
            defaults.removeObject(forKey: storageKey)
        }
    }
    private static func key(accountKey: String, segmentId: String) -> String {
        "recording.text-correction." + Data(accountKey.utf8).base64EncodedString() + "." + Data(segmentId.utf8).base64EncodedString()
    }
}

extension Documents {
    static let recordingTextCorrectionFields =
        "id recordingId recordingMediaId recordingTranscriptRunId recordingTranscriptSegmentId clientCorrectionId passageStartMS passageEndMS originalTranscriptText correctedTranscriptText sourceGeneration createdAt isRemoved"
    public static let readRecordingTranscriptTextCorrections =
        "query ($recordingId: String!) { ReadRecordingTranscriptTextCorrections(recordingId: $recordingId) { \(recordingTextCorrectionFields) } }"
    public static let correctRecordingTranscriptText =
        "mutation ($recordingId: String!, $recordingTranscriptSegmentId: String!, $expectedSourceGeneration: Int64!, $clientCorrectionId: String!, $correctedTranscriptText: String!) { CorrectRecordingTranscriptText(recordingId: $recordingId, recordingTranscriptSegmentId: $recordingTranscriptSegmentId, expectedSourceGeneration: $expectedSourceGeneration, clientCorrectionId: $clientCorrectionId, correctedTranscriptText: $correctedTranscriptText) { \(recordingTextCorrectionFields) } }"
    public static let removeRecordingTranscriptTextCorrection =
        "mutation ($recordingId: String!, $recordingTranscriptTextCorrectionId: String!, $expectedSourceGeneration: Int64!) { RemoveRecordingTranscriptTextCorrection(recordingId: $recordingId, recordingTranscriptTextCorrectionId: $recordingTranscriptTextCorrectionId, expectedSourceGeneration: $expectedSourceGeneration) }"
}

extension Connection {
    public func recordingTranscriptTextCorrections(recordingId: String) async throws -> [RecordingTranscriptTextCorrection] {
        struct Answer: Decodable, Sendable { let ReadRecordingTranscriptTextCorrections: [RecordingTranscriptTextCorrection] }
        return try await execute(Documents.readRecordingTranscriptTextCorrections, variables: ["recordingId": .string(recordingId)], as: Answer.self)
            .ReadRecordingTranscriptTextCorrections
    }
    public func correctRecordingTranscriptText(_ draft: RecordingTextCorrectionDraft) async throws -> RecordingTranscriptTextCorrection {
        guard accountKey == draft.accountKey else { throw TeaNodeError.signedOut }
        struct Answer: Decodable, Sendable { let CorrectRecordingTranscriptText: RecordingTranscriptTextCorrection }
        let correction = try await execute(
            Documents.correctRecordingTranscriptText,
            variables: [
                "recordingId": .string(draft.recordingId), "recordingTranscriptSegmentId": .string(draft.recordingTranscriptSegmentId),
                "expectedSourceGeneration": .number(Double(draft.expectedSourceGeneration)), "clientCorrectionId": .string(draft.clientCorrectionId),
                "correctedTranscriptText": .string(draft.correctedTranscriptText),
            ], as: Answer.self
        ).CorrectRecordingTranscriptText
        guard correction.recordingId == draft.recordingId, correction.recordingTranscriptSegmentId == draft.recordingTranscriptSegmentId,
            correction.clientCorrectionId == draft.clientCorrectionId, correction.correctedTranscriptText == draft.correctedTranscriptText
        else { throw TeaNodeError.unreadableAnswer }
        return correction
    }
    public func removeRecordingTranscriptTextCorrection(
        recordingId: String, recordingTranscriptTextCorrectionId: String, expectedSourceGeneration: Int64
    ) async throws {
        struct Answer: Decodable, Sendable { let RemoveRecordingTranscriptTextCorrection: Bool }
        _ = try await execute(
            Documents.removeRecordingTranscriptTextCorrection,
            variables: [
                "recordingId": .string(recordingId), "recordingTranscriptTextCorrectionId": .string(recordingTranscriptTextCorrectionId),
                "expectedSourceGeneration": .number(Double(expectedSourceGeneration)),
            ], as: Answer.self)
    }
}
