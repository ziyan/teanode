package com.teanode.app.mail

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.kit.mail.MailFilter
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of every screen and state of Mail, in demo mode: light and dark, and the largest text for the dense ones. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class MailScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun show(look: Look, fontScale: Float = 1f, waitsFor: String, content: @Composable (MailModel) -> Unit) {
        composeRule.setContent {
            DemoHost(appearance = look.appearance) {
                WithFontScale(fontScale) { content(rememberMailModel()) }
            }
        }
        composeRule.waitUntil(10_000) { composeRule.onAllNodesWithText(waitsFor, substring = true).fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
    }

    private fun both(name: String, waitsFor: String, fontScale: Float = 1f, content: @Composable (MailModel) -> Unit) {
        show(Look.Light, fontScale, waitsFor, content)
        composeRule.photograph("mail-$name-light")
    }

    private val inbox = MailPlace.Folder("folder-inbox")

    @Composable
    private fun list(place: MailPlace, mail: MailModel, filter: MailFilter? = null, selecting: Boolean = false, words: String? = null) =
        ThreadListScreen(mail, place, onBack = {}, onOpenThread = { _, _ -> }, onCompose = {}, searchedWords = words, startingFilter = filter, startsSelecting = selecting)

    @Test fun `folders light`() = both("folders", "Receipts") { mail -> MailboxesScreen(mail, {}, {}, {}, {}, {}) }
    @Test fun `folders dark`() { show(Look.Dark, waitsFor = "Pinned") { mail -> MailboxesScreen(mail, {}, {}, {}, {}, {}) }; composeRule.photograph("mail-folders-dark") }
    @Test fun `folders largest text`() { show(Look.Light, 2f, "Pinned") { mail -> MailboxesScreen(mail, {}, {}, {}, {}, {}) }; composeRule.photograph("mail-folders-largest-text-light") }

    @Test fun `inbox light`() { show(Look.Light, waitsFor = "Lunch on Friday?") { mail -> list(inbox, mail) }; composeRule.photograph("mail-inbox-light") }
    @Test fun `inbox dark`() { show(Look.Dark, waitsFor = "Lunch on Friday?") { mail -> list(inbox, mail) }; composeRule.photograph("mail-inbox-dark") }
    @Test fun `inbox largest text`() { show(Look.Light, 2f, "Lunch on Friday?") { mail -> list(inbox, mail) }; composeRule.photograph("mail-inbox-largest-text-light") }
    @Test fun `inbox selecting`() { show(Look.Light, waitsFor = "Lunch on Friday?", content = { mail -> list(inbox, mail, selecting = true) }); composeRule.photograph("mail-inbox-selecting-light") }
    @Test fun `inbox narrowed to nothing`() { show(Look.Light, waitsFor = "Nothing matches") { mail -> list(inbox, mail, MailFilter(searchText = "zzzz")) }; composeRule.photograph("mail-inbox-nothing-matches-light") }
    @Test fun `starred light`() { show(Look.Light, waitsFor = "receipt") { mail -> list(MailPlace.Starred, mail) }; composeRule.photograph("mail-starred-light") }
    @Test fun `priority dark`() { show(Look.Dark, waitsFor = "Lunch on Friday?") { mail -> list(MailPlace.Priority, mail) }; composeRule.photograph("mail-priority-dark") }
    @Test fun `trash empty`() { show(Look.Light, waitsFor = "Nothing here") { mail -> list(MailPlace.Folder("folder-trash"), mail) }; composeRule.photograph("mail-trash-empty-light") }
    @Test fun `search everywhere`() { show(Look.Light, waitsFor = "Lunch on Friday?") { mail -> list(inbox, mail, words = "lunch") }; composeRule.photograph("mail-search-light") }

    @Composable
    private fun thread(itemId: String, mail: MailModel, everythingOpen: Boolean = false) =
        MailThreadScreen(mail, itemId, "folder-inbox", onBack = {}, onCompose = { _, _ -> }, startsWithEverythingOpen = everythingOpen)

    @Test fun `thread light`() { show(Look.Light, waitsFor = "Hi Alex") { mail -> thread("item-lunch", mail) }; composeRule.photograph("mail-thread-light") }
    @Test fun `thread dark`() { show(Look.Dark, waitsFor = "Hi Alex") { mail -> thread("item-lunch", mail) }; composeRule.photograph("mail-thread-dark") }
    @Test fun `thread largest text`() { show(Look.Light, 2f, "Hi Alex") { mail -> thread("item-lunch", mail) }; composeRule.photograph("mail-thread-largest-text-light") }
    @Test fun `thread with everything open`() { show(Look.Light, waitsFor = "Are you free for lunch", content = { mail -> thread("item-lunch", mail, true) }); composeRule.photograph("mail-thread-all-open-light") }
    @Test fun `thread with an attachment`() { show(Look.Light, waitsFor = "receipt-1042.pdf") { mail -> thread("item-invoice", mail) }; composeRule.photograph("mail-thread-attachment-light") }
    @Test fun `thread with an attachment dark`() { show(Look.Dark, waitsFor = "receipt-1042.pdf") { mail -> thread("item-invoice", mail) }; composeRule.photograph("mail-thread-attachment-dark") }

    @Composable
    private fun compose(kind: ComposeKind, itemId: String?, mail: MailModel) = ComposeScreen(mail, ComposeStart(kind, itemId), onClose = {})

    @Test fun `new message`() { show(Look.Light, waitsFor = "New message") { mail -> compose(ComposeKind.New, null, mail) }; composeRule.photograph("mail-compose-new-light") }
    @Test fun `reply`() { show(Look.Light, waitsFor = "Re: Lunch on Friday?") { mail -> compose(ComposeKind.Reply, "item-lunch", mail) }; composeRule.photograph("mail-compose-reply-light") }
    @Test fun `reply dark`() { show(Look.Dark, waitsFor = "Re: Lunch on Friday?") { mail -> compose(ComposeKind.Reply, "item-lunch", mail) }; composeRule.photograph("mail-compose-reply-dark") }
    @Test fun `reply largest text`() { show(Look.Light, 2f, "Re: Lunch on Friday?") { mail -> compose(ComposeKind.Reply, "item-lunch", mail) }; composeRule.photograph("mail-compose-reply-largest-text-light") }
    @Test fun `draft with an attachment`() { show(Look.Light, waitsFor = "agenda.txt") { mail -> compose(ComposeKind.Draft, "item-draft", mail) }; composeRule.photograph("mail-compose-draft-light") }
    @Test fun `forward`() { show(Look.Dark, waitsFor = "Fwd: Lunch on Friday?") { mail -> compose(ComposeKind.Forward, "item-lunch", mail) }; composeRule.photograph("mail-compose-forward-dark") }

    @Test fun `subscriptions`() { show(Look.Light, waitsFor = "Garden Weekly") { mail -> SubscriptionsScreen(mail, onBack = {}, onOpen = {}) }; composeRule.photograph("mail-subscriptions-light") }
    @Test fun `subscriptions dark`() { show(Look.Dark, waitsFor = "Garden Weekly") { mail -> SubscriptionsScreen(mail, onBack = {}, onOpen = {}) }; composeRule.photograph("mail-subscriptions-dark") }
    @Test fun `subscriptions left`() {
        show(Look.Light, waitsFor = "Unsubscribed") { mail -> SubscriptionsScreen(mail, onBack = {}, onOpen = {}) }
        composeRule.onNodeWithText("Unsubscribed · 1").performClick()
        composeRule.waitUntil(10_000) { composeRule.onAllNodesWithText("Coupon Club").fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
        composeRule.photograph("mail-subscriptions-left-light")
    }
    @Test fun `subscription reader`() { show(Look.Light, waitsFor = "Which bulbs to plant now") { mail -> SubscriptionReaderScreen(mail, "garden", onBack = {}, onChanged = {}) }; composeRule.photograph("mail-subscription-reader-light") }

    @Test fun `rules`() { show(Look.Light, waitsFor = "Receipts") { mail -> MailRulesScreen(mail, onBack = {}) }; composeRule.photograph("mail-rules-light") }
    @Test fun `rules dark`() { show(Look.Dark, waitsFor = "Receipts") { mail -> MailRulesScreen(mail, onBack = {}) }; composeRule.photograph("mail-rules-dark") }
    @Test fun `rules tried on recent mail`() {
        show(Look.Light, waitsFor = "Try on recent mail") { mail -> MailRulesScreen(mail, onBack = {}) }
        composeRule.onNodeWithText("Try on recent mail").performClick()
        composeRule.waitUntil(10_000) { composeRule.onAllNodesWithText("What the rules would do").fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
        composeRule.photograph("mail-rules-trial-light")
    }
    @Test fun `rule editor`() { show(Look.Light, waitsFor = "Add a rule") { mail -> MailRulesScreen(mail, onBack = {}, startsEditing = true) }; composeRule.photograph("mail-rule-editor-light") }

    @Test fun `mailbox settings`() { show(Look.Light, waitsFor = "Addresses:") { mail -> MailboxSettingsScreen(mail, {}, {}, {}, {}, {}, {}) }; composeRule.photograph("mail-settings-light") }
    @Test fun `mailbox settings dark`() { show(Look.Dark, waitsFor = "Addresses:") { mail -> MailboxSettingsScreen(mail, {}, {}, {}, {}, {}, {}) }; composeRule.photograph("mail-settings-dark") }
    @Test fun `name and signature`() { show(Look.Light, waitsFor = "Signature") { mail -> MailboxGeneralScreen(mail, onBack = {}) }; composeRule.photograph("mail-settings-general-light") }
    @Test fun `auto reply`() { show(Look.Light, waitsFor = "Reply automatically") { mail -> AutoReplyScreen(mail, onBack = {}) }; composeRule.photograph("mail-settings-auto-reply-light") }
    @Test fun `folders settings`() { show(Look.Light, waitsFor = "Travel") { mail -> MailFoldersScreen(mail, onBack = {}) }; composeRule.photograph("mail-settings-folders-light") }
    @Test fun `mail programs`() { show(Look.Light, waitsFor = "Laptop mail program") { mail -> MailAppPasswordsScreen(mail, onBack = {}) }; composeRule.photograph("mail-settings-programs-light") }
    @Test fun `mail programs dark`() { show(Look.Dark, waitsFor = "Laptop mail program") { mail -> MailAppPasswordsScreen(mail, onBack = {}) }; composeRule.photograph("mail-settings-programs-dark") }
}
