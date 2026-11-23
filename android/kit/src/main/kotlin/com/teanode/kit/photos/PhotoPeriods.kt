package com.teanode.kit.photos

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/** One year or month of the Years and Months views, with the photo that stands for it and how many it holds. */
data class PhotoPeriod(
    /** 2030 for a year, 2030-07 for a month. */
    val id: String,
    val year: Int,
    /** 0 for January; null for a whole year. */
    val monthIndex: Int?,
    val takenAfter: Instant,
    val takenBefore: Instant,
    val coverPhoto: Photo,
    val photoCount: Int,
)

/** The newest and the oldest year a list has photos in. */
data class PhotoYearSpan(val newestYear: Int, val oldestYear: Int) {
    val years: List<Int> get() = PhotoPeriods.yearsBetween(newestYear, oldestYear)
}

/**
 * PhotoPeriods is the calendar math of the Years and Months views, as web/src/photos/overview.ts has it: a period is a
 * calendar year or month on the phone's clock, which is also the clock the filter's days are written in.
 */
object PhotoPeriods {
    /** The first moment of a year (monthIndex null) or of a month of it, and the first moment after it. */
    fun bounds(year: Int, monthIndex: Int?, zone: ZoneId = ZoneId.systemDefault()): Pair<Instant, Instant> {
        fun at(startYear: Int, startMonth: Int): Instant = LocalDate.of(startYear, 1, 1).plusMonths(startMonth.toLong()).atStartOfDay(zone).toInstant()
        return if (monthIndex == null) at(year, 0) to at(year + 1, 0) else at(year, monthIndex) to at(year, monthIndex + 1)
    }

    fun id(year: Int, monthIndex: Int?): String = if (monthIndex == null) "%04d".format(year) else "%04d-%02d".format(year, monthIndex + 1)

    /** The name of a month as the reader's language writes it on its own: "July". */
    fun monthText(monthIndex: Int, locale: Locale = Locale.getDefault()): String =
        DateTimeFormatter.ofPattern("LLLL", locale).format(LocalDate.of(2000, monthIndex + 1, 1))

    /** The years from the newest to the oldest, both included. */
    fun yearsBetween(newestYear: Int, oldestYear: Int): List<Int> = if (newestYear < oldestYear) emptyList() else (newestYear downTo oldestYear).toList()

    /** The months of a year from December back to January: the order a list that runs newest first has them in. */
    val monthIndexesNewestFirst: List<Int> = (11 downTo 0).toList()

    /** The year of the photo's place in the timeline (taken, or arrived when nothing says when it was taken). */
    fun year(photo: Photo, zone: ZoneId = ZoneId.systemDefault()): Int? =
        (ServerTime.instant(photo.takenAt) ?: ServerTime.instant(photo.createdAt))?.atZone(zone)?.year

    /**
     * The filter narrowed to a period: the photos the filter keeps that were also taken in it. Null when nothing can be,
     * because the filter's own dates and the period do not overlap.
     */
    fun narrow(filter: PhotoFilter, takenAfter: Instant, takenBefore: Instant, zone: ZoneId = ZoneId.systemDefault()): PhotoFilter? {
        val after = filter.firstTakenAfter(zone)?.let { own -> if (own > takenAfter) own else takenAfter } ?: takenAfter
        val before = filter.firstTakenBefore(zone)?.let { own -> if (own < takenBefore) own else takenBefore } ?: takenBefore
        if (after >= before) return null
        return filter.copy(takenAfter = after, takenBefore = before)
    }

    /**
     * The filter that counts the photos newer than the whole of a period: those taken from the period's end on. Null
     * when none can be, because the filter itself ends at or before the period's end.
     */
    fun newerThan(filter: PhotoFilter, periodTakenBefore: Instant, zone: ZoneId = ZoneId.systemDefault()): PhotoFilter? {
        val after = filter.firstTakenAfter(zone)?.let { own -> if (own > periodTakenBefore) own else periodTakenBefore } ?: periodTakenBefore
        val before = filter.firstTakenBefore(zone)
        if (before != null && before <= after) return null
        return filter.copy(takenAfter = after, takenBefore = before)
    }
}
