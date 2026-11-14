package com.teanode.kit.location

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class PlaceSearchTest {
    private val settings = listOf("Settings", "Settings")
    private val manage = listOf("Manage", "Manage")

    private val places = listOf(
        PlaceName("chats", listOf("Chats", "Chats")),
        PlaceName("settings", settings),
        PlaceName("location", listOf("Location", "Location")),
        PlaceName("passkeys", listOf("Passkeys", "Passkeys"), listOf(settings)),
        PlaceName("location-settings", listOf("Location settings", "Location settings"), listOf(listOf("Location", "Location"))),
        PlaceName("domains", listOf("Domains", "Domains"), listOf(settings, manage)),
        PlaceName("people-in-photos", listOf("People in Photos", "People in Photos"), listOf(settings)),
        PlaceName("people", listOf("People", "People"), listOf(settings, manage)),
        PlaceName("receipts", listOf("收据", "Receipts"), listOf(listOf("财务", "Finance"))),
    )

    @Test
    fun aPartComesBeforeThePlacesInsideOthersThatNameTheWords() {
        assertEquals(listOf("settings", "location-settings"), PlaceSearch.matchingPlaceIds(places, "settings"))
    }

    @Test
    fun aPlaceIsFoundByItsOwnName() {
        assertEquals(listOf("passkeys"), PlaceSearch.matchingPlaceIds(places, "passk"))
        assertEquals(listOf("domains"), PlaceSearch.matchingPlaceIds(places, "Domains"))
    }

    @Test
    fun theStartOfANameMatchesBeforeAWordInsideIt() {
        assertEquals(listOf("people-in-photos", "people"), PlaceSearch.matchingPlaceIds(places, "people"))
        assertEquals(listOf("people-in-photos"), PlaceSearch.matchingPlaceIds(places, "photos"))
    }

    @Test
    fun thePlaceItIsInAndItsOwnNameCanBeSaidTogether() {
        assertEquals(listOf("domains"), PlaceSearch.matchingPlaceIds(places, "manage dom"))
        assertEquals(listOf("passkeys"), PlaceSearch.matchingPlaceIds(places, "set pass"))
        assertEquals(listOf("people"), PlaceSearch.matchingPlaceIds(places, "manage peo"))
    }

    @Test
    fun lettersInsideAWordAreNoMatch() {
        assertEquals(emptyList<String>(), PlaceSearch.matchingPlaceIds(places, "ains"))
        assertEquals(emptyList<String>(), PlaceSearch.matchingPlaceIds(places, "chat dom"))
    }

    @Test
    fun aNameMatchesInEnglishWhateverTheLanguage() {
        assertEquals(listOf("receipts"), PlaceSearch.matchingPlaceIds(places, "receipts"))
        assertEquals(listOf("receipts"), PlaceSearch.matchingPlaceIds(places, "收据"))
        assertEquals(listOf("receipts"), PlaceSearch.matchingPlaceIds(places, "finance rec"))
    }

    @Test
    fun wordsWithNothingInThemFindNothing() {
        assertEquals(emptyList<String>(), PlaceSearch.matchingPlaceIds(places, "  "))
    }

    @Test
    fun noMoreThanTheLimitIsOffered() {
        val many = (0 until 20).map { index -> PlaceName("place-$index", listOf("Folder $index")) }
        assertEquals(PlaceSearch.PLACE_LIMIT, PlaceSearch.matchingPlaceIds(many, "folder").size)
    }
}
