import Foundation

/// PhotoListQuery says which list of photos is read: a list of the library
/// (all of it, the favorites, the Trash), narrowed by a filter, or the photos of
/// one album, or the photos with a face in one group of faces. The same query
/// is the arguments of the server's ListPhotos, which reads a page of it by its
/// place, and of CountPhotos, which says how many photos it has.
public struct PhotoListQuery: Hashable, Sendable {
    public var collection: PhotoCollection
    public var filter: PhotoFilter
    public var albumId: String?
    public var photoFaceGroupId: String?

    public init(collection: PhotoCollection = .library, filter: PhotoFilter = PhotoFilter(), albumId: String? = nil, photoFaceGroupId: String? = nil) {
        self.collection = collection
        self.filter = filter
        self.albumId = albumId
        self.photoFaceGroupId = photoFaceGroupId
    }

    /// The variables both ListPhotos and CountPhotos take for this list.
    public func variables(calendar: Calendar = .current) -> [String: JSONValue] {
        var variables: [String: JSONValue] = [:]
        if let isFavorite = collection.isFavoriteFilter { variables["isFavorite"] = .bool(isFavorite) }
        if let isTrashed = collection.isTrashedFilter { variables["isTrashed"] = .bool(isTrashed) }
        if let albumId { variables["albumId"] = .string(albumId) }
        if let photoFaceGroupId { variables["faceGroupId"] = .string(photoFaceGroupId) }
        for (name, value) in filter.variables(calendar: calendar) { variables[name] = value }
        return variables
    }

    /// The same list kept to the photos taken from a moment (included) to
    /// another (not included), within the days the list is already kept to.
    /// Nil when nothing can be in it, because the two do not overlap.
    public func narrowed(takenAfter: Date, takenBefore: Date, calendar: Calendar = .current) -> PhotoListQuery? {
        let after = max(takenAfter, filter.effectiveTakenAfter(calendar: calendar) ?? takenAfter)
        let before = min(takenBefore, filter.effectiveTakenBefore(calendar: calendar) ?? takenBefore)
        guard after < before else { return nil }
        var narrowed = self
        narrowed.filter.takenAfter = after
        narrowed.filter.takenBefore = before
        return narrowed
    }
}

extension PhotoFilter {
    /// The first moment the filter keeps: the moment given, else the start of
    /// the first day; nil for no start. This is what `variables` sends.
    public func effectiveTakenAfter(calendar: Calendar = .current) -> Date? {
        if let takenAfter { return takenAfter }
        guard let firstDay else { return nil }
        return calendar.startOfDay(for: firstDay)
    }

    /// The moment the filter stops at, which is not kept: the moment given,
    /// else the start of the day after the last day; nil for no end.
    public func effectiveTakenBefore(calendar: Calendar = .current) -> Date? {
        if let takenBefore { return takenBefore }
        guard let lastDay else { return nil }
        return calendar.date(byAdding: .day, value: 1, to: calendar.startOfDay(for: lastDay))
    }
}

extension Connection {
    /// A page of a list of photos, newest first, that starts `offset` photos
    /// from the newest. The server takes an offset of up to 100000.
    public func photos(in list: PhotoListQuery, offset: Int, pageSize: Int = PhotoPaging.pageSize) async throws -> [Photo] {
        struct Answer: Decodable, Sendable { let ListPhotos: [Photo] }
        var variables = list.variables()
        variables["pageSize"] = .number(Double(pageSize))
        if offset > 0 { variables["offset"] = .number(Double(offset)) }
        return try await execute(Documents.listPhotos, variables: variables, as: Answer.self).ListPhotos
    }

    /// How many photos a list has.
    public func countPhotos(in list: PhotoListQuery) async throws -> Int {
        struct Answer: Decodable, Sendable { let CountPhotos: Int }
        return try await execute(Documents.countPhotos, variables: list.variables(), as: Answer.self).CountPhotos
    }
}
