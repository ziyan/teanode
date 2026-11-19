import Foundation

/// The agent's feed: the cards it posts of its own accord (an alert, a
/// report, a question it needs answered), newest first. The same operations as
/// the dashboard's Feed page and the command line's `teanode agent feed`.

/// AgentFeedFact is one line of a card's short table of facts.
public struct AgentFeedFact: Codable, Hashable, Sendable {
    public let factLabel: String
    public let factValue: String

    public init(factLabel: String, factValue: String) {
        self.factLabel = factLabel
        self.factValue = factValue
    }
}

/// AgentFeedSourceLink is a page a card was made from, listed on its info
/// sheet.
public struct AgentFeedSourceLink: Codable, Hashable, Sendable, Identifiable {
    public let sourceTitle: String
    public let sourceUrl: String

    public var id: String { sourceUrl }

    public init(sourceTitle: String, sourceUrl: String) {
        self.sourceTitle = sourceTitle
        self.sourceUrl = sourceUrl
    }
}

/// AgentFeedChoiceRole is what pressing a choice does.
public enum AgentFeedChoiceRole: String, Sendable {
    /// Answers the agent, which carries on with the answer.
    case action
    /// Answers and closes the card.
    case dismiss
    /// Opens a conversation about the card.
    case discuss
    /// Opens the coding session the card is about.
    case open
}

/// AgentFeedChoice is a button a card offers.
public struct AgentFeedChoice: Codable, Hashable, Sendable, Identifiable {
    public let choiceKey: String
    public let choiceLabel: String
    /// action, dismiss, discuss or open; see `choiceRole`.
    public let choiceRoleName: String

    public var id: String { choiceKey }

    /// What the choice does. A role this app does not know answers the
    /// agent, as an action does, so a newer server's choice still works.
    public var choiceRole: AgentFeedChoiceRole { AgentFeedChoiceRole(rawValue: choiceRoleName) ?? .action }

    public init(choiceKey: String, choiceLabel: String, choiceRoleName: String) {
        self.choiceKey = choiceKey
        self.choiceLabel = choiceLabel
        self.choiceRoleName = choiceRoleName
    }

    private enum CodingKeys: String, CodingKey {
        case choiceKey, choiceLabel
        case choiceRoleName = "choiceRole"
    }
}

/// AgentFeedPictureKind is what a card's picture is drawn from.
public enum AgentFeedPictureKind: Equatable, Sendable {
    /// An SF Symbol, by name.
    case symbol(String)
    /// An emoji or other text.
    case emoji(String)
    /// Nothing usable: the newspaper.
    case fallback

    /// The symbol drawn when a card names none, or a name this phone does
    /// not have.
    public static let fallbackSymbolName = "newspaper"

    /// What a card's picture is: a name made only of letters, digits, dots
    /// and underscores is a symbol, anything else short is an emoji.
    public static func kind(of pictureSymbol: String) -> AgentFeedPictureKind {
        let trimmed = pictureSymbol.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return .fallback }
        let isSymbolName = trimmed.unicodeScalars.allSatisfy { scalar in
            scalar.isASCII && (CharacterSet.alphanumerics.contains(scalar) || scalar == "." || scalar == "_")
        }
        if isSymbolName { return .symbol(trimmed) }
        // An emoji is a few scalars (a flag or a family is several joined); a
        // sentence is not a picture.
        return trimmed.count <= 4 ? .emoji(trimmed) : .fallback
    }
}

