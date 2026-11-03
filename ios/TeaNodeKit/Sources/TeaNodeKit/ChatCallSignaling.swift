import Foundation

/// ChatCallSignalingCommand is a step the device takes with the media
/// server's peer connection.
public enum ChatCallSignalingCommand: Equatable, Sendable {
    /// Make the device's offer, wait for its addresses to be gathered, send
    /// it with JoinChatCallMedia, and set the answer that comes back.
    case join
    /// Set this offer of the server's as the remote description, make the
    /// answer and send it with AnswerChatCallMedia.
    case answer(offerSdp: String)
}

/// ChatCallSignaling is the order of a device's talk with the media server
/// (docs/subsystems/chat-calls.md, "A device on a call"). The device joins
/// first; the server's offers are answered one at a time and in order; and an
/// offer that comes before the join's answer has been set is kept until then.
/// It holds no connection: it says which step is next, and the one that owns
/// the connection takes it and tells it how it went.
public struct ChatCallSignaling: Equatable, Sendable {
    public enum Phase: Equatable, Sendable {
        /// Nothing has been done.
        case idle
        /// The join is under way: the offers that arrive wait.
        case joining
        /// Joined, and no offer is being answered.
        case ready
        /// An offer is being answered: the offers that arrive wait.
        case answering
        /// Something failed; nothing more is done.
        case failed
    }

    public private(set) var phase = Phase.idle
    /// The offers that have come and not been answered, oldest first.
    public private(set) var waitingOffers: [String] = []

    public init() {}

    /// The device begins: it joins.
    public mutating func start() -> [ChatCallSignalingCommand] {
        guard phase == .idle else { return [] }
        phase = .joining
        return [.join]
    }

    /// An offer from the server: answered at once when the device is joined
    /// and not answering another, kept otherwise.
    public mutating func receive(offer offerSdp: String) -> [ChatCallSignalingCommand] {
        switch phase {
        case .failed:
            return []
        case .idle, .joining, .answering:
            waitingOffers.append(offerSdp)
            return []
        case .ready:
            waitingOffers.append(offerSdp)
            return next()
        }
    }

    /// The join's answer has been set: the offers that came meanwhile are
    /// answered now, the oldest first.
    public mutating func joinAnswered() -> [ChatCallSignalingCommand] {
        guard phase == .joining else { return [] }
        phase = .ready
        return next()
    }

    /// The answer to the offer being answered has been sent: the next offer
    /// waiting is answered.
    public mutating func answerSent() -> [ChatCallSignalingCommand] {
        guard phase == .answering else { return [] }
        phase = .ready
        return next()
    }

    /// A step failed. The call goes no further.
    public mutating func fail() {
        phase = .failed
        waitingOffers = []
    }

    private mutating func next() -> [ChatCallSignalingCommand] {
        guard phase == .ready, !waitingOffers.isEmpty else { return [] }
        phase = .answering
        return [.answer(offerSdp: waitingOffers.removeFirst())]
    }
}

/// ChatCallSDP reads what the app needs out of a session description.
public enum ChatCallSDP {
    /// One media section of a description.
    public struct Section: Equatable, Sendable {
        /// audio or video.
        public let mediaKind: String
        public let mid: String
        /// The stream the section's track belongs to; the media server names
        /// it by the user id of whoever the track is from. Nil when the
        /// section has none (`a=msid:-`).
        public let streamId: String?
        /// sendrecv, sendonly, recvonly or inactive; sendrecv when the
        /// section does not say.
        public let direction: String

        /// Whether the side that wrote the description sends in this section.
        public var isSending: Bool { direction == "sendrecv" || direction == "sendonly" }
    }

    /// The media sections of a description, in order.
    public static func sections(in sdp: String) -> [Section] {
        var sections: [Section] = []
        var mediaKind: String?
        var mid = ""
        var streamId: String?
        var direction = "sendrecv"

        func finish() {
            if let mediaKind, !mid.isEmpty {
                sections.append(Section(mediaKind: mediaKind, mid: mid, streamId: streamId, direction: direction))
            }
        }

        // "\r\n" is one Character in Swift, which isNewline takes whole.
        let lines = sdp.split(whereSeparator: \.isNewline)
        for line in lines {
            if line.hasPrefix("m=") {
                finish()
                mediaKind = line.dropFirst(2).split(separator: " ").first.map(String.init)
                mid = ""
                streamId = nil
                direction = "sendrecv"
            } else if line.hasPrefix("a=mid:") {
                mid = String(line.dropFirst("a=mid:".count))
            } else if line.hasPrefix("a=msid:") {
                let stream = line.dropFirst("a=msid:".count).split(separator: " ").first.map(String.init)
                streamId = stream == "-" ? nil : stream
            } else if ["a=sendrecv", "a=sendonly", "a=recvonly", "a=inactive"].contains(String(line)) {
                direction = String(line.dropFirst(2))
            }
        }
        finish()
        return sections
    }
}
