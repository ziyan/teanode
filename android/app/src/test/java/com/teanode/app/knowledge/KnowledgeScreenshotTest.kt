package com.teanode.app.knowledge

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color as AndroidColor
import android.graphics.Paint
import androidx.compose.runtime.Composable
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.performScrollToNode
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

    @Test fun `page with files and a table light`() {
        draw(Look.Light) { KnowledgePageScreen("people/sam-example") }
        composeRule.onAllNodes(hasScrollAction())[0].performScrollToNode(hasText("bed-plan.docx"))
        composeRule.waitForIdle()
        composeRule.photograph("knowledge-page-files-light")
    }

    @Test fun `pdf preview and viewer dark`() {
        val pages = (1..3).map { number ->
            Bitmap.createBitmap(400, 560, Bitmap.Config.ARGB_8888).also { bitmap ->
                val canvas = Canvas(bitmap)
                canvas.drawColor(AndroidColor.WHITE)
                val paint = Paint().apply { color = AndroidColor.DKGRAY; textSize = 28f }
                canvas.drawText("Seed order, page $number", 30f, 60f, paint)
                for (line in 1..8) canvas.drawText("Seed potatoes, 10 kg", 30f, 60f + line * 40f, paint)
            }
        }
        draw(Look.Dark) {
            androidx.compose.foundation.layout.Column {
                PdfPreviewCard("seed-order.pdf", pages) {}
            }
        }
        composeRule.photograph("knowledge-pdf-preview-dark")
    }

    @Test fun `pdf viewer light`() {
        val pages = (1..3).map { number ->
            Bitmap.createBitmap(400, 560, Bitmap.Config.ARGB_8888).also { bitmap ->
                val canvas = Canvas(bitmap)
                canvas.drawColor(AndroidColor.WHITE)
                val paint = Paint().apply { color = AndroidColor.DKGRAY; textSize = 28f }
                canvas.drawText("Seed order, page $number", 30f, 60f, paint)
            }
        }
        draw(Look.Light) { PdfViewerDialog("seed-order.pdf", pages) {} }
        composeRule.photograph("knowledge-pdf-viewer-light")
    }
}
