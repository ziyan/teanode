import Foundation

/// SearchRecent is something the person opened from the Search tab: a place
/// of the app by its id, or a thing a search found with the words that found
/// it. The empty Search tab lists them, newest first, so going back to one is
/// a tap.
public struct SearchRecent: Codable, Hashable, Sendable, Identifiable {
    /// The place of the app, as the Search tab names its places
    /// ("finance-imports"); nil for something found.
    public let placeId: String?
    /// What a search found; nil for a place.
    public let result: SearchResult?
    /// The words that found the result, which it opens with.
    public let words: String

    public var id: String { placeId.map { "place:\($0)" } ?? "found:\(result?.id ?? "")" }

    public static func place(_ placeId: String) -> SearchRecent {
        SearchRecent(placeId: placeId, result: nil, words: "")
    }

    public static func found(_ result: SearchResult, words: String) -> SearchRecent {
        SearchRecent(placeId: nil, result: result, words: words)
    }
}

/// SearchRecents keeps what was opened from the Search tab on this phone, a
/// list for each account, so one account's never shows in another's.
public struct SearchRecents: @unchecked Sendable {
    /// How many the list keeps.
    public static let capacity = 8

    private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    private func key(accountKey: String) -> String { "searchRecents|\(accountKey)" }

    /// The list, newest first. A stored list that cannot be read is empty.
    public func load(accountKey: String) -> [SearchRecent] {
        guard let data = defaults.data(forKey: key(accountKey: accountKey)),
            let recents = try? JSONDecoder().decode([SearchRecent].self, from: data)
        else { return [] }
        return Array(recents.prefix(Self.capacity))
    }

    /// Puts one at the head of the list and returns the list.
    @discardableResult
    public func remember(_ recent: SearchRecent, accountKey: String) -> [SearchRecent] {
        let recents = Self.adding(recent, to: load(accountKey: accountKey))
        if let data = try? JSONEncoder().encode(recents) { defaults.set(data, forKey: key(accountKey: accountKey)) }
        return recents
    }

    public func clear(accountKey: String) {
        defaults.removeObject(forKey: key(accountKey: accountKey))
    }

    /// The list with this one at its head: an earlier copy of it is dropped,
    /// and the list keeps `capacity`.
    public static func adding(_ recent: SearchRecent, to recents: [SearchRecent]) -> [SearchRecent] {
        Array(([recent] + recents.filter { $0.id != recent.id }).prefix(capacity))
    }
}
