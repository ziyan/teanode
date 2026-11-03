import Foundation

// Calls in chats: a person in a chat with people calls it, the others are
// rung, and whoever answers talks through the server's media server. The
// operations are in internal/api/v1api/apigraph/chat_call.go and
// docs/subsystems/chat-calls.md tells how a device goes on a call. This file
// is what the server answers, as the app reads it, and the operations. The
// feeds are in ChatCallFeed.swift, the order of the signaling in
// ChatCallSignaling.swift, and the words in ChatCallWords.swift.

// MARK: What the server answers

/// ChatCallMemberView is one person's part in a call.
public struct ChatCallMemberView: Codable, Hashable, Sendable, Identifiable {
    public var userId: String
    public var name: String
    public var avatarUrl: String?
    public var avatarColorIndex: Int
    /// ringing, answered, declined, missed, or none for whoever started it.
    public var ringState: String
    /// Whether they are on the call now.
    public var isOn: Bool
    public var isVideoOn: Bool
    public var joinedAt: String?
    public var leftAt: String?

    public var id: String { userId }

    private enum CodingKeys: String, CodingKey {
        case userId, name, avatarUrl, avatarColorIndex, ringState, isOn, isVideoOn, joinedAt, leftAt
    }
}

extension ChatCallMemberView {
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        userId = try container.decode(String.self, forKey: .userId)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        avatarUrl = try container.decodeIfPresent(String.self, forKey: .avatarUrl)
        avatarColorIndex = try container.decodeIfPresent(Int.self, forKey: .avatarColorIndex) ?? 0
        ringState = try container.decodeIfPresent(String.self, forKey: .ringState) ?? ""
        isOn = try container.decodeIfPresent(Bool.self, forKey: .isOn) ?? false
        isVideoOn = try container.decodeIfPresent(Bool.self, forKey: .isVideoOn) ?? false
        joinedAt = try container.decodeIfPresent(String.self, forKey: .joinedAt)
        leftAt = try container.decodeIfPresent(String.self, forKey: .leftAt)
    }
}

/// ChatCallView is a call as the person asking sees it: what the server's
/// ChatCallView says, which is also what ChatCallEvents tells of each change.
public struct ChatCallView: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    /// The person's own conversation for the chat, the chat's name as they
    /// see it, and whether it is a group.
    public var conversationId: String
    public var chatTitle: String
    public var isGroup: Bool
    public var startedByUserId: String?
    public var startedByName: String
    public var startedAt: String
    public var answeredAt: String?
    public var endedAt: String?
    /// ended, missed, declined or failed; empty while the call goes.
    public var endReason: String
    public var isGoing: Bool
    /// How long it went from the first answer, to its end or to now.
    public var durationMs: Int64
    /// The person's part: ringing, answered, declined, missed, or none for
    /// whoever started it; empty when they had none. isAskerOn says they are
    /// on it now.
    public var askerRingState: String
    public var isAskerOn: Bool
    /// Who asked for a transcript (nil when nobody did), how it stands
    /// (waiting, done or failed, empty before the call ends), and the
    /// summary's message in the chat once there is one.
    public var transcriptionAskedByUserId: String?
    public var transcriptState: String
    public var summaryMessageId: String?
    public var members: [ChatCallMemberView]

    private enum CodingKeys: String, CodingKey {
        case id, conversationId, chatTitle, isGroup, startedByUserId, startedByName, startedAt, answeredAt, endedAt, endReason
        case isGoing, durationMs, askerRingState, isAskerOn, transcriptionAskedByUserId, transcriptState, summaryMessageId, members
    }
}

