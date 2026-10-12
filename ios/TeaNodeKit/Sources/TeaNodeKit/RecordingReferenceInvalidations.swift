import Foundation

public struct RecordingReferenceInvalidation: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let recordingId: String
    public let recordingTranscriptRunId: String
    public let recordingTranscriptionAttemptId: String
    public let referenceBlobId: String
    public let referenceRecordingId: String
    public let referenceTranscriptRunId: String
    public let speakerIdentityId: String
    public let invalidatedConfirmedName: String
    public let referencePassageStartMS: Int64
    public let referencePassageEndMS: Int64
    public let referenceSourceGeneration: Int64
    public let invalidationMessage: String
    public let createdAt: String
    public let isReconciled: Bool
}

extension Documents {
    public static let readRecordingReferenceInvalidations = """
        query ($recordingId: String!) { ReadRecordingReferenceInvalidations(recordingId: $recordingId) { id recordingId
        recordingTranscriptRunId recordingTranscriptionAttemptId referenceBlobId referenceRecordingId referenceTranscriptRunId
        speakerIdentityId invalidatedConfirmedName referencePassageStartMS referencePassageEndMS referenceSourceGeneration
        invalidationMessage createdAt isReconciled } }
        """
}

extension Connection {
    public func recordingReferenceInvalidations(recordingId: String) async throws -> [RecordingReferenceInvalidation] {
        struct Answer: Decodable, Sendable { let ReadRecordingReferenceInvalidations: [RecordingReferenceInvalidation] }
        return try await execute(Documents.readRecordingReferenceInvalidations, variables: ["recordingId": .string(recordingId)], as: Answer.self)
            .ReadRecordingReferenceInvalidations
    }
}
