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
}

extension Documents {
    public static let readLinkPreviews = """
        query ($urls: [String!]!) {
          ReadLinkPreviews(urls: $urls) {
            url cleanedUrl displayText isAvailable title description siteName imageUrl imageWidthPixels imageHeightPixels
          }
        }
        """
}

extension Connection {
    /// The preview of each link, in the order given, at most
    /// `LinkPreview.mostURLs` of them.
    public func linkPreviews(urls: [String]) async throws -> [LinkPreview] {
        struct Answer: Decodable, Sendable { let ReadLinkPreviews: [LinkPreview] }
        let variables: [String: JSONValue] = ["urls": .array(urls.map { .string($0) })]
        return try await execute(Documents.readLinkPreviews, variables: variables, as: Answer.self).ReadLinkPreviews
    }
}

/// LinkPreviewLoader asks for the previews of the links of a screenful in one
/// question, ten at a time, and never asks again for a link it has asked for
/// (until a question that failed has waited out `failedFor`). The server keeps
/// what it read, but a conversation holds many links, and one question a link
/// would be one trip each.
public actor LinkPreviewLoader {
    public typealias Load = @Sendable ([String]) async throws -> [LinkPreview]

    private let load: Load
    private let collectingFor: Duration
    private let failedFor: Duration
    private var known: [String: Task<LinkPreview?, Never>] = [:]
    private var waiting: [(url: String, settle: CheckedContinuation<LinkPreview?, Never>)] = []
    private var isCollecting = false

    public init(collectingFor: Duration = .milliseconds(60), failedFor: Duration = .seconds(60), load: @escaping Load) {
        self.collectingFor = collectingFor
        self.failedFor = failedFor
        self.load = load
    }

    /// The preview of a link, or nil when there is none to show.
    public func preview(for url: String) async -> LinkPreview? {
        if let asked = known[url] { return await asked.value }
        let asked = Task<LinkPreview?, Never> { await self.answer(for: url) }
        known[url] = asked
        return await asked.value
    }

    /// Waits for the question the link goes out in to be answered.
    private func answer(for url: String) async -> LinkPreview? {
        await withCheckedContinuation { settle in
            waiting.append((url, settle))
            guard !isCollecting else { return }
            isCollecting = true
            Task { await self.sendWaiting() }
        }
    }

    private func sendWaiting() async {
        try? await Task.sleep(for: collectingFor)
        while !waiting.isEmpty {
            let batch = Array(waiting.prefix(LinkPreview.mostURLs))
            waiting.removeFirst(batch.count)
            let previews = try? await load(batch.map(\.url))
            for (index, asked) in batch.enumerated() {
                let found = previews.flatMap { index < $0.count ? $0[index] : nil }
                asked.settle.resume(returning: found)
                if previews == nil { forgetLater(asked.url) }
            }
        }
        isCollecting = false
    }

    /// A question that failed is asked again after a while, not at once.
    private func forgetLater(_ url: String) {
        Task {
            try? await Task.sleep(for: failedFor)
            known[url] = nil
        }
    }
}