extension ChatCallView {
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        conversationId = try container.decodeIfPresent(String.self, forKey: .conversationId) ?? ""
        chatTitle = try container.decodeIfPresent(String.self, forKey: .chatTitle) ?? ""
        isGroup = try container.decodeIfPresent(Bool.self, forKey: .isGroup) ?? false
        startedByUserId = try container.decodeIfPresent(String.self, forKey: .startedByUserId)
        startedByName = try container.decodeIfPresent(String.self, forKey: .startedByName) ?? ""
        startedAt = try container.decodeIfPresent(String.self, forKey: .startedAt) ?? ""
        answeredAt = try container.decodeIfPresent(String.self, forKey: .answeredAt)
        endedAt = try container.decodeIfPresent(String.self, forKey: .endedAt)
        endReason = try container.decodeIfPresent(String.self, forKey: .endReason) ?? ""
        isGoing = try container.decodeIfPresent(Bool.self, forKey: .isGoing) ?? false
        durationMs = try container.decodeIfPresent(Int64.self, forKey: .durationMs) ?? 0
        askerRingState = try container.decodeIfPresent(String.self, forKey: .askerRingState) ?? ""
        isAskerOn = try container.decodeIfPresent(Bool.self, forKey: .isAskerOn) ?? false
        transcriptionAskedByUserId = try container.decodeIfPresent(String.self, forKey: .transcriptionAskedByUserId)
        transcriptState = try container.decodeIfPresent(String.self, forKey: .transcriptState) ?? ""
        summaryMessageId = try container.decodeIfPresent(String.self, forKey: .summaryMessageId)
        members = try container.decodeIfPresent([ChatCallMemberView].self, forKey: .members) ?? []
    }

    public var startedDate: Date? { ServerTime.date(startedAt) }
    public var answeredDate: Date? { ServerTime.date(answeredAt) }
    public var endedDate: Date? { ServerTime.date(endedAt) }

    /// How many people are on the call now.
    public var onCount: Int { members.filter(\.isOn).count }

    /// The people on the call now.
    public var onMembers: [ChatCallMemberView] { members.filter(\.isOn) }

    public func member(userId: String) -> ChatCallMemberView? {
        members.first { $0.userId == userId }
    }

    /// Whether the person who started the call is the one asking.
    public func isStarted(by userId: String?) -> Bool {
        guard let userId, !userId.isEmpty else { return askerRingState == "none" }
        return startedByUserId == userId
    }
}

/// ChatCallTranscriptSegment is one line of a transcribed call under its
/// speaker, timed from the start of the call.
public struct ChatCallTranscriptSegment: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var callId: String
    public var userId: String?
    public var speakerName: String
    public var startMs: Int64
    public var endMs: Int64
    public var text: String

    private enum CodingKeys: String, CodingKey {
        case id, callId, userId, speakerName, startMs, endMs, text
    }
}

extension ChatCallTranscriptSegment {
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        callId = try container.decodeIfPresent(String.self, forKey: .callId) ?? ""
        userId = try container.decodeIfPresent(String.self, forKey: .userId)
        speakerName = try container.decodeIfPresent(String.self, forKey: .speakerName) ?? ""
        startMs = try container.decodeIfPresent(Int64.self, forKey: .startMs) ?? 0
        endMs = try container.decodeIfPresent(Int64.self, forKey: .endMs) ?? 0
        text = try container.decodeIfPresent(String.self, forKey: .text) ?? ""
    }

    /// Where in the call the line was said, as "4:12".
    public var startText: String { ChatCallDuration.text(milliseconds: startMs) }
}

/// ChatCallDetailView is a call with its transcript.
public struct ChatCallDetailView: Codable, Hashable, Sendable {
    public var call: ChatCallView
    public var transcript: [ChatCallTranscriptSegment]
    /// The recording the call became, when the person may open it: their
    /// own (in Recordings), or one shared with them.
    public var recordingId: String?
    public var isRecordingOwned: Bool

    private enum CodingKeys: String, CodingKey {
        case call, transcript, recordingId, isRecordingOwned
    }
}

extension ChatCallDetailView {
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        call = try container.decode(ChatCallView.self, forKey: .call)
        transcript = try container.decodeIfPresent([ChatCallTranscriptSegment].self, forKey: .transcript) ?? []
        recordingId = try container.decodeIfPresent(String.self, forKey: .recordingId)
        isRecordingOwned = try container.decodeIfPresent(Bool.self, forKey: .isRecordingOwned) ?? false
    }
}

/// ChatCallSignalView is an offer the media server makes the device.
public struct ChatCallSignalView: Codable, Hashable, Sendable {
    public var offerSdp: String
}

/// ChatCallMemberRecord is one person's part in a call as a chat's message
/// carries it (`call` on a message): the call as it stood when the message
/// was read, without the names that ChatCallView adds.
public struct ChatCallMemberRecord: Codable, Hashable, Sendable {
    public var userId: String
    public var ringState: String
    public var joinedAt: String?
    public var leftAt: String?
    public var isVideoOn: Bool

    private enum CodingKeys: String, CodingKey {
        case userId, ringState, joinedAt, leftAt, isVideoOn
    }
}

