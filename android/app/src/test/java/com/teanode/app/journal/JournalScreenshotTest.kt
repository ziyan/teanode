package com.teanode.app.journal

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.junit4.createComposeRule
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

import com.teanode.kit.journal.JournalPeriodKind
import java.time.LocalDate

/** Photographs of Journal in demo mode: the home, a full day, a words-only day, an empty day, the week, month and year, writing, correcting and the settings. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class JournalScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val today = LocalDate.now().toString()
    private val yesterday = LocalDate.now().minusDays(1).toString()
    private val emptyDay = LocalDate.now().minusDays(4).toString()

    private fun draw(look: Look, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale, content) } }
        composeRule.waitForIdle()
    }

    @Test fun `home light`() { draw(Look.Light) { JournalSection() }; composeRule.photograph("journal-home-light") }

    @Test fun `home dark`() { draw(Look.Dark) { JournalSection() }; composeRule.photograph("journal-home-dark") }

    @Test fun `home at the largest text`() { draw(Look.Light, 2f) { JournalSection() }; composeRule.photograph("journal-home-largest-text-light") }

    @Test fun `full day light`() { draw(Look.Light) { JournalDayScreen(today) }; composeRule.photograph("journal-day-light") }

    @Test fun `full day dark`() { draw(Look.Dark) { JournalDayScreen(today) }; composeRule.photograph("journal-day-dark") }

    @Test fun `full day at the largest text`() { draw(Look.Light, 2f) { JournalDayScreen(today) }; composeRule.photograph("journal-day-largest-text-light") }

    @Test fun `words only day light`() { draw(Look.Light) { JournalDayScreen(yesterday) }; composeRule.photograph("journal-day-words-light") }

    @Test fun `empty day dark`() { draw(Look.Dark) { JournalDayScreen(emptyDay) }; composeRule.photograph("journal-day-empty-dark") }

    @Test fun `week light`() { draw(Look.Light) { JournalPeriodScreen(JournalPeriodKind.Week, today) }; composeRule.photograph("journal-week-light") }

    @Test fun `month light`() { draw(Look.Light) { JournalPeriodScreen(JournalPeriodKind.Month, today) }; composeRule.photograph("journal-month-light") }

    @Test fun `month dark`() { draw(Look.Dark) { JournalPeriodScreen(JournalPeriodKind.Month, today) }; composeRule.photograph("journal-month-dark") }

    @Test fun `month at the largest text`() { draw(Look.Light, 2f) { JournalPeriodScreen(JournalPeriodKind.Month, today) }; composeRule.photograph("journal-month-largest-text-light") }

    @Test fun `year light`() { draw(Look.Light) { JournalPeriodScreen(JournalPeriodKind.Year, today) }; composeRule.photograph("journal-year-light") }

    @Test fun `write day light`() { draw(Look.Light) { JournalWriteScreen(null, today) }; composeRule.photograph("journal-write-light") }

    @Test fun `write looking back dark`() { draw(Look.Dark) { JournalWriteScreen(JournalPeriodKind.Week, today) }; composeRule.photograph("journal-write-looking-back-dark") }

    @Test fun `correction light`() { draw(Look.Light) { JournalCorrectionScreen(null, today) }; composeRule.photograph("journal-correction-light") }

    @Test fun `settings light`() { draw(Look.Light) { JournalSettingsScreen() }; composeRule.photograph("journal-settings-light") }

    @Test fun `settings dark`() { draw(Look.Dark) { JournalSettingsScreen() }; composeRule.photograph("journal-settings-dark") }

    @Test fun `places with the map light`() {
        draw(Look.Light) { JournalDayScreen(today) }
        composeRule.onNode(hasScrollAction().and(androidx.compose.ui.test.SemanticsMatcher.keyIsDefined(androidx.compose.ui.semantics.SemanticsProperties.VerticalScrollAxisRange))).performScrollToNode(hasText("River park"))
        composeRule.waitForIdle()
        composeRule.photograph("journal-day-places-light")
    }
}
