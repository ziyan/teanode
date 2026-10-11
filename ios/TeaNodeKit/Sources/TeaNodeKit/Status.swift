import Foundation

/// AgentStatus is what the drawer's head shows: the browser tab and the
/// computers attached to the agent, and how much of today's budget is used.
public struct AgentStatus: Sendable, Equatable {
    public struct Tab: Codable, Sendable, Equatable {
        public let attached: Bool
        public let title: String?
        public let url: String?
    }

    public struct Computer: Codable, Sendable, Equatable, Identifiable {
        public let name: String
        public let system: String?
        public let description: String?
        public var id: String { name }
    }

    public struct Budget: Codable, Sendable, Equatable {
        /// Tokens used today, and the day's limit; 0 is no limit.
        public let used: Double
        public let limit: Double
        public let resetsAt: String?
        /// Money spent today, and the day's limit in money; 0 is no limit.
        public let cost: Double
        public let costLimit: Double
        public let currency: String?

        /// How much of the day's budget is used, by whichever limit is
        /// closer to running out; nil with no limit.
        public var fraction: Double? {
            var fractions: [Double] = []
            if limit > 0 { fractions.append(used / limit) }
            if costLimit > 0 { fractions.append(cost / costLimit) }
            return fractions.max()
        }
    }

    public let tab: Tab?
    public let computers: [Computer]
    public let budget: Budget?
    public let timezone: String?
}

/// AgentBackgroundCommand is a command the agent left running on a computer.
public struct AgentBackgroundCommand: Codable, Sendable, Identifiable, Equatable {
    public let computer: String
    public let id: String
    public let command: String
    public let directory: String?
    public let conversationId: String?
    public let startedAt: String?
    public let endedAt: String?
    public let isRunning: Bool
    public let exitCode: Int?
    public let stopReason: String?
    public var stdout: String? = nil
    public var stderr: String? = nil
    public var isStdoutTruncated: Bool? = nil
    public var isStderrTruncated: Bool? = nil
    public var stdoutByteCount: Int? = nil
    public var stderrByteCount: Int? = nil
}

extension Connection {
    /// What the drawer's head shows, read every few seconds while it is open.
    public func status() async throws -> AgentStatus {
        struct Answer: Decodable, Sendable {
            struct Computers: Decodable, Sendable { let computers: [AgentStatus.Computer] }
            struct Read: Decodable, Sendable {
                let budget: AgentStatus.Budget?
                let timezone: String?
            }
            let ReadAgentTab: AgentStatus.Tab?
            let ReadAgentComputers: Computers?
            let ReadAgent: Read?
        }
        let answer = try await execute(Documents.devices, as: Answer.self)
        return AgentStatus(
            tab: answer.ReadAgentTab, computers: answer.ReadAgentComputers?.computers ?? [], budget: answer.ReadAgent?.budget,
            timezone: answer.ReadAgent?.timezone)
    }

    /// The commands left running, for one conversation or all.
    public func backgroundCommands(in conversationId: String?) async throws -> [AgentBackgroundCommand] {
        struct Answer: Decodable, Sendable { let ListAgentBackgroundCommands: [AgentBackgroundCommand] }
        var variables: [String: JSONValue] = [:]
        if let conversationId { variables["conversationId"] = .string(conversationId) }
        return try await execute(Documents.backgroundCommands, variables: variables, as: Answer.self).ListAgentBackgroundCommands
    }

    /// One command with what it printed.
    public func backgroundCommand(computer: String, id: String) async throws -> AgentBackgroundCommand {
        struct Answer: Decodable, Sendable { let ReadAgentBackgroundCommand: AgentBackgroundCommand }
        return try await execute(Documents.backgroundCommand, variables: ["computer": .string(computer), "id": .string(id)], as: Answer.self)
            .ReadAgentBackgroundCommand
    }

    public func stopBackgroundCommand(computer: String, id: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Stopped: Decodable, Sendable { let id: String }
            let StopAgentBackgroundCommand: Stopped
        }
        _ = try await execute(Documents.stopBackgroundCommand, variables: ["computer": .string(computer), "id": .string(id)], as: Answer.self)
    }
}
