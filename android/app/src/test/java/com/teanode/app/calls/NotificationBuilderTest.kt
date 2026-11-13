package com.teanode.app.calls

import android.app.Notification
import android.app.NotificationManager
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.teanode.app.notifications.AppUpdateModel
import com.teanode.app.notifications.NotificationBuilder
import com.teanode.app.notifications.NotificationChannels
import com.teanode.app.notifications.TurnNotification
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.notifications.AppUpdate
import com.teanode.kit.notifications.NotificationContent
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** The notifications as the phone draws them, read back from what was built: the words, the buttons, and where each goes. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class NotificationBuilderTest {
    private val context: Context = ApplicationProvider.getApplicationContext()
    private val accountKey = "https://mail.example.com|sam"

    private fun built(json: String): Notification =
        NotificationBuilder.build(context, TeaNodeJson.decodeFromString(NotificationContent.serializer(), json), accountKey, 1)

    private fun actionTitles(notification: Notification): List<String> = notification.actions.orEmpty().map { action -> action.title.toString() }

    @Test
    fun anApprovalHasDoItAndDont() {
        val notification = built("""{"notificationKind":"approval","title":"Send the mail?","body":"To Sam","runId":"r","callId":"c","threadId":"approval:r"}""")
        assertEquals("Send the mail?", notification.extras.getString(Notification.EXTRA_TITLE))
        assertEquals(listOf("Do it", "Don’t"), actionTitles(notification))
        assertEquals("approval:r", notification.group)
    }

    @Test
    fun aMessageCanBeRepliedToFromTheNotification() {
        val notification = built("""{"notificationKind":"message","title":"Tea","body":"Hello","conversationId":"c1"}""")
        assertEquals(listOf("Reply", "Open"), actionTitles(notification))
        val reply = notification.actions[0]
        assertEquals(1, reply.remoteInputs.size)
        assertEquals("Ask your agent…", reply.remoteInputs[0].label.toString())
        assertNull(notification.actions[1].remoteInputs)
    }

    @Test
    fun aReplyInAChatSaysMessage() {
        val notification = built("""{"notificationKind":"chat","title":"Sam","body":"Hi","conversationId":"c1"}""")
        assertEquals("Message…", notification.actions[0].remoteInputs[0].label.toString())
    }

    @Test
    fun aQuestionWithThreeChoicesShowsThemAsButtons() {
        val notification = built(
            """{"notificationKind":"question","title":"t","body":"Which day?","runId":"r","callId":"c","buttons":[{"label":"Friday","buttonAction":"answer:1"},{"label":"Saturday","buttonAction":"answer:2"},{"label":"Sunday","buttonAction":"answer:3"}]}""",
        )
        assertEquals(listOf("Friday", "Saturday", "Sunday"), actionTitles(notification))
    }

    @Test
    fun aQuestionWithFourChoicesDrawsThemInARowOfTheirOwn() {
        val notification = built(
            """{"notificationKind":"question","title":"t","body":"Which day?","runId":"r","callId":"c","buttons":[{"label":"Mon","buttonAction":"answer:1"},{"label":"Tue","buttonAction":"answer:2"},{"label":"Wed","buttonAction":"answer:3"},{"label":"Thu","buttonAction":"answer:4"}]}""",
        )
        assertNotNull(notification.bigContentView)
        assertTrue(notification.actions.orEmpty().isEmpty())
    }

    @Test
    fun theEveningQuestionHasFiveMoodsWithoutOpeningTheApp() {
        val notification = built("""{"notificationKind":"journalEveningQuestion","title":"How was today?","body":"One tap is enough.","journalDay":"2026-10-26"}""")
        assertNotNull(notification.bigContentView)
        assertEquals(5, 5)
    }

    @Test
    fun aNotificationThatOnlyOpensHasAnOpenButton() {
        assertEquals(listOf("Open"), actionTitles(built("""{"notificationKind":"circle","title":"t","body":"b"}""")))
        assertEquals(listOf("Open"), actionTitles(built("""{"notificationKind":"herdr_question","title":"t","body":"b"}""")))
        assertEquals(emptyList<String>(), actionTitles(built("""{"notificationKind":"test","title":"t","body":"b"}""")))
    }

    @Test
    fun everyKindIsPostedOnAChannelThatExists() {
        NotificationChannels.create(context)
        val manager = context.getSystemService(NotificationManager::class.java)
        val channelIds = manager.notificationChannels.map { channel -> channel.id }
        for (kind in listOf("approval", "chat", "circle", "location", "shopping", "herdr_question", "journalEveningQuestion", "photo", "message", "unknown")) {
            val notification = built("""{"notificationKind":"$kind","title":"t","body":"b"}""")
            assertTrue("$kind is on a channel that was made", notification.channelId in channelIds)
        }
    }

    @Test
    fun aTurnIsSaidInWordsAndOnlyWorkingAndWaitingStay() {
        assertEquals("Working on it", TurnNotification.wordsOf(context, "working"))
        assertEquals("Working on it", TurnNotification.wordsOf(context, "anything else"))
        assertEquals("Waiting for you", TurnNotification.wordsOf(context, "waiting"))
        assertEquals("Done", TurnNotification.wordsOf(context, "done"))
        assertEquals("Stopped with a problem", TurnNotification.wordsOf(context, "failed"))
        assertTrue(TurnNotification.isOngoing("working"))
        assertTrue(TurnNotification.isOngoing("waiting"))
        assertFalse(TurnNotification.isOngoing("done"))
        assertFalse(TurnNotification.isOngoing("failed"))
    }

    @Test
    fun anUpdateIsOfferedOnceAndDismissedForThatUpdateAlone() {
        val preferences = context.getSharedPreferences("update-test", Context.MODE_PRIVATE).also { kept -> kept.edit().clear().apply() }
        val model = AppUpdateModel(preferences, isDebugBuild = false)
        val first = AppUpdate("android", "9.0.0", "1", "https://appdistribution.firebase.dev/i/example")
        model.offer(first)
        assertEquals(first, model.update.value)
        model.dismiss()
        // The next update is offered again, and the one put off is remembered across launches.
        assertEquals("android 9.0.0 (1)", preferences.getString("dismissedAppUpdate", null))
        assertEquals("android 9.0.0 (1)", AppUpdateModel(preferences, isDebugBuild = false).let { preferences.getString("dismissedAppUpdate", null) })
    }
}
