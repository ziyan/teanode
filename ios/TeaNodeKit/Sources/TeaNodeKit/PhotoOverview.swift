import Foundation

/// PhotoMonth is the photos of one month, a month being as the wall clock read
/// where they were taken. The library's Months view shows one picture for each.
public struct PhotoMonth: Identifiable, Hashable, Sendable {
    /// "2026-03", or "unknown" for photos with no date at all.
    public let id: String
    public let year: Int?
    public let month: Int?
    /// The photos of the month, in the order the list has them: newest first.
    public let photos: [Photo]

    /// The photo the month is shown by: the newest that has a small preview,
    /// or else the newest.
    public var coverPhoto: Photo? {
        photos.first { $0.gridPreviewPath != nil } ?? photos.first
    }

    /// The first day of the month at noon in UTC, to be written with a time
    /// zone of UTC so the text names this very month wherever the phone is.
    /// Nil for no date.
    public var firstDayNoonUTC: Date? {
        guard let year, let month else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0) ?? .gmt
        return calendar.date(from: DateComponents(year: year, month: month, day: 1, hour: 12))
    }

    /// "October 2026"; nil for no date.
    public func titleText(locale: Locale = .autoupdatingCurrent) -> String? {
        firstDayNoonUTC?.formatted(Date.FormatStyle(locale: locale, timeZone: TimeZone(secondsFromGMT: 0) ?? .gmt).month(.wide).year())
    }
}

/// PhotoYear is the photos of one year, as the wall clock read where they were
/// taken. The library's Years view shows one picture for each.
public struct PhotoYear: Identifiable, Hashable, Sendable {
    /// "2026", or "unknown" for photos with no date at all.
    public let id: String
    public let year: Int?
    /// The photos of the year, in the order the list has them: newest first.
    public let photos: [Photo]

    /// The photo the year is shown by: the newest that has a small preview,
    /// or else the newest.
    public var coverPhoto: Photo? {
        photos.first { $0.gridPreviewPath != nil } ?? photos.first
    }

    /// "2026"; nil for no date. Written as a date, so that it follows the
    /// person's numbering and never gets a thousands separator.
    public func titleText(locale: Locale = .autoupdatingCurrent) -> String? {
        guard let year else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0) ?? .gmt
        guard let noon = calendar.date(from: DateComponents(year: year, month: 6, day: 15, hour: 12)) else { return nil }
        return noon.formatted(Date.FormatStyle(locale: locale, timeZone: TimeZone(secondsFromGMT: 0) ?? .gmt).year())
    }
}

/// PhotoOverview is how the photos of a list are summed up for the library's
/// Years and Months views, and for the date range under its title.
public enum PhotoOverview {
    /// The photos, which the list holds newest first, in months. A month keeps
    /// the order the photos came in, and months come in the order of their
    /// first photo. Photos with no date are one group of their own.
    public static func months(_ photos: [Photo], phoneTimeZone: TimeZone = .current) -> [PhotoMonth] {
        var order: [String] = []
        var members: [String: [Photo]] = [:]
        var parts: [String: (year: Int, month: Int)] = [:]
        for day in PhotoTimeline.days(photos, phoneTimeZone: phoneTimeZone) {
            var key = "unknown"
            if let year = day.year, let month = day.month {
                key = String(format: "%04ld-%02ld", year, month)
                parts[key] = (year, month)
            }
            if members[key] == nil { order.append(key) }
            members[key, default: []].append(contentsOf: day.photos)
        }
        return order.map { key in
            PhotoMonth(id: key, year: parts[key]?.year, month: parts[key]?.month, photos: members[key] ?? [])
        }
    }

    /// The photos, which the list holds newest first, in years, in the same
    /// way as `months`.
    public static func years(_ photos: [Photo], phoneTimeZone: TimeZone = .current) -> [PhotoYear] {
        var order: [String] = []
        var members: [String: [Photo]] = [:]
        var years: [String: Int] = [:]
        for day in PhotoTimeline.days(photos, phoneTimeZone: phoneTimeZone) {
            var key = "unknown"
            if let year = day.year {
                key = String(format: "%04ld", year)
                years[key] = year
            }
            if members[key] == nil { order.append(key) }
            members[key, default: []].append(contentsOf: day.photos)
        }
        return order.map { key in
            PhotoYear(id: key, year: years[key], photos: members[key] ?? [])
        }
    }

    /// The days the photos span, as the library says them under its title:
    /// "Oct 3 to 6, 2026" for several days and "Oct 3, 2026" for one. The days
    /// are the ones on the wall clock where each photo was taken. Nil when no
    /// photo has a date.
    public static func dateRangeText(of photos: [Photo], phoneTimeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        let dayNoons = PhotoTimeline.days(photos, phoneTimeZone: phoneTimeZone).compactMap(\.noonUTC)
        guard let newest = dayNoons.max(), let oldest = dayNoons.min() else { return nil }
        let utc = TimeZone(secondsFromGMT: 0) ?? .gmt
        if oldest == newest {
            return oldest.formatted(Date.FormatStyle(date: .abbreviated, time: .omitted, locale: locale, timeZone: utc))
        }
        return (oldest..<newest).formatted(Date.IntervalFormatStyle(date: .abbreviated, locale: locale, timeZone: utc))
    }
}

extension PhotoTimeline {
    /// The day a photo shows at the top of the viewer: "October 5", with the
    /// year when it is not the phone's current year. Read in the zone the photo
    /// was taken in when it says one. Nil for no date.
    public static func viewerDayText(of photo: Photo, now: Date = Date(), phoneTimeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        guard let moment = moment(of: photo, phoneTimeZone: phoneTimeZone) else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = moment.timeZone
        var phoneCalendar = Calendar(identifier: .gregorian)
        phoneCalendar.timeZone = phoneTimeZone
        var style = Date.FormatStyle(locale: locale, timeZone: moment.timeZone).month(.wide).day()
        if calendar.component(.year, from: moment.date) != phoneCalendar.component(.year, from: now) { style = style.year() }
        return moment.date.formatted(style)
    }

    /// The time a photo shows under its day at the top of the viewer: "19:52"
    /// or "7:52 PM" as the person's region writes it, on the clock where the
    /// photo was taken. Nil for no date.
    public static func viewerTimeText(of photo: Photo, phoneTimeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        guard let moment = moment(of: photo, phoneTimeZone: phoneTimeZone) else { return nil }
        return moment.date.formatted(Date.FormatStyle(date: .omitted, time: .shortened, locale: locale, timeZone: moment.timeZone))
    }
}
