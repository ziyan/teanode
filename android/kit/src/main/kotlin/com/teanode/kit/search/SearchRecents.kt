package com.teanode.kit.search

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer

/**
 * SearchRecent is something the person opened from the Search screen: a place of the app by its id, or a thing a
 * search found with the words that found it. The empty Search screen lists them, newest first, so going back to one
 * is a tap.
 */
@Serializable
data class SearchRecent(
    /** The place of the app, as the Search screen names its places ("finance-Imports"); null for something found. */
    val placeId: String? = null,
    /** What a search found; null for a place. */
    val result: SearchResult? = null,
    /** The words that found the result, which it opens with. */
    val words: String = "",
) {
    val identity: String get() = placeId?.let { id -> "place:$id" } ?: "found:${result?.identity.orEmpty()}"

    companion object {
        fun place(placeId: String) = SearchRecent(placeId = placeId)

        fun found(result: SearchResult, words: String) = SearchRecent(result = result, words = words)
    }
}

/** SearchRecents is the list of what was opened from the Search screen, kept as text on the phone for each account. */
object SearchRecents {
    /** How many the list keeps. */
    const val CAPACITY = 8

    /** The list with this one at its head: an earlier copy of it is dropped, and the list keeps [CAPACITY]. */
    fun adding(recent: SearchRecent, recents: List<SearchRecent>): List<SearchRecent> =
        (listOf(recent) + recents.filter { kept -> kept.identity != recent.identity }).take(CAPACITY)

    fun encode(recents: List<SearchRecent>): String = TeaNodeJson.encodeToString(ListSerializer(SearchRecent.serializer()), recents)

    /** The stored list, newest first; text that cannot be read is an empty list. */
    fun decode(text: String?): List<SearchRecent> {
        if (text.isNullOrEmpty()) return emptyList()
        return runCatching { TeaNodeJson.decodeFromString(ListSerializer(SearchRecent.serializer()), text) }.getOrDefault(emptyList()).take(CAPACITY)
    }

    /** The key a list is kept under, one for each account. */
    fun preferenceKey(accountKey: String): String = "searchRecents|$accountKey"
}