/// AgentFeedItem is one card of the feed.
public struct AgentFeedItem: Codable, Hashable, Sendable, Identifiable {
    public let feedItemId: String
    /// update, decision, alert, report, idea or news.
    public let feedItemKind: String
    /// goal, schedule, alert, speak_first, coding_session or agent.
    public let feedSourceKind: String
    /// The goal, schedule or alert it came from; for a coding session the
    /// computer and the pane joined by a slash; empty for the agent's own post.
    public let feedSourceId: String
    /// The goal's or schedule's name, empty otherwise.
    public let feedSourceTitle: String
    public let runConversationId: String
    public let title: String
    /// Markdown: a few lines, links allowed.
    public let body: String
    /// An SF Symbol name; see `AgentFeedPictureKind`.
    public let pictureSymbol: String
    public let factRows: [AgentFeedFact]
    public let sourceLinks: [AgentFeedSourceLink]
    public let choices: [AgentFeedChoice]
    public let chosenChoiceKey: String?
    public let chosenAt: String?
    /// The agent's one line after a choice.
    public let outcomeText: String
    /// Whether a turn resumed by the choice is still running.
    public let isOutcomePending: Bool
    /// now, today or whenever.
    public let feedUrgency: String
    public let isLiked: Bool
    public let isRead: Bool
    public let isDismissed: Bool
    public let postedAt: String
    public let updatedAt: String
    /// What the cards of one stack share; empty for a card that stands alone.
    public let feedStackKey: String
    /// In the feed's list, how many cards the stack this one tops holds,
    /// itself included; 1 for a card alone and everywhere else.
    public let stackItemCount: Int
    /// How many of the stack's cards are unread, when the list says.
    public let stackUnreadCount: Int
    /// The stack's own name ("Morning brief, Wed Nov 19"), empty for a stack
    /// named only by its source.
    public let feedStackTitle: String
    /// Where the card sits in a stack the agent put in order, counted from 0;
    /// nil when the stack has no order.
    public let stackPosition: Int?
    /// text, list, weather or agenda; how the card is drawn. See `layout`.
    public let feedCardLayout: String
    /// The layout's content, a JSON object in a string; empty for text.
    public let layoutContent: String

    public var id: String { feedItemId }

    public init(
        feedItemId: String, feedItemKind: String = "update", feedSourceKind: String = "agent", feedSourceId: String = "",
        feedSourceTitle: String = "", runConversationId: String = "", title: String, body: String = "", pictureSymbol: String = "",
        factRows: [AgentFeedFact] = [], sourceLinks: [AgentFeedSourceLink] = [], choices: [AgentFeedChoice] = [],
        chosenChoiceKey: String? = nil, chosenAt: String? = nil, outcomeText: String = "", isOutcomePending: Bool = false,
        feedUrgency: String = "whenever", isLiked: Bool = false, isRead: Bool = false, isDismissed: Bool = false, postedAt: String = "",
        updatedAt: String = "", feedStackKey: String = "", stackItemCount: Int = 1, stackUnreadCount: Int = 0,
        feedStackTitle: String = "", stackPosition: Int? = nil,
        feedCardLayout: String = "text", layoutContent: String = ""
    ) {
        self.feedItemId = feedItemId
        self.feedItemKind = feedItemKind
        self.feedSourceKind = feedSourceKind
        self.feedSourceId = feedSourceId
        self.feedSourceTitle = feedSourceTitle
        self.runConversationId = runConversationId
        self.title = title
        self.body = body
        self.pictureSymbol = pictureSymbol
        self.factRows = factRows
        self.sourceLinks = sourceLinks
        self.choices = choices
        self.chosenChoiceKey = chosenChoiceKey
        self.chosenAt = chosenAt
        self.outcomeText = outcomeText
        self.isOutcomePending = isOutcomePending
        self.feedUrgency = feedUrgency
        self.isLiked = isLiked
        self.isRead = isRead
        self.isDismissed = isDismissed
        self.postedAt = postedAt
        self.updatedAt = updatedAt
        self.feedStackKey = feedStackKey
        self.stackItemCount = stackItemCount
        self.stackUnreadCount = stackUnreadCount
        self.feedStackTitle = feedStackTitle
        self.stackPosition = stackPosition
        self.feedCardLayout = feedCardLayout
        self.layoutContent = layoutContent
    }

