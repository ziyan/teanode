import Foundation

// What the page behind a link says about itself, for the card under a message
// (ReadLinkPreviews, internal/api/v1api/apigraph/link_preview.go). The server
// fetches the page and its picture, never this phone, and keeps both, so asking
// again is cheap. The picture is the server's copy, read from it like any other
// picture of an account.

/// LinkPreview is one link's preview. When `isAvailable` is false the page
/// could not be read or had nothing to say, and the rest is empty.
public struct LinkPreview: Decodable, Hashable, Sendable, Identifiable {
    /// The link as it was asked for.
    public let url: String
    /// The link without its tracking parameters: what a tap opens.
    public let cleanedUrl: String
    public let displayText: String
    public let isAvailable: Bool
    public let title: String
    public let description: String
    /// What the page calls its site, or its host when it says nothing.
    public let siteName: String
    /// The picture, on the person's own server; nil when the page has none.
    public let imageUrl: String?
    public let imageWidthPixels: Int?
    public let imageHeightPixels: Int?

    public var id: String { url }

    public init(
        url: String, cleanedUrl: String, displayText: String, isAvailable: Bool, title: String = "", description: String = "",
        siteName: String = "", imageUrl: String? = nil, imageWidthPixels: Int? = nil, imageHeightPixels: Int? = nil
    ) {
        self.url = url
        self.cleanedUrl = cleanedUrl
        self.displayText = displayText
        self.isAvailable = isAvailable
        self.title = title
        self.description = description
        self.siteName = siteName
        self.imageUrl = imageUrl
        self.imageWidthPixels = imageWidthPixels
        self.imageHeightPixels = imageHeightPixels
    }

    private enum CodingKeys: String, CodingKey {
        case url, cleanedUrl, displayText, isAvailable, title, description, siteName, imageUrl, imageWidthPixels, imageHeightPixels
    }

    /// A missing field is empty rather than a failure, so one odd preview does
    /// not lose the rest.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        url = try container.decode(String.self, forKey: .url)
        cleanedUrl = try container.decodeIfPresent(String.self, forKey: .cleanedUrl) ?? url
        displayText = try container.decodeIfPresent(String.self, forKey: .displayText) ?? url
        isAvailable = try container.decodeIfPresent(Bool.self, forKey: .isAvailable) ?? false
        title = try container.decodeIfPresent(String.self, forKey: .title) ?? ""
        description = try container.decodeIfPresent(String.self, forKey: .description) ?? ""
        siteName = try container.decodeIfPresent(String.self, forKey: .siteName) ?? ""
        imageUrl = try container.decodeIfPresent(String.self, forKey: .imageUrl)
        imageWidthPixels = try container.decodeIfPresent(Int.self, forKey: .imageWidthPixels)
        imageHeightPixels = try container.decodeIfPresent(Int.self, forKey: .imageHeightPixels)
    }

    /// Whether there is a card to draw: the page was read, its address is a
    /// web address, and there is a title or a line to put on it.
    public var hasCard: Bool {
        guard isAvailable, let scheme = URL(string: cleanedUrl)?.scheme?.lowercased(), scheme == "http" || scheme == "https" else { return false }
        return !title.isEmpty || !description.isEmpty
    }

    /// How many links one question may be about.
    public static let mostURLs = 10

    /// The links whose cards the lines read so far show, the newest line's
    /// first and each once, to be asked for before the lazy list draws those
    /// lines: a card that arrives after its message is drawn makes the message
    /// taller under the person's finger. Each line is read as its view reads
    /// it: a person's words as they stand and the agent's as Markdown, and
    /// none of a message still being written or sent, or taken back.
    public static func linksAhead(of lines: [TranscriptLine]) -> [String] {
        var seen = Set<String>()
        var links: [String] = []
        for line in lines.reversed() {
            let link: String?
            switch line.kind {
            case .person where !line.isBeingSent && !line.isDeleted:
                link = LinkText.firstLink(in: line.text, isMarkdown: false)
            case .answer where !line.isStreaming && !line.isDeleted:
                link = LinkText.firstLink(in: line.text, isMarkdown: true)
            default:
                link = nil
            }
            if let link, seen.insert(link).inserted { links.append(link) }
        }
        return links
    }
}

