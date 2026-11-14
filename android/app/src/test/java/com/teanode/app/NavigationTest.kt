package com.teanode.app

import com.teanode.app.shell.barTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.assertIsNotDisplayed
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import com.teanode.app.demo.rememberDemoSession
import com.teanode.app.signin.SignInScreen
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.rememberSnackbarController
import com.teanode.app.ui.theme.TeaNodeTheme
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** The shell's behavior: the bar, the drawer, deep links, and the sign-in screen's buttons. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], qualifiers = "w393dp-h852dp-xxhdpi")
class NavigationTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val placeholder = "This part of the app is not built yet."

    /** A bottom bar item, told apart from the drawer's row of the same name, which stays composed beside the screen. */
    private fun barItem(name: String) = composeRule.onNodeWithTag(barTag(name))

    @Test
    fun theAppOpensOnTheFirstPartInTheBar() {
        composeRule.setContent { AppRoot(rememberDemoSession()) }
        barItem("agent").assertIsSelected()
        barItem("mail").assertIsNotSelected()
        // Chats is built: its first screen is the list of conversations, which starts with the main chat.
        composeRule.waitUntil(15_000) { composeRule.onAllNodesWithText("Main chat").fetchSemanticsNodes().isNotEmpty() }
        composeRule.onNodeWithText("Main chat").assertIsDisplayed()
    }

    @Test
    fun aPartInTheBarOpensFromIt() {
        composeRule.setContent { AppRoot(rememberDemoSession()) }
        barItem("mail").performClick()
        composeRule.waitForIdle()
        barItem("mail").assertIsSelected()
        barItem("agent").assertIsNotSelected()
    }

    @Test
    fun theDrawerHoldsEveryPartAndOpensOne() {
        composeRule.setContent { AppRoot(rememberDemoSession()) }
        composeRule.onNodeWithContentDescription("Open the menu").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Every day").assertIsDisplayed()
        composeRule.onNodeWithText("Calendar").performClick()
        composeRule.waitForIdle()
        // Calendar is not in the bar, so no tab is selected.
        barItem("agent").assertIsNotSelected()
        composeRule.onNodeWithText("Every day").assertIsNotDisplayed()
    }

    @Test
    fun moreOpensTheDrawer() {
        composeRule.setContent { AppRoot(rememberDemoSession()) }
        barItem("more").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Every day").assertIsDisplayed()
    }

    @Test
    fun aDeepLinkOpensItsRouteAtLaunch() {
        composeRule.setContent { AppRoot(rememberDemoSession(), startRoute = "settings/home") }
        composeRule.waitForIdle()
        barItem("agent").assertIsNotSelected()
        // Settings is built, so its own first screen is on screen, not the placeholder.
        composeRule.onNodeWithText("Your account").assertIsDisplayed()
    }

    @Test
    fun theSearchButtonOpensSearch() {
        composeRule.setContent { AppRoot(rememberDemoSession()) }
        barItem("search").performClick()
        composeRule.waitForIdle()
        barItem("search").assertIsSelected()
    }

    @Test
    fun theSignInScreenReportsWhatWasTypedAndPressed() {
        var typed = ""
        var signInCount = 0
        var scanCount = 0
        composeRule.setContent {
            var text by remember { mutableStateOf("") }
            TeaNodeTheme {
                CompositionLocalProvider(LocalSnackbar provides rememberSnackbarController(null)) {
                    SignInScreen(
                        serverText = text, onServerTextChange = { changed -> text = changed; typed = changed }, problemText = null, isConnecting = false,
                        onSignIn = { signInCount += 1 }, onScan = { scanCount += 1 }, onPasteLink = {}, onOpenSetupGuide = {},
                    )
                }
            }
        }
        // Signing in waits for an address.
        composeRule.onNodeWithText("Sign in").performClick()
        assertEquals(0, signInCount)
        composeRule.onNodeWithText("Server address").performTextInput("mail.example.com")
        assertEquals("mail.example.com", typed)
        composeRule.onNodeWithText("Sign in").performClick()
        assertEquals(1, signInCount)
        composeRule.onNodeWithText("Scan code").performClick()
        assertEquals(1, scanCount)
    }
}