    /// Every field but the id and the title may be missing or null, so a
    /// server that leaves one out does not make the whole feed unreadable.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        feedItemId = try container.decode(String.self, forKey: .feedItemId)
        feedItemKind = try container.decodeIfPresent(String.self, forKey: .feedItemKind) ?? "update"
        feedSourceKind = try container.decodeIfPresent(String.self, forKey: .feedSourceKind) ?? "agent"
        feedSourceId = try container.decodeIfPresent(String.self, forKey: .feedSourceId) ?? ""
        feedSourceTitle = try container.decodeIfPresent(String.self, forKey: .feedSourceTitle) ?? ""
        runConversationId = try container.decodeIfPresent(String.self, forKey: .runConversationId) ?? ""
        title = try container.decodeIfPresent(String.self, forKey: .title) ?? ""
        body = try container.decodeIfPresent(String.self, forKey: .body) ?? ""
        pictureSymbol = try container.decodeIfPresent(String.self, forKey: .pictureSymbol) ?? ""
        factRows = try container.decodeIfPresent([AgentFeedFact?].self, forKey: .factRows)?.compactMap { $0 } ?? []
        sourceLinks = try container.decodeIfPresent([AgentFeedSourceLink?].self, forKey: .sourceLinks)?.compactMap { $0 } ?? []
        choices = try container.decodeIfPresent([AgentFeedChoice?].self, forKey: .choices)?.compactMap { $0 } ?? []
        chosenChoiceKey = try container.decodeIfPresent(String.self, forKey: .chosenChoiceKey)
        chosenAt = try container.decodeIfPresent(String.self, forKey: .chosenAt)
        outcomeText = try container.decodeIfPresent(String.self, forKey: .outcomeText) ?? ""
        isOutcomePending = try container.decodeIfPresent(Bool.self, forKey: .isOutcomePending) ?? false
        feedUrgency = try container.decodeIfPresent(String.self, forKey: .feedUrgency) ?? "whenever"
        isLiked = try container.decodeIfPresent(Bool.self, forKey: .isLiked) ?? false
        isRead = try container.decodeIfPresent(Bool.self, forKey: .isRead) ?? false
        isDismissed = try container.decodeIfPresent(Bool.self, forKey: .isDismissed) ?? false
        postedAt = try container.decodeIfPresent(String.self, forKey: .postedAt) ?? ""
        updatedAt = try container.decodeIfPresent(String.self, forKey: .updatedAt) ?? ""
        feedStackKey = try container.decodeIfPresent(String.self, forKey: .feedStackKey) ?? ""
        stackItemCount = try container.decodeIfPresent(Int.self, forKey: .stackItemCount) ?? 1
        stackUnreadCount = try container.decodeIfPresent(Int.self, forKey: .stackUnreadCount) ?? 0
        feedStackTitle = try container.decodeIfPresent(String.self, forKey: .feedStackTitle) ?? ""
        stackPosition = try container.decodeIfPresent(Int.self, forKey: .stackPosition)
        feedCardLayout = try container.decodeIfPresent(String.self, forKey: .feedCardLayout) ?? "text"
        layoutContent = try container.decodeIfPresent(String.self, forKey: .layoutContent) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case feedItemId, feedItemKind, feedSourceKind, feedSourceId, feedSourceTitle, runConversationId, title, body, pictureSymbol
        case factRows, sourceLinks, choices, chosenChoiceKey, chosenAt, outcomeText, isOutcomePending, feedUrgency
        case isLiked, isRead, isDismissed, postedAt, updatedAt, feedStackKey, stackItemCount, stackUnreadCount, feedStackTitle, stackPosition, feedCardLayout, layoutContent
    }

    /// Whether the card tops a stack of more cards from its source.
    public var isStack: Bool { stackItemCount > 1 && !feedStackKey.isEmpty }

    /// What to call the stack: its own title, else its source's name; empty
    /// when it has neither.
    public var stackDisplayTitle: String { feedStackTitle.isEmpty ? feedSourceTitle : feedStackTitle }

    /// How many more cards the stack holds under this one.
    public var stackMoreCount: Int { max(0, stackItemCount - 1) }

    /// Whether the person already answered the card.
    public var isChosen: Bool { !(chosenChoiceKey ?? "").isEmpty }

    /// The choice the person made, when the card still lists it.
    public var chosenChoice: AgentFeedChoice? {
        guard let chosenChoiceKey, !chosenChoiceKey.isEmpty else { return nil }
        return choices.first { $0.choiceKey == chosenChoiceKey }
    }

    /// Whether the card is a question the person has yet to answer.
    public var isWaitingForAnswer: Bool { !isChosen && !isDismissed && choices.contains { $0.choiceRole != .discuss } }

    /// When the card was last posted or replaced; a replaced card moves to
    /// the top, so this and not the first posting is the date shown.
    public var shownAt: Date? { ServerTime.date(updatedAt.isEmpty ? postedAt : updatedAt) }

    /// The computer and pane of the coding session a coding_session card is
    /// about, from its source id (the two joined by a slash); nil for any
    /// other card.
    public var codingSessionLocation: (computer: String, paneId: String)? {
        guard feedSourceKind == "coding_session", let slash = feedSourceId.lastIndex(of: "/") else { return nil }
        let computer = String(feedSourceId[..<slash])
        let paneId = String(feedSourceId[feedSourceId.index(after: slash)...])
        guard !computer.isEmpty, !paneId.isEmpty else { return nil }
        return (computer, paneId)
    }

    /// The picture's name or text.
    public var pictureKind: AgentFeedPictureKind { AgentFeedPictureKind.kind(of: pictureSymbol) }
}

