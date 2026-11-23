package com.teanode.kit.location

import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.ZoneId

/** Pausing where the phone records: what the server's pause decodes to, what each call sends, what the phone does with a pause it made itself, and which readings it keeps while one is on. */
class LocationPauseTest {
    private fun pause(startedAt: String, endedAt: String?, id: String = "pause-1") = LocationPause(id, startedAt, endedAt)

    // The pause

    @Test
    fun aPauseIsInEffectFromItsStartUntilItsEnd() {
        val timed = pause("2024-01-02T14:00:00Z", "2024-01-02T15:00:00Z")
        assertFalse(timed.isInEffect(instantOf("2024-01-02T13:59:59Z")))
        assertTrue(timed.isInEffect(instantOf("2024-01-02T14:00:00Z")))
        assertTrue(timed.isInEffect(instantOf("2024-01-02T14:59:59Z")))
        assertFalse(timed.isInEffect(instantOf("2024-01-02T15:00:00Z")))
        assertFalse(timed.isOpenEnded)
        val open = pause("2024-01-02T14:00:00Z", null)
        assertTrue(open.isOpenEnded)
        assertTrue(open.isInEffect(instantOf("2030-01-01T00:00:00Z")))
        assertFalse(open.isInEffect(instantOf("2024-01-02T13:00:00Z")))
    }

    @Test
    fun aPauseDecodesTheTimesTheServerWrites() {
        val decoded = TeaNodeJson.decodeFromString(LocationPause.serializer(), """{"id":"pause-1","startedAt":"2024-01-02T14:00:00.123456789Z","endedAt":null}""")
        assertEquals("pause-1", decoded.id)
        assertEquals(instantOf("2024-01-02T14:00:00Z"), decoded.startedInstant)
        assertNull(decoded.endedInstant)
        assertTrue(decoded.isOpenEnded)
    }

    @Test
    fun aPersonLocationCarriesThePauseOnlyWhenTheServerSendsOne() {
        val paused = """
            {"userId":"user-2","username":"sam","name":null,"avatarUrl":null,"avatarColorIndex":1,"shareLevel":"current",
             "latestPoint":null,"openVisit":null,"pause":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":"2024-01-02T15:00:00Z"}}
            """
        val person = TeaNodeJson.decodeFromString(PersonLocation.serializer(), paused)
        assertNull(person.latestPoint)
        assertTrue(person.isPaused(instantOf("2024-01-02T14:30:00Z")))
        assertFalse(person.isPaused(instantOf("2024-01-02T15:30:00Z")))
        val pauseText = ""","pause":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":"2024-01-02T15:00:00Z"}"""
        val unpaused = TeaNodeJson.decodeFromString(PersonLocation.serializer(), paused.replace(pauseText, ""))
        assertNull(unpaused.pause)
        assertFalse(unpaused.isPaused(instantOf("2024-01-02T14:30:00Z")))
    }

    // What the choices mean

    @Test
    fun forAnHourEndsAnHourFromNow() {
        val now = instantOf("2024-01-02T09:30:00Z")
        assertEquals(instantOf("2024-01-02T10:30:00Z"), LocationPauseChoice.AnHour.endInstant(now))
    }

    @Test
    fun untilResumedHasNoEnd() {
        assertNull(LocationPauseChoice.UntilResumed.endInstant(instantOf("2024-01-02T09:30:00Z")))
    }

    @Test
    fun untilTomorrowMorningIsEightTheNextDayInTheZoneGiven() {
        val tokyo = ZoneId.of("Asia/Tokyo")
        // 23:30 on the 2nd in Tokyo.
        assertEquals(instantOf("2024-01-02T23:00:00Z"), LocationPauseChoice.UntilTomorrowMorning.endInstant(instantOf("2024-01-02T14:30:00Z"), tokyo))
    }

