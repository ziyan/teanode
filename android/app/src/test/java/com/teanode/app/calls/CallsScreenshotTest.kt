package com.teanode.app.calls

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import com.teanode.app.demo.DemoHost
import com.teanode.app.notifications.AppUpdateCard
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.kit.calls.ChatCallAudioRoute
import com.teanode.kit.calls.ChatCallDetailView
import com.teanode.kit.calls.ChatCallLine
import com.teanode.kit.calls.ChatCallTranscriptSegment
import com.teanode.kit.notifications.AppUpdate
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of every screen and state of calls: the call screen, its bar, the ring, and the pieces that sit in a chat. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class CallsScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val routes = listOf(ChatCallAudioRoute.Phone, ChatCallAudioRoute.Speaker, ChatCallAudioRoute.Bluetooth("Example Buds"))

    @Composable
    private fun Shown(look: Look, fontScale: Float = 1f, content: @Composable () -> Unit) {
        DemoHost(appearance = look.appearance) {
            CompositionLocalProvider(LocalCallClockMS provides CallsDemo.referenceNowMS) {
                if (fontScale == 1f) content() else WithFontScale(fontScale, content)
            }
        }
    }

    private fun photographCallScreen(name: String, state: CallSessionState, look: Look, fontScale: Float = 1f) {
        composeRule.setContent { Shown(look, fontScale) { ChatCallScreen(state, routes, CallActions()) } }
        composeRule.photograph("calls-$name-${look.fileName}")
    }

    private fun call_screen_with_three_people(look: Look) {
        photographCallScreen("screen", CallsDemo.callScreenSession, look)
    }

    @Test
    fun `call screen with three people light`() = call_screen_with_three_people(Look.Light)

    @Test
    fun `call screen with three people dark`() = call_screen_with_three_people(Look.Dark)

    @Test
    fun `call screen at the largest text`() = photographCallScreen("screen-largest-text", CallsDemo.callScreenSession, Look.Light, fontScale = 2f)

    private fun call_of_five_with_one_featured(look: Look) {
        photographCallScreen("featured", CallsDemo.featuredSession, look)
    }

    @Test
    fun `call of five with one featured light`() = call_of_five_with_one_featured(Look.Light)

    @Test
    fun `call of five with one featured dark`() = call_of_five_with_one_featured(Look.Dark)

    @Test
    fun `call with a person chosen`() = photographCallScreen("chosen", CallsDemo.chosenSession, Look.Dark)

    @Test
    fun `call being rung and muted`() = photographCallScreen("ringing-out", CallsDemo.ringingOutSession, Look.Light)

    @Test
    fun `call reconnecting with the camera on`() = photographCallScreen("reconnecting", CallsDemo.reconnectingSession, Look.Light)

    @Test
    fun `call put away to its bar`() {
        composeRule.setContent {
            Shown(Look.Light) {
                Column(Modifier.fillMaxSize()) {
                    CallBar(CallsDemo.minimizedSession, CallActions())
                    Surface(Modifier.fillMaxSize()) { Text("The rest of the app", style = MaterialTheme.typography.titleLarge) }
                }
            }
        }
        composeRule.photograph("calls-bar-light")
    }

    @Test
    fun `call put away muted at the largest text`() {
        composeRule.setContent {
            Shown(Look.Dark, fontScale = 2f) {
                Column(Modifier.fillMaxSize()) {
                    CallBar(CallsDemo.minimizedSession.copy(isMuted = true), CallActions())
                    Surface(Modifier.fillMaxSize()) { Text("The rest of the app", style = MaterialTheme.typography.titleLarge) }
                }
            }
        }
        composeRule.photograph("calls-bar-largest-text-dark")
    }

    private fun incoming_call(look: Look) {
        composeRule.setContent { Shown(look) { IncomingCallContent("Sam", onAnswer = {}, onDecline = {}) } }
        composeRule.photograph("calls-incoming-${look.fileName}")
    }

    @Test
    fun `incoming call light`() = incoming_call(Look.Light)

    @Test
    fun `incoming call dark`() = incoming_call(Look.Dark)

    private fun banner_of_a_chat_with_a_call_going(look: Look) {
        composeRule.setContent { Shown(look) { Column { ChatCallBannerContent(onCount = 2, onJoin = {}) } } }
        composeRule.photograph("calls-banner-${look.fileName}")
    }

    @Test
    fun `banner of a chat with a call going light`() = banner_of_a_chat_with_a_call_going(Look.Light)

    @Test
    fun `banner of a chat with a call going dark`() = banner_of_a_chat_with_a_call_going(Look.Dark)

    @Composable
    private fun Lines() {
        Column {
            val records = CallsDemo.directChatRecords + CallsDemo.otherRecords
            records.forEach { record ->
                val line: ChatCallLine = CallsDemo.lineOf(record)
                val detail = when {
                    line.isGoing || line.isTranscriptDone -> "You and Sam"
                    record.id == "call-direct-missed" -> "Sam called"
                    else -> null
                }
                ChatCallLineCard(line, detail, isOnThisCall = false, onJoin = {}, onOpenTranscript = {})
            }
        }
    }

    private fun lines_of_calls_in_a_conversation(look: Look) {
        composeRule.setContent { Shown(look) { Lines() } }
        composeRule.photograph("calls-lines-${look.fileName}")
    }

    @Test
    fun `lines of calls in a conversation light`() = lines_of_calls_in_a_conversation(Look.Light)

    @Test
    fun `lines of calls in a conversation dark`() = lines_of_calls_in_a_conversation(Look.Dark)

    @Test
    fun `lines of calls at the largest text`() {
        composeRule.setContent { Shown(Look.Light, fontScale = 2f) { Lines() } }
        composeRule.photograph("calls-lines-largest-text-light")
    }

    private val people = listOf(
        ChatCallPerson("sam", "Sam", null, 5), ChatCallPerson("alex", "Alex", null, 8),
        ChatCallPerson("casey", "Casey", null, 1), ChatCallPerson("morgan", "Morgan", null, 10),
    )

    private fun sheet_of_who_to_call(look: Look) {
        composeRule.setContent {
            Shown(look) {
                Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.surfaceContainerLow) {
                    ChatCallPeopleContent(people, chosenUserIds = listOf("sam", "alex", "morgan"), onToggle = {}, onCall = {})
                }
            }
        }
        composeRule.photograph("calls-who-to-call-${look.fileName}")
    }

    @Test
    fun `sheet of who to call light`() = sheet_of_who_to_call(Look.Light)

    @Test
    fun `sheet of who to call dark`() = sheet_of_who_to_call(Look.Dark)

    @Test
    fun `sheet of who to call with nobody chosen`() {
        composeRule.setContent {
            Shown(Look.Light) {
                Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.surfaceContainerLow) {
                    ChatCallPeopleContent(people, chosenUserIds = emptyList(), onToggle = {}, onCall = {})
                }
            }
        }
        composeRule.photograph("calls-who-to-call-nobody-light")
    }

    private val transcript = ChatCallDetailView(
        call = CallsDemo.callScreenCall,
        transcript = listOf(
            ChatCallTranscriptSegment("segment-1", "call-1", "robin", "Robin", 4_000, 9_000, "Are we still having the tart on Saturday?"),
            ChatCallTranscriptSegment("segment-2", "call-1", "sam", "Sam", 10_000, 15_000, "Yes, bring two. Mom is making soup."),
            ChatCallTranscriptSegment("segment-3", "call-1", "robin", "Robin", 66_000, 70_000, "Perfect, I will be there at six."),
        ),
        recordingId = "recording-1", isRecordingOwned = true,
    )

    private fun transcript_of_a_call(look: Look) {
        composeRule.setContent { Shown(look) { ChatCallTranscriptList(transcript, onOpenRecording = {}) } }
        composeRule.photograph("calls-transcript-${look.fileName}")
    }

    @Test
    fun `transcript of a call light`() = transcript_of_a_call(Look.Light)

    @Test
    fun `transcript of a call dark`() = transcript_of_a_call(Look.Dark)

    @Test
    fun `transcript at the largest text`() {
        composeRule.setContent { Shown(Look.Light, fontScale = 2f) { ChatCallTranscriptList(transcript, onOpenRecording = {}) } }
        composeRule.photograph("calls-transcript-largest-text-light")
    }

    private fun notice_that_transcription_is_on(look: Look) {
        composeRule.setContent {
            Shown(look) {
                Column {
                    TranscriptionNotice("Alex", isAskedByPerson = false, onClose = {})
                    TranscriptionNotice("", isAskedByPerson = true, onClose = {})
                }
            }
        }
        composeRule.photograph("calls-transcription-notice-${look.fileName}")
    }

    @Test
    fun `notice that transcription is on light`() = notice_that_transcription_is_on(Look.Light)

    @Test
    fun `notice that transcription is on dark`() = notice_that_transcription_is_on(Look.Dark)

    private fun update_card(look: Look) {
        composeRule.setContent {
            Shown(look) {
                Column { AppUpdateCard(AppUpdate("android", "1.4.0", "42", "https://appdistribution.firebase.google.com/testerapps/example/releases/example"), onInstall = {}, onDismiss = {}) }
            }
        }
        composeRule.photograph("notifications-update-card-${look.fileName}")
    }

    @Test
    fun `update card light`() = update_card(Look.Light)

    @Test
    fun `update card dark`() = update_card(Look.Dark)

    @Test
    fun `update card at the largest text`() {
        composeRule.setContent {
            Shown(Look.Dark, fontScale = 2f) {
                Column { AppUpdateCard(AppUpdate("android", "1.4.0", "42", "https://appdistribution.firebase.google.com/testerapps/example/releases/example"), onInstall = {}, onDismiss = {}) }
            }
        }
        composeRule.photograph("notifications-update-card-largest-text-dark")
    }
}
