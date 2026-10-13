import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// Recording is account-owned source evidence, independent of an agent conversation.
public struct Recording: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let userId: String?
    public let createdAt: String
    public let updatedAt: String
    public let clientRecordingId: String
    public let recordingTitle: String
    public let recordingOrigin: String
    public var recordedAt: String? = nil
    /// Who wrote the title: "none", "generated" or "user". Older servers do not say.
    public var recordingTitleSource: String? = nil
    /// The short summary the server wrote from the transcript; empty until it has.
    public var recordingSummary: String? = nil
    public let recordingCaptureState: String
    public let timelineDurationMS: Int64
    public let sourceGeneration: Int64
    public let captureGaps: [RecordingGap]
}

public struct RecordingGap: Codable, Hashable, Sendable, Identifiable {
    public let gapId: String
    public var gapStartMS: Int64
    public var gapEndMS: Int64?
    public let recordingGapReason: String
    public var id: String { gapId }

    public init(gapId: String = UUID().uuidString, gapStartMS: Int64, gapEndMS: Int64? = nil, recordingGapReason: String) {
        self.gapId = gapId
        self.gapStartMS = gapStartMS
        self.gapEndMS = gapEndMS
        self.recordingGapReason = recordingGapReason
    }

    var variables: JSONValue {
        .object([
            "gapId": .string(gapId), "gapStartMS": .number(Double(gapStartMS)),
            "gapEndMS": gapEndMS.map { .number(Double($0)) } ?? .null,
            "recordingGapReason": .string(recordingGapReason),
        ])
    }
}

public struct RecordingMedia: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let recordingId: String
    public let clientMediaId: String
    public let originalFilename: String
    public let originalContentType: String
    public let originalSizeBytes: Int64
    public let originalSha256: String
    public let timelineStartMS: Int64
    public let mediaDurationMS: Int64
    public let partCount: Int
    public let recordingUploadState: String
    public let verifiedAt: String?

    public func playbackOffsetMS(for passageStartMS: Int64) -> Int64 {
        guard passageStartMS >= timelineStartMS else { return 0 }
        let (offsetMS, hasOverflow) = passageStartMS.subtractingReportingOverflow(timelineStartMS)
        return hasOverflow ? Int64.max : offsetMS
    }

    public func confirmsOriginal(_ expected: RecordingMedia) -> Bool {
        id == expected.id && recordingId == expected.recordingId && clientMediaId == expected.clientMediaId
            && originalSizeBytes == expected.originalSizeBytes && originalSha256 == expected.originalSha256
            && recordingUploadState == "complete" && ServerTime.date(verifiedAt ?? "") != nil
    }
}

public struct RecordingMediaPart: Codable, Hashable, Sendable {
    public let recordingMediaId: String
    public let partIndex: Int
    public let partSizeBytes: Int64
    public let partSha256: String
    public let storedAt: String

    public func confirmsPart(mediaId: String, partIndex: Int, partSizeBytes: Int64, partSha256: String) -> Bool {
        recordingMediaId == mediaId && self.partIndex == partIndex && self.partSizeBytes == partSizeBytes
            && self.partSha256 == partSha256 && ServerTime.date(storedAt) != nil
    }
}

public struct RecordingView: Codable, Hashable, Sendable {
    public let recording: Recording
    public let mediaFiles: [RecordingMedia]
    public let uploadParts: [RecordingMediaPart]
    public var transcriptRuns: [RecordingTranscriptRun]? = nil
}

public struct RecordingLimits: Codable, Hashable, Sendable {
    public let maximumMediaBytes: Int64
    public let maximumAccountBytes: Int64
    public let uploadPartBytes: Int64
    public var isTranscriptionAvailable: Bool? = nil
    public var transcriptionUnavailableMessage: String? = nil
    public var transcriptionProviderModelName: String? = nil
}

