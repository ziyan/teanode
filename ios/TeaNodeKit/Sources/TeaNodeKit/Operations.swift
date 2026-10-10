import Foundation

// The GraphQL the app sends, each document beside the call that sends it.
// The server's tests read every document here and check it against the schema
// (TestTheAppDocumentsMatchTheSchema), so a server change that would break the
// app breaks the server's build first.

public enum Documents {
    public static let session = """
        query { GetSession { username } }
        """
    public static let conversations = """
        query { ListAgentConversations(archived: false) { id kind title summary lastAt } }
        """
    public static let conversation = """
        query ($conversationId: String, $first: Int) {
          ReadAgentConversation(conversationId: $conversationId, first: $first) {
            conversation { id kind title summary lastAt }
            messages { id createdAt role content name toolCallId toolCalls { id name arguments } attachments { id name contentType size } }
            total
          }
          ListAgentInteractions(conversationId: $conversationId) { runId callId interactionKind toolName interactionText interactionChoices createdAt }
        }
        """
    public static let ask = """
        mutation ($conversationId: String, $message: String!, $surface: String, $attachmentIds: [String!], $drawerId: String) {
          AskAgent(conversationId: $conversationId, message: $message, surface: $surface, attachmentIds: $attachmentIds, drawerId: $drawerId) { runId conversationId }
        }
        """
    public static let startConversation = """
        mutation ($title: String) { StartAgentConversation(title: $title) { id kind title summary lastAt } }
        """
    public static let resolveConfirmation = """
        mutation ($runId: String!, $callId: String!, $approve: Boolean!) { ResolveAgentConfirmation(runId: $runId, callId: $callId, approve: $approve) }
        """
    public static let answerQuestion = """
        mutation ($runId: String!, $callId: String!, $answer: String!) { AnswerAgentQuestion(runId: $runId, callId: $callId, answer: $answer) }
        """
    public static let stopRun = """
        mutation ($runId: String!) { StopAgentRun(runId: $runId) }
        """
    public static let answerLocation = """
        mutation ($runId: String!, $callId: String!, $latitudeDegrees: Float, $longitudeDegrees: Float, $accuracyMeters: Float, $measuredAt: String, $errorMessage: String) {
          AnswerAgentLocation(runId: $runId, callId: $callId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, accuracyMeters: $accuracyMeters, measuredAt: $measuredAt, errorMessage: $errorMessage)
        }
        """
    public static let reportPresence = """
        mutation ($isVisible: Boolean!, $idleSeconds: Int!) { ReportAgentPresence(isVisible: $isVisible, idleSeconds: $idleSeconds) }
        """
    public static let registerPhone = """
        mutation ($phoneName: String!, $phoneModel: String!, $pushRegistration: String, $payloadKey: String) {
          RegisterAgentPhone(phoneName: $phoneName, phoneModel: $phoneModel, pushRegistration: $pushRegistration, payloadKey: $payloadKey) { id notificationKinds canBeNotified }
        }
        """
    public static let updatePhone = """
        mutation ($phoneId: String!, $notificationKinds: [String!]) {
          UpdateAgentPhone(phoneId: $phoneId, notificationKinds: $notificationKinds) { id notificationKinds canBeNotified }
        }
        """
    public static let answerCodingQuestion = """
        mutation ($computer: String, $paneId: String!, $questionFingerprint: String!, $optionNumbers: [Int!], $optionLabels: [String!], $freeText: String) {
          AnswerAgentHerdrQuestion(computer: $computer, paneId: $paneId, questionFingerprint: $questionFingerprint, optionNumbers: $optionNumbers, optionLabels: $optionLabels, freeText: $freeText) { isAnswerAccepted answeredWith }
        }
        """
    public static let conversationEvents = """
        subscription ($conversationId: String!) {
          AgentConversationEvents(conversationId: $conversationId) { kind runId sequence at text tool callId arguments risk note noteKind noteDetail error drawerId }
        }
        """

