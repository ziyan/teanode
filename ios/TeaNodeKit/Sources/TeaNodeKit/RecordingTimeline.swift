import Foundation

public struct RecordingTranscriptTimeline: Codable, Hashable, Sendable {
    public let sourceGeneration: Int64
    public let segments: [RecordingTranscriptSegment]
    public let transcriptRuns: [RecordingTranscriptRun]
    public let segmentCount: Int
    public let hasMoreSegments: Bool
    /// The speakers of a capture still under way that nobody has named, or has named since. Older servers do not say.
    public var temporarySpeakers: [RecordingTemporarySpeaker]? = nil
    /// Words heard while the recording was still being captured that no finished transcript covers yet, in timeline order and
    /// without a speaker. Only the first page has them. Older servers do not say.
    public var liveUtterances: [RecordingLiveUtterance]? = nil
}

/// RecordingTemporarySpeaker is a speaker of a capture under way, known by the label the timeline gives it ("Speaker 1").
public struct RecordingTemporarySpeaker: Codable, Hashable, Sendable {
    public let speakerLabel: String
    public let speakerIdentityId: String?
}

extension Documents {
    public static let readRecordingTranscriptTimeline = """
        query ($recordingId: String!, $segmentOffset: Int!, $pageSize: Int!) { ReadRecordingTranscriptTimeline(recordingId:
        $recordingId, segmentOffset: $segmentOffset, pageSize: $pageSize) { sourceGeneration segments { id recordingTranscriptRunId
        recordingTranscriptionAttemptId localSpeakerId segmentStartMS segmentEndMS transcriptText effectiveTranscriptText
        appliedTextCorrectionId hasUnalignedTextCorrection effectiveSpeakerIdentityId effectiveSpeakerName hasUserConfirmedSpeaker isRecognizedSpeaker
        isAccountOwner hasUnalignedSpeakerCorrection appliedSpeakerCorrectionId temporarySpeakerLabel } transcriptRuns { id recordingId recordingMediaId versionNumber
        recordingProcessingState providerModelName processingErrorMessage createdAt } temporarySpeakers { speakerLabel speakerIdentityId } segmentCount
        hasMoreSegments liveUtterances { utteranceId utteranceStartMS utteranceEndMS transcriptText } } }
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
