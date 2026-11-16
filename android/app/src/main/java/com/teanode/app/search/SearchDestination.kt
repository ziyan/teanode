package com.teanode.app.search

import android.net.Uri
import com.teanode.app.contacts.ContactsRoutes
import com.teanode.app.knowledge.KnowledgeRoutes
import com.teanode.app.mail.MailRoutes
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.search.SearchArea
import com.teanode.kit.search.SearchOpening
import com.teanode.kit.search.SearchResult

/** SearchDestination is where a row of the Search screen goes (ios AppShell.openSearchResult and showAllInPart). */
sealed class SearchDestination {
    /** An item in a part: the part opens first, so back goes to it. */
    data class Item(val section: AppSection, val route: String) : SearchDestination()

    /** A part with the words in its own search, where it has one. */
    data class PartWithWords(val section: AppSection, val search: PartSearch) : SearchDestination()

    /** Nothing in the app shows it. */
    data object Nowhere : SearchDestination()
}

/** The part each kind of search finds things in; null for documents, which no part of the app shows. */
fun sectionOfSearchArea(area: SearchArea): AppSection? = when (area) {
    SearchArea.Chats -> AppSection.Agent
    SearchArea.Mail -> AppSection.Mail
    SearchArea.Contacts -> AppSection.Contacts
    SearchArea.Photos -> AppSection.Photos
    SearchArea.Memory -> AppSection.Knowledge
    SearchArea.Recordings -> AppSection.Recordings
    SearchArea.Finance -> AppSection.Finance
    SearchArea.Places -> AppSection.Location
    SearchArea.Documents -> null
}

object SearchDestinations {
    /** Where a result opens: the item where a part has a screen for it, the part with the words in its search otherwise. */
    fun of(result: SearchResult, words: String): SearchDestination = when (val opening = result.opening) {
        is SearchOpening.Conversation -> SearchDestination.Item(AppSection.Agent, Routes.child(AppSection.Agent, "conversation/${Uri.encode(opening.conversationId)}"))
        is SearchOpening.MailItem -> SearchDestination.Item(AppSection.Mail, MailRoutes.thread(opening.mailItemId, null))
        is SearchOpening.Contact -> SearchDestination.Item(AppSection.Contacts, ContactsRoutes.contact(opening.contactId))
        is SearchOpening.Photo -> SearchDestination.PartWithWords(AppSection.Photos, PartSearch(SearchArea.Photos, words, opening.photoId))
        is SearchOpening.KnowledgePage -> SearchDestination.Item(AppSection.Knowledge, KnowledgeRoutes.page(opening.path))
        is SearchOpening.Recording -> SearchDestination.Item(AppSection.Recordings, Routes.child(AppSection.Recordings, "detail/${Uri.encode(opening.recordingId)}"))
        is SearchOpening.PartOnly -> showAll(opening.area, words)
        SearchOpening.Nowhere -> SearchDestination.Nowhere
    }

    /**
     * Show all: the part with the words in its own search. Mail and Finance take them in a route of theirs (Mail's
     * search and the transactions' text), the others take the request.
     */
    fun showAll(area: SearchArea, words: String): SearchDestination {
        val section = sectionOfSearchArea(area) ?: return SearchDestination.Nowhere
        return when (area) {
            SearchArea.Mail -> SearchDestination.Item(section, MailRoutes.search(words))
            SearchArea.Finance -> SearchDestination.Item(section, com.teanode.app.finance.FinanceRoutes.transactions(com.teanode.kit.finance.FinanceTransactionFilter(text = words)))
            else -> SearchDestination.PartWithWords(section, PartSearch(area, words))
        }
    }
}
