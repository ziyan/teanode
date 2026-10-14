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
        query ($recordingId: String!, $segmentOffset: Int!, $pageSize: Int!, $isUnnamedOnly: Boolean) {
        ReadRecordingTranscriptTimeline(recordingId: $recordingId, segmentOffset: $segmentOffset, pageSize: $pageSize,
        isUnnamedOnly: $isUnnamedOnly) { sourceGeneration segments { id recordingTranscriptRunId
        recordingTranscriptionAttemptId localSpeakerId segmentStartMS segmentEndMS transcriptText effectiveTranscriptText
        appliedTextCorrectionId hasUnalignedTextCorrection effectiveSpeakerIdentityId effectiveSpeakerName hasUserConfirmedSpeaker isRecognizedSpeaker
        isAccountOwner hasUnalignedSpeakerCorrection appliedSpeakerCorrectionId temporarySpeakerLabel } transcriptRuns { id recordingId recordingMediaId versionNumber
        recordingProcessingState providerModelName processingErrorMessage createdAt } temporarySpeakers { speakerLabel speakerIdentityId } segmentCount
        hasMoreSegments liveUtterances { utteranceId utteranceStartMS utteranceEndMS transcriptText } } }
        """
}

extension Connection {
    /// `isUnnamedOnly` reads only the passages whose speaker nobody has named, what is left to label; the offset and the count
    /// are then of those passages alone.
    public func recordingTranscriptTimeline(recordingId: String, segmentOffset: Int = 0, pageSize: Int = 100, isUnnamedOnly: Bool = false)
        async throws -> RecordingTranscriptTimeline
    {
        struct Answer: Decodable, Sendable { let ReadRecordingTranscriptTimeline: RecordingTranscriptTimeline }
        var variables: [String: JSONValue] = [
            "recordingId": .string(recordingId), "segmentOffset": .number(Double(segmentOffset)), "pageSize": .number(Double(pageSize)),
        ]
        if isUnnamedOnly { variables["isUnnamedOnly"] = .bool(true) }
        return try await execute(Documents.readRecordingTranscriptTimeline, variables: variables, as: Answer.self)
            .ReadRecordingTranscriptTimeline
    }
}