extension ChatCallMemberRecord {
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        userId = try container.decode(String.self, forKey: .userId)
        ringState = try container.decodeIfPresent(String.self, forKey: .ringState) ?? ""
        joinedAt = try container.decodeIfPresent(String.self, forKey: .joinedAt)
        leftAt = try container.decodeIfPresent(String.self, forKey: .leftAt)
        isVideoOn = try container.decodeIfPresent(Bool.self, forKey: .isVideoOn) ?? false
    }

    /// Whether they are on the call: joined, and not left since.
    public var isOn: Bool { joinedAt != nil && leftAt == nil }
}

/// ChatCallRecord is the call a message in a chat is the line of, as the
/// message carries it. It does not change after the message is read: the
/// app keeps the latest ChatCallView of each call it hears of, and draws the
/// line from that when it has one.
public struct ChatCallRecord: Codable, Hashable, Sendable {
    public var id: String
    public var startedByUserId: String?
    public var startedAt: String
    public var answeredAt: String?
    public var endedAt: String?
    public var endReason: String
    public var transcriptionAskedByUserId: String?
    public var transcriptState: String
    public var summaryMessageId: String?
    public var members: [ChatCallMemberRecord]

    private enum CodingKeys: String, CodingKey {
        case id, startedByUserId, startedAt, answeredAt, endedAt, endReason, transcriptionAskedByUserId, transcriptState, summaryMessageId, members
    }
}

extension ChatCallRecord {
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        startedByUserId = try container.decodeIfPresent(String.self, forKey: .startedByUserId)
        startedAt = try container.decodeIfPresent(String.self, forKey: .startedAt) ?? ""
        answeredAt = try container.decodeIfPresent(String.self, forKey: .answeredAt)
        endedAt = try container.decodeIfPresent(String.self, forKey: .endedAt)
        endReason = try container.decodeIfPresent(String.self, forKey: .endReason) ?? ""
        transcriptionAskedByUserId = try container.decodeIfPresent(String.self, forKey: .transcriptionAskedByUserId)
        transcriptState = try container.decodeIfPresent(String.self, forKey: .transcriptState) ?? ""
        summaryMessageId = try container.decodeIfPresent(String.self, forKey: .summaryMessageId)
        members = try container.decodeIfPresent([ChatCallMemberRecord].self, forKey: .members) ?? []
    }

    /// Whether the call has not ended.
    public var isGoing: Bool { endedAt == nil }

    /// How many people are on the call now.
    public var onCount: Int { members.filter(\.isOn).count }

    /// How long the call went from its first answer, to its end or to now.
    /// Zero for a call nobody answered.
    public func durationMs(now: Date = Date()) -> Int64 {
        guard let answered = ServerTime.date(answeredAt) else { return 0 }
        let end = ServerTime.date(endedAt) ?? now
        return max(0, Int64(end.timeIntervalSince(answered) * 1000))
    }

    /// The person's part: ringing, answered, declined, missed, or none for
    /// whoever started it; empty when they had none.
    public func ringState(of userId: String?) -> String {
        guard let userId else { return "" }
        return members.first { $0.userId == userId }?.ringState ?? ""
    }
}

// MARK: The documents

extension Documents {
    /// What every read of a call answers with. Written out in full, since the
    /// server's test of the app's documents fills in one level of shared
    /// fields.
    static let chatCallFields = """
        id conversationId chatTitle isGroup startedByUserId startedByName startedAt answeredAt endedAt endReason isGoing durationMs
        askerRingState isAskerOn transcriptionAskedByUserId transcriptState summaryMessageId
        members { userId name avatarUrl avatarColorIndex ringState isOn isVideoOn joinedAt leftAt }
        """