/// AgentFeedPage is a page of the feed, newest first, and how many cards the
/// person has not read.
public struct AgentFeedPage: Decodable, Sendable {
    public let feedItems: [AgentFeedItem]
    /// Pass as `after` for the next page; nil on the last.
    public let nextCursor: String?
    public let unreadCount: Int

    public init(feedItems: [AgentFeedItem], nextCursor: String?, unreadCount: Int) {
        self.feedItems = feedItems
        self.nextCursor = nextCursor
        self.unreadCount = unreadCount
    }

    private enum CodingKeys: String, CodingKey {
        case feedItems, nextCursor, unreadCount
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        feedItems = try container.decodeIfPresent([AgentFeedItem?].self, forKey: .feedItems)?.compactMap { $0 } ?? []
        nextCursor = try container.decodeIfPresent(String.self, forKey: .nextCursor)
        unreadCount = try container.decodeIfPresent(Int.self, forKey: .unreadCount) ?? 0
    }
}

/// AgentFeedEvent is the server telling a viewer that the feed changed.
public struct AgentFeedEvent: Codable, Hashable, Sendable {
    public let feedItemId: String
    /// posted, replaced, settled or dismissed.
    public let feedChange: String
    public let unreadCount: Int
}

extension Documents {
    static let agentFeedItemFields = """
        feedItemId feedItemKind feedSourceKind feedSourceId feedSourceTitle runConversationId title body pictureSymbol
        factRows { factLabel factValue } sourceLinks { sourceTitle sourceUrl } choices { choiceKey choiceLabel choiceRole }
        chosenChoiceKey chosenAt outcomeText isOutcomePending feedUrgency isLiked isRead isDismissed postedAt updatedAt
        feedStackKey stackItemCount stackUnreadCount feedStackTitle stackPosition feedCardLayout layoutContent
        """
    public static let agentFeedStack = """
        query ($feedStackKey: String!, $first: Int, $after: String) {
          ListAgentFeedStack(feedStackKey: $feedStackKey, first: $first, after: $after) {
            feedItems { \(agentFeedItemFields) }
            nextCursor
            unreadCount
          }
        }
        """
    public static let agentFeed = """
        query ($first: Int, $after: String, $includeDismissed: Boolean) {
          ListAgentFeed(first: $first, after: $after, includeDismissed: $includeDismissed) {
            feedItems { \(agentFeedItemFields) }
            nextCursor
            unreadCount
          }
        }
        """
    public static let chooseAgentFeedItem = """
        mutation ($feedItemId: String!, $choiceKey: String!) {
          ChooseAgentFeedItem(feedItemId: $feedItemId, choiceKey: $choiceKey) { \(agentFeedItemFields) }
        }
        """
    public static let likeAgentFeedItem = """
        mutation ($feedItemId: String!, $isLiked: Boolean!) {
          LikeAgentFeedItem(feedItemId: $feedItemId, isLiked: $isLiked) { \(agentFeedItemFields) }
        }
        """
    public static let showLessLikeAgentFeedItem = """
        mutation ($feedItemId: String!) {
          ShowLessLikeAgentFeedItem(feedItemId: $feedItemId) { \(agentFeedItemFields) }
        }
        """
    public static let dismissAgentFeedItem = """
        mutation ($feedItemId: String!) {
          DismissAgentFeedItem(feedItemId: $feedItemId) { \(agentFeedItemFields) }
        }
        """
    public static let markAgentFeedRead = """
        mutation ($feedItemIds: [String!]!) {
          MarkAgentFeedRead(feedItemIds: $feedItemIds)
        }
        """
    public static let discussAgentFeedItem = """
        mutation ($feedItemId: String!) {
          DiscussAgentFeedItem(feedItemId: $feedItemId)
        }
        """
    public static let agentFeedEvents = """
        subscription { AgentFeedChanged { feedItemId feedChange unreadCount } }
        """

    /// The feed's documents, for the tests that check them against the
    /// operations' names.
    public static let agentFeedDocuments: [String: String] = [
        "agentFeed": agentFeed, "agentFeedStack": agentFeedStack, "chooseAgentFeedItem": chooseAgentFeedItem, "likeAgentFeedItem": likeAgentFeedItem,
        "showLessLikeAgentFeedItem": showLessLikeAgentFeedItem, "dismissAgentFeedItem": dismissAgentFeedItem,
        "markAgentFeedRead": markAgentFeedRead, "discussAgentFeedItem": discussAgentFeedItem, "agentFeedEvents": agentFeedEvents,
    ]
}

