package com.teanode.kit.photos

/**
 * PhotoWindow is the part of a photo list an app holds: how many photos the list has, and the pages of them read so
 * far, by page number. The list is laid out over 0 until [totalCount]; a place whose page is not held is drawn as a
 * placeholder of the same size. Pages far from the screen are let go ([pagesToRelease]) and read again when the person
 * scrolls back ([missingPages]).
 *
 * [stalePageIndexes] are held pages that a removal made out of date: they are still shown, and read again.
 */
data class PhotoWindow(
    val totalCount: Int = 0,
    val pageSize: Int = PAGE_SIZE,
    val pages: Map<Int, List<Photo>> = emptyMap(),
    val stalePageIndexes: Set<Int> = emptySet(),
) {
    val pageCount: Int get() = if (pageSize > 0) (totalCount + pageSize - 1) / pageSize else 0

    /** The photo at a place in the list, or null while its page is not held (or the place is past the end of it). */
    fun photo(at: Int): Photo? {
        if (at < 0 || at >= totalCount) return null
        return pages[pageIndex(at)]?.getOrNull(at - pageIndex(at) * pageSize)
    }

    fun pageIndex(of: Int): Int = if (pageSize > 0) maxOf(0, of) / pageSize else 0

    /** The pages near a place that are not held, or are held out of date, nearest first. */
    fun missingPages(around: Int, reach: Int = MISSING_REACH): List<Int> {
        if (totalCount <= 0) return emptyList()
        val center = pageIndex(minOf(maxOf(0, around), totalCount - 1))
        val missing = mutableListOf<Int>()
        for (distance in 0..reach) {
            for (index in listOf(center + distance, center - distance).distinct()) {
                if (index !in 0 until pageCount) continue
                if (index !in pages || index in stalePageIndexes) missing.add(index)
            }
        }
        return missing
    }

    /** The held pages that are further than [keep] pages from a place. */
    fun pagesToRelease(around: Int, keep: Int = KEEP_REACH): List<Int> {
        val center = pageIndex(maxOf(0, around))
        return pages.keys.filter { index -> kotlin.math.abs(index - center) > keep }.sorted()
    }

    fun withPage(pageIndex: Int, photos: List<Photo>): PhotoWindow =
        copy(pages = pages + (pageIndex to photos), stalePageIndexes = stalePageIndexes - pageIndex)

    fun releasing(pageIndexes: Collection<Int>): PhotoWindow {
        if (pageIndexes.isEmpty()) return this
        return copy(pages = pages - pageIndexes.toSet(), stalePageIndexes = stalePageIndexes - pageIndexes.toSet())
    }

    /** A new count: pages that no longer hold a place are let go. */
    fun withTotalCount(count: Int): PhotoWindow {
        val total = maxOf(0, count)
        val kept = copy(totalCount = total)
        return kept.releasing(pages.keys.filter { index -> index >= kept.pageCount })
    }

    /** A changed photo (a heart, a description) takes its place where it is held. */
    fun replace(photo: Photo): PhotoWindow {
        var changed = false
        val replaced = pages.mapValues { (_, page) ->
            page.map { held -> if (held.id == photo.id) { changed = true; photo } else held }
        }
        return if (changed) copy(pages = replaced) else this
    }

    /**
     * Photos taken out of the list: the count goes down, they leave the pages that held them, and every page from the
     * first of those on is read again, because what followed them moved up.
     */
    fun remove(photoIds: Collection<String>): PhotoWindow {
        if (photoIds.isEmpty()) return this
        val gone = photoIds.toSet()
        val firstAffected = pages.filter { (_, page) -> page.any { photo -> photo.id in gone } }.keys.minOrNull() ?: 0
        val trimmed = pages.mapValues { (_, page) -> page.filter { photo -> photo.id !in gone } }
        val stale = trimmed.keys.filter { index -> index >= firstAffected }.toSet()
        return copy(pages = trimmed, stalePageIndexes = stalePageIndexes + stale).withTotalCount(totalCount - gone.size)
    }

    /** The place of a photo among the held pages, or null when it is on none of them. */
    fun index(of: String): Int? {
        for ((pageIndex, page) in pages.toSortedMap()) {
            val offset = page.indexOfFirst { photo -> photo.id == of }
            if (offset >= 0) return pageIndex * pageSize + offset
        }
        return null
    }

    /** The photos of the held pages, in the order of the list. */
    val heldPhotos: List<Photo> get() = pages.toSortedMap().values.flatten()

    /** Every page is read again; what is held stays on screen until the new page comes. */
    fun markingAllStale(): PhotoWindow = copy(stalePageIndexes = pages.keys.toSet())

    companion object {
        const val PAGE_SIZE = 200

        /** How many pages each side of the one on screen are read ahead. */
        const val MISSING_REACH = 2

        /** How many pages each side of the one on screen are kept. */
        const val KEEP_REACH = 4
    }
}
