package com.teanode.kit.search

import kotlinx.serialization.json.Json
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class SearchTest {
    @Test
    fun wordsAreMadeReady() {
        assertEquals("a b c", SearchWords.normalized("  a \n b\t c "))
        assertNull(SearchWords.normalized("   "))
    }

    @Test
    fun aResultOpensWhereItsAreaSays() {
        fun open(area: String, target: String) = SearchResult(area, target).opening
        assertEquals(SearchOpening.Conversation("c1"), open("chats", "c1"))
        assertEquals(SearchOpening.Nowhere, open("chats", ""))
        assertEquals(SearchOpening.PartOnly(SearchArea.Places), open("places", "x"))
        assertEquals(SearchOpening.Nowhere, open("documents", "d"))
        assertEquals(SearchOpening.Nowhere, open("unheard-of", "d"))
    }

    @Test
    fun aMissingTextIsEmptyNotAFailure() {
        val results = Json { ignoreUnknownKeys = true }.decodeFromString(SearchEverythingResults.serializer(), """{"areas":[{"searchArea":"mail","results":[{"searchArea":"mail"}]}]}""")
        assertEquals("", results.areas[0].results[0].titleText)
        assertTrue(results.failedAreas.isEmpty())
        assertEquals(false, results.isEmpty)
    }

    private val places = listOf(
        PlaceName("settings", listOf("Settings")),
        PlaceName("passkeys", listOf("Passkeys"), listOf(listOf("Settings"))),
        PlaceName("passwords", listOf("Passwords")),
        PlaceName("manage-domains", listOf("Domains"), listOf(listOf("Settings"), listOf("Manage"))),
    )

    @Test
    fun partsComeFirstThenPlacesInsideThem() {
        assertEquals(listOf("passwords", "passkeys"), PlaceSearch.matchingPlaceIds(places, "pass"))
    }

    @Test
    fun aParentAndItsChildAreSaidTogether() {
        assertEquals(listOf("passkeys"), PlaceSearch.matchingPlaceIds(places, "set pass"))
        assertEquals(listOf("manage-domains"), PlaceSearch.matchingPlaceIds(places, "manage dom"))
    }

    @Test
    fun lettersInsideAWordAreNoMatch() {
        assertEquals(emptyList<String>(), PlaceSearch.matchingPlaceIds(places, "word"))
        assertEquals(emptyList<String>(), PlaceSearch.matchingPlaceIds(places, "  "))
    }
}
