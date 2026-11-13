package com.teanode.app.knowledge

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of Knowledge in demo mode: the top folder, a folder, a page, recall, documents, a shared page and sharing. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class KnowledgeScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale, content) } }
        composeRule.waitForIdle()
    }

    @Test fun `home light`() { draw(Look.Light) { KnowledgeSection() }; composeRule.photograph("knowledge-home-light") }

    @Test fun `home dark`() { draw(Look.Dark) { KnowledgeSection() }; composeRule.photograph("knowledge-home-dark") }

    @Test fun `home at the largest text`() { draw(Look.Light, 2f) { KnowledgeSection() }; composeRule.photograph("knowledge-home-largest-text-light") }

    @Test fun `folder light`() { draw(Look.Light) { KnowledgeFolderScreen("people") }; composeRule.photograph("knowledge-folder-light") }

    @Test fun `person page light`() { draw(Look.Light) { KnowledgePageScreen("people/sam-example") }; composeRule.photograph("knowledge-page-light") }

    @Test fun `person page dark`() { draw(Look.Dark) { KnowledgePageScreen("people/sam-example") }; composeRule.photograph("knowledge-page-dark") }

    @Test fun `person page at the largest text`() { draw(Look.Light, 2f) { KnowledgePageScreen("people/sam-example") }; composeRule.photograph("knowledge-page-largest-text-light") }

    @Test fun `recall light`() { draw(Look.Light) { RecallScreen() }; composeRule.photograph("knowledge-recall-light") }

    @Test fun `recall answered dark`() {
        draw(Look.Dark) { RecallScreen() }
        composeRule.onNodeWithText("What do you want to know?").performTextInput("seed potatoes")
        composeRule.onNodeWithText("Ask").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("knowledge-recall-answered-dark")
    }

    @Test fun `documents dark`() { draw(Look.Dark) { DocumentsScreen() }; composeRule.photograph("knowledge-documents-dark") }

    @Test fun `document reader light`() { draw(Look.Light) { DocumentReaderScreen("doc-1") }; composeRule.photograph("knowledge-document-light") }

    @Test fun `shared page light`() { draw(Look.Light) { SharedKnowledgePageScreen("user-2", "people/sam-example") }; composeRule.photograph("knowledge-shared-light") }

    @Test fun `share page dark`() { draw(Look.Dark) { KnowledgeShareScreen("people/sam-example", onClose = {}) }; composeRule.photograph("knowledge-share-dark") }
}
