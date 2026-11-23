package com.teanode.kit.charts

import java.math.BigDecimal
import java.time.Instant
import kotlin.math.abs

/** ChartShares is what the app's charts need that is not drawing: how many groups get a slice and a color of their own, which slice a touch on a donut lands in, and which point of a line is nearest a touch. */
object ChartShares {
    /** How many groups, largest first, get a slice and a color of their own. The rest share one slice, so the colors stay few enough to tell apart. */
    const val OWN_SLICE_LIMIT = 8

    /** The slice a group is drawn in, by its rank among the groups, largest first: its own, or the one the rest share, numbered [OWN_SLICE_LIMIT]. */
    fun sliceIndex(ofRank: Int): Int = minOf(ofRank, OWN_SLICE_LIMIT)

    /** The slices' amounts: each of the first [OWN_SLICE_LIMIT] on its own, then the rest added up as one, when there is a rest. */
    fun sliceAmounts(amounts: List<BigDecimal>): List<BigDecimal> {
        if (amounts.size <= OWN_SLICE_LIMIT) return amounts
        val restAmount = amounts.drop(OWN_SLICE_LIMIT).fold(BigDecimal.ZERO) { sum, amount -> sum + amount }
        return amounts.take(OWN_SLICE_LIMIT) + restAmount
    }

    /** The slice a donut's angle selection falls in: the first slice whose running total reaches the selection. */
    fun sliceIndex(atRunningTotal: Double, amounts: List<Double>): Int? {
        if (amounts.isEmpty()) return null
        var reachedAmount = 0.0
        for ((index, amount) in amounts.withIndex()) {
            reachedAmount += amount
            if (atRunningTotal <= reachedAmount) return index
        }
        return amounts.size - 1
    }

    /** The point nearest the instant a finger is on, for a line chart: a touch between two points picks whichever is closer. */
    fun nearestIndex(to: Instant, inInstants: List<Instant>): Int? =
        inInstants.indices.minByOrNull { index -> abs(inInstants[index].toEpochMilli() - to.toEpochMilli()) }
}
