package com.teanode.kit.photos

import kotlinx.serialization.json.JsonPrimitive
import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

class PhotosTest {
    private val utc = ZoneId.of("UTC")

    private fun photo(id: String, takenAt: String? = null, offsetMinutes: Int? = null, kind: String = "picture", trashedAt: String? = null) = Photo(
        id = id, createdAt = "2026-02-01T00:00:00Z", photoKind = kind, takenAt = takenAt, takenAtOffsetMinutes = offsetMinutes, trashedAt = trashedAt,
    )

    @Test
    fun aPhotoIsReadFromTheServersJsonWithItsDescriptionFields() {
        val json = """{"id":"p1","createdAt":"2026-02-01T00:00:00Z","photoKind":"video","widthPixels":10,"heightPixels":20,"durationMS":65400,
            "photoFileRoles":["original","edited"],"smallPreviewUrl":"/s","photoDescription":"A dog","describedAt":"2026-02-02T00:00:00Z","future":1}"""
        val photo = TeaNodeJson.decodeFromString(Photo.serializer(), json)
        assertTrue(photo.isVideo)
        assertTrue(photo.hasEditedFile)
        assertEquals("/s", photo.gridPreviewPath)
        assertNull(photo.viewerPreviewPath?.takeIf { path -> path != "/s" })
        assertEquals("video.mp4", photo.filename)
        assertTrue(photo.description.isDescribed)
        assertTrue(photo.isOwnedByCaller)
    }

    @Test
    fun daysAreReadInTheZoneThePhotoWasTakenIn() {
        // 23:30 on the 1st in UTC is already the 2nd at +02:00.
        val abroad = photo("a", "2026-03-01T23:30:00Z", 120)
        val home = photo("b", "2026-03-01T23:30:00Z")
        val days = PhotoTimeline.days(listOf(abroad, home), utc)
        assertEquals(listOf("2026-03-02", "2026-03-01"), days.map { day -> day.id })
        assertEquals(2, days.first().day)
    }

    @Test
    fun aPhotoWithoutAnyTimeLandsInTheUnknownDay() {
        val undated = photo("a").copy(createdAt = "not a time")
        assertEquals("unknown", PhotoTimeline.days(listOf(undated), utc).single().id)
    }

    @Test
    fun theTimelineListsNewestFirstThenByIdAndTheUndatedLast() {
        val sorted = PhotoTimeline.sorted(listOf(photo("a", "2026-01-01T00:00:00Z"), photo("b", "2026-03-01T00:00:00Z"), photo("c", "2026-01-01T00:00:00Z")))
        assertEquals(listOf("b", "c", "a"), sorted.map { item -> item.id })
    }

    @Test
    fun aChangedPhotoMovesToItsPlaceAndTheCursorFollows() {
        val held = listOf(photo("c", "2026-03-01T00:00:00Z"), photo("b", "2026-02-01T00:00:00Z"), photo("a", "2026-01-01T00:00:00Z"))
        val moved = photo("a", "2026-04-01T00:00:00Z")
        val placement = PhotoPlacement.place(moved, held, pageCursorPhotoId = "a", hasMore = true)
        assertEquals(listOf("a", "c", "b"), placement.photos.map { item -> item.id })
        assertEquals("b", placement.pageCursorPhotoId)
        assertFalse(placement.isBeyondHeldPhotos)
        val older = PhotoPlacement.place(photo("c", "2025-01-01T00:00:00Z"), held, null, hasMore = true)
        assertTrue(older.isBeyondHeldPhotos)
    }

    @Test
    fun pagingAsksForMoreNearTheEndAndDropsRepeats() {
        val photos = (1..100).map { number -> photo("p$number") }
        assertTrue(PhotoPaging.shouldLoadMore("p50", photos))
        assertFalse(PhotoPaging.shouldLoadMore("p10", photos))
        assertFalse(PhotoPaging.shouldLoadMore("missing", photos))
        assertEquals(101, PhotoPaging.appending(listOf(photo("p100"), photo("p101")), photos).size)
        assertTrue(PhotoPaging.hasMore(200))
        assertFalse(PhotoPaging.hasMore(199))
    }

    @Test
    fun batchesAreAtMostFiveHundredIds() {
        val ids = (1..1201).map { number -> "p$number" }
        assertEquals(listOf(500, 500, 201), PhotoBatches.batches(ids).map { batch -> batch.size })
        assertTrue(PhotoBatches.batches(emptyList()).isEmpty())
    }

    @Test
    fun trashDaysCountDownTheRetention() {
        val now = Instant.parse("2026-03-10T12:00:00Z")
        val photos = listOf(photo("a", trashedAt = "2026-03-09T10:00:00Z"), photo("b", trashedAt = "2026-03-09T08:00:00Z"), photo("c", trashedAt = "2026-02-01T08:00:00Z"))
        val days = PhotoTimeline.trashDays(photos, 30, now, utc)
        assertEquals(2, days.size)
        // Counted from the oldest photo of the run: trashed 28 hours ago, which is one whole day.
        assertEquals(29, days.first().daysLeft)
        assertEquals(0, days.last().daysLeft)
        assertNull(PhotoTimeline.trashDaysLeft(null, 30, now))
        assertNull(PhotoTimeline.trashDaysLeft(now, 0, now))
    }

    @Test
    fun durationsAreWrittenAsAPlayerWritesThem() {
        assertEquals("0:00", PhotoTimeline.durationText(0))
        assertEquals("0:05", PhotoTimeline.durationText(4_600))
        assertEquals("1:05", PhotoTimeline.durationText(65_400))
        assertEquals("1:01:01", PhotoTimeline.durationText(3_661_000))
    }

