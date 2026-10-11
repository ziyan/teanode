import Foundation

/// IdeaEntry is one idea as the Ideas page lists it: one found for the person
/// in their own mail and memory, or one of the catalog's, and what became of
/// it.
public struct IdeaEntry: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    /// "personal" or "catalog".
    public let ideaKind: String
    public let ideaCategory: String?
    public let emoji: String?
    public let headline: String
    public let body: String?
    public let openingRequest: String?
    /// What prompted a personal idea.
    public let suggestionReason: String?
    /// open, started, done, dismissed or expired.
    public let ideaStatus: String
    public let expiredReason: String?
    public let startedConversationId: String?
    public let createdAt: String
    public let startedAt: String?
    public let closedAt: String?

    public var isOpen: Bool { ideaStatus == "open" }

    /// Whether a closed idea may be opened again: an expired catalog idea
    /// comes back only when it expired because it was used.
    public var canRestore: Bool {
        guard ideaStatus != "open" else { return false }
        return !(ideaKind == "catalog" && ideaStatus == "expired" && expiredReason != "already_used")
    }
}

/// IdeaList is every idea and the catalog's categories, in their order.
public struct IdeaList: Codable, Hashable, Sendable {
    public struct Category: Codable, Hashable, Sendable {
        public let ideaCategory: String
        public let emojis: [String]
    }

    public let ideas: [IdeaEntry]
    public let ideaCategories: [Category]

    /// The open ideas found for the person.
    public var forYou: [IdeaEntry] { ideas.filter { $0.isOpen && $0.ideaKind == "personal" } }

    /// The open catalog ideas, by category in the catalog's order.
    public var byCategory: [(category: String, ideas: [IdeaEntry])] {
        ideaCategories.compactMap { category in
            let shown = ideas.filter { $0.isOpen && $0.ideaKind == "catalog" && $0.ideaCategory == category.ideaCategory }
            return shown.isEmpty ? nil : (category.ideaCategory, shown)
        }
    }

    /// What became of the rest, the latest first.
    public var history: [IdeaEntry] {
        ideas.filter { !$0.isOpen }.sorted { ($0.closedAt ?? $0.startedAt ?? $0.createdAt) > ($1.closedAt ?? $1.startedAt ?? $1.createdAt) }
    }
}

extension Documents {
    public static let allIdeas = """
        query ($language: String) {
          ListAgentIdeas(language: $language) {
            ideas { id ideaKind ideaCategory emoji headline body openingRequest suggestionReason ideaStatus expiredReason startedConversationId createdAt startedAt closedAt }
            ideaCategories { ideaCategory emojis }
          }
        }
        """
    public static let setIdeaStatus = """
        mutation ($ideaId: String!, $ideaStatus: String!) { SetAgentIdeaStatus(ideaId: $ideaId, ideaStatus: $ideaStatus) { id ideaStatus } }
        """
}

extension Connection {
    /// Every idea, open or not, in the person's language.
    public func allIdeas(language: String) async throws -> IdeaList {
        struct Answer: Decodable, Sendable { let ListAgentIdeas: IdeaList }
        return try await execute(Documents.allIdeas, variables: ["language": .string(language)], as: Answer.self).ListAgentIdeas
    }

    /// Says what became of an idea: done, dismissed, or open again.
    public func setIdeaStatus(_ ideaId: String, to ideaStatus: String) async throws {
        struct Answer: Decodable, Sendable { let SetAgentIdeaStatus: JSONValue? }
        _ = try await execute(Documents.setIdeaStatus, variables: ["ideaId": .string(ideaId), "ideaStatus": .string(ideaStatus)], as: Answer.self)
    }
}
