package com.teanode.app.chats

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.performTouchInput
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.ui.theme.AppearanceChoice
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of every screen and state of Chats, in demo mode: the list, each kind of conversation, the sheets and the composer. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ChatsScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale) { content() } } }
    }

    private fun waitFor(text: String, timeoutMillis: Long = 15_000) {
        composeRule.waitUntil(timeoutMillis) { composeRule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
    }

    private fun conversation(id: String?, look: Look, name: String, waitingFor: String, fontScale: Float = 1f): Unit {
        draw(look, fontScale) { ConversationScreen(conversationId = id, onBack = {}, onOpenConversation = {}) }
        waitFor(waitingFor)
        composeRule.photograph("chats-$name-${look.fileName}")
    }

    @Test fun `list light`() = list(Look.Light)
    @Test fun `list dark`() = list(Look.Dark)

    private fun list(look: Look) {
        draw(look) { ChatsSection() }
        waitFor("Weekend plans")
        composeRule.photograph("chats-list-${look.fileName}")
    }

    @Test fun `list at the largest text`() {
        draw(Look.Light, 2f) { ChatsSection() }
        waitFor("Weekend plans")
        composeRule.photograph("chats-list-largest-text-light")
    }

    @Test fun `list with the archived chats open`() {
        draw(Look.Light) { ChatsSection() }
        waitFor("Archived chats")
        composeRule.onNodeWithText("Archived chats").performClick()
        composeRule.waitForIdle()
        composeRule.waitUntil(15_000) {
            composeRule.onNode(hasScrollAction()).performScrollToNode(hasText("Birthday ideas"))
            true
        }
        waitFor("Birthday ideas")
        composeRule.photograph("chats-list-archived-light")
    }

    @Test fun `list search`() {
        draw(Look.Light) { ChatsSection() }
        waitFor("Weekend plans")
        composeRule.onNodeWithContentDescription("Search conversations").performClick()
        composeRule.onNodeWithText("Search conversations").performTextInput("trip")
        waitFor("Planning the trip")
        composeRule.photograph("chats-list-search-light")
    }

    @Test fun `main conversation light`() = conversation(null, Look.Light, "main", "Remind me to leave early")
    @Test fun `main conversation dark`() = conversation(null, Look.Dark, "main", "Remind me to leave early")
    @Test fun `main conversation at the largest text`() = conversation(null, Look.Light, "main-largest-text", "Remind me to leave early", 2f)
    @Test fun `conversation with cards light`() = conversation("demo-cards", Look.Light, "cards", "Which hotel should I book")
    @Test fun `conversation with cards dark`() = conversation("demo-cards", Look.Dark, "cards", "Which hotel should I book")
    @Test fun `empty conversation light`() = conversation("demo-empty", Look.Light, "empty", "Plan a weekend away")
    @Test fun `empty conversation dark`() = conversation("demo-empty", Look.Dark, "empty", "Plan a weekend away")
    @Test fun `long message light`() = conversation("demo-long", Look.Light, "long-message", "Understood")
    @Test fun `goal conversation light`() = conversation("demo-goal", Look.Light, "goal", "Which month do they belong to")
    @Test fun `run record light`() = conversation("demo-run", Look.Light, "run", "Two new threads")
    @Test fun `direct chat light`() {
        draw(Look.Light) { ConversationScreen(conversationId = "demo-sam", onBack = {}, onOpenConversation = {}) }
        waitFor("Tonight's menu")
        composeRule.photograph("chats-direct-chat-light")
    }
    @Test fun `direct chat dark`() = conversation("demo-sam", Look.Dark, "direct-chat", "Sounds good, thank you")
    @Test fun `group chat light`() = conversation("demo-group", Look.Light, "group-chat", "That settles it")
    @Test fun `group chat dark`() = conversation("demo-group", Look.Dark, "group-chat", "That settles it")

    @Test fun `mention menu`() {
        draw(Look.Light) { ConversationScreen(conversationId = "demo-group", onBack = {}, onOpenConversation = {}) }
        waitFor("That settles it")
        composeRule.onNodeWithContentDescription("Message, or @ to ask an agent").performTextInput("@")
        waitFor("Tells them")
        composeRule.photograph("chats-mention-menu-light")
    }

    @Test fun `message actions`() {
        draw(Look.Light) { ConversationScreen(conversationId = null, onBack = {}, onOpenConversation = {}) }
        waitFor("Remind me to leave early")
        composeRule.onNodeWithText("Remind me to leave early", substring = true).performTouchInput { longClick() }
        waitFor("Reply")
        composeRule.photograph("chats-message-actions-light")
    }

    @Test fun `message actions dark`() {
        draw(Look.Dark) { ConversationScreen(conversationId = "demo-sam", onBack = {}, onOpenConversation = {}) }
        waitFor("No problem, I will order a starter")
        composeRule.onNodeWithText("No problem, I will order a starter", substring = true).performTouchInput { longClick() }
        waitFor("Reply")
        composeRule.photograph("chats-message-actions-dark")
    }

    @Test fun `members sheet`() {
        draw(Look.Light) { ConversationScreen(conversationId = "demo-group", onBack = {}, onOpenConversation = {}) }
        waitFor("That settles it")
        composeRule.onNodeWithContentDescription("Chat options").performClick()
        composeRule.onNodeWithText("Members").performClick()
        waitFor("Your agent in this chat")
        composeRule.photograph("chats-members-light")
    }

    @Test fun `usage and devices sheet`() {
        draw(Look.Light) { ConversationScreen(conversationId = null, onBack = {}, onOpenConversation = {}) }
        waitFor("Remind me to leave early")
        composeRule.onNodeWithContentDescription("Usage and devices").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("chats-status-light")
    }

    @Test fun `people sheet for a new group`() {
        draw(Look.Light) { ChatsSection() }
        waitFor("Weekend plans")
        composeRule.onNodeWithContentDescription("New conversation").performClick()
        composeRule.onNodeWithText("New group").performClick()
        waitFor("Sam Rivera")
        composeRule.photograph("chats-new-group-light")
    }

    @Test fun `voice mode composer`() {
        draw(Look.Light) { ConversationScreen(conversationId = "demo-sam", onBack = {}, onOpenConversation = {}) }
        waitFor("Sounds good, thank you")
        composeRule.onNodeWithContentDescription("Record voice messages").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("chats-voice-mode-light")
    }


    @Test fun `share sheet`() {
        val shared = android.content.Intent(android.content.Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(android.content.Intent.EXTRA_TEXT, "https://www.example.com/story?utm_source=feed&id=4")
        }
        composeRule.setContent {
            val session = com.teanode.app.demo.rememberDemoSession()
            DemoHost(appearance = Look.Light.appearance, session = session) {
                ShareSheet(shared, session, session.connection.value, onFinish = {})
            }
        }
        waitFor("Send to Robin's agent")
        composeRule.photograph("chats-share-light")
    }

    @Test fun `share sheet dark`() {
        val shared = android.content.Intent(android.content.Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(android.content.Intent.EXTRA_TEXT, "Remember to look at the notes before the meeting.")
        }
        composeRule.setContent {
            val session = com.teanode.app.demo.rememberDemoSession()
            DemoHost(appearance = Look.Dark.appearance, session = session) {
                ShareSheet(shared, session, session.connection.value, onFinish = {})
            }
        }
        waitFor("Send to Robin's agent")
        composeRule.photograph("chats-share-dark")
    }

    private fun scrolledConversation(id: String?, look: Look, name: String, scrollTo: String, waitingFor: String, fontScale: Float = 1f) {
        draw(look, fontScale) { ConversationScreen(conversationId = id, onBack = {}, onOpenConversation = {}) }
        waitFor(waitingFor)
        composeRule.onNodeWithTag("conversation-list").performScrollToNode(hasText(scrollTo, substring = true))
        composeRule.waitForIdle()
        composeRule.photograph("chats-$name-${look.fileName}")
    }

    @Test fun `earlier cards`() = scrolledConversation("demo-cards", Look.Light, "cards-earlier", "Book the hotel near the station", "Which hotel should I book")
    @Test fun `earlier cards dark`() = scrolledConversation("demo-cards", Look.Dark, "cards-earlier", "Book the hotel near the station", "Which hotel should I book")
    @Test fun `earlier cards at the largest text`() = scrolledConversation("demo-cards", Look.Light, "cards-earlier-largest-text", "Which file should I edit", "Which hotel should I book", 2f)
    @Test fun `the table answer`() = scrolledConversation(null, Look.Light, "main-answer", "What is on my calendar tomorrow", "Remind me to leave early")
    @Test fun `the table answer dark`() = scrolledConversation(null, Look.Dark, "main-answer", "What is on my calendar tomorrow", "Remind me to leave early")
}