extension Documents {
    public static let readLinkPreviews = """
        query ($conversationId: String!, $urls: [String!]!) {
          ReadLinkPreviews(conversationId: $conversationId, urls: $urls) {
            url cleanedUrl displayText isAvailable title description siteName imageUrl imageWidthPixels imageHeightPixels
          }
        }
        """
}

extension Connection {
    /// The preview of each link, in the order given, at most
    /// `LinkPreview.mostURLs` of them, for links in the words of a
    /// conversation. The server previews a link only if its exact text is in a
    /// message of that conversation that the agent did not write, so the links
    /// are sent as they stand in the messages, never cleaned.
    public func linkPreviews(conversationId: String, urls: [String]) async throws -> [LinkPreview] {
        struct Answer: Decodable, Sendable { let ReadLinkPreviews: [LinkPreview] }
        let variables: [String: JSONValue] = ["conversationId": .string(conversationId), "urls": .array(urls.map { .string($0) })]
        return try await execute(Documents.readLinkPreviews, variables: variables, as: Answer.self).ReadLinkPreviews
    }
}

/// LinkPreviewLoader asks for the previews of the links of a screenful in one
/// question for each conversation, ten at a time, and never asks again for a
/// link in a conversation it has asked for (until a question that failed has
/// waited out `failedFor`). The server keeps what it read, but a conversation
/// holds many links, and one question a link would be one trip each.
public actor LinkPreviewLoader {
    public typealias Load = @Sendable (_ conversationId: String, _ urls: [String]) async throws -> [LinkPreview]

    private struct Waiting {
        let conversationId: String
        let url: String
        let settle: CheckedContinuation<LinkPreview?, Never>
    }

    private let load: Load
    private let collectingFor: Duration
    private let failedFor: Duration
    private var known: [String: Task<LinkPreview?, Never>] = [:]
    private var waiting: [Waiting] = []
    private var isCollecting = false

    public init(collectingFor: Duration = .milliseconds(60), failedFor: Duration = .seconds(60), load: @escaping Load) {
        self.collectingFor = collectingFor
        self.failedFor = failedFor
        self.load = load
    }

    private static func key(_ conversationId: String, _ url: String) -> String { conversationId + "\n" + url }

    /// The preview of a link written in a conversation, or nil when there is
    /// none to show.
    public func preview(conversationId: String, for url: String) async -> LinkPreview? {
        let key = Self.key(conversationId, url)
        if let asked = known[key] { return await asked.value }
        let asked = Task<LinkPreview?, Never> { await self.answer(conversationId: conversationId, for: url) }
        known[key] = asked
        return await asked.value
    }

    /// Waits for the question the link goes out in to be answered.
    private func answer(conversationId: String, for url: String) async -> LinkPreview? {
        await withCheckedContinuation { settle in
            waiting.append(Waiting(conversationId: conversationId, url: url, settle: settle))
            guard !isCollecting else { return }
            isCollecting = true
            Task { await self.sendWaiting() }
        }
    }

    private func sendWaiting() async {
        try? await Task.sleep(for: collectingFor)
        while let first = waiting.first {
            // The oldest question's conversation goes first, with as many of
            // its links as one question takes.
            let batch = Array(waiting.filter { $0.conversationId == first.conversationId }.prefix(LinkPreview.mostURLs))
            var taken = batch.count
            waiting.removeAll { each in
                guard taken > 0, each.conversationId == first.conversationId else { return false }
                taken -= 1
                return true
            }
            let previews = try? await load(first.conversationId, batch.map(\.url))
            for (index, asked) in batch.enumerated() {
                let found = previews.flatMap { index < $0.count ? $0[index] : nil }
                asked.settle.resume(returning: found)
                if previews == nil { forgetLater(Self.key(asked.conversationId, asked.url)) }
            }
        }
        isCollecting = false
    }

    /// A question that failed is asked again after a while, not at once.
    private func forgetLater(_ key: String) {
        Task {
            try? await Task.sleep(for: failedFor)
            known[key] = nil
        }
    }
}
