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

/// AgentFeedMediaKind is what a card's picture or video is of.
public enum AgentFeedMediaKind: String, Sendable {
    /// One of the person's own photos.
    case photo
    /// One of the person's own videos; imageUrl is its poster.
    case video
    /// The picture of a web page, kept by the server.
    case picture
    /// A still from one of the person's cameras.
    case cameraStill = "camera_still"
}

/// AgentFeedMedia is one picture or video of a card, or, when `listRowIndex`
/// is set, the thumbnail of that row of a list card. The addresses are server
/// paths that work for the signed-in person only.
public struct AgentFeedMedia: Codable, Hashable, Sendable, Identifiable {
    public let mediaKey: String
    /// photo, video or picture; see `mediaKind`.
    public let feedMediaKind: String
    public let imageUrl: String
    /// The video's preview path; for a video only.
    public let videoUrl: String
    /// The page a picture came from; for a picture only.
    public let linkUrl: String
    public let siteName: String
    public let mediaCaption: String
    public let widthPixels: Int
    public let heightPixels: Int
    /// nil is the card's own media; a number is the thumbnail of that row of a list.
    public let listRowIndex: Int?
    /// The camera a still is from; for a camera still only.
    public let cameraName: String
    /// When the still was taken (RFC 3339), on the camera's own clock with its
    /// offset; for a camera still only.
    public let capturedAt: String
    /// Why this camera was chosen; for a camera still only.
    public let stillReason: String
    /// Where the camera's recording of that moment plays; optional.
    public let playbackUrl: String
    /// Where the camera's live view plays; optional.
    public let liveUrl: String

    public var id: String { mediaKey }

    public init(
        mediaKey: String, feedMediaKind: String = "picture", imageUrl: String = "", videoUrl: String = "", linkUrl: String = "",
        siteName: String = "", mediaCaption: String = "", widthPixels: Int = 0, heightPixels: Int = 0, listRowIndex: Int? = nil,
        cameraName: String = "", capturedAt: String = "", stillReason: String = "", playbackUrl: String = "", liveUrl: String = ""
    ) {
        self.mediaKey = mediaKey
        self.feedMediaKind = feedMediaKind
        self.imageUrl = imageUrl
        self.videoUrl = videoUrl
        self.linkUrl = linkUrl
        self.siteName = siteName
        self.mediaCaption = mediaCaption
        self.widthPixels = widthPixels
        self.heightPixels = heightPixels
        self.listRowIndex = listRowIndex
        self.cameraName = cameraName
        self.capturedAt = capturedAt
        self.stillReason = stillReason
        self.playbackUrl = playbackUrl
        self.liveUrl = liveUrl
    }