    @Test
    fun untilTomorrowMorningBeforeFiveIsThisMorning() {
        val tokyo = ZoneId.of("Asia/Tokyo")
        // 00:30 on the 2nd means 08:00 the same day.
        assertEquals(instantOf("2024-01-01T23:00:00Z"), LocationPauseChoice.UntilTomorrowMorning.endInstant(instantOf("2024-01-01T15:30:00Z"), tokyo))
        // 04:59 is still before the line, 05:00 is not.
        assertEquals(instantOf("2024-01-01T23:00:00Z"), LocationPauseChoice.UntilTomorrowMorning.endInstant(instantOf("2024-01-01T19:59:00Z"), tokyo))
        assertEquals(instantOf("2024-01-02T23:00:00Z"), LocationPauseChoice.UntilTomorrowMorning.endInstant(instantOf("2024-01-01T20:00:00Z"), tokyo))
    }

    // A pause made on the phone

    @Test
    fun pausingOnThePhoneAppliesAtOnceAndWaitsForTheServer() {
        val now = instantOf("2024-01-02T14:00:00Z")
        val tracker = LocationPauseTracker()
        assertFalse(tracker.isPaused(now))
        val paused = tracker.startingPause(instantOf("2024-01-02T15:00:00Z"), now)
        assertTrue(paused.hasPendingChange)
        assertTrue(paused.isPaused(now))
        assertTrue(paused.isPaused(instantOf("2024-01-02T14:59:00Z")))
        assertFalse(paused.isPaused(instantOf("2024-01-02T15:00:00Z")))
        assertEquals(instantOf("2024-01-02T15:00:00Z"), paused.endInstant(now))
        assertEquals(LocationPauseTracker.UNCONFIRMED_PAUSE_ID, paused.pause?.id)
    }

    @Test
    fun theServerTakingThePauseReplacesTheLocalOne() {
        val now = instantOf("2024-01-02T14:00:00Z")
        val started = LocationPauseTracker().startingPause(null, now)
        val change = started.pendingChange!!
        val answer = pause("2024-01-02T14:00:01Z", null)
        val taken = started.changeWasTaken(change, answer, now)
        assertFalse(taken.hasPendingChange)
        assertEquals(answer, taken.pause)
        assertTrue(taken.isPaused(instantOf("2024-01-02T20:00:00Z")))
    }

    @Test
    fun pausingAgainChangesTheEndOfThePauseInEffect() {
        val now = instantOf("2024-01-02T14:30:00Z")
        val tracker = LocationPauseTracker().learning(pause("2024-01-02T14:00:00Z", "2024-01-02T15:00:00Z"), now)
            .startingPause(instantOf("2024-01-02T18:00:00Z"), now)
        assertEquals("pause-1", tracker.pause?.id)
        assertEquals(instantOf("2024-01-02T14:00:00Z"), tracker.pause?.startedInstant)
        assertEquals(instantOf("2024-01-02T18:00:00Z"), tracker.pause?.endedInstant)
        assertTrue(tracker.isPaused(instantOf("2024-01-02T17:00:00Z")))
    }

    @Test
    fun resumingOnThePhoneEndsThePauseAtOnceAndWaitsForTheServer() {
        val now = instantOf("2024-01-02T14:30:00Z")
        var tracker = LocationPauseTracker().learning(pause("2024-01-02T14:00:00Z", null), now)
        assertTrue(tracker.isPaused(now))
        tracker = tracker.endingPause(now)
        assertEquals(LocationPauseChange.Resume(now), tracker.pendingChange)
        assertFalse(tracker.isPaused(now))
        // What was measured while it lasted is still inside it.
        assertTrue(tracker.isPaused(instantOf("2024-01-02T14:10:00Z")))
        tracker = tracker.changeWasTaken(tracker.pendingChange!!, null, now)
        assertFalse(tracker.hasPendingChange)
        assertNull(tracker.pause)
    }

    @Test
    fun resumingAPauseNotYetAnsweredForStillSendsTheResume() {
        val tracker = LocationPauseTracker().startingPause(null, instantOf("2024-01-02T14:00:00Z")).endingPause(instantOf("2024-01-02T14:05:00Z"))
        // The pause may already be on its way, so the resume goes after it.
        assertEquals(LocationPauseChange.Resume(instantOf("2024-01-02T14:05:00Z")), tracker.pendingChange)
        assertFalse(tracker.isPaused(instantOf("2024-01-02T14:06:00Z")))
    }

