package com.teanode.app.screenshots

import androidx.compose.ui.test.junit4.createComposeRule
import com.teanode.app.signin.SignInScreen
import com.teanode.app.signin.ServerSetupGuideScreen
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.rememberSnackbarController
import com.teanode.app.ui.theme.TeaNodeTheme
import androidx.compose.runtime.CompositionLocalProvider
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of the sign-in screen: empty, with a problem, connecting, adding an account, at the largest text, and the server guide. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class SignInScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f, serverText: String = "", problemText: String? = null, isConnecting: Boolean = false, isAdding: Boolean = false) {
        composeRule.setContent {
            TeaNodeTheme(look.appearance) {
                CompositionLocalProvider(LocalSnackbar provides rememberSnackbarController(null)) {
                    WithFontScale(fontScale) {
                        SignInScreen(
                            serverText = serverText, onServerTextChange = {}, problemText = problemText, isConnecting = isConnecting,
                            onSignIn = {}, onScan = {}, onPasteLink = {}, onOpenSetupGuide = {}, isAdding = isAdding, onCancel = {},
                        )
                    }
                }
            }
        }
    }

    @Test fun `sign in light`() { draw(Look.Light); composeRule.photograph("sign-in-light") }

    @Test fun `sign in dark`() { draw(Look.Dark); composeRule.photograph("sign-in-dark") }

    @Test fun `sign in with an address typed`() { draw(Look.Light, serverText = "mail.example.com"); composeRule.photograph("sign-in-typed-light") }

    @Test fun `sign in with a problem`() {
        draw(Look.Light, serverText = "http://mail.example.com", problemText = "That address is not encrypted. Use an https address.")
        composeRule.photograph("sign-in-problem-light")
    }

    @Test fun `sign in while connecting`() { draw(Look.Dark, serverText = "mail.example.com", isConnecting = true); composeRule.photograph("sign-in-connecting-dark") }

    @Test fun `sign in at the largest text`() { draw(Look.Light, fontScale = 2f); composeRule.photograph("sign-in-largest-text-light") }

    @Test fun `adding an account`() { draw(Look.Light, isAdding = true); composeRule.photograph("sign-in-adding-light") }

    @Test fun `server setup guide`() {
        composeRule.setContent { TeaNodeTheme(Look.Light.appearance) { ServerSetupGuideScreen(onBack = {}) } }
        composeRule.photograph("sign-in-setup-guide-light")
    }

    @Test fun `server setup guide dark`() {
        composeRule.setContent { TeaNodeTheme(Look.Dark.appearance) { ServerSetupGuideScreen(onBack = {}) } }
        composeRule.photograph("sign-in-setup-guide-dark")
    }
}
