import Foundation

/// RecordingModelCapability is model-specific provider evidence, never a name-based guess.
public struct RecordingModelCapability: Codable, Hashable, Sendable {
    public let name: String
    public let canTranscribe: Bool?
    public let hasSpeakerDiarization: Bool?
    public let hasSegmentTimestamps: Bool?
    public let hasSpeakerReferences: Bool?
    public let maximumAudioBytes: Int64?
    public let maximumSpeakerReferenceCount: Int?

    public var canAttributeRecordingSpeakers: Bool {
        canTranscribe == true && hasSpeakerDiarization == true && hasSegmentTimestamps == true
    }
}

extension Connection {
    public func recordingModelCapabilities() async throws -> [RecordingModelCapability] {
        struct Answer: Decodable, Sendable { let ListAgentModels: [RecordingModelCapability?] }
        return try await execute(Documents.manageAgentModels, as: Answer.self).ListAgentModels.compactMap { $0 }
    }
}