    /// Every document, for the server's schema check.
    public static let all: [String: String] = [
        "session": session, "conversations": conversations, "conversation": conversation, "ask": ask,
        "startConversation": startConversation, "resolveConfirmation": resolveConfirmation, "answerQuestion": answerQuestion,
        "stopRun": stopRun, "answerLocation": answerLocation, "reportPresence": reportPresence, "registerPhone": registerPhone,
        "updatePhone": updatePhone, "conversationEvents": conversationEvents, "answerCodingQuestion": answerCodingQuestion,
    ]
}

/// The surface the app names on its turns (internal/agent/surface.go).
public enum Surface {
    public static let typed = "ios"
    public static let voice = "ios_voice"
    public static let siri = "siri"
}

/// ConversationPage is a conversation's newest messages and its open cards.
public struct ConversationPage: Sendable {
    public let conversation: AgentConversation?
    public let messages: [AgentMessage]
    public let interactions: [AgentInteraction]
}

extension Connection {
    /// Who the token signs in as.
    public func username() async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Session: Decodable, Sendable { let username: String? }
            let GetSession: Session
        }
        return try await execute(Documents.session, as: Answer.self).GetSession.username ?? ""
    }

    public func conversations() async throws -> [AgentConversation] {
        struct Answer: Decodable, Sendable { let ListAgentConversations: [AgentConversation] }
        return try await execute(Documents.conversations, as: Answer.self).ListAgentConversations
    }

    /// A conversation's newest messages; nil is the main conversation.
    public func conversation(_ conversationId: String?, first: Int = 120) async throws -> ConversationPage {
        struct Answer: Decodable, Sendable {
            struct Read: Decodable, Sendable {
                let conversation: AgentConversation?
                let messages: [AgentMessage]
            }
            let ReadAgentConversation: Read
            let ListAgentInteractions: [AgentInteraction]?
        }
        var variables: [String: JSONValue] = ["first": .number(Double(first))]
        if let conversationId { variables["conversationId"] = .string(conversationId) }
        let answer = try await execute(Documents.conversation, variables: variables, as: Answer.self)
        return ConversationPage(
            conversation: answer.ReadAgentConversation.conversation, messages: answer.ReadAgentConversation.messages,
            interactions: answer.ListAgentInteractions ?? [])
    }

    /// Asks the agent something; returns the run and the conversation it
    /// runs in.
    public func ask(_ message: String, in conversationId: String?, attachmentIds: [String] = [], surface: String = Surface.typed, drawerId: String) async throws -> (runId: String, conversationId: String) {
        struct Answer: Decodable, Sendable {
            struct Asked: Decodable, Sendable {
                let runId: String
                let conversationId: String
            }
            let AskAgent: Asked
        }
        var variables: [String: JSONValue] = [
            "message": .string(message), "surface": .string(surface), "drawerId": .string(drawerId),
            "attachmentIds": .array(attachmentIds.map { .string($0) }),
        ]
        if let conversationId { variables["conversationId"] = .string(conversationId) }
        let asked = try await execute(Documents.ask, variables: variables, as: Answer.self).AskAgent
        return (asked.runId, asked.conversationId)
    }

    public func startConversation(title: String?) async throws -> AgentConversation {
        struct Answer: Decodable, Sendable { let StartAgentConversation: AgentConversation }
        var variables: [String: JSONValue] = [:]
        if let title { variables["title"] = .string(title) }
        return try await execute(Documents.startConversation, variables: variables, as: Answer.self).StartAgentConversation
    }

    public func resolveConfirmation(runId: String, callId: String, isApproved: Bool) async throws {
        struct Answer: Decodable, Sendable { let ResolveAgentConfirmation: Bool? }
        _ = try await execute(Documents.resolveConfirmation, variables: [
            "runId": .string(runId), "callId": .string(callId), "approve": .bool(isApproved),
        ], as: Answer.self)
    }

    public func answerQuestion(runId: String, callId: String, answer: String) async throws {
        struct Answer: Decodable, Sendable { let AnswerAgentQuestion: Bool? }
        _ = try await execute(Documents.answerQuestion, variables: [
            "runId": .string(runId), "callId": .string(callId), "answer": .string(answer),
        ], as: Answer.self)
    }

    /// Answers a coding session's question: the options chosen, or what to
    /// type. Returns how the session took it, in its words.
    public func answerCodingQuestion(_ question: CodingQuestion, options: [CodingQuestion.Option], freeText: String?) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Answered: Decodable, Sendable {
                let isAnswerAccepted: Bool
                let answeredWith: String
            }
            let AnswerAgentHerdrQuestion: Answered
        }
        var variables: [String: JSONValue] = [
            "computer": .string(question.computer), "paneId": .string(question.paneId),
            "questionFingerprint": .string(question.questionFingerprint),
        ]
        if let freeText {
            variables["freeText"] = .string(freeText)
        } else {
            variables["optionNumbers"] = .array(options.map { .number(Double($0.optionNumber)) })
            variables["optionLabels"] = .array(options.map { .string($0.optionLabel) })
        }
        let answered = try await execute(Documents.answerCodingQuestion, variables: variables, as: Answer.self).AnswerAgentHerdrQuestion
        guard answered.isAnswerAccepted else {
            throw TeaNodeError.refused("The coding session did not take that answer. It may have moved on.")
        }
        return answered.answeredWith
    }

    public func stopRun(_ runId: String) async throws {
        struct Answer: Decodable, Sendable { let StopAgentRun: Bool? }
        _ = try await execute(Documents.stopRun, variables: ["runId": .string(runId)], as: Answer.self)
    }

    /// Answers the agent's question of where the person is: a place, or why
    /// there is none.
    public func answerLocation(runId: String, callId: String, latitudeDegrees: Double?, longitudeDegrees: Double?, accuracyMeters: Double?, measuredAt: Date?, errorMessage: String?) async throws {
        struct Answer: Decodable, Sendable { let AnswerAgentLocation: Bool? }
        var variables: [String: JSONValue] = ["runId": .string(runId), "callId": .string(callId)]
        if let latitudeDegrees, let longitudeDegrees {
            variables["latitudeDegrees"] = .number(latitudeDegrees)
            variables["longitudeDegrees"] = .number(longitudeDegrees)
            if let accuracyMeters { variables["accuracyMeters"] = .number(accuracyMeters) }
            if let measuredAt { variables["measuredAt"] = .string(measuredAt.formatted(.iso8601)) }
        } else {
            variables["errorMessage"] = .string(errorMessage ?? "The location is not available.")
        }
        _ = try await execute(Documents.answerLocation, variables: variables, as: Answer.self)
    }

    public func reportPresence(isVisible: Bool) async throws {
        struct Answer: Decodable, Sendable { let ReportAgentPresence: Bool? }
        _ = try await execute(Documents.reportPresence, variables: ["isVisible": .bool(isVisible), "idleSeconds": .number(0)], as: Answer.self)
    }

    /// Registers this phone with the server; push registration and key come
    /// once the person allows notifications.
    @discardableResult
    public func registerPhone(phoneName: String, phoneModel: String, pushRegistration: String? = nil, payloadKey: String? = nil) async throws -> AgentPhone {
        struct Answer: Decodable, Sendable { let RegisterAgentPhone: AgentPhone }
        var variables: [String: JSONValue] = ["phoneName": .string(phoneName), "phoneModel": .string(phoneModel)]
        if let pushRegistration { variables["pushRegistration"] = .string(pushRegistration) }
        if let payloadKey { variables["payloadKey"] = .string(payloadKey) }
        return try await execute(Documents.registerPhone, variables: variables, as: Answer.self).RegisterAgentPhone
    }

    /// Chooses which kinds of notification this phone gets; none is every
    /// kind.
    public func updatePhone(_ phoneId: String, notificationKinds: [String]) async throws -> AgentPhone {
        struct Answer: Decodable, Sendable { let UpdateAgentPhone: AgentPhone }
        return try await execute(Documents.updatePhone, variables: [
            "phoneId": .string(phoneId), "notificationKinds": .array(notificationKinds.map { .string($0) }),
        ], as: Answer.self).UpdateAgentPhone
    }
}