extension Connection {
    /// A page of the feed, newest first. `after` is the previous page's
    /// `nextCursor`.
    public func agentFeed(after cursor: String? = nil, pageSize: Int = 30, includeDismissed: Bool = false) async throws -> AgentFeedPage {
        struct Answer: Decodable, Sendable { let ListAgentFeed: AgentFeedPage }
        var variables: [String: JSONValue] = ["first": .number(Double(pageSize))]
        if let cursor, !cursor.isEmpty { variables["after"] = .string(cursor) }
        if includeDismissed { variables["includeDismissed"] = .bool(true) }
        return try await execute(Documents.agentFeed, variables: variables, as: Answer.self).ListAgentFeed
    }

    /// One stack's cards, newest first. `after` is the previous page's
    /// `nextCursor`.
    public func agentFeedStack(_ feedStackKey: String, after cursor: String? = nil, pageSize: Int = 30) async throws -> AgentFeedPage {
        struct Answer: Decodable, Sendable { let ListAgentFeedStack: AgentFeedPage }
        var variables: [String: JSONValue] = ["feedStackKey": .string(feedStackKey), "first": .number(Double(pageSize))]
        if let cursor, !cursor.isEmpty { variables["after"] = .string(cursor) }
        return try await execute(Documents.agentFeedStack, variables: variables, as: Answer.self).ListAgentFeedStack
    }

    /// Answers a card with one of its choices; the card comes back as it is
    /// then, settled on the choice.
    public func chooseAgentFeedItem(_ feedItemId: String, choiceKey: String) async throws -> AgentFeedItem {
        struct Answer: Decodable, Sendable { let ChooseAgentFeedItem: AgentFeedItem }
        return try await execute(
            Documents.chooseAgentFeedItem, variables: ["feedItemId": .string(feedItemId), "choiceKey": .string(choiceKey)], as: Answer.self
        ).ChooseAgentFeedItem
    }

    /// Hearts a card, or takes the heart back; the agent learns what the
    /// person likes from it.
    public func likeAgentFeedItem(_ feedItemId: String, isLiked: Bool) async throws -> AgentFeedItem {
        struct Answer: Decodable, Sendable { let LikeAgentFeedItem: AgentFeedItem }
        return try await execute(
            Documents.likeAgentFeedItem, variables: ["feedItemId": .string(feedItemId), "isLiked": .bool(isLiked)], as: Answer.self
        ).LikeAgentFeedItem
    }

    /// Records that the person wants fewer cards like this one, and closes it.
    public func showLessLikeAgentFeedItem(_ feedItemId: String) async throws -> AgentFeedItem {
        struct Answer: Decodable, Sendable { let ShowLessLikeAgentFeedItem: AgentFeedItem }
        return try await execute(Documents.showLessLikeAgentFeedItem, variables: ["feedItemId": .string(feedItemId)], as: Answer.self)
            .ShowLessLikeAgentFeedItem
    }

    public func dismissAgentFeedItem(_ feedItemId: String) async throws -> AgentFeedItem {
        struct Answer: Decodable, Sendable { let DismissAgentFeedItem: AgentFeedItem }
        return try await execute(Documents.dismissAgentFeedItem, variables: ["feedItemId": .string(feedItemId)], as: Answer.self)
            .DismissAgentFeedItem
    }

    /// Marks cards read; returns how many were newly read.
    @discardableResult
    public func markAgentFeedRead(_ feedItemIds: [String]) async throws -> Int {
        struct Answer: Decodable, Sendable { let MarkAgentFeedRead: Int }
        return try await execute(
            Documents.markAgentFeedRead, variables: ["feedItemIds": .array(feedItemIds.map { .string($0) })], as: Answer.self
        ).MarkAgentFeedRead
    }

    /// Opens the main conversation about a card, with the card attached to
    /// the next message; returns the conversation's id.
    public func discussAgentFeedItem(_ feedItemId: String) async throws -> String {
        struct Answer: Decodable, Sendable { let DiscussAgentFeedItem: String }
        return try await execute(Documents.discussAgentFeedItem, variables: ["feedItemId": .string(feedItemId)], as: Answer.self)
            .DiscussAgentFeedItem
    }
}
