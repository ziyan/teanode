import Foundation

/// PlaceName is one place of the app a search can jump to, as the search box
/// matches it: its own name, and the names of the places it is inside, each
/// in the reader's language and in English, so the words find it whatever
/// the language. A place inside none is a part of the app.
public struct PlaceName: Hashable, Sendable {
    public let placeId: String
    /// The place's own name, in the reader's language and in English.
    public let names: [String]
    /// The names of the places it is inside, outermost first, each in the
    /// reader's language and in English.
    public let parentNames: [[String]]

    public init(placeId: String, names: [String], parentNames: [[String]] = []) {
        self.placeId = placeId
        self.names = names
        self.parentNames = parentNames
    }

    /// Whether the place is a part of the app rather than a place inside one.
    public var isPart: Bool { parentNames.isEmpty }
}

/// PlaceSearch picks the places whose names the typed words are, the way the
/// dashboard's search box offers its pages and their tabs: first the parts of
/// the app, then the places inside them, each best match first and in the
/// app's own order within a match.
public enum PlaceSearch {
    /// How many places the search offers at most.
    public static let placeLimit = 8

    /// How well the words match a name: 0 when the name starts with them, 1
    /// when a word of the name does, and nil otherwise. Letters inside a word
    /// are no match: a person's name typed to be searched for would otherwise
    /// jump to whichever place happens to hold those letters. Both are lower
    /// case.
    static func nameRank(_ name: String, words: String) -> Int? {
        if name.hasPrefix(words) { return 0 }
        let nameWords = name.split(whereSeparator: { $0.isWhitespace || "-_/".contains($0) })
        if nameWords.contains(where: { $0.hasPrefix(words) }) { return 1 }
        return nil
    }

    /// How well the words match a place. Its own name matches as a part's
    /// does ("passkeys"); so do the start of a name it is inside and the
    /// start of its own, said together ("set pass", "manage dom"). The words
    /// are cut at each space to find the two halves.
    static func placeRank(_ place: PlaceName, words: String) -> Int? {
        var ranks = place.names.compactMap { nameRank($0.lowercased(), words: words) }
        let parentNames = place.parentNames.flatMap { $0 }.map { $0.lowercased() }
        if !parentNames.isEmpty {
            for gap in words.indices where words[gap].isWhitespace {
                let parentWords = words[..<gap].trimmingCharacters(in: .whitespaces)
                let ownWords = words[words.index(after: gap)...].trimmingCharacters(in: .whitespaces)
                guard !parentWords.isEmpty, !ownWords.isEmpty else { continue }
                guard parentNames.contains(where: { $0.hasPrefix(parentWords) }) else { continue }
                ranks += place.names.compactMap { nameRank($0.lowercased(), words: ownWords) }
            }
        }
        return ranks.min()
    }

    /// The ids of the places the words name, in the order to offer them, at
    /// most placeLimit; none for words with nothing in them.
    public static func matchingPlaceIds(_ places: [PlaceName], words: String) -> [String] {
        let needle = words.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !needle.isEmpty else { return [] }
        let ranked = places.enumerated().compactMap { order, place -> (place: PlaceName, rank: Int, order: Int)? in
            guard let rank = placeRank(place, words: needle) else { return nil }
            return (place, rank, order)
        }
        let sorted = ranked.sorted { left, right in
            if left.place.isPart != right.place.isPart { return left.place.isPart }
            if left.rank != right.rank { return left.rank < right.rank }
            return left.order < right.order
        }
        return sorted.prefix(placeLimit).map(\.place.placeId)
    }
}
