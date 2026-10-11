import Foundation

/// AgentGoal is something the agent works toward on its own, in a
/// conversation of its own.
public struct AgentGoal: Codable, Hashable, Sendable, Identifiable {
    public struct Activity: Codable, Hashable, Sendable, Identifiable {
        public let id: String
        public let createdAt: String
        public let goalActivityKind: String
        public let activityHeadline: String
        public let activityDetail: String
    }

    public struct Schedule: Codable, Hashable, Sendable, Identifiable {
        public let id: String
        public let name: String
        public let cron: String
        public let enabled: Bool
        public let nextRunAt: String?
    }

    public let conversationId: String
    public let goalTitle: String
    public let goalDescription: String
    /// working, waiting, met or dropped.
    public let goalState: String
    public let goalStatus: String
    public let goalSetAt: String?
    public let goalNextAt: String?
    public let lastAt: String?
    public let goalOriginTitle: String?
    public let activity: [Activity?]?
    public let schedules: [Schedule?]?

    public var id: String { conversationId }
    public var isOpen: Bool { goalState == "working" || goalState == "waiting" }
}

extension Documents {
    static let goalFields = """
        conversationId goalTitle goalDescription goalState goalStatus goalSetAt goalNextAt lastAt
        """
    public static let goals = """
        query { ListAgentGoals { \(goalFields) } }
        """
    public static let goal = """
        query ($conversationId: String!) {
          GetAgentGoal(conversationId: $conversationId) {
            \(goalFields)
            goalOriginTitle
            activity { id createdAt goalActivityKind activityHeadline activityDetail }
            schedules { id name cron enabled nextRunAt }
          }
        }
        """
    public static let startGoal = """
        mutation ($goalTitle: String!, $goalDescription: String!) {
          StartAgentGoal(goalTitle: $goalTitle, goalDescription: $goalDescription) { conversationId }
        }
        """
    public static let tellGoal = """
        mutation ($conversationId: String!, $text: String!) {
          TellAgentGoal(conversationId: $conversationId, text: $text) { conversationId }
        }
        """
    public static let setGoalState = """
        mutation ($conversationId: String!, $goalState: String!) {
          SetAgentGoalState(conversationId: $conversationId, goalState: $goalState) { conversationId }
        }
        """
}

extension Connection {
    public func goals() async throws -> [AgentGoal] {
        struct Answer: Decodable, Sendable { let ListAgentGoals: [AgentGoal?] }
        return try await execute(Documents.goals, as: Answer.self).ListAgentGoals.compactMap { $0 }
    }

    public func goal(_ conversationId: String) async throws -> AgentGoal? {
        struct Answer: Decodable, Sendable { let GetAgentGoal: AgentGoal? }
        return try await execute(Documents.goal, variables: ["conversationId": .string(conversationId)], as: Answer.self).GetAgentGoal
    }

    /// Starts a goal; returns its conversation.
    public func startGoal(title: String, description: String) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Started: Decodable, Sendable { let conversationId: String }
            let StartAgentGoal: Started
        }
        return try await execute(Documents.startGoal, variables: ["goalTitle": .string(title), "goalDescription": .string(description)], as: Answer.self)
            .StartAgentGoal.conversationId
    }

    public func tellGoal(_ conversationId: String, text: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Told: Decodable, Sendable { let conversationId: String }
            let TellAgentGoal: Told?
        }
        _ = try await execute(Documents.tellGoal, variables: ["conversationId": .string(conversationId), "text": .string(text)], as: Answer.self)
    }

    /// Marks a goal met or dropped, or opens it again as working.
    public func setGoalState(_ conversationId: String, to state: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Set: Decodable, Sendable { let conversationId: String }
            let SetAgentGoalState: Set?
        }
        _ = try await execute(Documents.setGoalState, variables: ["conversationId": .string(conversationId), "goalState": .string(state)], as: Answer.self)
    }
}
