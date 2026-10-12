import Foundation

public struct RecordingTranscriptRun: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let recordingId: String
    public let recordingMediaId: String
    public let versionNumber: Int
    public let recordingProcessingState: String
    public let providerModelName: String
    public let processingErrorMessage: String
    public let createdAt: String
    public var isRunning: Bool { ["queued", "preparing", "transcribing"].contains(recordingProcessingState) }
}

public struct RecordingTranscriptSegment: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let recordingTranscriptRunId: String
    public let recordingTranscriptionAttemptId: String
    public let localSpeakerId: String
    public let segmentStartMS: Int64
    public let segmentEndMS: Int64
    public let transcriptText: String
}

public struct RecordingTranscriptionAttempt: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let recordingAttemptState: String
    public let providerModelName: String
    public let hasProviderUsage: Bool
    public let estimatedCostAmount: Double?
    public let actualCostAmount: Double?
    public let currencyCode: String
    public let attemptErrorMessage: String
}

public struct RecordingTranscriptView: Codable, Hashable, Sendable {
    public let transcriptRun: RecordingTranscriptRun
    public let segments: [RecordingTranscriptSegment]
    public let attempts: [RecordingTranscriptionAttempt]
    public let segmentCount: Int
    public let hasMoreSegments: Bool
}

public struct RecordingSpeakerIdentity: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let confirmedName: String
}

public struct RecordingSpeakerCorrection: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let recordingMediaId: String
    public let recordingTranscriptionAttemptId: String
    public let speakerIdentityId: String
    public let localSpeakerId: String
    public let passageStartMS: Int64
    public let passageEndMS: Int64
    public let sourceGeneration: Int64
    public let isRemoved: Bool
}

public struct RecordingVoiceReference: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let speakerIdentityId: String
    public let recordingMediaId: String
    public let recordingTranscriptRunId: String
    public let passageStartMS: Int64
    public let passageEndMS: Int64
    public let sourceGeneration: Int64
    public let isRevoked: Bool
}

public struct RecordingSpeakersView: Codable, Hashable, Sendable {
    public let sourceGeneration: Int64
    public let speakerIdentities: [RecordingSpeakerIdentity]
    public let speakerCorrections: [RecordingSpeakerCorrection]
    public let voiceReferences: [RecordingVoiceReference]

    public func confirmedName(for segment: RecordingTranscriptSegment, mediaId: String) -> String? {
        let correction = speakerCorrections.filter { correction in
            correction.recordingMediaId == mediaId
                && ((correction.passageStartMS >= 0 && correction.passageEndMS > correction.passageStartMS
                    && correction.passageStartMS <= segment.segmentStartMS && correction.passageEndMS >= segment.segmentEndMS)
                    || (correction.passageStartMS == -1 && correction.passageEndMS == -1
                        && correction.recordingTranscriptionAttemptId == segment.recordingTranscriptionAttemptId
                        && correction.localSpeakerId == segment.localSpeakerId))
        }.sorted { left, right in
            if (left.passageStartMS >= 0) != (right.passageStartMS >= 0) { return left.passageStartMS >= 0 }
            return left.sourceGeneration > right.sourceGeneration
        }.first
        guard let correction, !correction.isRemoved else { return nil }
        return speakerIdentities.first { $0.id == correction.speakerIdentityId }?.confirmedName
    }
}

