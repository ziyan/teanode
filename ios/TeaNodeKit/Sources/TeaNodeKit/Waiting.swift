import Foundation

/// AgentWaitingInteraction is a question or an approval the agent put to
/// the person and still waits on.
public struct AgentWaitingInteraction: Codable, Hashable, Identifiable, Sendable {
    public let id: String
    public let conversationId: String
    /// "question" or "approval".
    public let interactionKind: String
    public let interactionText: String
    public let createdAt: String

    public init(id: String, conversationId: String, interactionKind: String, interactionText: String, createdAt: String) {
        self.id = id
        self.conversationId = conversationId
        self.interactionKind = interactionKind
        self.interactionText = interactionText
        self.createdAt = createdAt
    }
}

/// AgentWaiting is what waits for the person across every conversation.
public struct AgentWaiting: Codable, Hashable, Sendable {
    public let waitingCount: Int
    public let interactions: [AgentWaitingInteraction]

    /// How many wait in each conversation, among those read.
    public var countByConversation: [String: Int] {
        interactions.reduce(into: [:]) { counted, interaction in counted[interaction.conversationId, default: 0] += 1 }
    }
}

extension Documents {
    public static let waiting = """
        query ($first: Int) {
          ReadAgentWaiting(first: $first) { waitingCount interactions { id conversationId interactionKind interactionText createdAt } }
        }
        """
}

extension Connection {
    /// How many questions and approvals wait for the person, and the newest
    /// `first` of them.
    public func waiting(first: Int = 5) async throws -> AgentWaiting {
        struct Answer: Decodable, Sendable { let ReadAgentWaiting: AgentWaiting }
        return try await execute(Documents.waiting, variables: ["first": .number(Double(first))], as: Answer.self).ReadAgentWaiting
    }
}