    /// Every field but the key may be missing or null.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        mediaKey = try container.decode(String.self, forKey: .mediaKey)
        feedMediaKind = (try? container.decodeIfPresent(String.self, forKey: .feedMediaKind)) ?? "picture"
        imageUrl = (try? container.decodeIfPresent(String.self, forKey: .imageUrl)) ?? ""
        videoUrl = (try? container.decodeIfPresent(String.self, forKey: .videoUrl)) ?? ""
        linkUrl = (try? container.decodeIfPresent(String.self, forKey: .linkUrl)) ?? ""
        siteName = (try? container.decodeIfPresent(String.self, forKey: .siteName)) ?? ""
        mediaCaption = (try? container.decodeIfPresent(String.self, forKey: .mediaCaption)) ?? ""
        widthPixels = (try? container.decodeIfPresent(Int.self, forKey: .widthPixels)) ?? 0
        heightPixels = (try? container.decodeIfPresent(Int.self, forKey: .heightPixels)) ?? 0
        listRowIndex = (try? container.decodeIfPresent(Int.self, forKey: .listRowIndex)) ?? nil
        cameraName = (try? container.decodeIfPresent(String.self, forKey: .cameraName)) ?? ""
        capturedAt = (try? container.decodeIfPresent(String.self, forKey: .capturedAt)) ?? ""
        stillReason = (try? container.decodeIfPresent(String.self, forKey: .stillReason)) ?? ""
        playbackUrl = (try? container.decodeIfPresent(String.self, forKey: .playbackUrl)) ?? ""
        liveUrl = (try? container.decodeIfPresent(String.self, forKey: .liveUrl)) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case mediaKey, feedMediaKind, imageUrl, videoUrl, linkUrl, siteName, mediaCaption, widthPixels, heightPixels, listRowIndex
        case cameraName, capturedAt, stillReason, playbackUrl, liveUrl
    }

    /// A kind this app does not know is drawn as a picture.
    public var mediaKind: AgentFeedMediaKind { AgentFeedMediaKind(rawValue: feedMediaKind) ?? .picture }

    /// Whether the picture is taller than wide, which the sizes say.
    public var isPortrait: Bool { widthPixels > 0 && heightPixels > widthPixels }

    /// The width over the height one item shown alone is cropped to: 16:9, or
    /// 4:5 for a portrait photo.
    public var singleAspectRatio: Double { isPortrait ? 4.0 / 5.0 : 16.0 / 9.0 }

    /// The page a picture opens; only http and https are followed.
    public var linkAddress: URL? {
        guard !linkUrl.isEmpty, let url = URL(string: linkUrl), let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https" else {
            return nil
        }
        return url
    }

    /// The address of a camera's recording or live view: https, or the app's
    /// own scheme; anything else is not followed.
    private static func cameraAddress(_ text: String) -> URL? {
        guard !text.isEmpty, let url = URL(string: text), let scheme = url.scheme?.lowercased(), scheme == "https" || scheme == "teanode" else {
            return nil
        }
        return url
    }

    /// Where a tap on a camera still goes.
    public var playbackAddress: URL? { Self.cameraAddress(playbackUrl) }

    /// Where the Watch Live button goes.
    public var liveAddress: URL? { Self.cameraAddress(liveUrl) }

    /// The camera's own time of day ("14:02:11"), read from the text as the
    /// camera wrote it, never converted; empty when it cannot be read.
    public var capturedClockText: String {
        guard let timeStart = capturedAt.firstIndex(where: { $0 == "T" || $0 == "t" }) else { return "" }
        let clock = capturedAt[capturedAt.index(after: timeStart)...].prefix(8)
        let parts = clock.split(separator: ":")
        guard clock.count == 8, parts.count == 3, parts.allSatisfy({ $0.count == 2 && $0.allSatisfy(\.isNumber) }) else { return "" }
        return String(clock)
    }

    /// The camera's zone, when it is not the phone's ("UTC+9"); empty otherwise.
    public var capturedZoneText: String { AgentFeedZone.foreignName(of: capturedAt) ?? "" }

    /// The photo a video is, from its path (`/api/v1/photos/<id>/previews/video`),
    /// which the app asks a playable address for.
    public var videoPhotoId: String? {
        let parts = videoUrl.split(separator: "?").first.map { $0.split(separator: "/").map(String.init) } ?? []
        guard let index = parts.firstIndex(of: "photos"), index + 1 < parts.count else { return nil }
        return parts[index + 1]
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
    /// When the card was dismissed (RFC 3339); nil for a card that is not.
    public let dismissedAt: String?
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
    /// The card's pictures and videos, and the thumbnails of a list's rows;
    /// see `cardMedia` and `rowThumbnails`.
    public let feedMedia: [AgentFeedMedia]
    /// Whether the problem the card told of has cleared.
    public let isResolved: Bool
    /// When the card was resolved (RFC 3339); nil for a card that is not.
    public let resolvedAt: String?
    /// One line on how it was resolved; empty when none was given.
    public let resolutionNote: String
    /// goal, person or agent; empty for a card that is not resolved.
    public let resolvedByKind: String
    /// The card's charts, at most two.
    public let chartBlocks: [AgentFeedChart]

    public var id: String { feedItemId }

    public init(
        feedItemId: String, feedItemKind: String = "update", feedSourceKind: String = "agent", feedSourceId: String = "",
        feedSourceTitle: String = "", runConversationId: String = "", title: String, body: String = "", pictureSymbol: String = "",
        factRows: [AgentFeedFact] = [], sourceLinks: [AgentFeedSourceLink] = [], choices: [AgentFeedChoice] = [],
        chosenChoiceKey: String? = nil, chosenAt: String? = nil, outcomeText: String = "", isOutcomePending: Bool = false,
        feedUrgency: String = "whenever", isLiked: Bool = false, isRead: Bool = false, isDismissed: Bool = false, dismissedAt: String? = nil,
        postedAt: String = "", updatedAt: String = "", feedStackKey: String = "", stackItemCount: Int = 1, stackUnreadCount: Int = 0,
        feedStackTitle: String = "", stackPosition: Int? = nil,
        feedCardLayout: String = "text", layoutContent: String = "", feedMedia: [AgentFeedMedia] = [],
        isResolved: Bool = false, resolvedAt: String? = nil, resolutionNote: String = "", resolvedByKind: String = "",
        chartBlocks: [AgentFeedChart] = []
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
        self.dismissedAt = dismissedAt
        self.postedAt = postedAt
        self.updatedAt = updatedAt
        self.feedStackKey = feedStackKey
        self.stackItemCount = stackItemCount
        self.stackUnreadCount = stackUnreadCount
        self.feedStackTitle = feedStackTitle
        self.stackPosition = stackPosition
        self.feedCardLayout = feedCardLayout
        self.layoutContent = layoutContent
        self.feedMedia = feedMedia
        self.isResolved = isResolved
        self.resolvedAt = resolvedAt
        self.resolutionNote = resolutionNote
        self.resolvedByKind = resolvedByKind
        self.chartBlocks = chartBlocks
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
        dismissedAt = try container.decodeIfPresent(String.self, forKey: .dismissedAt)
        postedAt = try container.decodeIfPresent(String.self, forKey: .postedAt) ?? ""
        updatedAt = try container.decodeIfPresent(String.self, forKey: .updatedAt) ?? ""
        feedStackKey = try container.decodeIfPresent(String.self, forKey: .feedStackKey) ?? ""
        stackItemCount = try container.decodeIfPresent(Int.self, forKey: .stackItemCount) ?? 1
        stackUnreadCount = try container.decodeIfPresent(Int.self, forKey: .stackUnreadCount) ?? 0
        feedStackTitle = try container.decodeIfPresent(String.self, forKey: .feedStackTitle) ?? ""
        stackPosition = try container.decodeIfPresent(Int.self, forKey: .stackPosition)
        feedCardLayout = try container.decodeIfPresent(String.self, forKey: .feedCardLayout) ?? "text"
        layoutContent = try container.decodeIfPresent(String.self, forKey: .layoutContent) ?? ""
        feedMedia = (try? container.decodeIfPresent([AgentFeedMedia?].self, forKey: .feedMedia))?.compactMap { $0 } ?? []
        // A server that does not know resolving leaves these out: the card is not resolved.
        isResolved = (try? container.decodeIfPresent(Bool.self, forKey: .isResolved)) ?? false
        resolvedAt = (try? container.decodeIfPresent(String.self, forKey: .resolvedAt)) ?? nil
        resolutionNote = (try? container.decodeIfPresent(String.self, forKey: .resolutionNote)) ?? ""
        resolvedByKind = (try? container.decodeIfPresent(String.self, forKey: .resolvedByKind)) ?? ""
        chartBlocks = (try? container.decodeIfPresent([AgentFeedChart?].self, forKey: .chartBlocks))?.compactMap { $0 } ?? []
    }

    private enum CodingKeys: String, CodingKey {
        case feedItemId, feedItemKind, feedSourceKind, feedSourceId, feedSourceTitle, runConversationId, title, body, pictureSymbol
        case factRows, sourceLinks, choices, chosenChoiceKey, chosenAt, outcomeText, isOutcomePending, feedUrgency
        case isLiked, isRead, isDismissed, dismissedAt, postedAt, updatedAt, feedStackKey, stackItemCount, stackUnreadCount, feedStackTitle, stackPosition, feedCardLayout, layoutContent
        case feedMedia, isResolved, resolvedAt, resolutionNote, resolvedByKind, chartBlocks
    }

    /// When the card was resolved, for a card that is.
    public var resolvedDate: Date? { resolvedAt.flatMap { ServerTime.date($0) } }

    /// When the card was dismissed, for a card that is.
    public var dismissedDate: Date? { dismissedAt.flatMap { ServerTime.date($0) } }

    /// This card with the stack it tops counted as `stackItemCount` cards,
    /// `stackUnreadCount` of them unread. A list that groups cards into decks
    /// itself (see `AgentFeedDeck`) uses it so the top card is drawn as a deck.
    public func withStackCounts(stackItemCount: Int, stackUnreadCount: Int) -> AgentFeedItem {
        AgentFeedItem(
            feedItemId: feedItemId, feedItemKind: feedItemKind, feedSourceKind: feedSourceKind, feedSourceId: feedSourceId,
            feedSourceTitle: feedSourceTitle, runConversationId: runConversationId, title: title, body: body, pictureSymbol: pictureSymbol,
            factRows: factRows, sourceLinks: sourceLinks, choices: choices, chosenChoiceKey: chosenChoiceKey, chosenAt: chosenAt,
            outcomeText: outcomeText, isOutcomePending: isOutcomePending, feedUrgency: feedUrgency, isLiked: isLiked, isRead: isRead,
            isDismissed: isDismissed, dismissedAt: dismissedAt, postedAt: postedAt, updatedAt: updatedAt, feedStackKey: feedStackKey,
            stackItemCount: stackItemCount, stackUnreadCount: stackUnreadCount, feedStackTitle: feedStackTitle, stackPosition: stackPosition,
            feedCardLayout: feedCardLayout, layoutContent: layoutContent, feedMedia: feedMedia,
            isResolved: isResolved, resolvedAt: resolvedAt, resolutionNote: resolutionNote, resolvedByKind: resolvedByKind, chartBlocks: chartBlocks)
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

    /// The card's own pictures and videos, in order; what has no picture to
    /// draw is left out.
    public var cardMedia: [AgentFeedMedia] { feedMedia.filter { $0.listRowIndex == nil && !$0.imageUrl.isEmpty } }

    /// The card's camera stills, which are drawn apart from its other pictures.
    public var cameraStills: [AgentFeedMedia] { cardMedia.filter { $0.mediaKind == .cameraStill } }

    /// The card's pictures and videos but its camera stills.
    public var pictureMedia: [AgentFeedMedia] { cardMedia.filter { $0.mediaKind != .cameraStill } }

    /// The charts there is something to draw for.
    public var drawableCharts: [AgentFeedChart] { chartBlocks.filter(\.isDrawable) }

    /// The thumbnails of a list card's rows, by the row's index; the first
    /// one given for a row wins.
    public var rowThumbnails: [Int: AgentFeedMedia] {
        var thumbnails: [Int: AgentFeedMedia] = [:]
        for media in feedMedia where !media.imageUrl.isEmpty {
            guard let rowIndex = media.listRowIndex, thumbnails[rowIndex] == nil else { continue }
            thumbnails[rowIndex] = media
        }
        return thumbnails
    }

    /// The small line above the headline, as a news app labels a story: the
    /// first picture's site, else the source's name; empty when there is neither.
    public var headerText: String {
        let siteName = cardMedia.first { $0.mediaKind == .picture && !$0.siteName.isEmpty }?.siteName ?? ""
        return siteName.isEmpty ? feedSourceTitle : siteName
    }

    /// The picture's name or text.
    public var pictureKind: AgentFeedPictureKind { AgentFeedPictureKind.kind(of: pictureSymbol) }
}

/// AgentFeedDeck is the cards of a list that share one stack key and sit next
/// to each other, drawn as one deck with the newest on top. The dismissed list
/// has no folding by the server, so the client makes the decks.
public struct AgentFeedDeck: Hashable, Sendable, Identifiable {
    /// The deck's cards in the order the list gave them; never empty.
    public let feedItems: [AgentFeedItem]

    /// `feedItems` must not be empty.
    public init(feedItems: [AgentFeedItem]) {
        precondition(!feedItems.isEmpty, "a deck holds at least one card")
        self.feedItems = feedItems
    }

    public var id: String { feedItems[0].feedItemId }
    public var feedStackKey: String { feedItems[0].feedStackKey }
    /// Whether the deck holds more than one card.
    public var isDeck: Bool { feedItems.count > 1 }

    /// The top card, counting the deck's cards as the stack it tops, so a card
    /// view draws it with its "more" row.
    public var topItem: AgentFeedItem {
        let top = feedItems[0]
        guard isDeck else { return top }
        return top.withStackCounts(stackItemCount: feedItems.count, stackUnreadCount: feedItems.filter { !$0.isRead }.count)
    }

    /// Groups consecutive entries with the same non-empty stack key into
    /// decks. A card with no key, or whose neighbours have another key, is a
    /// deck of its own, and the order of the list is kept.
    public static func group(_ feedItems: [AgentFeedItem]) -> [AgentFeedDeck] {
        var runs: [[AgentFeedItem]] = []
        for feedItem in feedItems {
            if let last = runs.last?.first, !feedItem.feedStackKey.isEmpty, last.feedStackKey == feedItem.feedStackKey {
                runs[runs.count - 1].append(feedItem)
            } else {
                runs.append([feedItem])
            }
        }
        return runs.map { AgentFeedDeck(feedItems: $0) }
    }
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
    /// posted, replaced, settled, dismissed, resolved or unresolved.
    public let feedChange: String
    public let unreadCount: Int
}

extension Documents {
    static let agentFeedItemFields = """
        feedItemId feedItemKind feedSourceKind feedSourceId feedSourceTitle runConversationId title body pictureSymbol
        factRows { factLabel factValue } sourceLinks { sourceTitle sourceUrl } choices { choiceKey choiceLabel choiceRole }
        chosenChoiceKey chosenAt outcomeText isOutcomePending feedUrgency isLiked isRead isDismissed postedAt updatedAt
        feedStackKey stackItemCount stackUnreadCount feedStackTitle stackPosition feedCardLayout layoutContent dismissedAt
        feedMedia {
          mediaKey feedMediaKind imageUrl videoUrl linkUrl siteName mediaCaption widthPixels heightPixels listRowIndex
          cameraName capturedAt stillReason playbackUrl liveUrl
        }
        isResolved resolvedAt resolutionNote resolvedByKind
        chartBlocks {
          chartKind chartTitle unit provenance xLabels
          series { seriesName seriesRole values }
          markers { markerAt markerLabel }
          bars { barLabel barValue }
        }
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
        query ($first: Int, $after: String, $includeDismissed: Boolean, $isDismissedOnly: Boolean) {
          ListAgentFeed(first: $first, after: $after, includeDismissed: $includeDismissed, isDismissedOnly: $isDismissedOnly) {
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
    public static let dismissAgentFeedItems = """
        mutation ($feedItemIds: [String!], $feedStackKey: String, $isEveryReadCard: Boolean) {
          DismissAgentFeedItems(feedItemIds: $feedItemIds, feedStackKey: $feedStackKey, isEveryReadCard: $isEveryReadCard)
        }
        """
    public static let restoreAgentFeedItems = """
        mutation ($feedItemIds: [String!]!) {
          RestoreAgentFeedItems(feedItemIds: $feedItemIds)
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
    public static let resolveAgentFeedItem = """
        mutation ($feedItemId: String!, $resolutionNote: String) {
          ResolveAgentFeedItem(feedItemId: $feedItemId, resolutionNote: $resolutionNote) { \(agentFeedItemFields) }
        }
        """
    public static let unresolveAgentFeedItem = """
        mutation ($feedItemId: String!) {
          UnresolveAgentFeedItem(feedItemId: $feedItemId) { \(agentFeedItemFields) }
        }
        """
    public static let agentFeedEvents = """
        subscription { AgentFeedChanged { feedItemId feedChange unreadCount } }
        """

    /// The feed's documents, for the tests that check them against the
    /// operations' names.
    public static let agentFeedDocuments: [String: String] = [
        "agentFeed": agentFeed, "agentFeedStack": agentFeedStack, "chooseAgentFeedItem": chooseAgentFeedItem, "likeAgentFeedItem": likeAgentFeedItem,
        "showLessLikeAgentFeedItem": showLessLikeAgentFeedItem, "dismissAgentFeedItems": dismissAgentFeedItems,
        "restoreAgentFeedItems": restoreAgentFeedItems,
        "markAgentFeedRead": markAgentFeedRead, "discussAgentFeedItem": discussAgentFeedItem, "agentFeedEvents": agentFeedEvents,
        "resolveAgentFeedItem": resolveAgentFeedItem, "unresolveAgentFeedItem": unresolveAgentFeedItem,
    ]
}

extension Connection {
    /// A page of the feed, newest first. `after` is the previous page's
    /// `nextCursor`. `isDismissedOnly` lists only the dismissed cards, each
    /// on its own, and wins over `includeDismissed`.
    public func agentFeed(after cursor: String? = nil, pageSize: Int = 30, includeDismissed: Bool = false, isDismissedOnly: Bool = false) async throws -> AgentFeedPage {
        struct Answer: Decodable, Sendable { let ListAgentFeed: AgentFeedPage }
        var variables: [String: JSONValue] = ["first": .number(Double(pageSize))]
        if let cursor, !cursor.isEmpty { variables["after"] = .string(cursor) }
        if includeDismissed { variables["includeDismissed"] = .bool(true) }
        if isDismissedOnly { variables["isDismissedOnly"] = .bool(true) }
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

    /// Marks a card resolved, with an optional line on how; the card comes
    /// back as it is then. A card already resolved is refused by the server,
    /// so the first resolution stands.
    public func resolveAgentFeedItem(_ feedItemId: String, resolutionNote: String = "") async throws -> AgentFeedItem {
        struct Answer: Decodable, Sendable { let ResolveAgentFeedItem: AgentFeedItem }
        var variables: [String: JSONValue] = ["feedItemId": .string(feedItemId)]
        let trimmedNote = resolutionNote.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmedNote.isEmpty { variables["resolutionNote"] = .string(trimmedNote) }
        return try await execute(Documents.resolveAgentFeedItem, variables: variables, as: Answer.self).ResolveAgentFeedItem
    }

    /// Takes the resolution off a card.
    public func unresolveAgentFeedItem(_ feedItemId: String) async throws -> AgentFeedItem {
        struct Answer: Decodable, Sendable { let UnresolveAgentFeedItem: AgentFeedItem }
        return try await execute(Documents.unresolveAgentFeedItem, variables: ["feedItemId": .string(feedItemId)], as: Answer.self)
            .UnresolveAgentFeedItem
    }

    /// Records that the person wants fewer cards like this one, and closes it.
    public func showLessLikeAgentFeedItem(_ feedItemId: String) async throws -> AgentFeedItem {
        struct Answer: Decodable, Sendable { let ShowLessLikeAgentFeedItem: AgentFeedItem }
        return try await execute(Documents.showLessLikeAgentFeedItem, variables: ["feedItemId": .string(feedItemId)], as: Answer.self)
            .ShowLessLikeAgentFeedItem
    }

    /// What a dismissal is asked of: these cards, every card of a stack, or
    /// every card already read. The server takes exactly one of the three.
    public enum AgentFeedDismissal: Sendable, Equatable {
        case feedItemIds([String])
        case feedStackKey(String)
        case everyReadCard
    }

    /// Dismisses cards and returns the ids actually dismissed, which leave
    /// out cards that were dismissed already.
    public func dismissAgentFeedItems(_ dismissal: AgentFeedDismissal) async throws -> [String] {
        struct Answer: Decodable, Sendable { let DismissAgentFeedItems: [String] }
        var variables: [String: JSONValue] = [:]
        switch dismissal {
        case .feedItemIds(let feedItemIds): variables["feedItemIds"] = .array(feedItemIds.map { .string($0) })
        case .feedStackKey(let feedStackKey): variables["feedStackKey"] = .string(feedStackKey)
        case .everyReadCard: variables["isEveryReadCard"] = .bool(true)
        }
        return try await execute(Documents.dismissAgentFeedItems, variables: variables, as: Answer.self).DismissAgentFeedItems
    }

    /// Brings back cards that were dismissed; returns the ids restored.
    public func restoreAgentFeedItems(_ feedItemIds: [String]) async throws -> [String] {
        struct Answer: Decodable, Sendable { let RestoreAgentFeedItems: [String] }
        return try await execute(
            Documents.restoreAgentFeedItems, variables: ["feedItemIds": .array(feedItemIds.map { .string($0) })], as: Answer.self
        ).RestoreAgentFeedItems
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