extension Documents {
    static let recordingFields = """
        id userId createdAt updatedAt clientRecordingId recordingTitle recordingTitleSource recordingSummary recordingOrigin recordedAt
        recordingCaptureState timelineDurationMS sourceGeneration captureGaps { gapId gapStartMS gapEndMS recordingGapReason }
        """
    static let recordingMediaFields = """
        id recordingId clientMediaId originalFilename originalContentType originalSizeBytes originalSha256 timelineStartMS
        mediaDurationMS partCount recordingUploadState verifiedAt
        """
    public static let listRecordings = """
        query ($beforeRecordingId: String!, $pageSize: Int!, $searchText: String) { ListRecordings(beforeRecordingId: $beforeRecordingId,
        pageSize: $pageSize, searchText: $searchText) { \(recordingFields) } }
        """
    public static let readRecording = """
        query ($recordingId: String!) { ReadRecording(recordingId: $recordingId) { recording { \(recordingFields) } mediaFiles {
        \(recordingMediaFields) } uploadParts { recordingMediaId partIndex partSizeBytes partSha256 storedAt } transcriptRuns {
        \(recordingTranscriptRunFields) } } }
        """
    public static let recordingLimits = """
        query { ReadRecordingLimits { maximumMediaBytes maximumAccountBytes uploadPartBytes isTranscriptionAvailable transcriptionUnavailableMessage transcriptionProviderModelName } }
        """
    public static let createRecording = """
        mutation ($clientRecordingId: String!, $recordingTitle: String!, $recordingOrigin: String!) { CreateRecording(clientRecordingId:
        $clientRecordingId, recordingTitle: $recordingTitle, recordingOrigin: $recordingOrigin) { \(recordingFields) } }
        """
    public static let createRecordingMedia = """
        mutation ($recordingId: String!, $clientMediaId: String!, $originalFilename: String!, $originalContentType: String!, $originalSizeBytes: Int64!, $originalSha256: String!, $timelineStartMS: Int64!, $mediaDurationMS: Int64!) {
          CreateRecordingMedia(recordingId: $recordingId, clientMediaId: $clientMediaId, originalFilename: $originalFilename, originalContentType: $originalContentType,
            originalSizeBytes: $originalSizeBytes, originalSha256: $originalSha256, timelineStartMS: $timelineStartMS, mediaDurationMS: $mediaDurationMS) { \(recordingMediaFields) }
        }
        """
    public static let updateRecordingCapture = """
        mutation ($recordingId: String!, $expectedGeneration: Int64!, $recordingCaptureState: String!, $timelineDurationMS: Int64!, $captureGaps: [RecordingGapInput!]!) {
          UpdateRecordingCapture(recordingId: $recordingId, expectedGeneration: $expectedGeneration, recordingCaptureState: $recordingCaptureState,
            timelineDurationMS: $timelineDurationMS, captureGaps: $captureGaps) { \(recordingFields) }
        }
        """
    public static let updateRecordingMetadata = """
        mutation ($recordingId: String!, $expectedSourceGeneration: Int64!, $recordingTitle: String, $recordedAt: DateTime) {
        UpdateRecordingMetadata(recordingId: $recordingId, expectedSourceGeneration: $expectedSourceGeneration, recordingTitle:
        $recordingTitle, recordedAt: $recordedAt) { \(recordingFields) } }
        """
    public static let deleteRecording = """
        mutation ($recordingId: String!) { DeleteRecording(recordingId: $recordingId) }
        """
}

extension Connection {
    /// The account's recordings, newest first; a search keeps those whose title, summary or transcript contains it.
    public func recordings(beforeRecordingId: String = "", pageSize: Int = 50, searchText: String = "") async throws -> [Recording] {
        struct Answer: Decodable, Sendable { let ListRecordings: [Recording] }
        var variables: [String: JSONValue] = ["beforeRecordingId": .string(beforeRecordingId), "pageSize": .number(Double(pageSize))]
        if !searchText.isEmpty { variables["searchText"] = .string(searchText) }
        return try await execute(Documents.listRecordings, variables: variables, as: Answer.self).ListRecordings
    }