extension Documents {
    static let recordingTranscriptRunFields =
        "id recordingId recordingMediaId versionNumber recordingProcessingState providerModelName processingErrorMessage createdAt"
    static let recordingSpeakersFields =
        "sourceGeneration speakerIdentities { id confirmedName } speakerCorrections { id recordingMediaId recordingTranscriptionAttemptId speakerIdentityId localSpeakerId passageStartMS passageEndMS sourceGeneration isRemoved } voiceReferences { id speakerIdentityId recordingMediaId recordingTranscriptRunId passageStartMS passageEndMS sourceGeneration isRevoked }"
    public static let readRecordingTranscript =
        "query ($recordingTranscriptRunId: String!, $segmentOffset: Int!, $pageSize: Int!) { ReadRecordingTranscript(recordingTranscriptRunId: $recordingTranscriptRunId, segmentOffset: $segmentOffset, pageSize: $pageSize) { transcriptRun { \(recordingTranscriptRunFields) } segments { id recordingTranscriptRunId recordingTranscriptionAttemptId localSpeakerId segmentStartMS segmentEndMS transcriptText } attempts { id recordingAttemptState providerModelName hasProviderUsage estimatedCostAmount actualCostAmount currencyCode attemptErrorMessage } segmentCount hasMoreSegments } }"
    public static let requestRecordingTranscription =
        "mutation ($recordingMediaId: String!, $clientRunId: String!, $isReprocessingRequested: Boolean!) { RequestRecordingTranscription(recordingMediaId: $recordingMediaId, clientRunId: $clientRunId, isReprocessingRequested: $isReprocessingRequested) { \(recordingTranscriptRunFields) } }"
    public static let cancelRecordingTranscription =
        "mutation ($recordingTranscriptRunId: String!) { CancelRecordingTranscription(recordingTranscriptRunId: $recordingTranscriptRunId) { \(recordingTranscriptRunFields) } }"
    public static let readRecordingSpeakers =
        "query ($recordingId: String!) { ReadRecordingSpeakers(recordingId: $recordingId) { \(recordingSpeakersFields) } }"
    public static let correctRecordingSpeaker =
        "mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $clientCorrectionId: String!, $confirmedName: String!, $recordingTranscriptionAttemptId: String!, $localSpeakerId: String!, $passageStartMS: Int64!, $passageEndMS: Int64!) { CorrectRecordingSpeaker(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration, clientCorrectionId: $clientCorrectionId, confirmedName: $confirmedName, recordingTranscriptionAttemptId: $recordingTranscriptionAttemptId, localSpeakerId: $localSpeakerId, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS) { \(recordingSpeakersFields) } }"
    public static let renameRecordingSpeaker =
        "mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $speakerIdentityId: String!, $confirmedName: String!) { RenameRecordingSpeaker(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration, speakerIdentityId: $speakerIdentityId, confirmedName: $confirmedName) { \(recordingSpeakersFields) } }"
    public static let removeRecordingSpeakerCorrection =
        "mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $recordingSpeakerCorrectionId: String!) { RemoveRecordingSpeakerCorrection(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration, recordingSpeakerCorrectionId: $recordingSpeakerCorrectionId) { \(recordingSpeakersFields) } }"
    public static let deleteRecordingVoiceReference =
        "mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $recordingVoiceReferenceId: String!) { DeleteRecordingVoiceReference(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration, recordingVoiceReferenceId: $recordingVoiceReferenceId) { \(recordingSpeakersFields) } }"
}

