package com.teanode.kit.location

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import java.time.Instant

class LocationMotionTest {
    private val now = Instant.parse("2026-03-04T10:00:00.700Z")

    @Test
    fun `each android activity has the server's word`() {
        assertEquals("automotive", LocationMotion.activityKind(LocationMotion.IN_VEHICLE))
        assertEquals("cycling", LocationMotion.activityKind(LocationMotion.ON_BICYCLE))
        assertEquals("walking", LocationMotion.activityKind(LocationMotion.ON_FOOT))
        assertEquals("walking", LocationMotion.activityKind(LocationMotion.WALKING))
        assertEquals("running", LocationMotion.activityKind(LocationMotion.RUNNING))
        assertEquals("stationary", LocationMotion.activityKind(LocationMotion.STILL))
    }

    @Test
    fun `tilting and unknown have no word`() {
        assertNull(LocationMotion.activityKind(5))
        assertNull(LocationMotion.activityKind(4))
    }

    @Test
    fun `every word is one the outbox accepts`() {
        for (type in listOf(0, 1, 2, 3, 7, 8)) {
            val activity = LocationMotion.reportedActivity(type, isEntering = true, at = now)!!
            assertEquals(true, LocationOutbox(accountKey = "key").isAcceptable(activity, now))
        }
    }

    @Test
    fun `entering starts a stretch dated to the whole second`() {
        val activity = LocationMotion.reportedActivity(LocationMotion.IN_VEHICLE, isEntering = true, at = now)!!
        assertEquals(Instant.parse("2026-03-04T10:00:00Z"), activity.startedAt)
        assertEquals("automotive", activity.activityKind)
        assertEquals(LocationMotion.TRANSITION_CONFIDENCE, activity.activityConfidence)
    }

    @Test
    fun `leaving an activity starts nothing`() {
        assertNull(LocationMotion.reportedActivity(LocationMotion.WALKING, isEntering = false, at = now))
    }

    @Test
    fun `a late delivery is dated to when it happened`() {
        val happened = LocationMotion.transitionInstant(now, nowElapsedNanos = 5_000_000_000L, eventElapsedNanos = 3_000_000_000L)
        assertEquals(now.minusSeconds(2), happened)
    }

    @Test
    fun `a transition stamped in the future counts as now`() {
        assertEquals(now, LocationMotion.transitionInstant(now, nowElapsedNanos = 1L, eventElapsedNanos = 9L))
    }

    @Test
    fun `queued stretches keep one for each start`() {
        val outbox = LocationOutbox(accountKey = "key")
        val first = LocationMotion.reportedActivity(LocationMotion.WALKING, true, now)!!
        outbox.addActivities(listOf(first, first), now)
        assertEquals(1, outbox.activities.size)
    }
}
