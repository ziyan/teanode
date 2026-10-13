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
    public var effectiveTranscriptText: String? = nil
    public var appliedTextCorrectionId: String? = nil
    public var hasUnalignedTextCorrection: Bool? = nil
    public var effectiveSpeakerIdentityId: String? = nil
    public var effectiveSpeakerName: String? = nil
    public var hasUserConfirmedSpeaker: Bool? = nil
    /// Whether the transcription named this speaker after one of the person's voice samples, without the person confirming it here.
    public var isRecognizedSpeaker: Bool? = nil
    /// Whether the confirmed speaker is the person who owns the account, which is the side of the conversation their bubbles sit on.
    public var isAccountOwner: Bool? = nil
    public var hasUnalignedSpeakerCorrection: Bool? = nil
    public var appliedSpeakerCorrectionId: String? = nil
    /// "Speaker 1" and so on for a speaker of a capture under way whom nobody has named: the same speaker has the same label in
    /// every piece. Empty or nil otherwise.
    public var temporarySpeakerLabel: String? = nil
    /// The speaker's name when the person confirmed it or a voice sample recognized it; nil while it is only a label.
    public var confirmedSpeakerName: String? {
        guard hasUserConfirmedSpeaker == true || isRecognizedSpeaker == true, let effectiveSpeakerIdentityId, !effectiveSpeakerIdentityId.isEmpty,
            let effectiveSpeakerName, !effectiveSpeakerName.isEmpty
        else { return nil }
        return effectiveSpeakerName
    }
    public var displayedTranscriptText: String { effectiveTranscriptText ?? transcriptText }
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
    public var isAccountOwner: Bool? = nil
}

public struct RecordingSpeakerCorrection: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let recordingMediaId: String
    public let recordingTranscriptionAttemptId: String
    public let speakerIdentityId: String
    public var clientCorrectionId: String? = nil
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

}