    public func recording(_ recordingId: String) async throws -> RecordingView {
        struct Answer: Decodable, Sendable { let ReadRecording: RecordingView }
        return try await execute(Documents.readRecording, variables: ["recordingId": .string(recordingId)], as: Answer.self).ReadRecording
    }

    public func recordingLimits() async throws -> RecordingLimits {
        struct Answer: Decodable, Sendable { let ReadRecordingLimits: RecordingLimits }
        return try await execute(Documents.recordingLimits, as: Answer.self).ReadRecordingLimits
    }

    public func createRecording(clientRecordingId: String, recordingTitle: String, recordingOrigin: String) async throws -> Recording {
        struct Answer: Decodable, Sendable { let CreateRecording: Recording }
        let recording = try await execute(
            Documents.createRecording,
            variables: [
                "clientRecordingId": .string(clientRecordingId), "recordingTitle": .string(recordingTitle),
                "recordingOrigin": .string(recordingOrigin),
            ], as: Answer.self
        ).CreateRecording
        guard recording.clientRecordingId == clientRecordingId, recording.recordingOrigin == recordingOrigin,
            Self.isRecordingIdentifier(recording.id)
        else { throw TeaNodeError.unreadableAnswer }
        return recording
    }

    public func createRecordingMedia(
        recordingId: String, clientMediaId: String, originalFilename: String, originalContentType: String, originalSizeBytes: Int64,
        originalSha256: String, timelineStartMS: Int64, mediaDurationMS: Int64
    ) async throws -> RecordingMedia {
        struct Answer: Decodable, Sendable { let CreateRecordingMedia: RecordingMedia }
        let media = try await execute(
            Documents.createRecordingMedia,
            variables: [
                "recordingId": .string(recordingId), "clientMediaId": .string(clientMediaId), "originalFilename": .string(originalFilename),
                "originalContentType": .string(originalContentType), "originalSizeBytes": .number(Double(originalSizeBytes)),
                "originalSha256": .string(originalSha256), "timelineStartMS": .number(Double(timelineStartMS)),
                "mediaDurationMS": .number(Double(mediaDurationMS)),
            ], as: Answer.self
        ).CreateRecordingMedia
        guard originalSizeBytes > 0, originalSizeBytes <= Int64.max - 8 * 1024 * 1024,
            media.recordingId == recordingId, media.clientMediaId == clientMediaId, Self.isRecordingIdentifier(media.id),
            media.originalSizeBytes == originalSizeBytes, media.originalSha256 == originalSha256,
            media.originalFilename == originalFilename, media.originalContentType == originalContentType,
            media.timelineStartMS == timelineStartMS, media.mediaDurationMS == mediaDurationMS,
            media.partCount == Int((originalSizeBytes + 8 * 1024 * 1024 - 1) / (8 * 1024 * 1024))
        else { throw TeaNodeError.unreadableAnswer }
        return media
    }

    public func updateRecordingCapture(
        recordingId: String, expectedGeneration: Int64, recordingCaptureState: String, timelineDurationMS: Int64, captureGaps: [RecordingGap]
    ) async throws -> Recording {
        struct Answer: Decodable, Sendable { let UpdateRecordingCapture: Recording }
        return try await execute(
            Documents.updateRecordingCapture,
            variables: [
                "recordingId": .string(recordingId), "expectedGeneration": .number(Double(expectedGeneration)),
                "recordingCaptureState": .string(recordingCaptureState), "timelineDurationMS": .number(Double(timelineDurationMS)),
                "captureGaps": .array(captureGaps.map(\.variables)),
            ], as: Answer.self
        ).UpdateRecordingCapture
    }

    /// A nil `recordingTitle` keeps the title as it is; an empty one clears it, and the server names the recording again.
    /// A nil `recordedAt` clears the date, so a caller that only means to change the title sends the date it has.
    public func updateRecordingMetadata(recordingId: String, expectedSourceGeneration: Int64, recordingTitle: String?, recordedAt: String?)
        async throws -> Recording
    {
        struct Answer: Decodable, Sendable { let UpdateRecordingMetadata: Recording }
        return try await execute(
            Documents.updateRecordingMetadata,
            variables: [
                "recordingId": .string(recordingId), "expectedSourceGeneration": .number(Double(expectedSourceGeneration)),
                "recordingTitle": recordingTitle.map { .string($0) } ?? .null, "recordedAt": recordedAt.map { .string($0) } ?? .null,
            ], as: Answer.self
        ).UpdateRecordingMetadata
    }