    @Test
    fun aReferenceTitleNamesThePlaceAndTheDay() {
        assertEquals("Lakeside · 2026-03-01", PhotoTimeline.referenceTitle(photo("a", "2026-03-01T10:00:00Z").copy(placeName = " Lakeside "), utc))
        assertEquals("a.jpg", PhotoTimeline.referenceTitle(photo("a").copy(originalFilename = "a.jpg"), utc))
    }

    @Test
    fun monthsAndYearsGroupTheDays() {
        val photos = listOf(photo("a", "2026-03-05T10:00:00Z"), photo("b", "2026-03-01T10:00:00Z"), photo("c", "2025-12-30T10:00:00Z"))
        assertEquals(listOf("2026-03", "2025-12"), PhotoOverview.months(photos, utc).map { month -> month.id })
        assertEquals(listOf(2, 1), PhotoOverview.months(photos, utc).map { month -> month.photos.size })
        assertEquals(listOf("2026", "2025"), PhotoOverview.years(photos, utc).map { year -> year.id })
        assertTrue(PhotoOverview.dateRangeText(photos, utc)!!.contains(" - "))
        assertNull(PhotoOverview.dateRangeText(emptyList(), utc))
    }

    @Test
    fun theFilterSaysItsPlaceAllOrNothingAndItsDaysInThePhonesZone() {
        val filter = PhotoFilter(
            photoKindFilter = PhotoKindFilter.Video, firstDay = LocalDate.parse("2026-03-01"), lastDay = LocalDate.parse("2026-03-02"),
            photoNearFilter = PhotoNearFilter(1.0, 2.0),
        )
        val variables = filter.variables(utc)
        assertEquals(JsonPrimitive("video"), variables["photoKind"])
        assertEquals(JsonPrimitive("2026-03-01T00:00:00Z"), variables["takenAfter"])
        assertEquals(JsonPrimitive("2026-03-03T00:00:00Z"), variables["takenBefore"])
        assertEquals(3, listOf("nearLatitude", "nearLongitude", "nearDistanceKilometers").count { name -> name in variables })
        assertTrue(filter.isActive)
        assertFalse(PhotoFilter().isActive)
        assertTrue(PhotoFilter(firstDay = LocalDate.parse("2026-03-05"), lastDay = LocalDate.parse("2026-03-01")).isRangeBackwards)
    }

    @Test
    fun aDatePickedForAPhotoIsWrittenInTheZoneOfThePhoto() {
        val instant = Instant.parse("2026-03-01T23:30:00Z")
        assertEquals("2026-03-02T01:30:00+02:00", PhotoDateChange.takenAtText(instant, photo("a", offsetMinutes = 120), utc))
        assertEquals("2026-03-01T18:30:00-05:00", PhotoDateChange.takenAtText(instant, -5 * 3600))
        assertEquals("2026-03-01T23:30:00+00:00", PhotoDateChange.takenAtText(instant, photo("b"), utc))
    }

    @Test
    fun albumsAreGroupedIntoOwnSharedAndCircles() {
        val own = PhotoAlbum(id = "1", albumTitle = "Mine")
        val shared = PhotoAlbum(id = "2", albumTitle = "Theirs", isOwnedByCaller = false, canCallerManage = false, canCallerContribute = false)
        val circleB = PhotoAlbum(id = "3", albumTitle = "B1", isOwnedByCaller = false, circleId = "cb", circleName = "Beta")
        val circleA = PhotoAlbum(id = "4", albumTitle = "A1", circleId = "ca", circleName = "alpha")
        val sections = PhotoAlbumSections.of(listOf(own, shared, circleB, circleA))
        assertEquals(listOf("1"), sections.own.map { album -> album.id })
        assertEquals(listOf("2"), sections.sharedWithYou.map { album -> album.id })
        assertEquals(listOf("alpha", "Beta"), sections.circles.map { circle -> circle.circleName })
        assertTrue(sections.hasOthers)
        assertTrue(own.canCallerShare)
        assertFalse(circleA.canCallerShare)
        assertFalse(shared.canCallerRemove(photo("x").copy(isOwnedByCaller = false)))
    }

    @Test
    fun aSuggestionWhoseTitleOnlyRepeatsItsPlacesSaysSo() {
        val suggestion = PhotoAlbumSuggestion("2026-03-01T00:00:00Z", "2026-03-04T00:00:00Z", listOf("Lakeside, Hilltown"), "Lakeside and Hilltown", 12, emptyList())
        assertTrue(suggestion.isTitleMadeOfPlaces)
        assertFalse(suggestion.copy(albumTitle = "Spring trip").isTitleMadeOfPlaces)
    }

    @Test
    fun namingOffersANewContactOnlyForANewName() {
        assertTrue(PhotoNaming.offersNewContact(" Alex ", listOf("Sam")))
        assertFalse(PhotoNaming.offersNewContact("alex", listOf("Alex")))
        assertFalse(PhotoNaming.offersNewContact("  ", emptyList()))
    }

    @Test
    fun aDescriptionIsPolledQuicklyThenSlowly() {
        assertEquals(3, PhotoDescriptionPolling.secondsBeforeAsking(0))
        assertEquals(15, PhotoDescriptionPolling.secondsBeforeAsking(120))
        assertTrue(PhotoDescriptionPolling.isSlow(121))
    }
}