    @Test
    fun theAnswerToAPauseDoesNotUndoAResumeMadeBeforeIt() {
        var tracker = LocationPauseTracker().startingPause(null, instantOf("2024-01-02T14:00:00Z"))
        val sent = tracker.pendingChange!!
        // Resumed while the call was on its way, and the server began the pause a moment after the tap.
        tracker = tracker.endingPause(instantOf("2024-01-02T14:00:01Z"))
        tracker = tracker.changeWasTaken(sent, pause("2024-01-02T14:00:02Z", null), instantOf("2024-01-02T14:00:03Z"))
        assertTrue(tracker.hasPendingChange)
        assertFalse(tracker.isPaused(instantOf("2024-01-02T14:00:03Z")))
        // The resume goes next, and the server's answer to it leaves no pause.
        tracker = tracker.changeWasTaken(tracker.pendingChange!!, null, instantOf("2024-01-02T14:00:04Z"))
        assertFalse(tracker.hasPendingChange)
        assertNull(tracker.pause)
    }

    @Test
    fun aPauseNeverSentIsEndedByTheResumeTheServerAnswersWithNothing() {
        var tracker = LocationPauseTracker().startingPause(null, instantOf("2024-01-02T14:00:00Z")).endingPause(instantOf("2024-01-02T14:05:00Z"))
        tracker = tracker.changeWasTaken(tracker.pendingChange!!, null, instantOf("2024-01-02T14:05:01Z"))
        assertEquals(LocationPauseTracker(), tracker)
    }

    @Test
    fun resumingWhenNotPausedDoesNothing() {
        assertEquals(LocationPauseTracker(), LocationPauseTracker().endingPause(instantOf("2024-01-02T14:05:00Z")))
    }

    @Test
    fun aChangeWaitingForTheServerWinsOverWhatTheServerLastSaid() {
        val tracker = LocationPauseTracker().startingPause(null, instantOf("2024-01-02T14:00:00Z")).learning(null, instantOf("2024-01-02T14:00:05Z"))
        assertTrue(tracker.isPaused(instantOf("2024-01-02T14:00:05Z")))
        assertTrue(tracker.hasPendingChange)
    }

    @Test
    fun aPausePausedElsewhereIsLearnedAndALaterResumeToo() {
        var tracker = LocationPauseTracker().learning(pause("2024-01-02T14:00:00Z", "2024-01-02T16:00:00Z"), instantOf("2024-01-02T14:30:00Z"))
        assertTrue(tracker.isPaused(instantOf("2024-01-02T14:30:00Z")))
        tracker = tracker.learning(null, instantOf("2024-01-02T14:45:00Z"))
        assertFalse(tracker.isPaused(instantOf("2024-01-02T14:45:00Z")))
    }

    @Test
    fun aRefusedChangeIsDroppedAndTheServersPauseStands() {
        val now = instantOf("2024-01-02T14:30:00Z")
        var tracker = LocationPauseTracker().learning(pause("2024-01-02T14:00:00Z", "2024-01-02T15:00:00Z"), now).startingPause(instantOf("2024-01-02T18:00:00Z"), now)
        tracker = tracker.changeWasRefused(tracker.pendingChange!!)
        assertFalse(tracker.hasPendingChange)
        assertEquals(instantOf("2024-01-02T15:00:00Z"), tracker.pause?.endedInstant)
    }

    @Test
    fun aNewerChangeSurvivesTheAnswerToAnOlderOne() {
        var tracker = LocationPauseTracker().startingPause(instantOf("2024-01-02T15:00:00Z"), instantOf("2024-01-02T14:00:00Z"))
        val older = tracker.pendingChange!!
        tracker = tracker.startingPause(null, instantOf("2024-01-02T14:01:00Z"))
        tracker = tracker.changeWasTaken(older, pause("2024-01-02T14:00:00Z", "2024-01-02T15:00:00Z"), instantOf("2024-01-02T14:01:00Z"))
        assertTrue(tracker.hasPendingChange)
        assertNull(tracker.pause?.endedInstant)
    }