    public func deleteRecording(_ recordingId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteRecording: Bool }
        _ = try await execute(Documents.deleteRecording, variables: ["recordingId": .string(recordingId)], as: Answer.self)
    }

    public func putRecordingPart(mediaId: String, partIndex: Int, content: Data, partSha256: String) async throws -> RecordingMediaPart {
        guard partIndex >= 0, !content.isEmpty, content.count <= 8 * 1024 * 1024, Self.isRecordingDigest(partSha256) else {
            throw TeaNodeError.unreadableAnswer
        }
        let receipt: RecordingMediaPart = try await recordingMediaRequest(
            mediaId: mediaId, suffix: "/parts/\(partIndex)", method: "PUT", content: content, partSha256: partSha256)
        guard receipt.confirmsPart(mediaId: mediaId, partIndex: partIndex, partSizeBytes: Int64(content.count), partSha256: partSha256) else {
            throw TeaNodeError.unreadableAnswer
        }
        return receipt
    }

    public func completeRecordingUpload(_ expected: RecordingMedia) async throws -> RecordingMedia {
        let receipt: RecordingMedia = try await recordingMediaRequest(
            mediaId: expected.id, suffix: "/complete", method: "POST", content: Data("{}".utf8))
        guard receipt.confirmsOriginal(expected) else { throw TeaNodeError.unreadableAnswer }
        return receipt
    }

    public func recordingPlaybackRequest(mediaId: String) async throws -> URLRequest {
        guard Self.isRecordingIdentifier(mediaId) else { throw TeaNodeError.unreadableAnswer }
        var request = URLRequest(url: account.server.endpoint("/api/v1/recordings/media/\(mediaId)"))
        request.setValue("Bearer \(try await accessToken())", forHTTPHeaderField: "Authorization")
        return request
    }

    private func recordingMediaRequest<Receipt: Decodable & Sendable>(
        mediaId: String, suffix: String, method: String, content: Data, partSha256: String = ""
    ) async throws -> Receipt {
        guard Self.isRecordingIdentifier(mediaId) else { throw TeaNodeError.unreadableAnswer }
        var request = URLRequest(url: account.server.endpoint("/api/v1/recordings/media/\(mediaId)\(suffix)"))
        request.httpMethod = method
        request.httpBody = content
        request.setValue(String(content.count), forHTTPHeaderField: "Content-Length")
        request.setValue(partSha256.isEmpty ? "application/json" : "application/octet-stream", forHTTPHeaderField: "Content-Type")
        if !partSha256.isEmpty { request.setValue(partSha256, forHTTPHeaderField: "X-Recording-Part-SHA256") }
        let (receiptData, response) = try await send(authorizing: request)
        if response.statusCode == 410 { throw TeaNodeError.recordingDeleted }
        guard receiptData.count <= 32 * 1024 else { throw TeaNodeError.unreadableAnswer }
        guard response.statusCode == 200 else {
            let refusalReason = (try? JSONDecoder().decode(RecordingRefusal.self, from: receiptData))?.error
            throw TeaNodeError.refused(refusalReason ?? "The recording request was refused. Keep the original and retry.")
        }
        return try JSONDecoder().decode(Receipt.self, from: receiptData)
    }

    public nonisolated static func isRecordingDigest(_ digest: String) -> Bool {
        digest.count == 64 && digest.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) }
    }

    public nonisolated static func isRecordingIdentifier(_ identifier: String) -> Bool {
        !identifier.isEmpty
            && identifier.utf8.allSatisfy { (48...57).contains($0) || (65...90).contains($0) || (97...122).contains($0) || $0 == 45 || $0 == 95 }
    }
}

private struct RecordingRefusal: Decodable { let error: String? }