extension Documents {
    static let recordingTranscriptRunFields = """
        id recordingId recordingMediaId versionNumber recordingProcessingState providerModelName processingErrorMessage createdAt
        """
    static let recordingSpeakersFields = """
        sourceGeneration speakerIdentities { id confirmedName isAccountOwner } speakerCorrections { id recordingMediaId recordingTranscriptionAttemptId
        speakerIdentityId clientCorrectionId localSpeakerId passageStartMS passageEndMS sourceGeneration isRemoved } voiceReferences {
        id speakerIdentityId recordingMediaId recordingTranscriptRunId passageStartMS passageEndMS sourceGeneration isRevoked }
        """
    public static let readRecordingTranscript = """
        query ($recordingTranscriptRunId: String!, $segmentOffset: Int!, $pageSize: Int!) {
        ReadRecordingTranscript(recordingTranscriptRunId: $recordingTranscriptRunId, segmentOffset: $segmentOffset, pageSize: $pageSize)
        { transcriptRun { \(recordingTranscriptRunFields) } segments { id recordingTranscriptRunId recordingTranscriptionAttemptId
        localSpeakerId segmentStartMS segmentEndMS transcriptText effectiveTranscriptText appliedTextCorrectionId
        hasUnalignedTextCorrection effectiveSpeakerIdentityId effectiveSpeakerName hasUserConfirmedSpeaker isRecognizedSpeaker isAccountOwner hasUnalignedSpeakerCorrection
        appliedSpeakerCorrectionId } attempts { id recordingAttemptState providerModelName hasProviderUsage estimatedCostAmount
        actualCostAmount currencyCode attemptErrorMessage } segmentCount hasMoreSegments } }
        """
    public static let requestRecordingTranscription = """
        mutation ($recordingMediaId: String!, $clientRunId: String!, $isReprocessingRequested: Boolean!) {
        RequestRecordingTranscription(recordingMediaId: $recordingMediaId, clientRunId: $clientRunId, isReprocessingRequested:
        $isReprocessingRequested) { \(recordingTranscriptRunFields) } }
        """
    public static let retryRecordingTranscription = """
        mutation ($recordingTranscriptRunId: String!) { RetryRecordingTranscription(recordingTranscriptRunId: $recordingTranscriptRunId)
        { \(recordingTranscriptRunFields) } }
        """
    public static let cancelRecordingTranscription = """
        mutation ($recordingTranscriptRunId: String!) { CancelRecordingTranscription(recordingTranscriptRunId:
        $recordingTranscriptRunId) { \(recordingTranscriptRunFields) } }
        """
    public static let suggestRecordingSpeakerNames = """
        query ($searchText: String, $pageSize: Int) { SuggestRecordingSpeakerNames(searchText: $searchText, pageSize: $pageSize) {
        confirmedName speakerNameSource } }
        """
    public static let readRecordingSpeakers = """
        query ($recordingId: String!) { ReadRecordingSpeakers(recordingId: $recordingId) { \(recordingSpeakersFields) } }
        """
    public static let correctRecordingSpeaker = """
        mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $clientCorrectionId: String!, $confirmedName: String!,
        $isAccountOwner: Boolean, $recordingTranscriptionAttemptId: String!, $localSpeakerId: String!, $passageStartMS: Int64!, $passageEndMS: Int64!) {
        CorrectRecordingSpeaker(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration, clientCorrectionId:
        $clientCorrectionId, confirmedName: $confirmedName, isAccountOwner: $isAccountOwner, recordingTranscriptionAttemptId: $recordingTranscriptionAttemptId,
        localSpeakerId: $localSpeakerId, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS) { \(recordingSpeakersFields) } }
        """
    public static let nameRecordingTemporarySpeaker = """
        mutation ($recordingId: String!, $speakerLabel: String!, $confirmedName: String!, $isAccountOwner: Boolean) {
        NameRecordingTemporarySpeaker(recordingId: $recordingId, speakerLabel: $speakerLabel, confirmedName: $confirmedName, isAccountOwner:
        $isAccountOwner) }
        """
    public static let renameRecordingSpeaker = """
        mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $speakerIdentityId: String!, $confirmedName: String!) {
        RenameRecordingSpeaker(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration, speakerIdentityId:
        $speakerIdentityId, confirmedName: $confirmedName) { \(recordingSpeakersFields) } }
        """
    public static let removeRecordingSpeakerCorrection = """
        mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $recordingSpeakerCorrectionId: String!) {
        RemoveRecordingSpeakerCorrection(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration,
        recordingSpeakerCorrectionId: $recordingSpeakerCorrectionId) { \(recordingSpeakersFields) } }
        """
    public static let undoRecordingSpeakerNaming = """
        mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $recordingSpeakerCorrectionId: String!) {
        UndoRecordingSpeakerNaming(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration,
        recordingSpeakerCorrectionId: $recordingSpeakerCorrectionId) { \(recordingSpeakersFields) } }
        """
    public static let deleteRecordingVoiceReference = """
        mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $recordingVoiceReferenceId: String!) {
        DeleteRecordingVoiceReference(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration,
        recordingVoiceReferenceId: $recordingVoiceReferenceId) { \(recordingSpeakersFields) } }
        """
}