    @Test
    fun aTimedPauseThatEndedBeforeTheServerHeardIsDropped() {
        val tracker = LocationPauseTracker().startingPause(instantOf("2024-01-02T15:00:00Z"), instantOf("2024-01-02T14:00:00Z")).settled(instantOf("2024-01-02T15:00:00Z"))
        assertFalse(tracker.hasPendingChange)
        assertFalse(tracker.isPaused(instantOf("2024-01-02T15:00:00Z")))
    }

    @Test
    fun aResumeIsKeptWhenTheServerSaysThereIsNoPause() {
        val now = instantOf("2024-01-02T14:30:00Z")
        val tracker = LocationPauseTracker().learning(pause("2024-01-02T14:00:00Z", null), now).endingPause(now).learning(null, instantOf("2024-01-02T14:31:00Z"))
        // Sent anyway: the server does not need it, and it does no harm.
        assertTrue(tracker.hasPendingChange)
        assertFalse(tracker.isPaused(instantOf("2024-01-02T14:31:00Z")))
    }

    @Test
    fun anAnswerFromBeforeThePauseDoesNotUndoAResume() {
        var tracker = LocationPauseTracker().startingPause(null, instantOf("2024-01-02T14:00:00Z"))
        val sent = tracker.pendingChange!!
        tracker = tracker.endingPause(instantOf("2024-01-02T14:00:01Z"))
        // A report answer that left the server before the pause reached it.
        tracker = tracker.learning(null, instantOf("2024-01-02T14:00:02Z"))
        tracker = tracker.changeWasTaken(sent, pause("2024-01-02T14:00:03Z", null), instantOf("2024-01-02T14:00:04Z"))
        assertTrue(tracker.hasPendingChange)
        assertFalse(tracker.isPaused(instantOf("2024-01-02T14:00:04Z")))
    }

    // What is kept while paused

    @Test
    fun aPositionInsideThePauseIsNotRecordable() {
        val tracker = LocationPauseTracker().learning(pause("2024-01-02T14:00:00Z", "2024-01-02T15:00:00Z"), instantOf("2024-01-02T14:30:00Z"))
        assertTrue(tracker.isRecordable(pointMeasuredAt = instantOf("2024-01-02T13:59:00Z")))
        assertFalse(tracker.isRecordable(pointMeasuredAt = instantOf("2024-01-02T14:00:00Z")))
        assertFalse(tracker.isRecordable(pointMeasuredAt = instantOf("2024-01-02T14:59:00Z")))
        assertTrue(tracker.isRecordable(pointMeasuredAt = instantOf("2024-01-02T15:00:00Z")))
    }

    @Test
    fun aVisitIsJudgedByWhenItBeganOrByWhenItEndedIfTheBeginningWasNotSeen() {
        val tracker = LocationPauseTracker().learning(pause("2024-01-02T14:00:00Z", "2024-01-02T15:00:00Z"), instantOf("2024-01-02T14:30:00Z"))
        // Began before the pause, ended in it: sent whole, the server ends it at the pause's start.
        assertTrue(tracker.isRecordable(instantOf("2024-01-02T13:00:00Z"), instantOf("2024-01-02T14:20:00Z")))
        assertFalse(tracker.isRecordable(instantOf("2024-01-02T14:10:00Z"), null))
        assertFalse(tracker.isRecordable(null, instantOf("2024-01-02T14:10:00Z")))
        assertTrue(tracker.isRecordable(instantOf("2024-01-02T15:10:00Z"), null))
        assertTrue(tracker.isRecordable(null, null))
    }

    @Test
    fun aDepartureInsideThePauseIsToldAtThePausesStart() {
        val tracker = LocationPauseTracker().learning(pause("2024-01-02T14:00:00Z", "2024-01-02T15:00:00Z"), instantOf("2024-01-02T14:30:00Z"))
        assertEquals(instantOf("2024-01-02T14:00:00Z"), tracker.departureInstant(instantOf("2024-01-02T14:20:00Z")))
        assertEquals(instantOf("2024-01-02T13:20:00Z"), tracker.departureInstant(instantOf("2024-01-02T13:20:00Z")))
        assertEquals(instantOf("2024-01-02T15:20:00Z"), tracker.departureInstant(instantOf("2024-01-02T15:20:00Z")))
    }

