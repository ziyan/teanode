package com.teanode.app.search

import com.teanode.kit.AppSection
import com.teanode.kit.search.SearchArea
import com.teanode.kit.search.SearchResult
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class SearchDestinationTest {
    private fun resultOf(area: SearchArea, targetId: String) = SearchResult(searchArea = area.serverName, targetId = targetId)

    @Test fun `a contact opens its card and a page of memory opens the page`() {
        assertEquals(SearchDestination.Item(AppSection.Contacts, "contacts/contact/c1"), SearchDestinations.of(resultOf(SearchArea.Contacts, "c1"), "ada"))
        assertEquals(SearchDestination.Item(AppSection.Knowledge, "knowledge/page?path=people%2Fada"), SearchDestinations.of(resultOf(SearchArea.Memory, "people/ada"), "ada"))
    }

    @Test fun `a photo opens in the library searched for the words`() {
        assertEquals(
            SearchDestination.PartWithWords(AppSection.Photos, PartSearch(SearchArea.Photos, "harbor", "p1")),
            SearchDestinations.of(resultOf(SearchArea.Photos, "p1"), "harbor"),
        )
    }

    @Test fun `show all carries the words to every part that searches`() {
        assertEquals(SearchDestination.Item(AppSection.Mail, "mail/search?words=two%20words"), SearchDestinations.showAll(SearchArea.Mail, "two words"))
        assertEquals(SearchDestination.PartWithWords(AppSection.Agent, PartSearch(SearchArea.Chats, "plan")), SearchDestinations.showAll(SearchArea.Chats, "plan"))
        assertEquals(SearchDestination.PartWithWords(AppSection.Contacts, PartSearch(SearchArea.Contacts, "ada")), SearchDestinations.showAll(SearchArea.Contacts, "ada"))
        assertEquals(SearchDestination.PartWithWords(AppSection.Photos, PartSearch(SearchArea.Photos, "sea")), SearchDestinations.showAll(SearchArea.Photos, "sea"))
        assertEquals(SearchDestination.PartWithWords(AppSection.Knowledge, PartSearch(SearchArea.Memory, "tea")), SearchDestinations.showAll(SearchArea.Memory, "tea"))
        assertEquals(SearchDestination.PartWithWords(AppSection.Recordings, PartSearch(SearchArea.Recordings, "call")), SearchDestinations.showAll(SearchArea.Recordings, "call"))
        assertEquals(SearchDestination.PartWithWords(AppSection.Location, PartSearch(SearchArea.Places, "cafe")), SearchDestinations.showAll(SearchArea.Places, "cafe"))
        assertEquals(SearchDestination.Nowhere, SearchDestinations.showAll(SearchArea.Documents, "tea"))
    }

    @Test fun `a finance search opens the transactions with the words as their text`() {
        val destination = SearchDestinations.showAll(SearchArea.Finance, "rent") as SearchDestination.Item
        assertEquals(AppSection.Finance, destination.section)
        assertEquals(true, destination.route.startsWith("finance/transactions?") && destination.route.contains("text=rent"))
    }
}
