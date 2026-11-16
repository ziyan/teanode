package com.teanode.kit.location

/**
 * PlaceName is one place of the app a search can jump to, as the search box matches it: its own name, and the names of
 * the places it is inside, each in the reader's language and in English, so the words find it whatever the language. A
 * place inside none is a part of the app.
 */
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
 * PlaceSearch picks the places whose names the typed words are, the way the dashboard's search box offers its pages and
 * their tabs: first the parts of the app, then the places inside them, each best match first and in the app's own order
 * within a match.
 */
object PlaceSearch {
    /** How many places the search offers at most. */
    const val PLACE_LIMIT = 8

    /**
     * How well the words match a name: 0 when the name starts with them, 1 when a word of the name does, and null
     * otherwise. Letters inside a word are no match. Both are lower case.
     */
    internal fun nameRank(name: String, words: String): Int? {
        if (name.startsWith(words)) return 0
        val nameWords = name.split(Regex("""[\s\-_/]""")).filter { word -> word.isNotEmpty() }
        if (nameWords.any { word -> word.startsWith(words) }) return 1
        return null
    }

    /**
     * How well the words match a place. Its own name matches as a part's does; so do the start of a name it is inside
     * and the start of its own, said together ("set pass", "manage dom"). The words are cut at each space to find the
     * two halves.
     */
    internal fun placeRank(place: PlaceName, words: String): Int? {
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
        val ranked = places.withIndex().mapNotNull { indexed ->
            placeRank(indexed.value, needle)?.let { rank -> Ranked(indexed.value, rank, indexed.index) }
        }
        val sorted = ranked.sortedWith(
            Comparator { left, right ->
                when {
                    left.place.isPart != right.place.isPart -> if (left.place.isPart) -1 else 1
                    left.rank != right.rank -> left.rank.compareTo(right.rank)
                    else -> left.order.compareTo(right.order)
                }
            },
        )
        return sorted.take(PLACE_LIMIT).map { entry -> entry.place.placeId }
    }
}
