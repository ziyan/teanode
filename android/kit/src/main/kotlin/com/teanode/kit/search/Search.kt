package com.teanode.kit.search

import com.teanode.kit.Connection
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import java.time.Instant

/** One kind of thing the search across everything finds. The names are the server's area names, in the order it lists them. */
enum class SearchArea(val serverName: String) {
    Chats("chats"), Mail("mail"), Contacts("contacts"), Photos("photos"), Memory("memory"), Documents("documents"),
    Recordings("recordings"), Places("places"), Finance("finance");

    /** Whether a part of the app can show all the area's results. Documents are found for the agent to read; no screen of the app lists them. */
    val isShownInApp: Boolean get() = this != Documents

    companion object {
        fun fromServerName(serverName: String): SearchArea? = entries.firstOrNull { area -> area.serverName == serverName }
    }
}

/** Where a search result is opened: which part of the app, and which item in it. A result the app has no screen for opens nowhere. */
sealed interface SearchOpening {
    data class Conversation(val conversationId: String) : SearchOpening
    data class MailItem(val mailItemId: String) : SearchOpening
    data class Contact(val contactId: String) : SearchOpening
    data class Photo(val photoId: String) : SearchOpening
    data class KnowledgePage(val path: String) : SearchOpening
    data class Recording(val recordingId: String) : SearchOpening

    /** A place or finance transaction: the part opens, and the item is found there by the words. */
    data class PartOnly(val area: SearchArea) : SearchOpening

    data object Nowhere : SearchOpening
}

/** One thing found: what it is, how to open it, and what to show for it. */
@Serializable
data class SearchResult(
    val searchArea: String,
    /** The conversation, mail item, contact, photo, page path, document, recording, place or transaction to open, as the area says. */
    val targetId: String = "",
    val titleText: String = "",
    /** A snippet, a sender, an account. */
    val detailText: String = "",
    val happenedAt: String? = null,
    /** Whether this is the person's main conversation, which the app names in the person's language. */
    val isMain: Boolean = false,
) {
    val identity: String get() = "$searchArea/$targetId"

    val area: SearchArea? get() = SearchArea.fromServerName(searchArea)

    /** When it happened, if the server said. */
    val happenedInstant: Instant? get() = ServerTime.instant(happenedAt)

    /** Where the result opens. */
    val opening: SearchOpening
        get() {
            val found = area ?: return SearchOpening.Nowhere
            return when (found) {
                SearchArea.Chats -> if (targetId.isEmpty()) SearchOpening.Nowhere else SearchOpening.Conversation(targetId)
                SearchArea.Mail -> if (targetId.isEmpty()) SearchOpening.Nowhere else SearchOpening.MailItem(targetId)
                SearchArea.Contacts -> if (targetId.isEmpty()) SearchOpening.Nowhere else SearchOpening.Contact(targetId)
                SearchArea.Photos -> if (targetId.isEmpty()) SearchOpening.Nowhere else SearchOpening.Photo(targetId)
                SearchArea.Memory -> if (targetId.isEmpty()) SearchOpening.Nowhere else SearchOpening.KnowledgePage(targetId)
                SearchArea.Recordings -> if (targetId.isEmpty()) SearchOpening.Nowhere else SearchOpening.Recording(targetId)
                SearchArea.Places, SearchArea.Finance -> SearchOpening.PartOnly(found)
                // The app has no screen for a document; it is read as the passage the search shows.
                SearchArea.Documents -> SearchOpening.Nowhere
            }
        }
}

/** One area's results, with whether the area holds more than these. */
@Serializable
data class SearchAreaResults(val searchArea: String, val results: List<SearchResult> = emptyList(), val hasMore: Boolean = false) {
    val area: SearchArea? get() = SearchArea.fromServerName(searchArea)
}

/** A search across everything: the areas that found something, in the server's fixed order, and the areas that failed or did not answer in time. */
@Serializable
data class SearchEverythingResults(val words: String = "", val areas: List<SearchAreaResults> = emptyList(), val failedAreas: List<String> = emptyList()) {
    /** Whether nothing was found in any area. */
    val isEmpty: Boolean get() = areas.all { areaResults -> areaResults.results.isEmpty() }
}

