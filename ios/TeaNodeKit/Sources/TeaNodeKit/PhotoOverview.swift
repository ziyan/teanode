import Foundation

/// PhotoOverview writes the date range under the library's title. (The Years
/// and Months are PhotoPeriod, which the server answers for.)
public enum PhotoOverview {
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
