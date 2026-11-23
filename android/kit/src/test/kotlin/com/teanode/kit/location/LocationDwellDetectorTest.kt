package com.teanode.kit.location

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** A stay is noticed from the positions kept: long enough inside one place, and over when the person moves away. */
class LocationDwellDetectorTest {
    private val detector = LocationDwellDetector()
    private val start = instantOf("2024-01-02T09:00:00Z")

    private fun feed(vararg steps: Triple<Double, Long, Double>): Pair<LocationDwellDetector.Stay?, List<LocationDwellDetector.Event>> {
        var stay: LocationDwellDetector.Stay? = null
        val events = mutableListOf<LocationDwellDetector.Event>()
        for ((latitude, minutes, accuracy) in steps) {
            val (next, happened) = detector.consume(stay, latitude, 20.5, accuracy, start.plusSeconds(minutes * 60))
            stay = next
            events += happened
        }
        return stay to events
    }

    @Test
    fun aStayOfEightMinutesIsReportedAsAnArrivalAtItsBeginning() {
        val (stay, events) = feed(Triple(10.5, 0, 10.0), Triple(10.5001, 4, 10.0), Triple(10.5002, 9, 10.0))
        assertEquals(listOf(LocationDwellDetector.Event.Arrived(10.5, 20.5, start)), events)
        assertTrue(stay!!.isArrivalReported)
    }

    @Test
    fun aShortStopIsNotAVisit() {
        val (_, events) = feed(Triple(10.5, 0, 10.0), Triple(10.5001, 5, 10.0), Triple(10.52, 6, 10.0))
        assertTrue(events.isEmpty())
    }

    @Test
    fun leavingAfterAnArrivalIsADepartureDatedToTheLastPositionInside() {
        val (stay, events) = feed(Triple(10.5, 0, 10.0), Triple(10.5001, 10, 10.0), Triple(10.5002, 30, 10.0), Triple(10.52, 40, 10.0))
        assertEquals(2, events.size)
        assertEquals(LocationDwellDetector.Event.Departed(10.5, 20.5, start, start.plusSeconds(30 * 60)), events[1])
        // The new stay begins where the person went.
        assertEquals(10.52, stay!!.latitudeDegrees)
        assertEquals(false, stay.isArrivalReported)
    }

    @Test
    fun standingAtTheEdgeIsNeitherLeavingNorArriving() {
        // About 130 meters away: beyond the stay radius, inside the leave radius.
        val (stay, events) = feed(Triple(10.5, 0, 10.0), Triple(10.5, 9, 10.0), Triple(10.5012, 12, 10.0), Triple(10.5, 20, 10.0))
        assertEquals(1, events.size)
        assertEquals(start.plusSeconds(20 * 60), stay!!.lastInside)
    }

    @Test
    fun aReadingTooRoughToPlaceThePersonIsIgnored() {
        val (stay, events) = feed(Triple(10.5, 0, 10.0), Triple(10.6, 10, 500.0))
        assertTrue(events.isEmpty())
        assertEquals(10.5, stay!!.latitudeDegrees)
        assertNull(detector.consume(null, 200.0, 20.5, 5.0, start).first)
    }

    @Test
    fun theVisitsTheOutboxIsToldShareOneKeySoTheDepartureFindsItsArrival() {
        val arrival = detector.visitOf(LocationDwellDetector.Event.Arrived(10.5, 20.5, start))
        val departure = detector.visitOf(LocationDwellDetector.Event.Departed(10.5, 20.5, start, start.plusSeconds(1800)))
        assertEquals(arrival.clientVisitKey, departure.clientVisitKey)
        assertEquals("visit", arrival.visitSource)
        val outbox = LocationOutbox("account")
        outbox.addVisit(arrival, start)
        outbox.addVisit(departure, start)
        assertEquals(2, outbox.visits.size)
    }
}
