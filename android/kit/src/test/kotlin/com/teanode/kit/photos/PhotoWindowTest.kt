package com.teanode.kit.photos

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertSame
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class PhotoWindowTest {
    private fun photo(id: String) = Photo(id = id, createdAt = "2026-02-01T00:00:00Z", photoKind = "picture")

    /** Pages of ten: page 0 holds p0..p9, page 1 holds p10..p19. */
    private fun page(index: Int, pageSize: Int = 10, count: Int = pageSize) = (0 until count).map { offset -> photo("p${index * pageSize + offset}") }

    private fun window(totalCount: Int, vararg held: Int) =
        PhotoWindow(totalCount, 10).let { empty -> held.fold(empty) { current, index -> current.withPage(index, page(index)) } }

    @Test
    fun aPlaceIsAPhotoOnlyWhileItsPageIsHeld() {
        val held = window(35, 1)
        assertNull(held.photo(5))
        assertEquals("p12", held.photo(12)?.id)
        assertNull(held.photo(35))
        assertNull(held.photo(-1))
        assertEquals(1, held.pageIndex(10))
        assertEquals(0, held.pageIndex(9))
        assertEquals(4, held.pageCount)
    }

    @Test
    fun theLastPageMayBeShort() {
        val held = window(25).withPage(2, page(2, count = 5))
        assertEquals("p24", held.photo(24)?.id)
        assertNull(held.photo(25))
    }

    @Test
    fun missingPagesAreTheOnesNearAPlaceNearestFirst() {
        val held = window(100, 4)
        assertEquals(listOf(5, 3, 6, 2), held.missingPages(around = 45))
        assertEquals(listOf(0, 1, 2), window(100).missingPages(around = 0))
        assertEquals(emptyList<Int>(), PhotoWindow(0, 10).missingPages(around = 0))
    }

    @Test
    fun aPlacePastTheEndIsReadAsTheLastOne() {
        assertEquals(listOf(2, 1, 0), window(25).missingPages(around = 500))
    }

    @Test
    fun pagesFarFromAPlaceAreReleased() {
        val held = window(200, 0, 1, 5, 6, 12)
        assertEquals(listOf(0, 1, 5, 6), held.pagesToRelease(around = 120, keep = 4))
        assertEquals(listOf(0, 12), held.pagesToRelease(around = 50, keep = 4))
        assertEquals(emptyList<Int>(), held.pagesToRelease(around = 55, keep = 20))
        assertEquals(setOf(5, 6, 12), held.releasing(listOf(0, 1)).pages.keys)
    }

    @Test
    fun aChangedPhotoReplacesItselfWhereItIsHeld() {
        val held = window(30, 0)
        val changed = photo("p3").copy(isFavorite = true)
        assertTrue(held.replace(changed).photo(3)!!.isFavorite)
        assertFalse(held.replace(changed).photo(4)!!.isFavorite)
        assertSame(held, held.replace(photo("elsewhere")))
    }

    @Test
    fun removingPhotosLowersTheCountAndMarksTheFollowingPagesStale() {
        val held = window(30, 0, 1, 2).remove(listOf("p12", "p13"))
        assertEquals(28, held.totalCount)
        assertEquals(setOf(1, 2), held.stalePageIndexes)
        assertEquals(8, held.pages.getValue(1).size)
        assertEquals(listOf(1, 2), held.missingPages(around = 15, reach = 2))
    }

    @Test
    fun readingAStalePageAgainClearsIt() {
        val held = window(30, 0, 1, 2).remove(listOf("p12")).withPage(1, page(1))
        assertEquals(setOf(2), held.stalePageIndexes)
    }

    @Test
    fun aSmallerCountLetsGoPagesThatHoldNoPlace() {
        val held = window(30, 0, 1, 2).withTotalCount(15)
        assertEquals(setOf(0, 1), held.pages.keys)
        assertEquals(2, held.pageCount)
    }

    @Test
    fun aPhotoIsFoundAmongTheHeldPages() {
        val held = window(100, 3, 4)
        assertEquals(37, held.index("p37"))
        assertNull(held.index("p5"))
        assertEquals(20, held.heldPhotos.size)
        assertEquals("p30", held.heldPhotos.first().id)
    }
}
