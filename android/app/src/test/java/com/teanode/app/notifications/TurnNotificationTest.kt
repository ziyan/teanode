package com.teanode.app.notifications

import android.app.Application
import android.app.NotificationManager
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class TurnNotificationTest {
    private val context: Application = ApplicationProvider.getApplicationContext()
    private val manager get() = shadowOf(context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager)

    private fun turn(state: String, changedAt: Long, isEnded: Boolean = false) = mapOf(
        "pushKind" to "turn", "activityState" to state, "isActivityEnded" to isEnded.toString(), "activityChangedAt" to changedAt.toString(),
    )

    @Before
    fun forgetTheLastTurn() {
        TurnNotification.rememberAgentTitle(context, "Juno")
        TurnNotification.forgetPushOrder(context)
        TurnNotification.end(context)
    }

    private fun shownTitleAndText(): Pair<String?, String?> {
        val extras = manager.allNotifications.single().extras
        return extras.getString("android.title") to extras.getString("android.text")
    }

    @Test
    fun aPushedStateIsShownUnderTheAgentsNameWhileTheAppIsOut() {
        TurnNotification.handlePush(context, turn("waiting", 100L), isAppInFront = false)
        assertEquals("Juno" to context.getString(com.teanode.app.R.string.notifications_turn_waiting), shownTitleAndText())
    }

    @Test
    fun withNoNameKeptTheAppsOwnNameIsUsed() {
        context.getSharedPreferences("turn_notification", Context.MODE_PRIVATE).edit().clear().commit()
        TurnNotification.handlePush(context, turn("working", 100L), isAppInFront = false)
        assertEquals(context.getString(com.teanode.app.R.string.app_name), shownTitleAndText().first)
    }

    @Test
    fun anOlderStateDoesNotReplaceANewerOne() {
        TurnNotification.handlePush(context, turn("done", 200L, isEnded = true), isAppInFront = false)
        TurnNotification.handlePush(context, turn("working", 150L), isAppInFront = false)
        assertEquals(context.getString(com.teanode.app.R.string.notifications_turn_done), shownTitleAndText().second)
    }

    @Test
    fun nothingIsShownWhileTheAppIsInFrontAndWhatWasShownGoes() {
        TurnNotification.handlePush(context, turn("working", 100L), isAppInFront = false)
        assertEquals(1, manager.allNotifications.size)
        TurnNotification.handlePush(context, turn("waiting", 110L), isAppInFront = true)
        assertTrue(manager.allNotifications.isEmpty())
    }

    @Test
    fun aMessageWithAStateTheServerDoesNotHaveShowsNothing() {
        TurnNotification.handlePush(context, turn("exploding", 100L), isAppInFront = false)
        assertTrue(manager.allNotifications.isEmpty())
    }
}
