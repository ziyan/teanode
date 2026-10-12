import Foundation

/// RecordingLink keeps a citation on its original version and absolute passage interval.
public struct RecordingLink: Hashable, Sendable, Identifiable {
    public let recordingId: String
    public let recordingTranscriptRunId: String?
    public let passageStartMS: Int64?
    public let passageEndMS: Int64?
    public var id: String {
        [recordingId, recordingTranscriptRunId ?? "", passageStartMS.map(String.init) ?? "", passageEndMS.map(String.init) ?? ""].joined(
            separator: "/")
    }

    public init?(_ address: URL, server: ServerAddress) {
        guard address.absoluteString.utf8.count <= 4096,
            let components = URLComponents(url: address, resolvingAgainstBaseURL: false), components.user == nil, components.password == nil
        else {
            return nil
        }
        if components.scheme != nil || components.host != nil {
            guard let origin = try? ServerAddress(parsing: address.absoluteString), origin == server else { return nil }
        }
        let segments = components.path.split(separator: "/", omittingEmptySubsequences: false)
        guard segments.count == 3, segments[0].isEmpty, segments[1] == "recordings",
            Connection.isRecordingIdentifier(String(segments[2]))
        else { return nil }
        let queryItems = components.queryItems ?? []
        let keys = ["version", "run", "startMS", "endMS"]
        guard keys.allSatisfy({ key in queryItems.filter { $0.name == key }.count <= 1 }),
            !queryItems.contains(where: { ["version", "run"].contains($0.name) && $0.value == nil })
        else { return nil }
        let version = queryItems.first { $0.name == "version" }?.value
        let legacyRun = queryItems.first { $0.name == "run" }?.value
        if let version, let legacyRun, version != legacyRun { return nil }
        let transcriptRunId = version ?? legacyRun
        if let transcriptRunId, !Connection.isRecordingIdentifier(transcriptRunId) { return nil }
        let start = queryItems.first { $0.name == "startMS" }
        let end = queryItems.first { $0.name == "endMS" }
        if start != nil || end != nil {
            guard transcriptRunId != nil, let startText = start?.value, let endText = end?.value,
                let startMS = Int64(startText), let endMS = Int64(endText), startMS >= 0, endMS > startMS
            else { return nil }
            passageStartMS = startMS
            passageEndMS = endMS
        } else {
            passageStartMS = nil
            passageEndMS = nil
        }
        recordingId = String(segments[2])
        recordingTranscriptRunId = transcriptRunId
    }

    public func includes(_ segment: RecordingTranscriptSegment) -> Bool {
        guard let passageStartMS, let passageEndMS else { return true }
        return segment.segmentEndMS > passageStartMS && segment.segmentStartMS < passageEndMS
    }
}

extension Documents {
    public static let readRecordingTranscriptPassageOffset = """
        query ($recordingTranscriptRunId: String!, $passageStartMS: Int64!, $passageEndMS: Int64!) {
            ReadRecordingTranscriptPassageOffset(recordingTranscriptRunId: $recordingTranscriptRunId,
                passageStartMS: $passageStartMS, passageEndMS: $passageEndMS)
        }
        """
}

extension Connection {
    public func recordingTranscriptPassageOffset(recordingTranscriptRunId: String, passageStartMS: Int64, passageEndMS: Int64) async throws -> Int {
        guard passageStartMS >= 0, passageEndMS > passageStartMS else { throw TeaNodeError.unreadableAnswer }
        struct Answer: Decodable, Sendable { let ReadRecordingTranscriptPassageOffset: Int }
        let offset = try await execute(
            Documents.readRecordingTranscriptPassageOffset,
            variables: [
                "recordingTranscriptRunId": .string(recordingTranscriptRunId), "passageStartMS": .number(Double(passageStartMS)),
                "passageEndMS": .number(Double(passageEndMS)),
            ], as: Answer.self
        ).ReadRecordingTranscriptPassageOffset
        guard offset >= 0 else { throw TeaNodeError.unreadableAnswer }
        return offset
    }
}
