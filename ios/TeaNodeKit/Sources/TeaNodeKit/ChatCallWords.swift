import Foundation

// What the screens say of a call, worked out from what the server sent and
// kept apart from the screens so that it is tested here. The app words each
// kind in the person's language (the String Catalog); `englishText` is the
// English it starts from.

/// ChatCallDuration writes how long a call went.
public enum ChatCallDuration {
    /// "4:12", and from an hour "1:04:12". A part of a second is dropped.
    public static func text(milliseconds: Int64) -> String {
        let totalSeconds = max(0, milliseconds) / 1000
        let seconds = totalSeconds % 60
        let minutes = (totalSeconds / 60) % 60
        let hours = totalSeconds / 3600
        let secondsText = seconds < 10 ? "0\(seconds)" : "\(seconds)"
        if hours > 0 {
            let minutesText = minutes < 10 ? "0\(minutes)" : "\(minutes)"
            return "\(hours):\(minutesText):\(secondsText)"
        }
        return "\(minutes):\(secondsText)"
    }
}

/// ChatCallLineKind is what a call's line in a chat says.
public enum ChatCallLineKind: Equatable, Sendable {
    /// The call goes now.
    case going
    /// It was answered and has ended, after the time given ("4:12").
    case ended(durationText: String)
    /// Nobody answered, for somebody it rang.
    case missed
    /// Nobody answered, for whoever started it.
    case noAnswer
    case declined
    /// The server stopped under it.
    case failed

    /// The line in English: "Call, 4:12", "Call going", "Missed call",
    /// "No answer", "Declined call" or "Call failed".
    public var englishText: String {
        switch self {
        case .going: "Call going"
        case .ended(let durationText): "Call, \(durationText)"
        case .missed: "Missed call"
        case .noAnswer: "No answer"
        case .declined: "Declined call"
        case .failed: "Call failed"
        }
    }

    /// The kind for a call that has the facts given. A call that ended with
    /// nobody answering it (the person who started it hung up while it rang)
    /// reads as missed or as no answer, not as a call of no length.
    public static func of(endReason: String, isGoing: Bool, durationMs: Int64, isStartedByPerson: Bool, isAnswered: Bool) -> ChatCallLineKind {
        if isGoing { return .going }
        switch endReason {
        case "missed": return isStartedByPerson ? .noAnswer : .missed
        case "declined": return .declined
        case "failed": return .failed
        default:
            guard isAnswered else { return isStartedByPerson ? .noAnswer : .missed }
            return .ended(durationText: ChatCallDuration.text(milliseconds: durationMs))
        }
    }
}

/// ChatCallLine is what a call's line in a chat, and the banner of a chat with
/// a call going, say: from the call as a message carries it, or from the
/// newest view of it the app has heard.
public struct ChatCallLine: Equatable, Sendable {
    public let callId: String
    public let kind: ChatCallLineKind
    /// How many people are on the call now.
    public let onCount: Int
    public let isGoing: Bool
    /// Whether the call has a transcript to read.
    public let isTranscriptDone: Bool

    public init(callId: String, kind: ChatCallLineKind, onCount: Int, isGoing: Bool, isTranscriptDone: Bool) {
        self.callId = callId
        self.kind = kind
        self.onCount = onCount
        self.isGoing = isGoing
        self.isTranscriptDone = isTranscriptDone
    }

    /// From the newest view of the call.
    public init(view: ChatCallView, ownUserId: String?) {
        self.init(
            callId: view.id,
            kind: ChatCallLineKind.of(
                endReason: view.endReason, isGoing: view.isGoing, durationMs: view.durationMs,
                isStartedByPerson: view.isStarted(by: ownUserId), isAnswered: view.answeredAt != nil),
            onCount: view.onCount, isGoing: view.isGoing, isTranscriptDone: view.transcriptState == "done")
    }

    /// From the call as the message carries it.
    public init(record: ChatCallRecord, ownUserId: String?, now: Date = Date()) {
        let isStartedByPerson = ownUserId != nil && record.startedByUserId == ownUserId
        self.init(
            callId: record.id,
            kind: ChatCallLineKind.of(
                endReason: record.endReason, isGoing: record.isGoing, durationMs: record.durationMs(now: now),
                isStartedByPerson: isStartedByPerson, isAnswered: record.answeredAt != nil),
            onCount: record.onCount, isGoing: record.isGoing, isTranscriptDone: record.transcriptState == "done")
    }
}

/// ChatCallTranscriptionStatus is what the call screen says of a transcript:
/// whether the call is being transcribed, and who turned it on. Turning it on
/// transcribes everybody on the call; nobody is asked, everybody is told.
public enum ChatCallTranscriptionStatus: Equatable, Sendable {
    /// Nobody turned it on.
    case notAsked
    /// Somebody turned it on: "Mira turned on transcription", or "You turned
    /// on transcription" when it was the person.
    case transcribing(askerName: String, isAskedByPerson: Bool)

    public init(view: ChatCallView, ownUserId: String?) {
        guard let askerId = view.transcriptionAskedByUserId, !askerId.isEmpty, view.isGoing else {
            self = .notAsked
            return
        }
        self = .transcribing(askerName: view.member(userId: askerId)?.name ?? "", isAskedByPerson: askerId == ownUserId)
    }

    /// Whether the transcribe button is offered: nobody has asked yet.
    public var canAsk: Bool { self == .notAsked }
}

/// ChatCallGrid lays the people on a call out.
public enum ChatCallGrid {
    /// How many columns the tiles of the people are laid in: one or two
    /// people fill the screen one above the other, up to six are two across,
    /// and more are three across.
    public static func columnCount(tileCount: Int) -> Int {
        if tileCount <= 2 { return 1 }
        if tileCount <= 6 { return 2 }
        return 3
    }

    /// How many rows the tiles take.
    public static func rowCount(tileCount: Int) -> Int {
        guard tileCount > 0 else { return 0 }
        let columns = columnCount(tileCount: tileCount)
        return (tileCount + columns - 1) / columns
    }
}