    public static let startChatCall = """
        mutation ($conversationId: String!, $isVideoOn: Boolean) {
          StartChatCall(conversationId: $conversationId, isVideoOn: $isVideoOn) { \(chatCallFields) }
        }
        """
    public static let joinChatCall = """
        mutation ($callId: String!, $agentPhoneId: String) { JoinChatCall(callId: $callId, agentPhoneId: $agentPhoneId) { \(chatCallFields) } }
        """
    public static let declineChatCall = """
        mutation ($callId: String!, $agentPhoneId: String) {
          DeclineChatCall(callId: $callId, agentPhoneId: $agentPhoneId) { \(chatCallFields) }
        }
        """
    public static let leaveChatCall = """
        mutation ($callId: String!) { LeaveChatCall(callId: $callId) { \(chatCallFields) } }
        """
    public static let joinChatCallMedia = """
        mutation ($callId: String!, $offerSdp: String!) { JoinChatCallMedia(callId: $callId, offerSdp: $offerSdp) { answerSdp } }
        """
    public static let answerChatCallMedia = """
        mutation ($callId: String!, $answerSdp: String!) { AnswerChatCallMedia(callId: $callId, answerSdp: $answerSdp) }
        """
    public static let setChatCallVideo = """
        mutation ($callId: String!, $isVideoOn: Boolean!) {
          SetChatCallVideo(callId: $callId, isVideoOn: $isVideoOn) { \(chatCallFields) }
        }
        """
    public static let askChatCallTranscription = """
        mutation ($callId: String!) { AskChatCallTranscription(callId: $callId) { \(chatCallFields) } }
        """
    public static let listGoingChatCalls = """
        query { ListGoingChatCalls { \(chatCallFields) } }
        """
    public static let readGoingChatCall = """
        query ($conversationId: String!) { ReadGoingChatCall(conversationId: $conversationId) { \(chatCallFields) } }
        """
    public static let listChatCalls = """
        query ($conversationId: String!, $first: Int, $before: DateTime) {
          ListChatCalls(conversationId: $conversationId, first: $first, before: $before) { \(chatCallFields) }
        }
        """
    public static let readChatCall = """
        query ($callId: String!) {
          ReadChatCall(callId: $callId) {
            call { \(chatCallFields) }
            transcript { id callId userId speakerName startMs endMs text }
            recordingId isRecordingOwned
          }
        }
        """
    public static let chatCallEvents = """
        subscription { ChatCallEvents { \(chatCallFields) } }
        """
    public static let chatCallSignals = """
        subscription ($callId: String!) { ChatCallSignals(callId: $callId) { offerSdp } }
        """
    public static let updatePhoneRinging = """
        mutation ($phoneId: String!, $voipPushRegistration: String) {
          UpdateAgentPhone(phoneId: $phoneId, voipPushRegistration: $voipPushRegistration) { id notificationKinds canBeNotified }
        }
        """
}

// MARK: The operations

extension Connection {
    /// Starts a call in the chat, ringing everybody else in it. When a call is
    /// going in the chat already, it is that one, to join.
    public func startChatCall(conversationId: String, isVideoOn: Bool = false) async throws -> ChatCallView {
        struct Answer: Decodable, Sendable { let StartChatCall: ChatCallView }
        let variables: [String: JSONValue] = ["conversationId": .string(conversationId), "isVideoOn": .bool(isVideoOn)]
        return try await execute(Documents.startChatCall, variables: variables, as: Answer.self).StartChatCall
    }

    /// Answers a call ringing the person, or joins one going in their chat.
    /// The device then joins its media with `joinChatCallMedia`. The phone
    /// that answers names itself, so that it is not told to stop ringing.
    public func joinChatCall(callId: String, agentPhoneId: String? = nil) async throws -> ChatCallView {
        struct Answer: Decodable, Sendable { let JoinChatCall: ChatCallView }
        return try await execute(Documents.joinChatCall, variables: Self.answerVariables(callId: callId, agentPhoneId: agentPhoneId), as: Answer.self)
            .JoinChatCall
    }

    /// Says no to a call ringing the person; the phone that does names
    /// itself, as when it answers.
    @discardableResult
    public func declineChatCall(callId: String, agentPhoneId: String? = nil) async throws -> ChatCallView {
        struct Answer: Decodable, Sendable { let DeclineChatCall: ChatCallView }
        return try await execute(Documents.declineChatCall, variables: Self.answerVariables(callId: callId, agentPhoneId: agentPhoneId), as: Answer.self)
            .DeclineChatCall
    }

    private static func answerVariables(callId: String, agentPhoneId: String?) -> [String: JSONValue] {
        var variables: [String: JSONValue] = ["callId": .string(callId)]
        if let agentPhoneId, !agentPhoneId.isEmpty { variables["agentPhoneId"] = .string(agentPhoneId) }
        return variables
    }

    /// Takes the person off a call; it ends when nobody is left on it.
    @discardableResult
    public func leaveChatCall(callId: String) async throws -> ChatCallView {
        struct Answer: Decodable, Sendable { let LeaveChatCall: ChatCallView }
        return try await execute(Documents.leaveChatCall, variables: ["callId": .string(callId)], as: Answer.self).LeaveChatCall
    }

