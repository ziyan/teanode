package com.teanode.app.chats

import android.graphics.Bitmap
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.unit.dp
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.kit.chats.EarlierRowState
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of the new composer in each of its shapes and states, the voice message bar, and the rows of pulling for earlier messages. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ComposerScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f, isVoice: Boolean = false, setup: (ConversationModel) -> Unit = {}) {
        composeRule.setContent {
            DemoHost(appearance = look.appearance) {
                WithFontScale(fontScale) {
                    val context = LocalContext.current
                    val connection = LocalConnection.current
                    val session = LocalAppSession.current
                    val environment = remember { ChatsEnvironment.of(context, connection, session) }
                    val model = remember { ConversationModel(environment, "demo-sam", environment.known("demo-sam")) }
                    val dictation = remember { Dictation(context) }
                    LaunchedEffect(model) {
                        if (isVoice) environment.chooseComposerMode("voice")
                        model.startFollowing()
                    }
                    LaunchedEffect(model.isVoiceMessageAvailable) { if (model.isVoiceMessageAvailable) setup(model) }
                    Column(Modifier.fillMaxSize()) {
                        Box(Modifier.weight(1f))
                        Composer(model, environment, dictation)
                    }
                }
            }
        }
        composeRule.waitUntil(15_000) { composeRule.onAllNodes(androidx.compose.ui.test.hasContentDescription("Add files") or androidx.compose.ui.test.hasContentDescription("Use the keyboard")).fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
    }

    private fun photograph(name: String, look: Look) { composeRule.waitForIdle(); composeRule.photograph("composer-$name-${look.fileName}") }

    private val longDraft = "Could you check whether the flight on Friday is still on time, and if it is, move the car booking to the evening?"

    @Test fun `pill light`() { draw(Look.Light); photograph("pill", Look.Light) }
    @Test fun `pill dark`() { draw(Look.Dark); photograph("pill", Look.Dark) }
    @Test fun `pill at the largest text`() { draw(Look.Light, 2f); photograph("pill-largest-text", Look.Light) }
    @Test fun `pill with a short draft`() { draw(Look.Light) { model -> model.updateDraft("On my way") }; photograph("pill-draft", Look.Light) }
    @Test fun `card with a long draft light`() { draw(Look.Light) { model -> model.updateDraft(longDraft) }; photograph("card-draft", Look.Light) }
    @Test fun `card with a long draft dark`() { draw(Look.Dark) { model -> model.updateDraft(longDraft) }; photograph("card-draft", Look.Dark) }
    @Test fun `card with a long draft at the largest text`() { draw(Look.Light, 2f) { model -> model.updateDraft(longDraft) }; photograph("card-draft-largest-text", Look.Light) }

    private fun attach(model: ConversationModel) {
        if (model.pendingFiles.isNotEmpty()) return
        val picture = Bitmap.createBitmap(64, 64, Bitmap.Config.ARGB_8888).apply { eraseColor(0xFF4C9A6A.toInt()) }
        model.addPendingFile(PendingFile("a", "garden.jpg", "image/jpeg", ByteArray(10), picture))
        model.addPendingFile(PendingFile("b", "notes.pdf", "application/pdf", ByteArray(10), null))
        model.updateDraft("Here they are")
    }

    @Test fun `card with attachments light`() { draw(Look.Light, setup = ::attach); photograph("card-attachments", Look.Light) }
    @Test fun `card with attachments dark`() { draw(Look.Dark, setup = ::attach); photograph("card-attachments", Look.Dark) }
    @Test fun `card with attachments at the largest text`() { draw(Look.Light, 2f, setup = ::attach); photograph("card-attachments-largest-text", Look.Light) }

    @Test fun `add menu`() {
        draw(Look.Light)
        composeRule.onNodeWithContentDescription("Add files").performClick()
        composeRule.waitForIdle()
        photograph("menu", Look.Light)
    }

    @Test fun `voice mode idle light`() { draw(Look.Light, isVoice = true); photograph("voice-idle", Look.Light) }
    @Test fun `voice mode idle dark`() { draw(Look.Dark, isVoice = true); photograph("voice-idle", Look.Dark) }
    @Test fun `voice mode idle at the largest text`() { draw(Look.Light, 2f, isVoice = true); photograph("voice-idle-largest-text", Look.Light) }

    @Test fun `recording light`() { draw(Look.Light, isVoice = true) { model -> model.voiceRecorder.showDemoHold() }; photograph("voice-recording", Look.Light) }
    @Test fun `recording dark`() { draw(Look.Dark, isVoice = true) { model -> model.voiceRecorder.showDemoHold() }; photograph("voice-recording", Look.Dark) }
    @Test fun `recording at the largest text`() { draw(Look.Light, 2f, isVoice = true) { model -> model.voiceRecorder.showDemoHold() }; photograph("voice-recording-largest-text", Look.Light) }
    @Test fun `cancel armed light`() { draw(Look.Light, isVoice = true) { model -> model.voiceRecorder.showDemoHold(); model.voiceRecorder.isCancelArmed = true }; photograph("voice-cancel", Look.Light) }
    @Test fun `cancel armed dark`() { draw(Look.Dark, isVoice = true) { model -> model.voiceRecorder.showDemoHold(); model.voiceRecorder.isCancelArmed = true }; photograph("voice-cancel", Look.Dark) }
    @Test fun `countdown light`() { draw(Look.Light, isVoice = true) { model -> model.voiceRecorder.showDemoCountdown() }; photograph("voice-countdown", Look.Light) }
    @Test fun `countdown dark`() { draw(Look.Dark, isVoice = true) { model -> model.voiceRecorder.showDemoCountdown() }; photograph("voice-countdown", Look.Dark) }

    private fun topRows(look: Look, fontScale: Float = 1f) {
        composeRule.setContent {
            DemoHost(appearance = look.appearance) {
                WithFontScale(fontScale) {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        EarlierMessagesRow(EarlierRowState.PullHint, 0f, null, {})
                        EarlierMessagesRow(EarlierRowState.PullHint, 40f, null, {})
                        EarlierMessagesRow(EarlierRowState.Armed, 80f, null, {})
                        EarlierMessagesRow(EarlierRowState.Loading, 0f, null, {})
                        EarlierMessagesRow(EarlierRowState.Failed, 0f, "The server could not be reached.", {})
                        EarlierMessagesRow(EarlierRowState.StartOfConversation, 0f, null, {})
                        NewMessagesButton {}
                        JumpToEndButton {}
                    }
                }
            }
        }
        composeRule.waitForIdle()
        composeRule.photograph("composer-top-rows-${look.fileName}")
    }

    @Test fun `top row states light`() = topRows(Look.Light)
    @Test fun `top row states dark`() = topRows(Look.Dark)
    @Test fun `top row states at the largest text`() = topRows(Look.Light, 2f)

    @Test fun `the top of a conversation says it is the start`() {
        composeRule.setContent { DemoHost(appearance = Look.Light.appearance) { ConversationScreen(conversationId = "demo-sam", onBack = {}, onOpenConversation = {}) } }
        composeRule.waitUntil(15_000) { composeRule.onAllNodesWithText("Sounds good, thank you", substring = true).fetchSemanticsNodes().isNotEmpty() }
        composeRule.onNodeWithTag("conversation-list").performScrollToNode(hasText("Start of the conversation"))
        composeRule.waitForIdle()
        composeRule.photograph("composer-conversation-top-light")
    }
}
