package com.teanode.kit.location

import kotlin.math.hypot

/** One person's picture on the map, where it is on the screen in points (density-independent pixels). */
data class LocationMapPinPoint(val id: String, val xPoints: Double, val yPoints: Double)

/**
 * LocationMapPinGrouping decides which pictures of people are drawn as one, as the iPhone app's `LocationMapPinGroup`
 * does: pictures that would cover each other at the map's zoom become a group, so nobody is hidden under somebody
 * else. The chosen person leads their group, and their group comes last so it is drawn over the others.
 */
object LocationMapPinGrouping {
    /** How close two pictures' middles may come, in points, before one covers the other: a picture and its ring. */
    const val OVERLAP_POINTS = 42.0

    /** The most pictures drawn in a group; the rest are a count beside them. */
    const val PICTURE_COUNT_MOST = 3

    /** The ids in each group, the chosen person's group last; with no positions known, every picture is a group of its own. */
    fun groups(pins: List<LocationMapPinPoint>, chosenId: String, isPlaced: Boolean = true): List<List<String>> {
        val ordered = pins.filter { pin -> pin.id == chosenId } + pins.filter { pin -> pin.id != chosenId }
        val groups = mutableListOf<MutableList<LocationMapPinPoint>>()
        if (isPlaced) {
            // A picture joins the first group whose first picture it would cover, as the iPhone app does.
            for (pin in ordered) {
                val overlapping = groups.firstOrNull { group -> hypot(group[0].xPoints - pin.xPoints, group[0].yPoints - pin.yPoints) < OVERLAP_POINTS }
                if (overlapping != null) overlapping.add(pin) else groups.add(mutableListOf(pin))
            }
        } else {
            ordered.forEach { pin -> groups.add(mutableListOf(pin)) }
        }
        val made = groups.map { group -> group.map { pin -> pin.id } }
        return made.filter { group -> !group.contains(chosenId) } + made.filter { group -> group.contains(chosenId) }
    }
}
