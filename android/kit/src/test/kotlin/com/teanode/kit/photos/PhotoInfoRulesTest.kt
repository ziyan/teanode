package com.teanode.kit.photos

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.ZoneId
import java.util.Locale

class PhotoInfoRulesTest {
    private fun photo(isOwned: Boolean = true, latitude: Double? = null, longitude: Double? = null) =
        Photo(id = "p1", createdAt = "2026-02-01T00:00:00Z", photoKind = "picture", isOwnedByCaller = isOwned, latitude = latitude, longitude = longitude)

    private fun library(isAvailable: Boolean, message: String = "") = PhotoLibrary(
        photoCount = 1, videoCount = 0, favoriteCount = 0, trashedCount = 0, usedBytes = 0, trashRetentionDays = 30,
        isDescriptionAvailable = isAvailable, descriptionUnavailableMessage = message,
    )

    private val described = PhotoDescription(photoDescription = "A pier", describedAt = "2026-02-02T00:00:00Z")

    @Test
    fun aPhotoNotDescribedYetOffersToDescribeItWhenTheServerCan() {
        assertEquals(PhotoDescriptionOffer.Describe, PhotoDescriptionOffers.offer(photo(), PhotoDescription(), library(true), isAsking = false))
        assertTrue(PhotoDescriptionOffers.isHintShown(photo(), PhotoDescription(), library(true)))
    }

    @Test
    fun aDescribedPhotoOffersADescriptionAgainAndNoHint() {
        assertEquals(PhotoDescriptionOffer.DescribeAgain, PhotoDescriptionOffers.offer(photo(), described, library(true), isAsking = false))
        assertFalse(PhotoDescriptionOffers.isHintShown(photo(), described, library(true)))
    }

    @Test
    fun aDescriptionOnItsWayOrJustAskedForIsWaitedFor() {
        val requested = PhotoDescription(isDescriptionRequested = true)
        assertEquals(PhotoDescriptionOffer.Waiting, PhotoDescriptionOffers.offer(photo(), requested, library(true), isAsking = false))
        assertEquals(PhotoDescriptionOffer.Waiting, PhotoDescriptionOffers.offer(photo(), PhotoDescription(), library(true), isAsking = true))
        assertFalse(PhotoDescriptionOffers.isHintShown(photo(), requested, library(true)))
    }

    @Test
    fun whenTheServerCannotDescribeTheReasonIsGivenOnlyBeforeAnyDescription() {
        assertEquals(PhotoDescriptionOffer.Unavailable("no model"), PhotoDescriptionOffers.offer(photo(), PhotoDescription(), library(false, "no model"), isAsking = false))
        assertEquals(PhotoDescriptionOffer.Nothing, PhotoDescriptionOffers.offer(photo(), described, library(false, "no model"), isAsking = false))
    }

    @Test
    fun nothingIsOfferedForSomebodyElsesPhotoOrBeforeTheLibraryIsRead() {
        assertEquals(PhotoDescriptionOffer.Nothing, PhotoDescriptionOffers.offer(photo(isOwned = false), PhotoDescription(), library(true), isAsking = false))
        assertEquals(PhotoDescriptionOffer.Nothing, PhotoDescriptionOffers.offer(photo(), PhotoDescription(), null, isAsking = false))
        assertFalse(PhotoDescriptionOffers.isHintShown(photo(isOwned = false), PhotoDescription(), library(true)))
    }

    private fun face(id: String, groupId: String, name: String) = PhotoFace(id = id, photoId = "p1", photoFaceGroupId = groupId, contactName = name)

    @Test
    fun onlyGroupedFacesArePeopleAndTheirNamesAreTrimmed() {
        val faces = listOf(face("f1", "g1", " Alex "), face("f2", "", ""), face("f3", "g2", ""), face("f4", "g1", "Alex"))
        assertEquals(listOf("f1", "f3", "f4"), PhotoFaces.people(faces).map { face -> face.id })
        assertEquals("Alex", PhotoFaces.shownName(faces[0]))
        assertNull(PhotoFaces.shownName(faces[2]))
        assertEquals(listOf("Alex"), PhotoFaces.namesInSharedPhoto(faces))
    }

    @Test
    fun aPositionIsWrittenAsFourDecimals() {
        assertEquals("46.5000, 7.2500", PhotoPositionText.coordinates(photo(latitude = 46.5, longitude = 7.25)))
        assertNull(PhotoPositionText.coordinates(photo(latitude = 46.5)))
    }

    @Test
    fun aWindowOfTimeIsWrittenWithBothEndsInOrderOrTheOneThereIs() {
        val zone = ZoneId.of("UTC")
        val start = Instant.parse("2026-10-12T09:00:00Z")
        val end = Instant.parse("2026-10-12T11:30:00Z")
        val both = PhotoPositionText.windowText(start, end, zone, Locale.US)!!
        assertTrue(both.contains(" - "), both)
        assertEquals(PhotoPositionText.windowText(start, null, zone, Locale.US), PhotoPositionText.windowText(null, start, zone, Locale.US))
        assertNull(PhotoPositionText.windowText(null, null, zone, Locale.US))
    }
}

class PhotoFavoritesTest {
    private fun photo(id: String, isFavorite: Boolean) = Photo(id = id, createdAt = "2026-02-01T00:00:00Z", photoKind = "picture", isFavorite = isFavorite)

    @Test
    fun theHeartTakesTheHeartAwayOnlyWhenEveryPhotoHasOne() {
        assertTrue(PhotoFavorites.isFavoriteTarget(listOf(photo("a", true), photo("b", false))))
        assertFalse(PhotoFavorites.isFavoriteTarget(listOf(photo("a", true), photo("b", true))))
        assertTrue(PhotoFavorites.isFavoriteTarget(listOf(photo("a", false))))
    }

    @Test
    fun onlyPhotosNotAlreadyAsWantedAreChanged() {
        val photos = listOf(photo("a", true), photo("b", false), photo("c", false))
        assertEquals(listOf("b", "c"), PhotoFavorites.photosToChange(photos, true).map { each -> each.id })
        assertEquals(listOf("a"), PhotoFavorites.photosToChange(photos, false).map { each -> each.id })
    }
}
