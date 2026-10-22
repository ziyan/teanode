import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

// Sharing a recording with a person in a circle, or with a whole circle, at a
// level, and reading the ones shared with the person. The dashboard, the
// command line and the agent's recording tool use the same operations and the
// same two words for the levels. Neither level gives the audio, a voice sample
// or the contact a speaker's name is tied to.

/// RecordingShareLevel is how much of a recording somebody may read.
public enum RecordingShareLevel: String, CaseIterable, Sendable {
    /// The title, the date, the length, the summary and the names of the speakers the owner named.
    case summary
    /// The summary, and the words, with the owner's corrections.
    case transcript

    /// The levels a recording can be shared at, least to most.
    public static var grantable: [RecordingShareLevel] { [.summary, .transcript] }

    /// Whether the words of the recording are shown.
    public var canReadTranscript: Bool { self == .transcript }
}

/// RecordingShare is one share of the person's recording, with one person or every member of one circle.
public struct RecordingShare: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let recordingId: String
    public let granteeUserId: String?
    public let granteeUsername: String?
    public let granteeName: String?
    public let granteeCircleId: String?
    public let granteeCircleName: String?
    /// summary or transcript.
    public let recordingShareLevel: String

    public var level: RecordingShareLevel? { RecordingShareLevel(rawValue: recordingShareLevel) }

    /// Whether it is with a circle rather than a person.
    public var isWithCircle: Bool { granteeCircleId != nil }

    /// Who it is with: the person's name, or the username when they have no name, or the circle's name.
    public var granteeDisplayName: String {
        if isWithCircle { return granteeCircleName ?? "" }
        if let granteeName, !granteeName.isEmpty { return granteeName }
        return granteeUsername ?? ""
    }
}

/// SharedRecordingPassage is one passage of a transcript as the person it was shared with sees it.
public struct SharedRecordingPassage: Codable, Hashable, Sendable {
    public let segmentStartMS: Int64
    public let segmentEndMS: Int64
    /// The name the owner confirmed, else the label the passage has in the owner's own transcript.
    public let speakerName: String
    public let transcriptText: String
    /// A passage of only filler ("Mm-hmm. Okay."), which the owner's screens leave out and so does this one.
    public let isFiller: Bool
}

/// SharedRecordingTurn is the passages of one speaker in a row, read as one bubble.
public struct SharedRecordingTurn: Identifiable, Hashable, Sendable {
    public let id: Int
    public let speakerName: String
    public let startMS: Int64
    public let text: String

    /// The passages as turns: filler and empty passages left out, and the passages of one speaker in a row together.
    public static func turns(of passages: [SharedRecordingPassage]) -> [SharedRecordingTurn] {
        var turns: [SharedRecordingTurn] = []
        for passage in passages where !passage.isFiller {
            let text = passage.transcriptText.trimmingCharacters(in: .whitespacesAndNewlines)
            if text.isEmpty { continue }
            if let last = turns.last, last.speakerName == passage.speakerName {
                turns[turns.count - 1] = SharedRecordingTurn(
                    id: last.id, speakerName: last.speakerName, startMS: last.startMS, text: last.text + " " + text)
            } else {
                turns.append(SharedRecordingTurn(id: turns.count, speakerName: passage.speakerName, startMS: passage.segmentStartMS, text: text))
            }
        }
        return turns
    }
}

/// SharedRecording is a recording as the person it was shared with sees it, at the level the share gives. A listing
/// leaves out the speaker names and the passages; a read of one has them.
public struct SharedRecording: Codable, Identifiable, Hashable, Sendable {
    public let recordingId: String
    public let ownerUserId: String
    public let ownerUsername: String
    public let ownerName: String
    public let recordingTitle: String
    public let createdAt: String
    public let recordedAt: String?
    public let timelineDurationMS: Int64
    public let recordingSummary: String
    /// summary or transcript.
    public let recordingShareLevel: String
    public var speakerNames: [String]? = nil
    public var passages: [SharedRecordingPassage]? = nil
    public var passageCount: Int? = nil
    public var hasMorePassages: Bool? = nil

    public var id: String { recordingId }

    public var level: RecordingShareLevel? { RecordingShareLevel(rawValue: recordingShareLevel) }

    /// Who shared it: their name, or the username when they have no name.
    public var ownerDisplayName: String { ownerName.isEmpty ? ownerUsername : ownerName }

    /// The loaded page of the transcript as turns.
    public var turns: [SharedRecordingTurn] { SharedRecordingTurn.turns(of: passages ?? []) }
}

