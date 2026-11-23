import Foundation

/// PhotoWindow is a list of photos as the phone holds it: how many photos the
/// list has, and only the pages of them that are near what is on screen. A page
/// is `pageSize` photos, counted from the newest, so the photo at a place in
/// the list is on page `place / pageSize`, and the server is asked for it by
/// that place (`offset`), never by the photo before it.
///
/// A list that is small and read whole (a search, the Trash, a few photos a
/// viewer was opened on) is a window of one page that holds all of it and is
/// never let go of (`isWhole`).
public struct PhotoWindow: Equatable, Sendable {
    /// How many pages round the one on screen are read.
    public static let defaultReach = 2
    /// How many pages away from the one on screen a page is kept before it is
    /// let go of.
    public static let defaultKeep = 4

    /// How many photos the list has, as the server last counted them.
    public private(set) var totalCount: Int
    public let pageSize: Int
    /// Whether the one page holds every photo of the list and stays.
    public let isWhole: Bool
    /// The photos of each page held, by the number of the page counted from 0.
    public private(set) var pages: [Int: [Photo]]
    /// The pages held that may no longer be what the server has at their
    /// places (after a photo left the list or the list was counted again).
    /// They still answer for their photos until they are read again.
    public private(set) var stalePageIndexes: Set<Int>

    public init(totalCount: Int, pageSize: Int = PhotoPaging.pageSize, pages: [Int: [Photo]] = [:]) {
        self.totalCount = max(0, totalCount)
        self.pageSize = max(1, pageSize)
        self.isWhole = false
        self.pages = pages
        self.stalePageIndexes = []
    }

    /// A window that holds all of a list.
    public init(whole photos: [Photo]) {
        self.totalCount = photos.count
        self.pageSize = max(1, photos.count)
        self.isWhole = true
        self.pages = photos.isEmpty ? [:] : [0: photos]
        self.stalePageIndexes = []
    }

    /// How many pages the list has.
    public var pageCount: Int { (totalCount + pageSize - 1) / pageSize }

    /// The number of the page a place in the list is on.
    public func pageIndex(of place: Int) -> Int { max(0, place) / pageSize }

    /// The photo at a place in the list, if its page is held.
    public func photo(at place: Int) -> Photo? {
        guard place >= 0, place < totalCount, let page = pages[pageIndex(of: place)] else { return nil }
        let offset = place % pageSize
        return offset < page.count ? page[offset] : nil
    }

    /// Every photo held, from the newest.
    public var heldPhotos: [Photo] {
        pages.keys.sorted().flatMap { pages[$0] ?? [] }
    }

    /// The place of a photo among the pages held; nil when it is on none of them.
    public func place(of photoId: String) -> Int? {
        for pageIndex in pages.keys.sorted() {
            if let offset = pages[pageIndex]?.firstIndex(where: { $0.id == photoId }) { return pageIndex * pageSize + offset }
        }
        return nil
    }

    /// The pages to read for a place on screen: those within `reach` pages of
    /// it that are not held, or are held but stale, nearest first.
    public func missingPages(around place: Int, reach: Int = PhotoWindow.defaultReach) -> [Int] {
        guard totalCount > 0 else { return [] }
        let centerIndex = min(pageIndex(of: place), pageCount - 1)
        let first = max(0, centerIndex - reach)
        let last = min(pageCount - 1, centerIndex + reach)
        guard first <= last else { return [] }
        return (first...last)
            .filter { pages[$0] == nil || stalePageIndexes.contains($0) }
            .sorted { abs($0 - centerIndex) != abs($1 - centerIndex) ? abs($0 - centerIndex) < abs($1 - centerIndex) : $0 < $1 }
    }

    /// The pages held that are more than `keep` pages from a place on screen,
    /// which can be let go of. A window that holds the whole list lets go of
    /// nothing.
    public func pagesToRelease(around place: Int, keep: Int = PhotoWindow.defaultKeep) -> [Int] {
        guard !isWhole else { return [] }
        let centerIndex = pageIndex(of: place)
        return pages.keys.filter { abs($0 - centerIndex) > keep }.sorted()
    }

    /// The held pages with a photo that is still being made ready, which are
    /// read again to show its picture when it is.
    public var pageIndexesWithWaitingPhotos: [Int] {
        pages.filter { $0.value.contains { $0.isWaiting } }.keys.sorted()
    }

    /// Holds a page just read, in place of the one held.
    public mutating func setPage(_ photos: [Photo], at pageIndex: Int) {
        guard pageIndex >= 0, pageIndex < pageCount else { return }
        pages[pageIndex] = photos
        stalePageIndexes.remove(pageIndex)
    }

    /// Lets go of pages.
    public mutating func release(_ pageIndexes: [Int]) {
        for pageIndex in pageIndexes {
            pages[pageIndex] = nil
            stalePageIndexes.remove(pageIndex)
        }
    }

    /// Takes the count the server just gave. Pages that are past the end of
    /// the list now are let go of. When the count changed, every page held is
    /// stale, since the photos after the change moved to other places.
    public mutating func setTotalCount(_ count: Int) {
        let changed = max(0, count) != totalCount
        totalCount = max(0, count)
        for pageIndex in Array(pages.keys) where pageIndex >= pageCount { pages[pageIndex] = nil }
        stalePageIndexes.formIntersection(pages.keys)
        if changed { markAllStale() }
    }

    /// Says that every page held may be out of date.
    public mutating func markAllStale() {
        guard !isWhole else { return }
        stalePageIndexes = Set(pages.keys)
    }

    /// Puts a photo as the server now has it in place of the one held. True
    /// when it was held.
    @discardableResult
    public mutating func replace(_ photo: Photo) -> Bool {
        for (pageIndex, page) in pages {
            guard let offset = page.firstIndex(where: { $0.id == photo.id }) else { continue }
            pages[pageIndex]?[offset] = photo
            return true
        }
        return false
    }

    /// Takes photos out of the list. Each one must be held, since only then is
    /// it known to be in the list: the count goes down by the photos taken
    /// out, and the pages from the first one that had one are stale, because
    /// the photos after it are now one place earlier. Photos that are not held
    /// are left to the caller, which reads the list again (see `heldPhotoIds`).
    public mutating func remove(_ photoIds: Set<String>) {
        var firstChangedPageIndex: Int?
        var removedCount = 0
        for pageIndex in pages.keys.sorted() {
            let before = pages[pageIndex]?.count ?? 0
            pages[pageIndex]?.removeAll { photoIds.contains($0.id) }
            let removed = before - (pages[pageIndex]?.count ?? 0)
            if removed > 0 {
                removedCount += removed
                if firstChangedPageIndex == nil { firstChangedPageIndex = pageIndex }
            }
        }
        guard let firstChangedPageIndex else { return }
        totalCount = max(0, totalCount - removedCount)
        if isWhole {
            if totalCount == 0 { pages = [:] }
            return
        }
        for pageIndex in Array(pages.keys) where pageIndex >= pageCount { pages[pageIndex] = nil }
        for pageIndex in pages.keys where pageIndex >= firstChangedPageIndex { stalePageIndexes.insert(pageIndex) }
        stalePageIndexes.formIntersection(pages.keys)
    }

    /// The ids of the photos held.
    public var heldPhotoIds: Set<String> {
        Set(pages.values.flatMap { $0.map(\.id) })
    }
}
