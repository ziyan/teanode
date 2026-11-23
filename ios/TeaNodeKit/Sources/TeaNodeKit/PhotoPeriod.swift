import Foundation

/// PhotoPeriod is one calendar year or one month of one, as the Years and
/// Months of the library show them. Its two ends are read on the phone's own
/// calendar, which is also what the date filters of the library are written
/// in, so a period holds the photos the server keeps between them.
public struct PhotoPeriod: Identifiable, Hashable, Sendable {
    public let year: Int
    /// 1 for January; nil for the whole year.
    public let month: Int?
    /// The first moment of the period.
    public let takenAfter: Date
    /// The first moment after the period.
    public let takenBefore: Date

    /// "2030" for a year, "2030-07" for a month.
    public var id: String {
        guard let month else { return String(format: "%04ld", year) }
        return String(format: "%04ld-%02ld", year, month)
    }

    /// A year, or a month of it (1 for January), with its two ends on the
    /// calendar of a time zone. Nil for a month that is not one.
    public init?(year: Int, month: Int? = nil, timeZone: TimeZone = .current) {
        if let month, !(1...12).contains(month) { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        guard let start = calendar.date(from: DateComponents(year: year, month: month ?? 1, day: 1)),
            let end = calendar.date(byAdding: month == nil ? .year : .month, value: 1, to: start)
        else { return nil }
        self.year = year
        self.month = month
        self.takenAfter = start
        self.takenBefore = end
    }

    /// The years from the newest to the oldest, both included.
    public static func years(newestYear: Int, oldestYear: Int) -> [Int] {
        guard newestYear >= oldestYear else { return [newestYear] }
        return Array((oldestYear...newestYear).reversed())
    }

    /// The months of a year from December back to January: the order a list
    /// that runs newest first has them in.
    public static let monthsNewestFirst = Array((1...12).reversed())

    /// The months of the years from the newest to the oldest, newest first.
    public static func months(newestYear: Int, oldestYear: Int, timeZone: TimeZone = .current) -> [PhotoPeriod] {
        years(newestYear: newestYear, oldestYear: oldestYear).flatMap { year in
            monthsNewestFirst.compactMap { PhotoPeriod(year: year, month: $0, timeZone: timeZone) }
        }
    }

    /// The year of a moment on the calendar of a time zone.
    public static func year(of moment: Date, timeZone: TimeZone = .current) -> Int {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        return calendar.component(.year, from: moment)
    }

    /// "2026" for a year and "October 2026" for a month, written as a date at
    /// noon in UTC so that it follows the person's numbering and never gets a
    /// thousands separator, and names this very period wherever the phone is.
    public func titleText(locale: Locale = .autoupdatingCurrent) -> String? {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0) ?? .gmt
        guard let noon = calendar.date(from: DateComponents(year: year, month: month ?? 6, day: month == nil ? 15 : 1, hour: 12)) else { return nil }
        let style = Date.FormatStyle(locale: locale, timeZone: TimeZone(secondsFromGMT: 0) ?? .gmt)
        return noon.formatted(month == nil ? style.year() : style.month(.wide).year())
    }
}

/// PhotoPeriodSummary is a period of a list that has photos in it: the newest
/// one, which stands for the period, and how many there are.
public struct PhotoPeriodSummary: Hashable, Sendable {
    public let period: PhotoPeriod
    public let coverPhoto: Photo
    public let photoCount: Int

    public init(period: PhotoPeriod, coverPhoto: Photo, photoCount: Int) {
        self.period = period
        self.coverPhoto = coverPhoto
        self.photoCount = photoCount
    }
}

/// PhotoYearSpan is the years a list has photos in, as far apart as its newest
/// and its oldest photo.
public struct PhotoYearSpan: Hashable, Sendable {
    public let newestYear: Int
    public let oldestYear: Int

    public init(newestYear: Int, oldestYear: Int) {
        self.newestYear = newestYear
        self.oldestYear = min(newestYear, oldestYear)
    }
}

