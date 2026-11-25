package com.teanode.app.chats

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.photograph
import com.teanode.kit.chats.TranscriptLine
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * Photographs of tool lines as a conversation shows them with tool calls on: a call that said what it was doing, one
 * that did not and is shown by the tool's name, one still running, and one whose words are longer than the line.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ToolLineScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val lines = listOf(
        TranscriptLine("t1", TranscriptLine.Kind.Tool, "3 messages", detail = "mail_search", isDone = true, intention = "Looking for the boiler invoice"),
        TranscriptLine("t2", TranscriptLine.Kind.Tool, "2 events", detail = "calendar", isDone = true),
        TranscriptLine("t3", TranscriptLine.Kind.Tool, "", detail = "web_search", intention = "Checking tomorrow's weather"),
        TranscriptLine(
            "t4", TranscriptLine.Kind.Tool, "done", detail = "mail_draft", isDone = true, resolution = "drafted",
            intention = "Drafting a reply that says Thursday at nine works for the visit",
        ),
    )

    private fun photograph(look: Look) {
        composeRule.setContent {
            DemoHost(appearance = look.appearance) {
                Surface(color = MaterialTheme.colorScheme.background) {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) { lines.forEach { line -> ToolLine(line) } }
                }
            }
        }
        composeRule.waitForIdle()
        composeRule.photograph("chats-tool-lines-${look.fileName}")
    }

    @Test fun `tool lines light`() = photograph(Look.Light)
    @Test fun `tool lines dark`() = photograph(Look.Dark)
}