    /// Connects the device to the call's audio and video: sends its offer and
    /// returns the server's answer. The signals are opened first
    /// (`ChatCallFeed.signals`), since the server's later offers come there.
    public func joinChatCallMedia(callId: String, offerSdp: String) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Media: Decodable, Sendable { let answerSdp: String }
            let JoinChatCallMedia: Media
        }
        let variables: [String: JSONValue] = ["callId": .string(callId), "offerSdp": .string(offerSdp)]
        return try await execute(Documents.joinChatCallMedia, variables: variables, as: Answer.self).JoinChatCallMedia.answerSdp
    }

    /// The device's answer to the latest offer the signals brought.
    @discardableResult
    public func answerChatCallMedia(callId: String, answerSdp: String) async throws -> Bool {
        struct Answer: Decodable, Sendable { let AnswerChatCallMedia: Bool }
        let variables: [String: JSONValue] = ["callId": .string(callId), "answerSdp": .string(answerSdp)]
        return try await execute(Documents.answerChatCallMedia, variables: variables, as: Answer.self).AnswerChatCallMedia
    }

    /// Tells the others the person's camera is on or off; the device starts
    /// or stops sending its picture itself.
    @discardableResult
    public func setChatCallVideo(callId: String, isVideoOn: Bool) async throws -> ChatCallView {
        struct Answer: Decodable, Sendable { let SetChatCallVideo: ChatCallView }
        let variables: [String: JSONValue] = ["callId": .string(callId), "isVideoOn": .bool(isVideoOn)]
        return try await execute(Documents.setChatCallVideo, variables: variables, as: Answer.self).SetChatCallVideo
    }

    /// Asks for the call to be transcribed. Everybody on it is asked, and
    /// only the voices of those who agree are transcribed.
    @discardableResult
    public func askChatCallTranscription(callId: String) async throws -> ChatCallView {
        struct Answer: Decodable, Sendable { let AskChatCallTranscription: ChatCallView }
        return try await execute(Documents.askChatCallTranscription, variables: ["callId": .string(callId)], as: Answer.self)
            .AskChatCallTranscription
    }

    /// The calls going that the person has a part in: ringing them, or that
    /// they are on. A phone that opens reads these to ring.
    public func listGoingChatCalls() async throws -> [ChatCallView] {
        struct Answer: Decodable, Sendable { let ListGoingChatCalls: [ChatCallView] }
        return try await execute(Documents.listGoingChatCalls, as: Answer.self).ListGoingChatCalls
    }

    /// The call going in a chat, nil when none is: what an open chat shows as
    /// a banner to join, whether or not the call rang the person.
    public func readGoingChatCall(conversationId: String) async throws -> ChatCallView? {
        struct Answer: Decodable, Sendable { let ReadGoingChatCall: ChatCallView? }
        return try await execute(Documents.readGoingChatCall, variables: ["conversationId": .string(conversationId)], as: Answer.self)
            .ReadGoingChatCall
    }

    /// A chat's calls, newest first, a page of `first` (the server's 50 when
    /// left out) started before the time given when there is one.
    public func listChatCalls(conversationId: String, first: Int? = nil, before: Date? = nil) async throws -> [ChatCallView] {
        struct Answer: Decodable, Sendable { let ListChatCalls: [ChatCallView] }
        var variables: [String: JSONValue] = ["conversationId": .string(conversationId)]
        if let first { variables["first"] = .number(Double(first)) }
        if let before { variables["before"] = .string(ISO8601DateFormatter().string(from: before)) }
        return try await execute(Documents.listChatCalls, variables: variables, as: Answer.self).ListChatCalls
    }

    /// One call, with its transcript when it was transcribed.
    public func readChatCall(callId: String) async throws -> ChatCallDetailView {
        struct Answer: Decodable, Sendable { let ReadChatCall: ChatCallDetailView }
        return try await execute(Documents.readChatCall, variables: ["callId": .string(callId)], as: Answer.self).ReadChatCall
    }

    /// Gives the server the registration the relay made for the phone's VoIP
    /// token, which is how the phone is rung for a call. An empty one takes
    /// the registration away.
    @discardableResult
    public func updatePhoneRinging(_ phoneId: String, voipPushRegistration: String) async throws -> AgentPhone {
        struct Answer: Decodable, Sendable { let UpdateAgentPhone: AgentPhone }
        let variables: [String: JSONValue] = ["phoneId": .string(phoneId), "voipPushRegistration": .string(voipPushRegistration)]
        return try await execute(Documents.updatePhoneRinging, variables: variables, as: Answer.self).UpdateAgentPhone
    }
}