/** What is typed in a search field, made ready to ask with. */
object SearchWords {
    /** How many results the search asks of each area: the server's default. */
    const val PER_AREA_LIMIT = 5

    /** The words with their ends trimmed and runs of white space made one space; null when nothing is left to look for. */
    fun normalized(typedText: String): String? = typedText.split(Regex("\\s+")).filter { word -> word.isNotEmpty() }.joinToString(" ").ifEmpty { null }
}

@Serializable private class SearchEverythingAnswer(val SearchEverything: SearchEverythingResults)

/** Looks for the words across everything: a few results from each area the person may use, and the areas that did not answer. */
suspend fun Connection.searchEverything(words: String, perAreaLimit: Int = SearchWords.PER_AREA_LIMIT): SearchEverythingResults =
    execute<SearchEverythingAnswer>(SearchDocuments.SEARCH_EVERYTHING, variablesOf("words" to words, "perAreaLimit" to perAreaLimit)).SearchEverything

/** One place of the app a search can jump to, as the search box matches it: its own name, and the names of the places it is inside, each in the reader's language and in English. */
data class PlaceName(
    val placeId: String,
    /** The place's own name, in the reader's language and in English. */
    val names: List<String>,
    /** The names of the places it is inside, outermost first, each in the reader's language and in English. */
    val parentNames: List<List<String>> = emptyList(),
) {
    /** Whether the place is a part of the app rather than a place inside one. */
    val isPart: Boolean get() = parentNames.isEmpty()
}

/**
 * Picks the places whose names the typed words are, the way the dashboard's search box offers its pages and their tabs:
 * first the parts of the app, then the places inside them, each best match first and in the app's own order within a match.
 */
object PlaceSearch {
    /** How many places the search offers at most. */
    const val PLACE_LIMIT = 8

    /**
     * How well the words match a name: 0 when the name starts with them, 1 when a word of the name does, and null
     * otherwise. Letters inside a word are no match. Both are lower case.
     */
    fun nameRank(name: String, words: String): Int? {
        if (name.startsWith(words)) return 0
        val nameWords = name.split(Regex("[\\s\\-_/]")).filter { word -> word.isNotEmpty() }
        if (nameWords.any { word -> word.startsWith(words) }) return 1
        return null
    }

    /**
     * How well the words match a place. Its own name matches as a part's does; so do the start of a name it is inside
     * and the start of its own, said together ("set pass", "manage dom"). The words are cut at each space to find the two halves.
     */
    fun placeRank(place: PlaceName, words: String): Int? {
        val ranks = place.names.mapNotNull { name -> nameRank(name.lowercase(), words) }.toMutableList()
        val parentNames = place.parentNames.flatten().map { name -> name.lowercase() }
        if (parentNames.isNotEmpty()) {
            for (gap in words.indices) {
                if (!words[gap].isWhitespace()) continue
                val parentWords = words.substring(0, gap).trim()
                val ownWords = words.substring(gap + 1).trim()
                if (parentWords.isEmpty() || ownWords.isEmpty()) continue
                if (parentNames.none { name -> name.startsWith(parentWords) }) continue
                ranks += place.names.mapNotNull { name -> nameRank(name.lowercase(), ownWords) }
            }
        }
        return ranks.minOrNull()
    }

    /** The ids of the places the words name, in the order to offer them, at most [PLACE_LIMIT]; none for words with nothing in them. */
    fun matchingPlaceIds(places: List<PlaceName>, words: String): List<String> {
        val needle = words.trim().lowercase()
        if (needle.isEmpty()) return emptyList()
        data class Ranked(val place: PlaceName, val rank: Int, val order: Int)
        val ranked = places.mapIndexedNotNull { order, place -> placeRank(place, needle)?.let { rank -> Ranked(place, rank, order) } }
        val sorted = ranked.sortedWith(compareBy<Ranked> { entry -> if (entry.place.isPart) 0 else 1 }.thenBy { entry -> entry.rank }.thenBy { entry -> entry.order })
        return sorted.take(PLACE_LIMIT).map { entry -> entry.place.placeId }
    }
}
