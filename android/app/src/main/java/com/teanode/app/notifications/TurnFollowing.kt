package com.teanode.app.notifications

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * TurnFollowing decides when a turn left running is shown as a notification and when the server is asked to keep it
 * current by push: the Android side of the iPhone app's `TurnActivities` (start when the person leaves with a turn
 * running, end when they come back). While the app is in front the turn is on screen and nothing is shown. When it
 * leaves with a turn running, or a turn goes on after it left, the notification is shown and the server is asked once
 * per run to push each change, so it stays current after the system has stopped the app's process.
 *
 * The three actions are handed in so that the order of events can be tested without a phone.
 */
class TurnFollowing(
    private val scope: CoroutineScope,
    /** Gives the server a registration for this run's pushes; false when none could be had. */
    private val followRun: suspend (runId: String) -> Boolean,
    private val show: (activityState: String) -> Unit,
    private val end: () -> Unit,
) {
    private var runningRunId: String? = null
    private var lastActivityState: String = "working"
    private var followedRunId: String? = null

    /** The conversation's feed brought a change in a turn: [activityState] is working, waiting, done or failed. */
    @Synchronized
    fun turnChanged(runId: String, activityState: String, isAppInFront: Boolean) {
        val isOver = activityState == "done" || activityState == "failed"
        runningRunId = if (isOver) null else runId
        lastActivityState = activityState
        if (isAppInFront) {
            end()
            return
        }
        show(activityState)
        if (!isOver) follow(runId)
    }

    /** The app went out of front. A turn still running is shown and followed from now on. */
    @Synchronized
    fun appLeft() {
        val runId = runningRunId ?: return
        show(lastActivityState)
        follow(runId)
    }

    /** The app is in front again: the turn is on screen, so the notification goes. */
    @Synchronized
    fun appCame() {
        end()
        followedRunId = null
    }

    private fun follow(runId: String) {
        if (followedRunId == runId) return
        followedRunId = runId
        scope.launch {
            if (!followRun(runId)) {
                // Nothing would ever update it by push: better none than one that says "working" after the turn is over.
                synchronized(this@TurnFollowing) { if (followedRunId == runId) followedRunId = null }
                end()
            }
        }
    }
}
