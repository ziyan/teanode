package com.teanode.app.settings

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onLast
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.ui.theme.AppearanceChoice
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.performTextInput
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** The sheet that shares a skill: the tools, Ask me every time, and the limits with the iPhone's checks. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class SkillShareDialogTest {
    @get:Rule val composeRule = createComposeRule()

    private fun open(appearance: AppearanceChoice = AppearanceChoice.Light) {
        composeRule.setContent { DemoHost(appearance = appearance) { CompositionLocalProvider(LocalDialogsInline provides true) { SkillSharingScreen(onBack = {}) } } }
        composeRule.mainClock.advanceTimeBy(500)
        composeRule.waitForIdle()
        composeRule.onNodeWithContentDescription("Lend a skill").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Skill").performClick()
        composeRule.waitForIdle()
        composeRule.onAllNodesWithText("Travel booking").onLast().performClick()
        composeRule.waitForIdle()
    }

    @Test fun `a paying tool starts chosen and needs a limit`() {
        open()
        composeRule.onNodeWithText("Tools to share").assertExists()
        composeRule.onNodeWithText("Spends money. Calls within your limits go ahead; bigger ones wait for you.").assertExists()
        composeRule.onNodeWithText("A tool that pays is shared with a limit, or with Ask me every time.").assertExists()
        composeRule.onNodeWithText("Share").assertIsNotEnabled()
        composeRule.photograph("settings-skill-share-light")
    }

    @Test fun `ask me every time hides the limits`() {
        open(AppearanceChoice.Dark)
        composeRule.onNodeWithText("Ask me every time").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Per payment").assertDoesNotExist()
        composeRule.onNodeWithText("Everything waits for your approval, whatever it costs.").assertExists()
        composeRule.photograph("settings-skill-share-ask-dark")
    }

    @Test fun `a payment above the monthly limit is refused in words`() {
        open()
        composeRule.onNodeWithText("Per payment").performTextInput("500.00")
        composeRule.onNodeWithText("Per month").performTextInput("100.00")
        composeRule.waitForIdle()
        composeRule.onNodeWithText("The limit for one payment cannot be above the monthly limit.").assertExists()
        composeRule.onNodeWithText("Share").assertIsNotEnabled()
        composeRule.photograph("settings-skill-share-limits-light")
    }
}
