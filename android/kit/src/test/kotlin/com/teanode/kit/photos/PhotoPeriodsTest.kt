package com.teanode.kit.photos

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

class PhotoPeriodsTest {
    private val utc = ZoneId.of("UTC")
    private val offsetZone = ZoneId.of("Etc/GMT-2")

    @Test
    fun aYearAndAMonthRunFromTheirFirstMomentToTheNextOnesOnThePhonesClock() {
        assertEquals(Instant.parse("2030-01-01T00:00:00Z") to Instant.parse("2031-01-01T00:00:00Z"), PhotoPeriods.bounds(2030, null, utc))
        assertEquals(Instant.parse("2030-07-01T00:00:00Z") to Instant.parse("2030-08-01T00:00:00Z"), PhotoPeriods.bounds(2030, 6, utc))
        assertEquals(Instant.parse("2030-12-01T00:00:00Z") to Instant.parse("2031-01-01T00:00:00Z"), PhotoPeriods.bounds(2030, 11, utc))
        // Midnight at +02:00 is two hours before midnight UTC.
        assertEquals(Instant.parse("2029-12-31T22:00:00Z") to Instant.parse("2030-12-31T22:00:00Z"), PhotoPeriods.bounds(2030, null, offsetZone))
    }

    @Test
    fun periodsAreNamedByYearAndMonth() {
        assertEquals("2030", PhotoPeriods.id(2030, null))
        assertEquals("2030-07", PhotoPeriods.id(2030, 6))
        assertEquals("0987", PhotoPeriods.id(987, null))
    }

    @Test
    fun aMonthIsNamedByItsIndex() {
        assertEquals("July", PhotoPeriods.monthText(6, java.util.Locale.ENGLISH))
        assertEquals("January", PhotoPeriods.monthText(0, java.util.Locale.ENGLISH))
    }

    @Test
    fun yearsRunNewestToOldestAndMonthsFromDecember() {
        assertEquals(listOf(2031, 2030, 2029), PhotoPeriods.yearsBetween(2031, 2029))
        assertEquals(listOf(2030), PhotoPeriods.yearsBetween(2030, 2030))
        assertEquals(emptyList<Int>(), PhotoPeriods.yearsBetween(2029, 2031))
        assertEquals(12, PhotoPeriods.monthIndexesNewestFirst.size)
        assertEquals(11, PhotoPeriods.monthIndexesNewestFirst.first())
        assertEquals(PhotoYearSpan(2031, 2029).years, listOf(2031, 2030, 2029))
    }

    @Test
    fun aPhotosYearIsWhenItWasTakenOrElseWhenItArrivedOnThePhonesClock() {
        val taken = Photo(id = "a", createdAt = "2031-01-05T00:00:00Z", photoKind = "picture", takenAt = "2029-12-31T23:30:00Z")
        assertEquals(2029, PhotoPeriods.year(taken, utc))
        assertEquals(2030, PhotoPeriods.year(taken, offsetZone))
        assertEquals(2031, PhotoPeriods.year(taken.copy(takenAt = null), utc))
        assertNull(PhotoPeriods.year(taken.copy(takenAt = null, createdAt = "nonsense"), utc))
    }

    @Test
    fun aFilterIsNarrowedToAPeriodOrToNothing() {
        val (after, before) = PhotoPeriods.bounds(2030, 6, utc)
        assertEquals(PhotoFilter(takenAfter = after, takenBefore = before), PhotoPeriods.narrow(PhotoFilter(), after, before, utc))
        // A filter that starts in the middle of the month keeps its own start.
        val midMonth = PhotoFilter(firstDay = LocalDate.of(2030, 7, 15))
        val narrowed = PhotoPeriods.narrow(midMonth, after, before, utc)!!
        assertEquals(Instant.parse("2030-07-15T00:00:00Z"), narrowed.takenAfter)
        assertEquals(before, narrowed.takenBefore)
        // A filter that ends before the month starts has no photo in it.
        assertNull(PhotoPeriods.narrow(PhotoFilter(lastDay = LocalDate.of(2030, 6, 30)), after, before, utc))
        assertNull(PhotoPeriods.narrow(PhotoFilter(takenAfter = before), after, before, utc))
    }

    @Test
    fun thePhotosNewerThanAPeriodAreThoseTakenFromItsEndOn() {
        val periodEnd = Instant.parse("2030-08-01T00:00:00Z")
        assertEquals(PhotoFilter(takenAfter = periodEnd), PhotoPeriods.newerThan(PhotoFilter(), periodEnd, utc))
        // The filter's own start wins when it is later than the period's end.
        val later = PhotoFilter(takenAfter = Instant.parse("2030-09-01T00:00:00Z"))
        assertEquals(Instant.parse("2030-09-01T00:00:00Z"), PhotoPeriods.newerThan(later, periodEnd, utc)!!.takenAfter)
        // The filter's own end is kept, and a filter that ends before the period's end has nothing newer.
        val capped = PhotoFilter(lastDay = LocalDate.of(2030, 10, 9))
        assertEquals(Instant.parse("2030-10-10T00:00:00Z"), PhotoPeriods.newerThan(capped, periodEnd, utc)!!.takenBefore)
        assertNull(PhotoPeriods.newerThan(PhotoFilter(takenBefore = periodEnd), periodEnd, utc))
    }

    @Test
    fun aListQuerySaysItsPartTheAlbumAndThePerson() {
        val variables = PhotoListQuery(PhotoCollection.Favorites, PhotoFilter(photoKindFilter = PhotoKindFilter.Video), photoAlbumId = "album1").variables(utc)
        assertEquals(setOf("isFavorite", "albumId", "photoKind"), variables.keys)
        assertEquals(setOf("faceGroupId"), PhotoListQuery(photoFaceGroupId = "person1").variables(utc).keys)
        assertEquals(emptySet<String>(), PhotoListQuery().variables(utc).keys)
    }
}
