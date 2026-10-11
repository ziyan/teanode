import Foundation

// The GraphQL the app sends, each document beside the call that sends it.
// The server's tests read every document here and check it against the schema
// (TestTheAppDocumentsMatchTheSchema), so a server change that would break the
// app breaks the server's build first.

public enum Documents {
    public static let session = """
        query { GetSession { username } }
        """
    public static let agent = """
        query { ReadAgent { agent { id enabled name } } }
        """
    public static let conversations = """
        query ($archived: Boolean, $query: String) {
          ListAgentConversations(archived: $archived, query: $query) { id kind jobKind title summary lastAt archivedAt goal goalState goalNote }
        }
        """
    public static let conversation = """
        query ($conversationId: String, $first: Int, $offset: Int) {
          ReadAgentConversation(conversationId: $conversationId, first: $first, offset: $offset) {
            conversation { id kind jobKind title summary lastAt archivedAt goal goalState goalNote }
            actingAs
            messages {
              id createdAt role content name toolCallId toolCalls { id name arguments }
              usage { promptTokens completionTokens cost }
              attachments { id name contentType size }
              references { itemId threadId subject from path name financeTransactionId postedOn amount currencyCode merchantName description agentMessageId quotedRole quotedText }
            }
            total
            todos { id text doneAt }
          }
          ListAgentInteractions(conversationId: $conversationId) { runId callId interactionKind toolName interactionText interactionChoices toolRisk createdAt }
        }
        """
    public static let ask = """
        mutation ($conversationId: String, $message: String!, $surface: String, $attachmentIds: [String!], $references: [AgentReferenceInput!], $drawerId: String, $interruptedAnswer: InterruptedAnswerInput) {
          AskAgent(conversationId: $conversationId, message: $message, surface: $surface, attachmentIds: $attachmentIds, references: $references, drawerId: $drawerId, interruptedAnswer: $interruptedAnswer) { runId conversationId }
        }
        """
    public static let voice = """
        query { ReadAgentVoice { isVoiceAvailable sampleRate } }
        """
    public static let updateConversation = """
        mutation ($conversationId: String!, $title: String, $archived: Boolean) {
          UpdateAgentConversation(conversationId: $conversationId, title: $title, archived: $archived) { id }
        }
        """
    public static let deleteConversation = """
        mutation ($conversationId: String!) { DeleteAgentConversation(conversationId: $conversationId) }
        """
    public static let setMainConversation = """
        mutation ($conversationId: String) { SetAgentMainConversation(conversationId: $conversationId) { id } }
        """
    public static let ideas = """
        query ($ideaStatuses: [String!], $language: String) {
          ListAgentIdeas(ideaStatuses: $ideaStatuses, language: $language) { ideas { id emoji headline body openingRequest } }
        }
        """
    public static let startIdea = """
        mutation ($ideaId: String!, $conversationId: String, $language: String) {
          StartAgentIdea(ideaId: $ideaId, conversationId: $conversationId, language: $language) { conversation { id } openingRequest }
        }
        """
    public static let devices = """
        query {
          ReadAgentTab { attached title url }
          ReadAgentComputers { computers { name system description } }
          ReadAgent { budget { used limit resetsAt cost costLimit currency } timezone }
        }
        """
    public static let backgroundCommands = """
        query ($conversationId: String) {
          ListAgentBackgroundCommands(conversationId: $conversationId) {
            computer id command directory conversationId startedAt endedAt isRunning exitCode stopReason
          }
        }
        """
    public static let backgroundCommand = """
        query ($computer: String!, $id: String!) {
          ReadAgentBackgroundCommand(computer: $computer, id: $id) {
            computer id command directory conversationId startedAt endedAt isRunning exitCode stopReason
            stdout stderr isStdoutTruncated isStderrTruncated stdoutByteCount stderrByteCount
          }
        }
        """
    public static let stopBackgroundCommand = """
        mutation ($computer: String!, $id: String!) {
          StopAgentBackgroundCommand(computer: $computer, id: $id) { id isRunning stopReason exitCode endedAt }
        }
        """
    public static let shareAttachment = """
        query ($attachmentId: String!) { ShareAgentAttachment(attachmentId: $attachmentId) }
        """
    public static let markIdeasShown = """
        mutation ($ideaIds: [String!]!) { MarkAgentIdeasShown(ideaIds: $ideaIds) }
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
    public static let registerLiveActivity = """
        mutation ($runId: String!, $pushRegistration: String!) { RegisterAgentLiveActivity(runId: $runId, pushRegistration: $pushRegistration) }
        """
    public static let readRun = """
        query ($runId: String!, $after: Int, $wait: Int) {
          ReadAgentRun(runId: $runId, after: $after, wait: $wait) { done events { kind runId sequence at text tool callId arguments risk note noteKind noteDetail error drawerId } }
        }
        """
    public static let conversationEvents = """
        subscription ($conversationId: String!) {
          AgentConversationEvents(conversationId: $conversationId) { kind runId sequence at text tool callId arguments risk note noteKind noteDetail error drawerId }
        }
        """

    /// Every document, for the server's schema check.
    public static let all: [String: String] = [
        "session": session, "agent": agent, "conversations": conversations, "conversation": conversation, "ask": ask,
        "startConversation": startConversation, "resolveConfirmation": resolveConfirmation, "answerQuestion": answerQuestion,
        "stopRun": stopRun, "answerLocation": answerLocation, "reportPresence": reportPresence, "registerPhone": registerPhone,
        "updatePhone": updatePhone, "conversationEvents": conversationEvents, "answerCodingQuestion": answerCodingQuestion,
        "updateConversation": updateConversation, "deleteConversation": deleteConversation,
        "setMainConversation": setMainConversation, "ideas": ideas, "startIdea": startIdea, "markIdeasShown": markIdeasShown, "shareAttachment": shareAttachment, "devices": devices, "voice": voice,
        "backgroundCommands": backgroundCommands, "backgroundCommand": backgroundCommand, "stopBackgroundCommand": stopBackgroundCommand,
        "readRun": readRun, "registerLiveActivity": registerLiveActivity,
    ]
}

/// RunAnswer is how a turn ended, for a caller that waits for it without
/// showing the conversation: Siri, a shortcut.
public enum RunAnswer: Equatable, Sendable {
    /// The agent answered: the whole answer.
    case answered(String)
    /// The agent waits for the person: an approval or a question, answered in
    /// the app.
    case waitingForPerson
    /// The turn failed: what the server said.
    case failed(String)
    /// It was still working when the caller stopped waiting.
    case stillWorking
}

extension Connection {
    /// A run's events from `after` on, waiting up to `waitSeconds` for the
    /// first (the server waits at most 25).
    public func runEvents(_ runId: String, after: Int = 0, waitSeconds: Int = 20) async throws -> (events: [AgentEvent], isDone: Bool) {
        struct Answer: Decodable, Sendable {
            struct Run: Decodable, Sendable {
                let done: Bool
                let events: [AgentEvent]
            }
            let ReadAgentRun: Run
        }
        let run = try await execute(
            Documents.readRun, variables: ["runId": .string(runId), "after": .number(Double(after)), "wait": .number(Double(waitSeconds))],
            as: Answer.self
        ).ReadAgentRun
        return (run.events, run.done)
    }

    /// Has a running turn push its state to a Live Activity: the sealed
    /// registration the relay made of the activity's push token.
    public func registerLiveActivity(runId: String, pushRegistration: String) async throws {
        struct Answer: Decodable, Sendable { let RegisterAgentLiveActivity: Bool? }
        _ = try await execute(
            Documents.registerLiveActivity, variables: ["runId": .string(runId), "pushRegistration": .string(pushRegistration)],
            as: Answer.self)
    }

    /// Waits for a run to end, until `deadline`, and says how it ended. The
    /// last whole message is the answer: those before it came between tool
    /// calls.
    public func answer(to runId: String, until deadline: Date) async throws -> RunAnswer {
        var after = 0
        var answer = ""
        while true {
            let secondsLeft = Int(deadline.timeIntervalSinceNow.rounded(.down))
            if secondsLeft <= 0 { return .stillWorking }
            let read = try await runEvents(runId, after: after, waitSeconds: min(secondsLeft, 20))
            for event in read.events {
                after = max(after, event.sequence + 1)
                switch event.kind {
                case "message": answer = event.text ?? ""
                case "confirmation", "question", "locate": return .waitingForPerson
                case "error": return .failed(event.error ?? event.text ?? "")
                default: break
                }
            }
            if read.isDone { return .answered(answer) }
        }
    }
}

/// The surface the app names on its turns (internal/agent/surface.go).
public enum Surface {
    public static let typed = "ios"
    public static let voice = "ios_voice"
    public static let siri = "siri"
}

/// ConversationPage is a page of a conversation's messages, newest last,
/// with its open cards and the agent's list for it.
public struct ConversationPage: Sendable {
    public let conversation: AgentConversation?
    public let messages: [AgentMessage]
    public let interactions: [AgentInteraction]
    /// How many messages the conversation has in all.
    public let total: Int
    public let todos: [AgentTodo]
    /// Whose agent this is, when an operator is looking at somebody else's.
    public let actingAs: String?
}

/// AgentIdea is something the agent suggests starting with.
public struct AgentIdea: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let emoji: String?
    public let headline: String
    public let body: String?
    public let openingRequest: String?
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

    /// The conversations, or the archived ones, or those matching words.
    /// The agent's name, which the app calls it by; empty when it has none.
    public func agentName() async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Read: Decodable, Sendable {
                struct Agent: Decodable, Sendable { let name: String? }
                let agent: Agent?
            }
            let ReadAgent: Read
        }
        return try await execute(Documents.agent, as: Answer.self).ReadAgent.agent?.name ?? ""
    }

    public func conversations(isArchived: Bool = false, matching words: String? = nil) async throws -> [AgentConversation] {
        struct Answer: Decodable, Sendable { let ListAgentConversations: [AgentConversation] }
        var variables: [String: JSONValue] = [:]
        if let words, !words.isEmpty {
            variables["query"] = .string(words)
        } else {
            variables["archived"] = .bool(isArchived)
        }
        return try await execute(Documents.conversations, variables: variables, as: Answer.self).ListAgentConversations
    }

    /// A page of a conversation's messages: the newest `first`, or older
    /// ones `offset` from the newest. A nil id is the main conversation.
    public func conversation(_ conversationId: String?, first: Int = 100, offset: Int = 0) async throws -> ConversationPage {
        struct Answer: Decodable, Sendable {
            struct Read: Decodable, Sendable {
                let conversation: AgentConversation?
                let actingAs: String?
                let messages: [AgentMessage]
                let total: Int?
                let todos: [AgentTodo]?
            }
            let ReadAgentConversation: Read
            let ListAgentInteractions: [AgentInteraction]?
        }
        var variables: [String: JSONValue] = ["first": .number(Double(first)), "offset": .number(Double(offset))]
        if let conversationId { variables["conversationId"] = .string(conversationId) }
        let answer = try await execute(Documents.conversation, variables: variables, as: Answer.self)
        let read = answer.ReadAgentConversation
        return ConversationPage(
            conversation: read.conversation, messages: read.messages, interactions: answer.ListAgentInteractions ?? [],
            total: read.total ?? read.messages.count, todos: read.todos ?? [],
            actingAs: (read.actingAs ?? "").isEmpty ? nil : read.actingAs)
    }

    /// Renames a conversation, or puts it away or back.
    public func updateConversation(_ conversationId: String, title: String? = nil, isArchived: Bool? = nil) async throws {
        struct Answer: Decodable, Sendable {
            struct Updated: Decodable, Sendable { let id: String }
            let UpdateAgentConversation: Updated
        }
        var variables: [String: JSONValue] = ["conversationId": .string(conversationId)]
        if let title { variables["title"] = .string(title) }
        if let isArchived { variables["archived"] = .bool(isArchived) }
        _ = try await execute(Documents.updateConversation, variables: variables, as: Answer.self)
    }

    public func deleteConversation(_ conversationId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteAgentConversation: Bool? }
        _ = try await execute(Documents.deleteConversation, variables: ["conversationId": .string(conversationId)], as: Answer.self)
    }

    /// Makes a conversation the main one; the one before is kept as a side
    /// conversation.
    public func setMainConversation(_ conversationId: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Main: Decodable, Sendable { let id: String }
            let SetAgentMainConversation: Main
        }
        _ = try await execute(Documents.setMainConversation, variables: ["conversationId": .string(conversationId)], as: Answer.self)
    }

    /// The agent's open ideas, in the person's language.
    public func ideas(language: String) async throws -> [AgentIdea] {
        struct Answer: Decodable, Sendable {
            struct Listed: Decodable, Sendable { let ideas: [AgentIdea] }
            let ListAgentIdeas: Listed
        }
        return try await execute(Documents.ideas, variables: [
            "ideaStatuses": .array([.string("open")]), "language": .string(language),
        ], as: Answer.self).ListAgentIdeas.ideas
    }

    /// Starts an idea: the conversation it goes to and what to ask.
    public func startIdea(_ ideaId: String, in conversationId: String?, language: String) async throws -> (conversationId: String?, openingRequest: String) {
        struct Answer: Decodable, Sendable {
            struct Started: Decodable, Sendable {
                struct Conversation: Decodable, Sendable { let id: String }
                let conversation: Conversation?
                let openingRequest: String
            }
            let StartAgentIdea: Started
        }
        var variables: [String: JSONValue] = ["ideaId": .string(ideaId), "language": .string(language)]
        if let conversationId { variables["conversationId"] = .string(conversationId) }
        let started = try await execute(Documents.startIdea, variables: variables, as: Answer.self).StartAgentIdea
        return (started.conversation?.id, started.openingRequest)
    }

    public func markIdeasShown(_ ideaIds: [String]) async throws {
        struct Answer: Decodable, Sendable { let MarkAgentIdeasShown: Bool? }
        _ = try await execute(Documents.markIdeasShown, variables: ["ideaIds": .array(ideaIds.map { .string($0) })], as: Answer.self)
    }

    /// Asks the agent something; returns the run and the conversation it
    /// runs in.
    public func ask(
        _ message: String, in conversationId: String?, attachmentIds: [String] = [], references: [AgentReference] = [],
        surface: String = Surface.typed, drawerId: String, interruptedAnswer: (heardText: String, unheardText: String)? = nil
    ) async throws -> (runId: String, conversationId: String) {
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
        if !references.isEmpty {
            let encoded = try JSONEncoder().encode(references)
            variables["references"] = try JSONDecoder().decode(JSONValue.self, from: encoded)
        }
        if let interruptedAnswer {
            variables["interruptedAnswer"] = .object([
                "heardText": .string(interruptedAnswer.heardText), "unheardText": .string(interruptedAnswer.unheardText),
            ])
        }
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

    /// Settles an approval; false when the card was answered already or
    /// is no longer waiting.
    @discardableResult
    public func resolveConfirmation(runId: String, callId: String, isApproved: Bool) async throws -> Bool {
        struct Answer: Decodable, Sendable { let ResolveAgentConfirmation: Bool? }
        return try await execute(Documents.resolveConfirmation, variables: [
            "runId": .string(runId), "callId": .string(callId), "approve": .bool(isApproved),
        ], as: Answer.self).ResolveAgentConfirmation ?? false
    }

    /// Answers the agent's question; false when the card was answered
    /// already or is no longer waiting.
    @discardableResult
    public func answerQuestion(runId: String, callId: String, answer: String) async throws -> Bool {
        struct Answer: Decodable, Sendable { let AnswerAgentQuestion: Bool? }
        return try await execute(Documents.answerQuestion, variables: [
            "runId": .string(runId), "callId": .string(callId), "answer": .string(answer),
        ], as: Answer.self).AnswerAgentQuestion ?? false
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
