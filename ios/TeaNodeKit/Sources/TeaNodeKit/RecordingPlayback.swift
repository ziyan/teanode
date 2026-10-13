import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// RecordingPlayback describes a locally prepared compatible derivative of an owned original.
public struct RecordingPlayback: Codable, Sendable {
    public let recordingMediaId: String
    public let playbackPath: String
    public let playbackContentType: String
    public let playbackSizeBytes: Int64
    public let playbackSha256: String
    public let playbackDurationMS: Int64
    public let timelineStartMS: Int64

    public func confirmsMedia(_ media: RecordingMedia) -> Bool {
        Connection.isRecordingIdentifier(media.id) && recordingMediaId == media.id
            && playbackPath == "/api/v1/recordings/media/\(media.id)/playback"
            && playbackContentType == "audio/wav" && playbackSizeBytes > 0
            && Connection.isRecordingDigest(playbackSha256) && playbackDurationMS > 0
            && timelineStartMS == media.timelineStartMS
    }
}

extension Documents {
    static let recordingPlaybackFields = """
        recordingMediaId playbackPath playbackContentType playbackSizeBytes playbackSha256 playbackDurationMS timelineStartMS
        """
    public static let readRecordingPlayback = """
        query ($recordingMediaId: String!) {
          ReadRecordingPlayback(recordingMediaId: $recordingMediaId) { \(recordingPlaybackFields) }
        }
        """
    public static let prepareRecordingPlayback = """
        mutation ($recordingMediaId: String!) {
          PrepareRecordingPlayback(recordingMediaId: $recordingMediaId) { \(recordingPlaybackFields) }
        }
        """
}

extension Connection {
    public func recordingPlayback(for media: RecordingMedia) async throws -> RecordingPlayback? {
        guard Self.isRecordingIdentifier(media.id) else { throw TeaNodeError.unreadableAnswer }
        struct Answer: Decodable, Sendable { let ReadRecordingPlayback: RecordingPlayback? }
        let playback = try await execute(
            Documents.readRecordingPlayback, variables: ["recordingMediaId": .string(media.id)], as: Answer.self
        ).ReadRecordingPlayback
        if let playback, !playback.confirmsMedia(media) { throw TeaNodeError.unreadableAnswer }
        return playback
    }

    public func compatibleRecordingPlayback(for media: RecordingMedia, isOriginalPlayable: Bool) async throws -> RecordingPlayback? {
        guard !isOriginalPlayable else { return nil }
        return try await prepareRecordingPlayback(for: media)
    }

    public func prepareRecordingPlayback(for media: RecordingMedia) async throws -> RecordingPlayback {
        guard Self.isRecordingIdentifier(media.id) else { throw TeaNodeError.unreadableAnswer }
        struct Answer: Decodable, Sendable { let PrepareRecordingPlayback: RecordingPlayback }
        let playback = try await execute(
            Documents.prepareRecordingPlayback, variables: ["recordingMediaId": .string(media.id)], as: Answer.self
        ).PrepareRecordingPlayback
        guard playback.confirmsMedia(media) else { throw TeaNodeError.unreadableAnswer }
        return playback
    }

    public func recordingCompatiblePlaybackRequest(_ playback: RecordingPlayback, for media: RecordingMedia) async throws -> URLRequest {
        guard playback.confirmsMedia(media) else { throw TeaNodeError.unreadableAnswer }
        var request = URLRequest(url: account.server.endpoint(playback.playbackPath))
        request.setValue("Bearer \(try await accessToken())", forHTTPHeaderField: "Authorization")
        return request
    }
}