extension Connection {
    public func recordingTranscript(recordingTranscriptRunId: String, segmentOffset: Int = 0, pageSize: Int = 100) async throws
        -> RecordingTranscriptView
    {
        struct Answer: Decodable, Sendable { let ReadRecordingTranscript: RecordingTranscriptView }
        let view = try await execute(
            Documents.readRecordingTranscript,
            variables: [
                "recordingTranscriptRunId": .string(recordingTranscriptRunId), "segmentOffset": .number(Double(segmentOffset)),
                "pageSize": .number(Double(pageSize)),
            ], as: Answer.self
        ).ReadRecordingTranscript
        guard view.transcriptRun.id == recordingTranscriptRunId,
            view.segments.allSatisfy({ $0.recordingTranscriptRunId == recordingTranscriptRunId })
        else { throw TeaNodeError.unreadableAnswer }
        return view
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
    public func retryRecordingTranscription(recordingTranscriptRunId: String) async throws -> RecordingTranscriptRun {
        struct Answer: Decodable, Sendable { let RetryRecordingTranscription: RecordingTranscriptRun }
        return try await execute(
            Documents.retryRecordingTranscription, variables: ["recordingTranscriptRunId": .string(recordingTranscriptRunId)], as: Answer.self
        ).RetryRecordingTranscription
    }

    public func cancelRecordingTranscription(recordingTranscriptRunId: String) async throws -> RecordingTranscriptRun {
        struct Answer: Decodable, Sendable { let CancelRecordingTranscription: RecordingTranscriptRun }
        return try await execute(
            Documents.cancelRecordingTranscription, variables: ["recordingTranscriptRunId": .string(recordingTranscriptRunId)], as: Answer.self
        ).CancelRecordingTranscription
    }
    /// Names to offer while a speaker is named: voices already named, then people in the agent's memory, then contacts.
    public func suggestRecordingSpeakerNames(searchText: String) async throws -> [RecordingSpeakerNameSuggestion] {
        struct Answer: Decodable, Sendable { let SuggestRecordingSpeakerNames: [RecordingSpeakerNameSuggestion] }
        return try await execute(
            Documents.suggestRecordingSpeakerNames, variables: ["searchText": .string(searchText)], as: Answer.self
        ).SuggestRecordingSpeakerNames
    }
    public func recordingSpeakers(recordingId: String) async throws -> RecordingSpeakersView {
        struct Answer: Decodable, Sendable { let ReadRecordingSpeakers: RecordingSpeakersView }
        return try await execute(Documents.readRecordingSpeakers, variables: ["recordingId": .string(recordingId)], as: Answer.self)
            .ReadRecordingSpeakers
    }
    public func correctRecordingSpeaker(
        recordingId: String, expectedSourceGeneration: Int64, clientCorrectionId: String, confirmedName: String, segment: RecordingTranscriptSegment,
        isWholeGroup: Bool, passageStartMS: Int64? = nil, passageEndMS: Int64? = nil, isAccountOwner: Bool? = nil
    ) async throws -> RecordingSpeakersView {
        struct Answer: Decodable, Sendable { let CorrectRecordingSpeaker: RecordingSpeakersView }
        let response = try await execute(
            Documents.correctRecordingSpeaker,
            variables: [
                "recordingId": .string(recordingId), "expectedSourceGeneration": .number(Double(expectedSourceGeneration)),
                "clientCorrectionId": .string(clientCorrectionId), "confirmedName": .string(confirmedName),
                "isAccountOwner": isAccountOwner.map { .bool($0) } ?? .null,
                "recordingTranscriptionAttemptId": .string(segment.recordingTranscriptionAttemptId),
                "localSpeakerId": .string(segment.localSpeakerId),
                "passageStartMS": .number(Double(isWholeGroup ? -1 : passageStartMS ?? segment.segmentStartMS)),
                "passageEndMS": .number(Double(isWholeGroup ? -1 : passageEndMS ?? segment.segmentEndMS)),
            ], as: Answer.self
        ).CorrectRecordingSpeaker
        guard
            response.speakerCorrections.contains(where: { correction in
                correction.clientCorrectionId == clientCorrectionId
                    && correction.recordingTranscriptionAttemptId == segment.recordingTranscriptionAttemptId
                    && correction.localSpeakerId == segment.localSpeakerId
            })
        else { throw TeaNodeError.unreadableAnswer }
        return response
    }
    /// Names a speaker of a capture under way by the label the timeline shows, in every piece. An empty name with `isAccountOwner`
    /// means the person who owns the account.
    public func nameRecordingTemporarySpeaker(recordingId: String, speakerLabel: String, confirmedName: String, isAccountOwner: Bool) async throws {
        struct Answer: Decodable, Sendable { let NameRecordingTemporarySpeaker: Bool }
        let answer = try await execute(
            Documents.nameRecordingTemporarySpeaker,
            variables: [
                "recordingId": .string(recordingId), "speakerLabel": .string(speakerLabel), "confirmedName": .string(confirmedName),
                "isAccountOwner": isAccountOwner ? .bool(true) : .null,
            ], as: Answer.self
        ).NameRecordingTemporarySpeaker
        guard answer else { throw TeaNodeError.unreadableAnswer }
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
    /// Takes back the naming a correction belongs to, whole: every passage that naming named returns to its speaker's label.
    public func undoRecordingSpeakerNaming(recordingId: String, expectedSourceGeneration: Int64, recordingSpeakerCorrectionId: String)
        async throws -> RecordingSpeakersView
    {
        struct Answer: Decodable, Sendable { let UndoRecordingSpeakerNaming: RecordingSpeakersView }
        return try await execute(
            Documents.undoRecordingSpeakerNaming,
            variables: [
                "recordingId": .string(recordingId), "expectedSourceGeneration": .number(Double(expectedSourceGeneration)),
                "recordingSpeakerCorrectionId": .string(recordingSpeakerCorrectionId),
            ], as: Answer.self
        ).UndoRecordingSpeakerNaming
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

/// RecordingSpeakerNameSuggestion is a name offered for a speaker and where it came from: speaker, memory or contact.
public struct RecordingSpeakerNameSuggestion: Codable, Hashable, Sendable, Identifiable {
    public let confirmedName: String
    public let speakerNameSource: String
    public var id: String { confirmedName }
}
