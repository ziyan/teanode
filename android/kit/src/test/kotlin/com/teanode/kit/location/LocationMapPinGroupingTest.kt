package com.teanode.kit.location

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class LocationMapPinGroupingTest {
    private fun pin(id: String, x: Double, y: Double) = LocationMapPinPoint(id, x, y)

    @Test
    fun `pictures far apart are each a group`() {
        val groups = LocationMapPinGrouping.groups(listOf(pin("a", 0.0, 0.0), pin("b", 100.0, 0.0)), chosenId = "")
        assertEquals(listOf(listOf("a"), listOf("b")), groups)
    }

    @Test
    fun `pictures that would cover each other are one group`() {
        val groups = LocationMapPinGrouping.groups(listOf(pin("a", 0.0, 0.0), pin("b", 30.0, 0.0), pin("c", 200.0, 0.0)), chosenId = "")
        assertEquals(listOf(listOf("a", "b"), listOf("c")), groups)
    }

    @Test
    fun `exactly the overlap distance apart does not group`() {
        val groups = LocationMapPinGrouping.groups(listOf(pin("a", 0.0, 0.0), pin("b", LocationMapPinGrouping.OVERLAP_POINTS, 0.0)), chosenId = "")
        assertEquals(2, groups.size)
    }

    @Test
    fun `the chosen person leads their group and the group comes last`() {
        val groups = LocationMapPinGrouping.groups(listOf(pin("a", 0.0, 0.0), pin("b", 10.0, 0.0), pin("c", 300.0, 0.0)), chosenId = "b")
        assertEquals(listOf(listOf("c"), listOf("b", "a")), groups)
    }

    @Test
    fun `a group is judged by its first picture, not by the others`() {
        // c is near b but not near a, the group's first: it starts a group of its own.
        val groups = LocationMapPinGrouping.groups(listOf(pin("a", 0.0, 0.0), pin("b", 30.0, 0.0), pin("c", 60.0, 0.0)), chosenId = "")
        assertEquals(listOf(listOf("a", "b"), listOf("c")), groups)
    }

    @Test
    fun `before the map has said where things are every picture is alone`() {
        val groups = LocationMapPinGrouping.groups(listOf(pin("a", 0.0, 0.0), pin("b", 1.0, 0.0)), chosenId = "", isPlaced = false)
        assertEquals(listOf(listOf("a"), listOf("b")), groups)
    }
}