    // Keeping it

    @Test
    fun theTrackerSurvivesBeingKeptAndReadBack() {
        val tracker = LocationPauseTracker().learning(pause("2024-01-02T14:00:00Z", null), instantOf("2024-01-02T14:30:00Z")).endingPause(instantOf("2024-01-02T14:30:00Z"))
        val read = TeaNodeJson.decodeFromString(LocationPauseTracker.serializer(), TeaNodeJson.encodeToString(LocationPauseTracker.serializer(), tracker))
        assertEquals(tracker, read)
        assertNotEquals(LocationPauseTracker.storageKey("a"), LocationPauseTracker.storageKey("b"))
    }

    // Sending it

    @Test
    fun retriesWaitLongerEachTimeUpToAQuarterOfAnHour() {
        assertEquals(15.0, LocationPauseRetry.delaySeconds(1))
        assertEquals(30.0, LocationPauseRetry.delaySeconds(2))
        assertEquals(60.0, LocationPauseRetry.delaySeconds(3))
        assertEquals(900.0, LocationPauseRetry.delaySeconds(40))
    }

    @Test
    fun onlyARefusalByTheServerIsFinal() {
        assertEquals(
            LocationPauseFailure.Refused("permission denied: location:use"),
            LocationPauseFailure.classify(TeaNodeError.Refused("permission denied: location:use")),
        )
        assertEquals(
            LocationPauseFailure.Refused("invalid arguments: the pause must end in the future"),
            LocationPauseFailure.classify(TeaNodeError.Refused("invalid arguments: the pause must end in the future")),
        )
        assertEquals(LocationPauseFailure.RetryLater, LocationPauseFailure.classify(TeaNodeError.Refused("something went wrong")))
        assertEquals(LocationPauseFailure.RetryLater, LocationPauseFailure.classify(java.io.IOException("offline")))
    }

