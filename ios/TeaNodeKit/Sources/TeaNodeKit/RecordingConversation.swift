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
    /// For a speaker of a capture under way whom nobody has named: the label the timeline gives the speaker ("Speaker 1"),
    /// the same in every piece. Nil otherwise.
    public let temporarySpeakerLabel: String?
    /// Whether these are words just heard, which no speaker-separated transcript covers yet: shown without a speaker.
    public let isLive: Bool

    public var bubbleText: String { segments.map(\.displayedTranscriptText).joined(separator: " ") }
}

/// RecordingConversation turns the passages of a transcript into bubbles.
public enum RecordingConversation {
    public static func bubbles(_ segments: [RecordingTranscriptSegment]) -> [RecordingConversationBubble] {
        bubbles(segments, liveUtterances: [])
    }

    /// The passages of a transcript with words just heard between them in time. A live utterance has no speaker yet, so each
    /// one is a bubble of its own.
    public static func bubbles(_ segments: [RecordingTranscriptSegment], liveUtterances: [RecordingLiveUtterance]) -> [RecordingConversationBubble] {
        let passages = (segments.filter(hasWords) + liveUtterances.map(RecordingTranscriptSegment.init(liveUtterance:))).enumerated()
            .sorted { ($0.element.segmentStartMS, $0.offset) < ($1.element.segmentStartMS, $1.offset) }.map(\.element)
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
                    startMS: first.segmentStartMS, temporarySpeakerLabel: first.confirmedSpeakerName == nil ? first.speakerLabelOfTemporarySpeaker : nil,
                    isLive: first.isLiveUtterance))
            currentSegments = []
        }
        for (passageIndex, segment) in passages.enumerated() {
            var key = speakerKey(segment)
            // A word or two the diarization split off would otherwise become a bubble of its own, and two speakers answering in
            // turn at the same second make a bubble for each word: it stays in the bubble that is being built. A named person's
            // few words stand as their answer, unless they came in the middle of someone's sentence (it has not ended) and that
            // person goes straight on.
            if let currentKey, key != currentKey, isBackchannel(segment) {
                let following = passageIndex + 1 < passages.count ? passages[passageIndex + 1] : nil
                let isMidSentence = !(currentSegments.last.map { endsSentence($0.displayedTranscriptText) } ?? true)
                if segment.confirmedSpeakerName == nil || (isMidSentence && following.map(speakerKey) == currentKey) { key = currentKey }
            }
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

    /// How many words and how long a passage can be and still be taken as a word thrown in (diarization splits a few words spoken together into passages under a second), which stays in the
    /// bubble it came in the middle of.
    static let backchannelWordsMaximum = 3
    static let backchannelMillisecondsMaximum: Int64 = 800

    /// Whether this is a passage that is only a few words and a moment long.
    static func isBackchannel(_ segment: RecordingTranscriptSegment) -> Bool {
        guard !segment.isLiveUtterance else { return false }
        let wordCount = segment.displayedTranscriptText.split(whereSeparator: \.isWhitespace).count
        return wordCount <= backchannelWordsMaximum && segment.segmentEndMS - segment.segmentStartMS <= backchannelMillisecondsMaximum
    }

    /// Whether a passage's words end a sentence.
    static func endsSentence(_ text: String) -> Bool {
        guard let last = text.trimmingCharacters(in: .whitespacesAndNewlines).last else { return true }
        return ".?!。？！".contains(last)
    }

    /// How many people spoke, counting a confirmed person once however many requests they spoke in.
    public static func speakerCount(_ segments: [RecordingTranscriptSegment]) -> Int {
        Set(segments.filter(hasWords).map(speakerKey)).count
    }

    /// A passage with no words (a breath or a click a provider gave its own time) is not drawn: it would be an empty bubble. Nor
    /// is one of only filler ("Mm-hmm. Okay."), which the server marks: it says nothing a reader needs.
    static func hasWords(_ segment: RecordingTranscriptSegment) -> Bool {
        segment.isFiller != true && !segment.displayedTranscriptText.allSatisfy(\.isWhitespace)
    }

    /// "A" for the first unconfirmed speaker, then "B", and so on; past "Z" the number.
    public static func speakerLetter(ordinal: Int) -> String {
        guard ordinal >= 0, ordinal < 26 else { return String(ordinal + 1) }
        return String(Character(UnicodeScalar(UInt8(65 + ordinal))))
    }

    /// Who spoke, as far as the data goes: a confirmed person is one speaker across requests; a label the
    /// transcription gave means something only inside the request that gave it.
    private static func speakerKey(_ segment: RecordingTranscriptSegment) -> String {
        if segment.isLiveUtterance { return "live:" + segment.id }
        if segment.confirmedSpeakerName == nil, let label = segment.speakerLabelOfTemporarySpeaker { return "temporary:" + label }
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

/// RecordingLiveUtterance is what the live stream heard one person say before they stopped, kept on the recording's timeline
/// until the speaker-separated transcript of that stretch is done.
public struct RecordingLiveUtterance: Codable, Hashable, Sendable, Identifiable {
    public let utteranceId: String
    public let utteranceStartMS: Int64
    public let utteranceEndMS: Int64
    public let transcriptText: String
    public var id: String { utteranceId }

    public init(utteranceId: String, utteranceStartMS: Int64, utteranceEndMS: Int64, transcriptText: String) {
        self.utteranceId = utteranceId
        self.utteranceStartMS = utteranceStartMS
        self.utteranceEndMS = utteranceEndMS
        self.transcriptText = transcriptText
    }
}

extension RecordingTranscriptSegment {
    static let liveIdPrefix = "live:"

    /// A passage standing for words just heard, so the conversation draws them with the same bubble as a transcript.
    init(liveUtterance: RecordingLiveUtterance) {
        self.init(
            id: Self.liveIdPrefix + liveUtterance.utteranceId, recordingTranscriptRunId: "", recordingTranscriptionAttemptId: "",
            localSpeakerId: "live", segmentStartMS: liveUtterance.utteranceStartMS, segmentEndMS: liveUtterance.utteranceEndMS,
            transcriptText: liveUtterance.transcriptText)
    }

    /// Whether this stands for words just heard rather than a passage of a speaker-separated transcript.
    public var isLiveUtterance: Bool { recordingTranscriptRunId.isEmpty && id.hasPrefix(Self.liveIdPrefix) }

    /// The label of the temporary speaker this passage was matched to, or nil.
    public var speakerLabelOfTemporarySpeaker: String? {
        guard let temporarySpeakerLabel, !temporarySpeakerLabel.isEmpty else { return nil }
        return temporarySpeakerLabel
    }
}

/// RecordingLiveWords chooses which words just heard are shown beside the transcript of a capture under way.
public enum RecordingLiveWords {
    /// How much of an utterance a passage has to cover for the utterance to be in the transcript already.
    static let coveredFraction = 0.6
    /// How many heard utterances the phone keeps; older ones have long been covered by the transcript.
    public static let heardUtterancesKept = 100

    /// The utterances the server listed and those this phone heard on its own stream (the server may not have listed them yet),
    /// each once, without those a passage of the transcript covers and those before `windowStartMS`, in timeline order, and
    /// last the words still being said.
    public static func shown(
        serverUtterances: [RecordingLiveUtterance], heardUtterances: [RecordingLiveUtterance], partialText: String, partialStartMS: Int64,
        segments: [RecordingTranscriptSegment], windowStartMS: Int64 = 0
    ) -> [RecordingLiveUtterance] {
        var utterancesById: [String: RecordingLiveUtterance] = [:]
        for utterance in serverUtterances + heardUtterances where utterancesById[utterance.utteranceId] == nil {
            utterancesById[utterance.utteranceId] = utterance
        }
        var shown = utterancesById.values.filter { utterance in
            utterance.utteranceEndMS >= windowStartMS && !isCovered(utterance, by: segments)
                && !utterance.transcriptText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }.sorted { ($0.utteranceStartMS, $0.utteranceId) < ($1.utteranceStartMS, $1.utteranceId) }
        let partial = partialText.trimmingCharacters(in: .whitespacesAndNewlines)
        if !partial.isEmpty {
            shown.append(
                RecordingLiveUtterance(
                    utteranceId: "partial", utteranceStartMS: max(partialStartMS, shown.last?.utteranceEndMS ?? 0),
                    utteranceEndMS: max(partialStartMS, shown.last?.utteranceEndMS ?? 0), transcriptText: partial))
        }
        return shown
    }

    /// Whether passages of the transcript cover enough of the utterance's time for the speaker-separated words to stand in its
    /// place.
    public static func isCovered(_ utterance: RecordingLiveUtterance, by segments: [RecordingTranscriptSegment]) -> Bool {
        let durationMS = utterance.utteranceEndMS - utterance.utteranceStartMS
        guard durationMS > 0 else {
            return segments.contains { $0.segmentStartMS <= utterance.utteranceStartMS && utterance.utteranceStartMS < $0.segmentEndMS }
        }
        let coveredMS = segments.reduce(Int64(0)) { total, segment in
            total + max(0, min(segment.segmentEndMS, utterance.utteranceEndMS) - max(segment.segmentStartMS, utterance.utteranceStartMS))
        }
        return Double(min(coveredMS, durationMS)) >= Double(durationMS) * coveredFraction
    }

    /// `heard` with `utterance` added (replacing one with the same identifier), the oldest dropped past the number kept.
    public static func appending(_ utterance: RecordingLiveUtterance, to heard: [RecordingLiveUtterance]) -> [RecordingLiveUtterance] {
        var kept = heard.filter { $0.utteranceId != utterance.utteranceId }
        kept.append(utterance)
        return Array(kept.suffix(heardUtterancesKept))
    }
}

/// RecordingLevelHistory is the latest input levels of a recording under way, for the graph of bars on its screen: a fixed number,
/// the newest last, so the bars scroll from right to left.
public struct RecordingLevelHistory: Sendable, Equatable {
    public static let barCount = 48
    private var levels: [Double] = []

    public init() {}

    /// Adds the meter's reading in decibels; the oldest goes once there are more than `barCount`.
    public mutating func append(decibels: Float) {
        levels.append(RecordingInputLevel.normalized(decibels: decibels))
        if levels.count > Self.barCount { levels.removeFirst(levels.count - Self.barCount) }
    }

    /// `barCount` levels from 0 to 1, oldest first; a recording just begun has silence before its first reading.
    public var bars: [Double] {
        [Double](repeating: 0, count: Self.barCount - levels.count) + levels
    }
}

/// RecordingLiveSession says when the live stream is closed on purpose and opened again, before the server's limit does it in the
/// middle of someone speaking.
public enum RecordingLiveSession {
    /// The server ends a live stream after 30 minutes.
    public static let recycleAfterSeconds: TimeInterval = 25 * 60
    public static func shouldRecycle(sessionAgeSeconds: TimeInterval) -> Bool { sessionAgeSeconds >= recycleAfterSeconds }

}