extension Connection {
    public func recordingTranscript(recordingTranscriptRunId: String, segmentOffset: Int = 0, pageSize: Int = 100) async throws
        -> RecordingTranscriptView
    {
        struct Answer: Decodable, Sendable { let ReadRecordingTranscript: RecordingTranscriptView }
        return try await execute(
            Documents.readRecordingTranscript,
            variables: [
                "recordingTranscriptRunId": .string(recordingTranscriptRunId), "segmentOffset": .number(Double(segmentOffset)),
                "pageSize": .number(Double(pageSize)),
            ], as: Answer.self
        ).ReadRecordingTranscript
    }
    public func requestRecordingTranscription(recordingMediaId: String, clientRunId: String, isReprocessingRequested: Bool) async throws
        -> RecordingTranscriptRun
    {
        struct Answer: Decodable, Sendable { let RequestRecordingTranscription: RecordingTranscriptRun }
        return try await execute(
            Documents.requestRecordingTranscription,
            variables: [
                "recordingMediaId": .string(recordingMediaId), "clientRunId": .string(clientRunId),
                "isReprocessingRequested": .bool(isReprocessingRequested),
            ], as: Answer.self
        ).RequestRecordingTranscription
    }
    public func cancelRecordingTranscription(recordingTranscriptRunId: String) async throws -> RecordingTranscriptRun {
        struct Answer: Decodable, Sendable { let CancelRecordingTranscription: RecordingTranscriptRun }
        return try await execute(
            Documents.cancelRecordingTranscription, variables: ["recordingTranscriptRunId": .string(recordingTranscriptRunId)], as: Answer.self
        ).CancelRecordingTranscription
    }
    public func recordingSpeakers(recordingId: String) async throws -> RecordingSpeakersView {
        struct Answer: Decodable, Sendable { let ReadRecordingSpeakers: RecordingSpeakersView }
        return try await execute(Documents.readRecordingSpeakers, variables: ["recordingId": .string(recordingId)], as: Answer.self)
            .ReadRecordingSpeakers
    }
    public func correctRecordingSpeaker(
        recordingId: String, expectedSourceGeneration: Int64, clientCorrectionId: String, confirmedName: String, segment: RecordingTranscriptSegment,
        isWholeGroup: Bool
    ) async throws -> RecordingSpeakersView {
        struct Answer: Decodable, Sendable { let CorrectRecordingSpeaker: RecordingSpeakersView }
        return try await execute(
            Documents.correctRecordingSpeaker,
            variables: [
                "recordingId": .string(recordingId), "expectedSourceGeneration": .number(Double(expectedSourceGeneration)),
                "clientCorrectionId": .string(clientCorrectionId), "confirmedName": .string(confirmedName),
                "recordingTranscriptionAttemptId": .string(segment.recordingTranscriptionAttemptId),
                "localSpeakerId": .string(segment.localSpeakerId), "passageStartMS": .number(Double(isWholeGroup ? -1 : segment.segmentStartMS)),
                "passageEndMS": .number(Double(isWholeGroup ? -1 : segment.segmentEndMS)),
            ], as: Answer.self
        ).CorrectRecordingSpeaker
    }
    public func renameRecordingSpeaker(recordingId: String, expectedSourceGeneration: Int64, speakerIdentityId: String, confirmedName: String)
        async throws -> RecordingSpeakersView
    {
        struct Answer: Decodable, Sendable { let RenameRecordingSpeaker: RecordingSpeakersView }
        return try await execute(
            Documents.renameRecordingSpeaker,
            variables: [
                "recordingId": .string(recordingId), "expectedSourceGeneration": .number(Double(expectedSourceGeneration)),
                "speakerIdentityId": .string(speakerIdentityId), "confirmedName": .string(confirmedName),
            ], as: Answer.self
        ).RenameRecordingSpeaker
    }
    public func removeRecordingSpeakerCorrection(recordingId: String, expectedSourceGeneration: Int64, recordingSpeakerCorrectionId: String)
        async throws -> RecordingSpeakersView
    {
        struct Answer: Decodable, Sendable { let RemoveRecordingSpeakerCorrection: RecordingSpeakersView }
        return try await execute(
            Documents.removeRecordingSpeakerCorrection,
            variables: [
                "recordingId": .string(recordingId), "expectedSourceGeneration": .number(Double(expectedSourceGeneration)),
                "recordingSpeakerCorrectionId": .string(recordingSpeakerCorrectionId),
            ], as: Answer.self
        ).RemoveRecordingSpeakerCorrection
    }
    public func deleteRecordingVoiceReference(recordingId: String, expectedSourceGeneration: Int64, recordingVoiceReferenceId: String) async throws
        -> RecordingSpeakersView
    {
        struct Answer: Decodable, Sendable { let DeleteRecordingVoiceReference: RecordingSpeakersView }
        return try await execute(
            Documents.deleteRecordingVoiceReference,
            variables: [
                "recordingId": .string(recordingId), "expectedSourceGeneration": .number(Double(expectedSourceGeneration)),
                "recordingVoiceReferenceId": .string(recordingVoiceReferenceId),
            ], as: Answer.self
        ).DeleteRecordingVoiceReference
    }
}
