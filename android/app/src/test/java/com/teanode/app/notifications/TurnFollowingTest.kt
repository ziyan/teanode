package com.teanode.app.notifications

import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.CoroutineScope
import org.junit.Assert.assertEquals
import org.junit.Test

class TurnFollowingTest {
    private val shown = mutableListOf<String>()
    private var endCount = 0
    private val followed = mutableListOf<String>()
    private var canFollow = true

    private fun following() = TurnFollowing(
        scope = CoroutineScope(UnconfinedTestDispatcher()),
        followRun = { runId -> followed.add(runId); canFollow },
        show = { activityState -> shown.add(activityState) },
        end = { endCount += 1 },
    )

    @Test
    fun nothingIsShownWhileTheAppIsInFront() {
        val turn = following()
        turn.turnChanged("run-1", "working", isAppInFront = true)
        turn.turnChanged("run-1", "waiting", isAppInFront = true)
        assertEquals(emptyList<String>(), shown)
        assertEquals(emptyList<String>(), followed)
        assertEquals(2, endCount)
    }

    @Test
    fun leavingWithATurnRunningShowsItAndAsksTheServerToFollowIt() {
        val turn = following()
        turn.turnChanged("run-1", "waiting", isAppInFront = true)
        turn.appLeft()
        assertEquals(listOf("waiting"), shown)
        assertEquals(listOf("run-1"), followed)
    }

    @Test
    fun leavingWithNoTurnRunningShowsNothing() {
        val turn = following()
        turn.appLeft()
        turn.turnChanged("run-1", "done", isAppInFront = true)
        turn.appLeft()
        assertEquals(emptyList<String>(), shown)
        assertEquals(emptyList<String>(), followed)
    }

    @Test
    fun aRunIsFollowedOnceHoweverManyChangesItHas() {
        val turn = following()
        turn.turnChanged("run-1", "working", isAppInFront = false)
        turn.turnChanged("run-1", "waiting", isAppInFront = false)
        turn.turnChanged("run-1", "working", isAppInFront = false)
        turn.turnChanged("run-1", "done", isAppInFront = false)
        assertEquals(listOf("working", "waiting", "working", "done"), shown)
        assertEquals(listOf("run-1"), followed)
    }

    @Test
    fun aNewRunIsFollowedAgainAndComingBackForgetsTheOldOne() {
        val turn = following()
        turn.turnChanged("run-1", "working", isAppInFront = false)
        turn.appCame()
        turn.turnChanged("run-1", "working", isAppInFront = false)
        turn.turnChanged("run-2", "working", isAppInFront = false)
        assertEquals(listOf("run-1", "run-1", "run-2"), followed)
    }

    @Test
    fun aTurnThatCannotBeFollowedTakesItsNotificationAway() {
        canFollow = false
        val turn = following()
        turn.turnChanged("run-1", "working", isAppInFront = false)
        assertEquals(1, endCount)
        // Asked again at its next change, since the failure may have been for now only.
        turn.turnChanged("run-1", "waiting", isAppInFront = false)
        assertEquals(listOf("run-1", "run-1"), followed)
    }
}
