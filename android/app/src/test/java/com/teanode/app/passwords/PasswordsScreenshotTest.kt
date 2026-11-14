package com.teanode.app.passwords

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.kit.passwords.PasswordsItemKind
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of every Passwords screen and state, in demo mode (a real unlock of invented, really encrypted items), light and dark, and the dense ones at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class PasswordsScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { WithFontScale(fontScale) { DemoHost(appearance = look.appearance) { content() } } }
        repeat(3) { composeRule.waitForIdle() }
    }

    /** Draws a screen once the invented Passwords has been opened, as a person's own unlock would. */
    @Composable
    private fun Opened(content: @Composable () -> Unit) {
        val model = rememberPasswordsModel()
        var isReady by remember { mutableStateOf(false) }
        LaunchedEffect(model) {
            model.start()
            isReady = model.phase == PasswordsPhase.Unlocked
        }
        if (isReady) content()
    }

    private fun photograph(name: String) = composeRule.photograph(name)

    @Test fun `intro light`() { draw(Look.Light) { PasswordsSection(PasswordsDemoStart.NotSetUp) }; photograph("passwords-intro-light") }

    @Test fun `intro dark`() { draw(Look.Dark) { PasswordsSection(PasswordsDemoStart.NotSetUp) }; photograph("passwords-intro-dark") }

    @Test fun `set up with a passphrase typed light`() {
        draw(Look.Light) { PasswordsSetUpScreen(demoPassphrase = PasswordsDemoStore.PASSPHRASE) }
        photograph("passwords-setup-light")
    }

    @Test fun `set up empty dark`() { draw(Look.Dark) { PasswordsSetUpScreen() }; photograph("passwords-setup-dark") }

    @Test fun `the recovery key after set up light`() {
        draw(Look.Light) {
            val model = rememberPasswordsModel(PasswordsDemoStart.NotSetUp)
            var done by remember { mutableStateOf(false) }
            LaunchedEffect(model.phase) { if (model.phase == PasswordsPhase.NotSetUp && !done) { done = true; model.setUp(PasswordsDemoStore.PASSPHRASE) } }
            PasswordsSection(PasswordsDemoStart.NotSetUp)
        }
        photograph("passwords-recovery-key-light")
    }

    @Test fun `locked light`() { draw(Look.Light) { PasswordsSection(PasswordsDemoStart.Locked) }; photograph("passwords-locked-light") }

    @Test fun `locked with the fingerprint dark`() { draw(Look.Dark) { PasswordsSection(PasswordsDemoStart.LockedWithBiometry) }; photograph("passwords-locked-biometry-dark") }

    @Test fun `a wrong passphrase is said in a snackbar`() {
        draw(Look.Light) { PasswordsSection(PasswordsDemoStart.Locked) }
        composeRule.onNodeWithText("Passphrase").performTextInput("not the passphrase")
        composeRule.onNodeWithText("Unlock").performClick()
        repeat(5) { composeRule.waitForIdle() }
        photograph("passwords-wrong-passphrase-light")
    }

    @Test fun `list light`() { draw(Look.Light) { PasswordsSection() }; photograph("passwords-list-light") }

    @Test fun `list dark`() { draw(Look.Dark) { PasswordsSection() }; photograph("passwords-list-dark") }

    @Test fun `list at the largest text`() { draw(Look.Light, 2f) { PasswordsSection() }; photograph("passwords-list-largest-text-light") }

    @Test fun `list filter menu`() {
        draw(Look.Light) { PasswordsSection() }
        composeRule.onNodeWithContentDescription("Show").performClick()
        composeRule.waitForIdle()
        photograph("passwords-list-filter-light")
    }

    @Test fun `list new item menu`() {
        draw(Look.Light) { PasswordsSection() }
        composeRule.onNodeWithContentDescription("New item").performClick()
        composeRule.waitForIdle()
        photograph("passwords-list-new-light")
    }

    @Test fun `list searched`() {
        draw(Look.Dark) { PasswordsSection() }
        composeRule.onNodeWithText("Search items").performTextInput("tea")
        composeRule.waitForIdle()
        photograph("passwords-list-search-dark")
    }

    @Test fun `list searched with no match`() {
        draw(Look.Light) { PasswordsSection() }
        composeRule.onNodeWithText("Search items").performTextInput("nothing like this")
        composeRule.waitForIdle()
        photograph("passwords-list-no-match-light")
    }

    @Test fun `a login light`() { draw(Look.Light) { Opened { PasswordsItemScreen(PasswordsDemoStore.mailLoginId) } }; photograph("passwords-login-light") }

    @Test fun `a login dark`() { draw(Look.Dark) { Opened { PasswordsItemScreen(PasswordsDemoStore.mailLoginId) } }; photograph("passwords-login-dark") }

    @Test fun `a login at the largest text`() { draw(Look.Light, 2f) { Opened { PasswordsItemScreen(PasswordsDemoStore.mailLoginId) } }; photograph("passwords-login-largest-text-light") }

    @Test fun `a login with the password shown`() {
        draw(Look.Light) { Opened { PasswordsItemScreen(PasswordsDemoStore.mailLoginId) } }
        composeRule.onNodeWithContentDescription("Show Password").performClick()
        composeRule.waitForIdle()
        photograph("passwords-login-revealed-light")
    }

    @Test fun `a login the agent may use`() { draw(Look.Light) { Opened { PasswordsItemScreen(PasswordsDemoStore.agentLoginId) } }; photograph("passwords-agent-login-light") }

    @Test fun `a card`() { draw(Look.Light) { Opened { PasswordsItemScreen(PasswordsDemoStore.cardId) } }; photograph("passwords-card-light") }

    @Test fun `a note dark`() { draw(Look.Dark) { Opened { PasswordsItemScreen(PasswordsDemoStore.noteId) } }; photograph("passwords-note-dark") }

    @Test fun `a deleted item`() { draw(Look.Light) { Opened { PasswordsItemScreen(PasswordsDemoStore.deletedItemId) } }; photograph("passwords-deleted-item-light") }

    @Test fun `recently deleted`() { draw(Look.Light) { Opened { PasswordsDeletedScreen() } }; photograph("passwords-deleted-light") }

    @Test fun `an item that is not here`() { draw(Look.Light) { Opened { PasswordsItemScreen("gone") } }; photograph("passwords-item-gone-light") }

    @Test fun `earlier versions`() { draw(Look.Light) { Opened { PasswordsVersionsScreen(PasswordsDemoStore.mailLoginId) } }; photograph("passwords-versions-light") }

    @Test fun `editor for a login`() { draw(Look.Light) { Opened { PasswordsEditorScreen(PasswordsItemKind.Login, PasswordsDemoStore.mailLoginId) } }; photograph("passwords-editor-login-light") }

    @Test fun `editor for a new card dark`() { draw(Look.Dark) { Opened { PasswordsEditorScreen(PasswordsItemKind.Card, null) } }; photograph("passwords-editor-card-dark") }

    @Test fun `editor for a new note`() { draw(Look.Light) { Opened { PasswordsEditorScreen(PasswordsItemKind.Note, null) } }; photograph("passwords-editor-note-light") }

    @Test fun `generator`() {
        draw(Look.Light) { Opened { PasswordsEditorScreen(PasswordsItemKind.Login, PasswordsDemoStore.mailLoginId, isGeneratorOpen = true) } }
        photograph("passwords-generator-light")
    }

    @Test fun `settings open light`() { draw(Look.Light) { Opened { PasswordsSettingsScreen() } }; photograph("passwords-settings-light") }

    @Test fun `settings open dark`() { draw(Look.Dark) { Opened { PasswordsSettingsScreen() } }; photograph("passwords-settings-dark") }

    @Test fun `settings while locked`() { draw(Look.Light) { PasswordsSettingsScreen() }; photograph("passwords-settings-locked-light") }
}
