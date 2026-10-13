import Foundation

public struct RecordingTranscriptTimeline: Codable, Hashable, Sendable {
    public let sourceGeneration: Int64
    public let segments: [RecordingTranscriptSegment]
    public let transcriptRuns: [RecordingTranscriptRun]
    public let segmentCount: Int
    public let hasMoreSegments: Bool
}

extension Documents {
    public static let readRecordingTranscriptTimeline = """
        query ($recordingId: String!, $segmentOffset: Int!, $pageSize: Int!) { ReadRecordingTranscriptTimeline(recordingId:
        $recordingId, segmentOffset: $segmentOffset, pageSize: $pageSize) { sourceGeneration segments { id recordingTranscriptRunId
        recordingTranscriptionAttemptId localSpeakerId segmentStartMS segmentEndMS transcriptText effectiveTranscriptText
        appliedTextCorrectionId hasUnalignedTextCorrection effectiveSpeakerIdentityId effectiveSpeakerName hasUserConfirmedSpeaker
        hasUnalignedSpeakerCorrection appliedSpeakerCorrectionId } transcriptRuns { id recordingId recordingMediaId versionNumber
        recordingProcessingState providerModelName processingErrorMessage createdAt } segmentCount hasMoreSegments } }
        """
}

extension Connection {
    public func recordingTranscriptTimeline(recordingId: String, segmentOffset: Int = 0, pageSize: Int = 100) async throws
        -> RecordingTranscriptTimeline
    {
        struct Answer: Decodable, Sendable { let ReadRecordingTranscriptTimeline: RecordingTranscriptTimeline }
        return try await execute(
            Documents.readRecordingTranscriptTimeline,
            variables: ["recordingId": .string(recordingId), "segmentOffset": .number(Double(segmentOffset)), "pageSize": .number(Double(pageSize))],
            as: Answer.self
        ).ReadRecordingTranscriptTimeline
    }
}
