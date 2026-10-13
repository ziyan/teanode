import Foundation

/// RecordingConversationBubble is what one speaker said before someone else spoke: consecutive passages joined.
public struct RecordingConversationBubble: Identifiable, Hashable, Sendable {
    /// The first passage's identifier.
    public let id: String
    public let segments: [RecordingTranscriptSegment]
    /// The name the person confirmed; nil while the speaker is only a label the transcription gave.
    public let confirmedSpeakerName: String?
    /// For an unconfirmed speaker, the place among the unconfirmed speakers in order of first speaking, from zero.
    public let unconfirmedSpeakerOrdinal: Int
    public let isAccountOwner: Bool
    public let startMS: Int64

    public var bubbleText: String { segments.map(\.displayedTranscriptText).joined(separator: " ") }
}

/// RecordingConversation turns the passages of a transcript into bubbles.
public enum RecordingConversation {
    public static func bubbles(_ segments: [RecordingTranscriptSegment]) -> [RecordingConversationBubble] {
        var bubbles: [RecordingConversationBubble] = []
        var currentKey: String?
        var currentSegments: [RecordingTranscriptSegment] = []
        var ordinalByKey: [String: Int] = [:]
        func flush() {
            guard let first = currentSegments.first, let currentKey else { return }
            bubbles.append(
                RecordingConversationBubble(
                    id: first.id, segments: currentSegments, confirmedSpeakerName: first.confirmedSpeakerName,
                    unconfirmedSpeakerOrdinal: ordinalByKey[currentKey] ?? 0, isAccountOwner: first.confirmedSpeakerName != nil && first.isAccountOwner == true,
                    startMS: first.segmentStartMS))
            currentSegments = []
        }
        for segment in segments {
            let key = speakerKey(segment)
            if key.hasPrefix("local:"), ordinalByKey[key] == nil {
                ordinalByKey[key] = ordinalByKey.count
            }
            if key != currentKey { flush() }
            currentKey = key
            currentSegments.append(segment)
        }
        flush()
        return bubbles
    }

    /// How many people spoke, counting a confirmed person once however many requests they spoke in.
    public static func speakerCount(_ segments: [RecordingTranscriptSegment]) -> Int {
        Set(segments.map(speakerKey)).count
    }

    /// "A" for the first unconfirmed speaker, then "B", and so on; past "Z" the number.
    public static func speakerLetter(ordinal: Int) -> String {
        guard ordinal >= 0, ordinal < 26 else { return String(ordinal + 1) }
        return String(Character(UnicodeScalar(UInt8(65 + ordinal))))
    }

    /// Who spoke, as far as the data goes: a confirmed person is one speaker across requests; a label the
    /// transcription gave means something only inside the request that gave it.
    private static func speakerKey(_ segment: RecordingTranscriptSegment) -> String {
        if segment.confirmedSpeakerName != nil, let identityId = segment.effectiveSpeakerIdentityId, !identityId.isEmpty {
            return "identity:" + identityId
        }
        return "local:" + segment.recordingTranscriptionAttemptId + ":" + segment.localSpeakerId
    }
}

/// RecordingInputLevel turns the recorder's decibel reading into the 0 to 1 a level meter draws.
public enum RecordingInputLevel {
    /// The quietest reading the meter shows; anything lower is the floor.
    public static let floorDecibels: Float = -50

    public static func normalized(decibels: Float) -> Double {
        guard decibels.isFinite else { return 0 }
        let clamped = min(0, max(floorDecibels, decibels))
        return Double((clamped - floorDecibels) / -floorDecibels)
    }
}
