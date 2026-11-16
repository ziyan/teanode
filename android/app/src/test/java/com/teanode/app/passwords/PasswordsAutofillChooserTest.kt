package com.teanode.app.passwords

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
import com.teanode.kit.passwords.PasswordsAutofillLogin
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** The list after the fingerprint: suggestions for the page, a search over every login, and the question before a loose match is filled. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class PasswordsAutofillChooserTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun login(id: String, userName: String, vararg sites: String, sharedByName: String? = null, hasOneTimeCode: Boolean = false) =
        PasswordsAutofillLogin(id, id, userName, sites.toList(), hasPassword = true, hasOneTimeCode = hasOneTimeCode, sharedByName = sharedByName)

    private val logins = listOf(
        login("Example work", "alex@example.com", "https://login.example.com"),
        login("Example account", "alex", "https://example.com", sharedByName = "Sam"),
        login("Mail", "alex@example.org", "https://mail.example.org", hasOneTimeCode = true),
        login("Shop", "alex", "https://shop.example.net"),
    )

    private fun draw(look: Look, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { WithFontScale(fontScale) { DemoHost(appearance = look.appearance) { content() } } }
        repeat(3) { composeRule.waitForIdle() }
    }

    @Test fun `suggested, related and all light`() {
        draw(Look.Light) { PasswordsAutofillChooser(logins, "https://login.example.com", false, {}, {}) }
        composeRule.onNodeWithText("Suggested").assertExists()
        composeRule.onNodeWithText("Related sites").assertExists()
        composeRule.onNodeWithText("All logins").assertExists()
        composeRule.photograph("passwords-autofill-list-light")
    }

    @Test fun `suggested, related and all dark at the largest text`() {
        draw(Look.Dark, fontScale = 2f) { PasswordsAutofillChooser(logins, "https://login.example.com", false, {}, {}) }
        composeRule.photograph("passwords-autofill-list-dark-large")
    }

    @Test fun `an app has one list with no heading and no question`() {
        var chosen: String? = null
        draw(Look.Light) { PasswordsAutofillChooser(logins, null, false, { login -> chosen = login.itemId }, {}) }
        composeRule.onNodeWithText("Suggested").assertDoesNotExist()
        composeRule.onNodeWithText("All logins").assertDoesNotExist()
        composeRule.photograph("passwords-autofill-app-light")
        composeRule.onNodeWithText("Shop").performClick()
        assertEquals("Shop", chosen)
    }

    @Test fun `a search finds a login for another site and asks before filling it`() {
        var chosen: String? = null
        draw(Look.Light) { PasswordsAutofillChooser(logins, "https://login.example.com", false, { login -> chosen = login.itemId }, {}) }
        composeRule.onNodeWithText("Search logins").performTextInput("shop")
        composeRule.onNodeWithText("Shop").assertExists()
        composeRule.onNodeWithText("Example work").assertDoesNotExist()
        composeRule.onNodeWithText("Shop").performClick()
        composeRule.onNodeWithText("Fill Shop into login.example.com?").assertExists()
        composeRule.onNodeWithText("It is saved for shop.example.net.").assertExists()
        assertTrue(chosen == null)
        composeRule.photograph("passwords-autofill-question-light")
        composeRule.onNodeWithText("Fill").performClick()
        assertEquals("Shop", chosen)
    }

    @Test fun `a suggested login is filled without a question`() {
        var chosen: String? = null
        draw(Look.Light) { PasswordsAutofillChooser(logins, "https://login.example.com", false, { login -> chosen = login.itemId }, {}) }
        composeRule.onNodeWithText("Example work").performClick()
        assertEquals("Example work", chosen)
    }

    @Test fun `a related login asks`() {
        var chosen: String? = null
        draw(Look.Dark) { PasswordsAutofillChooser(logins, "https://login.example.com", false, { login -> chosen = login.itemId }, {}) }
        composeRule.onNodeWithText("Example account").performClick()
        composeRule.onNodeWithText("Fill Example account into login.example.com?").assertExists()
        assertTrue(chosen == null)
        composeRule.photograph("passwords-autofill-question-dark")
    }

    @Test fun `no logins and a search with no match`() {
        draw(Look.Light) { PasswordsAutofillChooser(emptyList(), "https://example.com", false, {}, {}) }
        composeRule.onNodeWithText("No logins yet").assertExists()
        composeRule.photograph("passwords-autofill-empty-light")
    }

    @Test fun `a one-time code list holds only logins with a code`() {
        draw(Look.Light) { PasswordsAutofillChooser(logins, "https://mail.example.org", true, {}, {}) }
        composeRule.onNodeWithText("Mail").assertExists()
        composeRule.onNodeWithText("Shop").assertDoesNotExist()
    }
}
