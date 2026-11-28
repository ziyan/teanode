package com.teanode.kit.search

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class SearchRecentsTest {
    @Test
    fun theNewestComesFirstAndAnEarlierCopyGoes() {
        var recents = SearchRecents.adding(SearchRecent.place("finance-Imports"), emptyList())
        recents = SearchRecents.adding(SearchRecent.found(SearchResult("contacts", "contact-1", "Robin"), "rob"), recents)
        recents = SearchRecents.adding(SearchRecent.place("finance-Imports"), recents)
        assertEquals(listOf("place:finance-Imports", "found:contacts/contact-1"), recents.map { recent -> recent.identity })
    }

    @Test
    fun theListKeepsItsCapacityAndReadsBackAsWritten() {
        var recents = emptyList<SearchRecent>()
        for (index in 0 until SearchRecents.CAPACITY + 3) recents = SearchRecents.adding(SearchRecent.place("place-$index"), recents)
        assertEquals(SearchRecents.CAPACITY, recents.size)
        assertEquals(recents, SearchRecents.decode(SearchRecents.encode(recents)))
        assertTrue(SearchRecents.decode("not a list").isEmpty())
    }
}
