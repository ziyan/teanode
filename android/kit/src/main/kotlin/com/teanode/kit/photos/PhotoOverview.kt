package com.teanode.kit.photos

import java.time.LocalDate
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/** The date range over a set of photos and the viewer's headings, built on the days of the timeline. */
object PhotoOverview {
    /** "Jan 3, 2025 - Mar 9, 2025", or one date when every photo is of one day; null with no dated photo. */
    fun dateRangeText(photos: List<Photo>, phoneZone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String? {
        val dayNoons = PhotoTimeline.days(photos, phoneZone).mapNotNull { day -> day.noonUtc }
        val newest = dayNoons.maxOrNull() ?: return null
        val oldest = dayNoons.minOrNull() ?: return null
        val format = DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).withLocale(locale)
        val oldestText = format.format(oldest.atZone(ZoneOffset.UTC))
        if (oldest == newest) return oldestText
        return "$oldestText - ${format.format(newest.atZone(ZoneOffset.UTC))}"
    }

    /** The viewer's heading: "March 4", with the year when it is not this year. */
    fun viewerDayText(photo: Photo, now: java.time.Instant = java.time.Instant.now(), phoneZone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String? {
        val moment = PhotoTimeline.moment(photo, phoneZone) ?: return null
        val taken = moment.instant.atZone(moment.zone)
        val thisYear = now.atZone(phoneZone).year
        val pattern = if (taken.year != thisYear) "MMMM d, yyyy" else "MMMM d"
        return DateTimeFormatter.ofPattern(pattern, locale).format(taken)
    }

    /** The viewer's subheading: the time of day in the zone the photo was taken in. */
    fun viewerTimeText(photo: Photo, phoneZone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String? {
        val moment = PhotoTimeline.moment(photo, phoneZone) ?: return null
        return DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT).withLocale(locale).format(moment.instant.atZone(moment.zone))
    }
}