extension Documents {
    static let recordingShareFields = """
        id recordingId granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName recordingShareLevel
        """
    static let sharedRecordingListingFields = """
        recordingId ownerUserId ownerUsername ownerName recordingTitle createdAt recordedAt timelineDurationMS recordingSummary
        recordingShareLevel
        """
    public static let recordingShares = """
        query ($recordingId: String!) { ListRecordingShares(recordingId: $recordingId) { \(recordingShareFields) } }
        """
    public static let shareRecording = """
        mutation ($recordingId: String!, $granteeUserId: String, $granteeCircleId: String, $recordingShareLevel: String!) {
          ShareRecording(recordingId: $recordingId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId,
            recordingShareLevel: $recordingShareLevel) { id }
        }
        """
    public static let unshareRecording = """
        mutation ($recordingShareId: String!) { UnshareRecording(recordingShareId: $recordingShareId) }
        """
    public static let sharedRecordings = """
        query ($beforeRecordingId: String, $pageSize: Int!) {
          ListSharedRecordings(beforeRecordingId: $beforeRecordingId, pageSize: $pageSize) { \(sharedRecordingListingFields) }
        }
        """
    public static let sharedRecording = """
        query ($recordingId: String!, $passageOffset: Int!, $pageSize: Int!) {
          ReadSharedRecording(recordingId: $recordingId, passageOffset: $passageOffset, pageSize: $pageSize) {
            \(sharedRecordingListingFields) speakerNames passages { segmentStartMS segmentEndMS speakerName transcriptText isFiller }
            passageCount hasMorePassages
          }
        }
        """

    /// The documents of sharing a recording, for the server's schema check.
    public static let recordingSharing: [String: String] = [
        "recordingShares": recordingShares, "shareRecording": shareRecording, "unshareRecording": unshareRecording,
        "sharedRecordings": sharedRecordings, "sharedRecording": sharedRecording,
    ]
}

extension Connection {
    /// Who the person's recording is shared with, and at what level.
    public func recordingShares(recordingId: String) async throws -> [RecordingShare] {
        struct Answer: Decodable, Sendable { let ListRecordingShares: [RecordingShare] }
        return try await execute(Documents.recordingShares, variables: ["recordingId": .string(recordingId)], as: Answer.self).ListRecordingShares
    }

    /// Shares the person's recording with a person or a circle (one of the two identifiers), or changes the level of a
    /// share already made.
    public func shareRecording(recordingId: String, granteeUserId: String?, granteeCircleId: String?, level: RecordingShareLevel) async throws {
        struct Answer: Decodable, Sendable {
            struct Shared: Decodable, Sendable { let id: String }
            let ShareRecording: Shared
        }
        _ = try await execute(
            Documents.shareRecording,
            variables: [
                "recordingId": .string(recordingId), "granteeUserId": granteeUserId.map { .string($0) } ?? .null,
                "granteeCircleId": granteeCircleId.map { .string($0) } ?? .null, "recordingShareLevel": .string(level.rawValue),
            ], as: Answer.self)
    }

    /// Takes a share back.
    public func unshareRecording(shareId: String) async throws {
        struct Answer: Decodable, Sendable { let UnshareRecording: Bool? }
        _ = try await execute(Documents.unshareRecording, variables: ["recordingShareId": .string(shareId)], as: Answer.self)
    }

    /// The recordings other people shared with the person, newest first.
    public func sharedRecordings(beforeRecordingId: String? = nil, pageSize: Int = 50) async throws -> [SharedRecording] {
        struct Answer: Decodable, Sendable { let ListSharedRecordings: [SharedRecording] }
        var variables: [String: JSONValue] = ["pageSize": .number(Double(pageSize))]
        if let beforeRecordingId { variables["beforeRecordingId"] = .string(beforeRecordingId) }
        return try await execute(Documents.sharedRecordings, variables: variables, as: Answer.self).ListSharedRecordings
    }

    /// One recording shared with the person, as far as its level goes, with a page of the transcript at the transcript level.
    public func sharedRecording(recordingId: String, passageOffset: Int = 0, pageSize: Int = 50) async throws -> SharedRecording {
        struct Answer: Decodable, Sendable { let ReadSharedRecording: SharedRecording }
        return try await execute(
            Documents.sharedRecording,
            variables: [
                "recordingId": .string(recordingId), "passageOffset": .number(Double(passageOffset)), "pageSize": .number(Double(pageSize)),
            ], as: Answer.self
        ).ReadSharedRecording
    }
}
