package com.teanode.app.goals

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.performScrollToNode
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.settings.LocalDialogsInline
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of Goals in demo mode: the list, one goal open and one met, a goal on set times, one waiting for mail and a paused one, the When editor and the form that starts a goal. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class GoalsScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale) { CompositionLocalProvider(LocalDialogsInline provides true) { content() } } } }
        composeRule.waitForIdle()
    }

    @Test fun `list light`() { draw(Look.Light) { GoalsSection() }; composeRule.photograph("goals-list-light") }

    @Test fun `list dark`() { draw(Look.Dark) { GoalsSection() }; composeRule.photograph("goals-list-dark") }

    @Test fun `list at the largest text`() { draw(Look.Light, 2f) { GoalsSection() }; composeRule.photograph("goals-list-largest-text-light") }

    @Test fun `open goal light`() { draw(Look.Light) { GoalDetailScreen("goal-working") }; composeRule.photograph("goals-detail-light") }

    @Test fun `open goal dark`() { draw(Look.Dark) { GoalDetailScreen("goal-working") }; composeRule.photograph("goals-detail-dark") }

    @Test fun `waiting goal at the largest text`() { draw(Look.Light, 2f) { GoalDetailScreen("goal-waiting") }; composeRule.photograph("goals-detail-largest-text-light") }

    @Test fun `met goal light`() { draw(Look.Light) { GoalDetailScreen("goal-done") }; composeRule.photograph("goals-detail-met-light") }

    @Test fun `drop confirmation dark`() {
        draw(Look.Dark) { GoalDetailScreen("goal-working") }
        composeRule.onNode(hasScrollAction()).performScrollToNode(hasText("Drop goal"))
        composeRule.onNodeWithText("Drop goal").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("goals-drop-dark")
    }

    @Test fun `end of an open goal dark`() {
        draw(Look.Dark) { GoalDetailScreen("goal-working") }
        composeRule.onNode(hasScrollAction()).performScrollToNode(hasText("Drop goal"))
        composeRule.waitForIdle()
        composeRule.photograph("goals-detail-end-dark")
    }

    @Test fun `mail goal light`() { draw(Look.Light) { GoalDetailScreen("goal-mail") }; composeRule.photograph("goals-detail-mail-light") }

    @Test fun `paused goal light`() { draw(Look.Light) { GoalDetailScreen("goal-paused") }; composeRule.photograph("goals-detail-paused-light") }

    @Test fun `when editor light`() {
        draw(Look.Light) { GoalDetailScreen("goal-working") }
        composeRule.onNode(hasScrollAction()).performScrollToNode(hasText("Change when it runs"))
        composeRule.onNodeWithText("Change when it runs").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("goals-when-editor-light")
    }

    @Test fun `mail editor dark`() {
        draw(Look.Dark) { GoalDetailScreen("goal-mail") }
        composeRule.onNode(hasScrollAction()).performScrollToNode(hasText("Change when it runs"))
        composeRule.onNodeWithText("Change when it runs").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("goals-when-editor-mail-dark")
    }

    @Test fun `new goal at set times light`() {
        draw(Look.Light) { GoalStarterScreen() }
        composeRule.onNodeWithText("Set times").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("goals-new-set-times-light")
    }

    @Test fun `new goal light`() { draw(Look.Light) { GoalStarterScreen() }; composeRule.photograph("goals-new-light") }

    @Test fun `new goal filled in dark`() {
        draw(Look.Dark) { GoalStarterScreen() }
        composeRule.onNodeWithText("What to work toward").performTextInput("Plan the autumn trip")
        composeRule.waitForIdle()
        composeRule.photograph("goals-new-dark")
    }
}
