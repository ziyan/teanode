package com.teanode.kit.charts

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import java.math.BigDecimal
import java.time.Instant

class ChartSharesTest {
    private fun amounts(vararg values: Int) = values.map { value -> BigDecimal(value) }

    @Test
    fun groupsBeyondTheLimitShareOneSlice() {
        val amounts = amounts(900, 800, 700, 600, 500, 400, 300, 200, 100, 50, 25)
        val sliceAmounts = ChartShares.sliceAmounts(amounts)
        assertEquals(ChartShares.OWN_SLICE_LIMIT + 1, sliceAmounts.size)
        assertEquals(amounts.take(ChartShares.OWN_SLICE_LIMIT), sliceAmounts.take(ChartShares.OWN_SLICE_LIMIT))
        assertEquals(BigDecimal(175), sliceAmounts.last())
        assertEquals(0, ChartShares.sliceIndex(0))
        assertEquals(7, ChartShares.sliceIndex(7))
        assertEquals(8, ChartShares.sliceIndex(8))
        assertEquals(8, ChartShares.sliceIndex(10))
    }

    @Test
    fun fewGroupsKeepTheirOwnSlices() {
        val few = amounts(300, 200, 100)
        assertEquals(few, ChartShares.sliceAmounts(few))
        val exactlyAtTheLimit = List(ChartShares.OWN_SLICE_LIMIT) { BigDecimal(10) }
        assertEquals(exactlyAtTheLimit, ChartShares.sliceAmounts(exactlyAtTheLimit))
    }

    @Test
    fun anAngleFindsItsSlice() {
        val amounts = listOf(50.0, 30.0, 20.0)
        assertEquals(0, ChartShares.sliceIndex(0.0, amounts))
        assertEquals(0, ChartShares.sliceIndex(49.9, amounts))
        assertEquals(1, ChartShares.sliceIndex(50.1, amounts))
        assertEquals(2, ChartShares.sliceIndex(99.0, amounts))
        // A touch reported past the end, from rounding, is still the last slice.
        assertEquals(2, ChartShares.sliceIndex(100.5, amounts))
        assertNull(ChartShares.sliceIndex(10.0, emptyList()))
    }

    @Test
    fun aTouchPicksTheNearestPoint() {
        val start = Instant.ofEpochSecond(1_800_000_000)
        val instants = listOf(0L, 10L, 30L).map { day -> start.plusSeconds(day * 86_400) }
        assertEquals(0, ChartShares.nearestIndex(start.minusSeconds(86_400), instants))
        assertEquals(0, ChartShares.nearestIndex(start.plusSeconds(4 * 86_400), instants))
        assertEquals(1, ChartShares.nearestIndex(start.plusSeconds(6 * 86_400), instants))
        assertEquals(2, ChartShares.nearestIndex(start.plusSeconds(25 * 86_400), instants))
        assertNull(ChartShares.nearestIndex(start, emptyList()))
    }
}
