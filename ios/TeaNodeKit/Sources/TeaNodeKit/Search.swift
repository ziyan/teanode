import Foundation

/// SearchArea is one kind of thing the search across everything finds. The
/// raw values are the server's area names, in the order the server lists
/// them (internal/search/search.go).
public enum SearchArea: String, CaseIterable, Hashable, Sendable {
    case chats, mail, contacts, photos, memory, documents, recordings, places, finance
}

/// SearchOpening is where a search result is opened: which part of the app,
/// and which item in it, as the part's own link names them. A result the app
/// has no screen for opens nowhere.
public enum SearchOpening: Hashable, Sendable {
    case conversation(conversationId: String)
    case mailItem(mailItemId: String)
    case contact(contactId: String)
    case photo(photoId: String)
    case knowledgePage(path: String)
    case recording(recordingId: String)
    /// A place, finance transaction or document: the part opens, and the
    /// item is found there by the words.
    case partOnly(SearchArea)
    case nowhere
}

/// SearchResult is one thing found: what it is, how to open it, and what to
/// show for it.
public struct SearchResult: Decodable, Identifiable, Hashable, Sendable {
    public let searchArea: String
    /// The conversation, mail item, contact, photo, page path, document,
    /// recording, place or transaction to open, as the area says.
    public let targetId: String
    public let titleText: String
    /// A snippet, a sender, an account.
    public let detailText: String
    public let happenedAt: String?

    public var id: String { "\(searchArea)/\(targetId)" }

    public var area: SearchArea? { SearchArea(rawValue: searchArea) }

    /// When it happened, if the server said.
    public var happenedDate: Date? { ServerTime.date(happenedAt) }

    public init(searchArea: String, targetId: String, titleText: String, detailText: String = "", happenedAt: String? = nil) {
        self.searchArea = searchArea
        self.targetId = targetId
        self.titleText = titleText
        self.detailText = detailText
        self.happenedAt = happenedAt
    }

    private enum CodingKeys: String, CodingKey {
        case searchArea, targetId, titleText, detailText, happenedAt
    }

    /// A missing text is empty rather than a failure, so one odd result does
    /// not lose the rest.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        searchArea = try container.decode(String.self, forKey: .searchArea)
        targetId = try container.decodeIfPresent(String.self, forKey: .targetId) ?? ""
        titleText = try container.decodeIfPresent(String.self, forKey: .titleText) ?? ""
        detailText = try container.decodeIfPresent(String.self, forKey: .detailText) ?? ""
        happenedAt = try container.decodeIfPresent(String.self, forKey: .happenedAt)
    }

    /// Where the result opens.
    public var opening: SearchOpening {
        guard let area else { return .nowhere }
        switch area {
        case .chats: return targetId.isEmpty ? .nowhere : .conversation(conversationId: targetId)
        case .mail: return targetId.isEmpty ? .nowhere : .mailItem(mailItemId: targetId)
        case .contacts: return targetId.isEmpty ? .nowhere : .contact(contactId: targetId)
        case .photos: return targetId.isEmpty ? .nowhere : .photo(photoId: targetId)
        case .memory: return targetId.isEmpty ? .nowhere : .knowledgePage(path: targetId)
        case .recordings: return targetId.isEmpty ? .nowhere : .recording(recordingId: targetId)
        case .places, .finance: return .partOnly(area)
        // The app has no screen for a document; it is read as the passage the search shows.
        case .documents: return .nowhere
        }
    }
}

/// SearchAreaResults is one area's results, with whether the area holds more
/// than these.
public struct SearchAreaResults: Decodable, Identifiable, Hashable, Sendable {
    public let searchArea: String
    public let results: [SearchResult]
    public let hasMore: Bool

    public var id: String { searchArea }

    public var area: SearchArea? { SearchArea(rawValue: searchArea) }

    public init(searchArea: String, results: [SearchResult], hasMore: Bool) {
        self.searchArea = searchArea
        self.results = results
        self.hasMore = hasMore
    }

    private enum CodingKeys: String, CodingKey {
        case searchArea, results, hasMore
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        searchArea = try container.decode(String.self, forKey: .searchArea)
        results = try container.decodeIfPresent([SearchResult].self, forKey: .results) ?? []
        hasMore = try container.decodeIfPresent(Bool.self, forKey: .hasMore) ?? false
    }
}

/// SearchEverythingResults is a search across everything: the areas that
/// found something, in the server's fixed order, and the areas that failed or
/// did not answer in time.
public struct SearchEverythingResults: Decodable, Hashable, Sendable {
    public let words: String
    public let areas: [SearchAreaResults]
    public let failedAreas: [String]

    public init(words: String, areas: [SearchAreaResults], failedAreas: [String]) {
        self.words = words
        self.areas = areas
        self.failedAreas = failedAreas
    }

    private enum CodingKeys: String, CodingKey {
        case words, areas, failedAreas
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        words = try container.decodeIfPresent(String.self, forKey: .words) ?? ""
        areas = try container.decodeIfPresent([SearchAreaResults].self, forKey: .areas) ?? []
        failedAreas = try container.decodeIfPresent([String].self, forKey: .failedAreas) ?? []
    }

    /// Whether nothing was found in any area.
    public var isEmpty: Bool { areas.allSatisfy { $0.results.isEmpty } }
}

/// SearchWords is what is typed in a search field, made ready to ask with.
public enum SearchWords {
    /// How many results the search asks of each area: the server's default.
    public static let perAreaLimit = 5

    /// The words with their ends trimmed and runs of white space made one
    /// space; nil when nothing is left to look for.
    public static func normalized(_ typedText: String) -> String? {
        let words = typedText.split(whereSeparator: { $0.isWhitespace || $0.isNewline }).joined(separator: " ")
        return words.isEmpty ? nil : words
    }
}

extension Documents {
    public static let searchEverything = """
        query ($words: String!, $perAreaLimit: Int) {
          SearchEverything(words: $words, perAreaLimit: $perAreaLimit) {
            words
            areas { searchArea hasMore results { searchArea targetId titleText detailText happenedAt } }
            failedAreas
          }
        }
        """
}

extension Connection {
    /// Looks for the words across everything: a few results from each area
    /// the person may use, and the areas that did not answer.
    public func searchEverything(words: String, perAreaLimit: Int = SearchWords.perAreaLimit) async throws -> SearchEverythingResults {
        struct Answer: Decodable, Sendable { let SearchEverything: SearchEverythingResults }
        let variables: [String: JSONValue] = ["words": .string(words), "perAreaLimit": .number(Double(perAreaLimit))]
        return try await execute(Documents.searchEverything, variables: variables, as: Answer.self).SearchEverything
    }
}
