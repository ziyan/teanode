package com.teanode.kit.photos

import java.time.LocalDate
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/** One month of photos in the Months view. */
data class PhotoMonth(val id: String, val year: Int?, val month: Int?, val photos: List<Photo>) {
    /** The photo that stands for the month: the first with a picture to show. */
    val coverPhoto: Photo? get() = photos.firstOrNull { photo -> photo.gridPreviewPath != null } ?: photos.firstOrNull()

    fun titleText(locale: Locale = Locale.getDefault()): String? {
        if (year == null || month == null) return null
        return DateTimeFormatter.ofPattern("LLLL yyyy", locale).format(LocalDate.of(year, month, 1))
    }
}

/** One year of photos in the Years view. */
data class PhotoYear(val id: String, val year: Int?, val photos: List<Photo>) {
    val coverPhoto: Photo? get() = photos.firstOrNull { photo -> photo.gridPreviewPath != null } ?: photos.firstOrNull()

    fun titleText(): String? = year?.toString()
}

/** The Years and Months views and the date range over a set of photos, built on the days of the timeline. */
object PhotoOverview {
    fun months(photos: List<Photo>, phoneZone: ZoneId = ZoneId.systemDefault()): List<PhotoMonth> {
        val order = mutableListOf<String>()
        val members = mutableMapOf<String, MutableList<Photo>>()
        val parts = mutableMapOf<String, Pair<Int, Int>>()
        for (day in PhotoTimeline.days(photos, phoneZone)) {
            var key = "unknown"
            if (day.year != null && day.month != null) {
                key = "%04d-%02d".format(day.year, day.month)
                parts[key] = day.year to day.month
            }
            if (key !in members) order.add(key)
            members.getOrPut(key) { mutableListOf() }.addAll(day.photos)
        }
        return order.map { key -> PhotoMonth(key, parts[key]?.first, parts[key]?.second, members.getValue(key)) }
    }

    fun years(photos: List<Photo>, phoneZone: ZoneId = ZoneId.systemDefault()): List<PhotoYear> {
        val order = mutableListOf<String>()
        val members = mutableMapOf<String, MutableList<Photo>>()
        val years = mutableMapOf<String, Int>()
        for (day in PhotoTimeline.days(photos, phoneZone)) {
            var key = "unknown"
            if (day.year != null) {
                key = "%04d".format(day.year)
                years[key] = day.year
            }
            if (key !in members) order.add(key)
            members.getOrPut(key) { mutableListOf() }.addAll(day.photos)
        }
        return order.map { key -> PhotoYear(key, years[key], members.getValue(key)) }
    }

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