extension PhotoTimeline {
    /// The moment a photo sits at in the timeline: when it was taken, or when
    /// it arrived if nothing says when it was taken, as the server places it.
    /// Nil when it has neither.
    public static func timelineMoment(of photo: Photo) -> Date? {
        ServerTime.date(photo.takenAt) ?? ServerTime.date(photo.createdAt)
    }
}

extension PhotoListQuery {
    /// The list kept to a period. Nil when nothing can be in it.
    public func narrowed(to period: PhotoPeriod, calendar: Calendar = .current) -> PhotoListQuery? {
        narrowed(takenAfter: period.takenAfter, takenBefore: period.takenBefore, calendar: calendar)
    }

    /// The list kept to the photos newer than the whole of a period: counting
    /// them gives the place of the period's newest photo in the list, counted
    /// from 0. The list's own end is kept, since a photo after it was never in
    /// the list. Nil when there are none, which is place 0.
    public func newer(than period: PhotoPeriod, calendar: Calendar = .current) -> PhotoListQuery? {
        newer(thanMoment: period.takenBefore, calendar: calendar)
    }
}

extension PhotoListQuery {
    /// The list kept to the photos taken after a moment: counting them gives
    /// about the place in the list of a photo taken at it, which a page and the
    /// pages beside it then hold. Nil when there are none, which is place 0.
    public func newer(thanMoment moment: Date, calendar: Calendar = .current) -> PhotoListQuery? {
        let takenAfter = max(moment, filter.effectiveTakenAfter(calendar: calendar) ?? moment)
        if let end = filter.effectiveTakenBefore(calendar: calendar), end <= takenAfter { return nil }
        var newer = self
        newer.filter.takenAfter = takenAfter
        return newer
    }
}

extension Connection {
    /// The newest and the oldest year a list has photos in, or nil when it has
    /// none. Three questions: how many, the first, and the last.
    public func photoYearSpan(of list: PhotoListQuery, timeZone: TimeZone = .current) async throws -> PhotoYearSpan? {
        let totalCount = try await countPhotos(in: list)
        guard totalCount > 0 else { return nil }
        async let newestPage = try await photos(in: list, offset: 0, pageSize: 1)
        async let oldestPage = try await photos(in: list, offset: totalCount - 1, pageSize: 1)
        let (newest, oldest) = try await (newestPage, oldestPage)
        guard let newestPhoto = newest.first, let newestMoment = PhotoTimeline.timelineMoment(of: newestPhoto) else { return nil }
        let oldestMoment = PhotoTimeline.timelineMoment(of: oldest.first ?? newestPhoto) ?? newestMoment
        return PhotoYearSpan(newestYear: PhotoPeriod.year(of: newestMoment, timeZone: timeZone), oldestYear: PhotoPeriod.year(of: oldestMoment, timeZone: timeZone))
    }

    /// The newest photo of a list taken in a period and how many were, or nil
    /// when it has none in it.
    public func photoPeriodSummary(of list: PhotoListQuery, period: PhotoPeriod) async throws -> PhotoPeriodSummary? {
        guard let narrowed = list.narrowed(to: period) else { return nil }
        async let newestPage = try await photos(in: narrowed, offset: 0, pageSize: 1)
        async let photoCount = try await countPhotos(in: narrowed)
        let (newest, count) = try await (newestPage, photoCount)
        guard let coverPhoto = newest.first else { return nil }
        return PhotoPeriodSummary(period: period, coverPhoto: coverPhoto, photoCount: max(count, 1))
    }

    /// How many photos of a list are newer than the whole of a period: the
    /// place in the list of the period's newest photo, counted from 0.
    public func countPhotos(in list: PhotoListQuery, newerThan period: PhotoPeriod) async throws -> Int {
        guard let newer = list.newer(than: period) else { return 0 }
        return try await countPhotos(in: newer)
    }

    /// How many photos of a list are newer than a moment: about the place in
    /// the list of a photo taken at it.
    public func countPhotos(in list: PhotoListQuery, newerThanMoment moment: Date) async throws -> Int {
        guard let newer = list.newer(thanMoment: moment) else { return 0 }
        return try await countPhotos(in: newer)
    }
}
