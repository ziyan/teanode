import Foundation

/// RecordingLearningView reuses knowledge-source progress; processed passages do not certify learned facts.
public struct RecordingLearningView: Codable, Hashable, Sendable {
    public let sourceGeneration: Int64
    public let hasAgent: Bool
    public let knowledgeSource: KnowledgeSource?
    public let pendingDocumentCount: Int64
    public let digestedDocumentCount: Int64
    public let lastPublishedSourceGeneration: Int64
    public let isCurationPending: Bool
}

extension Documents {
    public static let readRecordingLearning = """
        query ($recordingId: String!) { ReadRecordingLearning(recordingId: $recordingId) { sourceGeneration hasAgent knowledgeSource {
        \(knowledgeSourceFields) nextRunAt } pendingDocumentCount digestedDocumentCount
        lastPublishedSourceGeneration isCurationPending } }
        """
}

extension Connection {
    public func recordingLearning(recordingId: String) async throws -> RecordingLearningView {
        struct Answer: Decodable, Sendable { let ReadRecordingLearning: RecordingLearningView }
        return try await execute(Documents.readRecordingLearning, variables: ["recordingId": .string(recordingId)], as: Answer.self)
            .ReadRecordingLearning
    }
}