    @Test
    fun pausesUntilTheEndGiven() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"PauseLocation":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":"2024-01-02T15:00:00Z"}}}""")
        val made = connection.pauseLocation(instantOf("2024-01-02T15:00:00Z"))
        assertEquals("pause-1", made.id)
        assertEquals(instantOf("2024-01-02T15:00:00Z"), made.endedInstant)
        assertEquals(mapOf("endedAt" to JsonPrimitive("2024-01-02T15:00:00Z")), sentVariables(fake))
    }

    @Test
    fun pausesUntilResumedSendsNoEnd() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"PauseLocation":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":null}}}""")
        assertTrue(connection.pauseLocation().isOpenEnded)
        assertEquals(emptyMap<String, Any>(), sentVariables(fake))
    }

    @Test
    fun resumes() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ResumeLocation":true}}""")
        assertTrue(connection.resumeLocation())
        assertEquals(emptyMap<String, Any>(), sentVariables(fake))
        val (otherConnection, _) = connectionAnswering("""{"data":{"ResumeLocation":false}}""")
        assertFalse(otherConnection.resumeLocation())
    }

    @Test
    fun readsThePausesOfARange() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """{"data":{"ListLocationPauses":[{"id":"pause-2","startedAt":"2024-01-02T14:00:00Z","endedAt":"2024-01-02T15:00:00Z"},{"id":"pause-1","startedAt":"2024-01-01T14:00:00Z","endedAt":null}]}}""",
        )
        val pauses = connection.locationPauses("user-2", instantOf("2024-01-01T00:00:00Z"), instantOf("2024-01-03T00:00:00Z"))
        assertEquals(listOf("pause-2", "pause-1"), pauses.map { each -> each.id })
        assertEquals(
            mapOf("userId" to JsonPrimitive("user-2"), "from" to JsonPrimitive("2024-01-01T00:00:00Z"), "until" to JsonPrimitive("2024-01-03T00:00:00Z")),
            sentVariables(fake),
        )
    }

    @Test
    fun aReportAnswerCarriesThePauseAndWhatItDropped() = runBlocking {
        val (connection, _) = connectionAnswering(
            """{"data":{"ReportLocation":{"newPointCount":1,"newVisitCount":0,"updatedVisitCount":0,"pausedPointCount":4,"pausedVisitCount":1,"pause":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":null}}}}""",
        )
        val result = connection.reportLocation(LocationBatch(emptyList(), emptyList()))
        assertEquals(4, result.pausedPointCount)
        assertEquals(1, result.pausedVisitCount)
        assertEquals("pause-1", result.pause?.id)
    }

    @Test
    fun aReportAnswerFromAServerThatCannotPauseDecodes() {
        val result = TeaNodeJson.decodeFromString(ReportLocationResult.serializer(), """{"newPointCount":1,"newVisitCount":1,"updatedVisitCount":0}""")
        assertNull(result.pause)
        assertEquals(0, result.pausedPointCount)
        assertEquals(0, result.pausedVisitCount)
    }

    @Test
    fun theLocationDocumentsAskForThePause() {
        assertTrue(LocationDocuments.LOCATION.contains("pause { id startedAt endedAt }"))
        assertTrue(LocationDocuments.SHARED_LOCATIONS.contains("pause { id startedAt endedAt }"))
        assertTrue(LocationDocuments.REPORT_LOCATION.contains("pausedPointCount"))
    }

    // The timeline

    private fun visit(id: String, arrivedAt: String, departedAt: String?): PlaceVisit {
        val place = LocationPlace(id = "place-1", placeName = "Fixture Park", latitudeDegrees = 10.5, longitudeDegrees = 20.5)
        return PlaceVisit(id = id, place = place, arrivedAt = arrivedAt, departedAt = departedAt, latitudeDegrees = 10.5, longitudeDegrees = 20.5)
    }

    @Test
    fun aPauseIsListedAmongTheVisitsOfTheDayItBegan() {
        val visits = listOf(
            visit("visit-1", "2024-01-02T09:00:00Z", "2024-01-02T10:00:00Z"),
            visit("visit-2", "2024-01-02T16:00:00Z", null),
            visit("visit-3", "2024-01-01T09:00:00Z", "2024-01-01T10:00:00Z"),
        )
        val pauses = listOf(pause("2024-01-02T12:00:00Z", "2024-01-02T13:00:00Z", "pause-1"), pause("2024-01-03T07:00:00Z", "2024-01-03T08:00:00Z", "pause-2"))
        val days = LocationDays.group(visits, pauses, zone = ZoneId.of("UTC"))
        assertEquals(3, days.size)
        assertEquals(listOf("pause:pause-2"), days[0].entries.map { entry -> entry.id })
        assertEquals(listOf("visit:visit-2", "pause:pause-1", "visit:visit-1"), days[1].entries.map { entry -> entry.id })
        assertEquals(listOf("visit-2", "visit-1"), days[1].visits.map { each -> each.id })
        assertEquals(listOf("visit:visit-3"), days[2].entries.map { entry -> entry.id })
    }

    @Test
    fun groupingWithNoPausesIsWhatItWas() {
        val days = LocationDays.group(listOf(visit("visit-1", "2024-01-02T09:00:00Z", null)), zone = ZoneId.of("UTC"))
        assertEquals(1, days.size)
        assertEquals(listOf("visit-1"), days[0].visits.map { each -> each.id })
        assertEquals(listOf("visit:visit-1"), days[0].entries.map { entry -> entry.id })
    }

    @Test
    fun oneStretchAsATimelineIsNewestFirst() {
        val entries = LocationDays.entries(
            visits = listOf(visit("visit-1", "2024-01-02T09:00:00Z", null)),
            pauses = listOf(pause("2024-01-02T12:00:00Z", null, "pause-1"), pause("2024-01-02T08:00:00Z", "2024-01-02T08:30:00Z", "pause-0")),
        )
        assertEquals(listOf("pause:pause-1", "visit:visit-1", "pause:pause-0"), entries.map { entry -> entry.id })
    }
}
