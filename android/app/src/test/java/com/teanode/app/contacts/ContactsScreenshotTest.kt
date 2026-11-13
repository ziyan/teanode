package com.teanode.app.contacts

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.performClick
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.session.LocalConnection
import com.teanode.kit.contacts.PersonCardTarget
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of every screen and state of Contacts and the person's card, in demo mode: light and dark, and the largest text for the dense ones. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ContactsScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun show(look: Look, fontScale: Float = 1f, waitsFor: String, content: @Composable (ContactsModel) -> Unit) {
        composeRule.setContent {
            DemoHost(appearance = look.appearance) {
                WithFontScale(fontScale) { content(rememberContactsModel()) }
            }
        }
        composeRule.waitUntil(10_000) { composeRule.onAllNodesWithText(waitsFor, substring = true).fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
    }

    @Composable
    private fun list(model: ContactsModel) = ContactsScreen(model, onOpenContact = {}, onNewContact = {}, onEditCircleContact = {}, onImport = {})

    @Test fun `list light`() { show(Look.Light, waitsFor = "Sam Fixture") { model -> list(model) }; composeRule.photograph("contacts-list-light") }
    @Test fun `list dark`() { show(Look.Dark, waitsFor = "Sam Fixture") { model -> list(model) }; composeRule.photograph("contacts-list-dark") }
    @Test fun `list largest text`() { show(Look.Light, 2f, "Alex Example") { model -> list(model) }; composeRule.photograph("contacts-list-largest-text-light") }
    @Test fun `circle book`() {
        show(Look.Light, waitsFor = "Sam Fixture") { model -> list(model) }
        composeRule.onNodeWithContentDescription("Address book").performClick()
        composeRule.onAllNodesWithText("Home").fetchSemanticsNodes()
        composeRule.onAllNodesWithText("Home")[0].performClick()
        composeRule.waitUntil(10_000) { composeRule.onAllNodesWithText("Home Plumbing").fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
        composeRule.photograph("contacts-circle-book-light")
    }

    @Composable
    private fun card(model: ContactsModel, target: PersonCardTarget) = PersonCardScreen(model, target, onBack = {})

    @Test fun `person card linked light`() { show(Look.Light, waitsFor = "Fixture Park") { model -> card(model, PersonCardTarget.Account("user-sam")) }; composeRule.photograph("contacts-person-card-light") }
    @Test fun `person card linked dark`() { show(Look.Dark, waitsFor = "Fixture Park") { model -> card(model, PersonCardTarget.Account("user-sam")) }; composeRule.photograph("contacts-person-card-dark") }
    @Test fun `person card largest text`() { show(Look.Light, 2f, "Sam Fixture") { model -> card(model, PersonCardTarget.Account("user-sam")) }; composeRule.photograph("contacts-person-card-largest-text-light") }
    @Test fun `person card without a contact`() { show(Look.Light, waitsFor = "Link a contact") { model -> card(model, PersonCardTarget.Account("user-alex")) }; composeRule.photograph("contacts-person-card-unlinked-light") }
    @Test fun `person card without a contact dark`() { show(Look.Dark, waitsFor = "Link a contact") { model -> card(model, PersonCardTarget.Account("user-alex")) }; composeRule.photograph("contacts-person-card-unlinked-dark") }
    @Test fun `person card contact only`() { show(Look.Light, waitsFor = "Your receipt for order 1042") { model -> card(model, PersonCardTarget.Contact("book-own", "contact-books")) }; composeRule.photograph("contacts-person-card-contact-only-light") }

    @Test fun `new contact`() { show(Look.Light, waitsFor = "Organization") { model -> ContactEditorScreen(model, null, onBack = {}) }; composeRule.photograph("contacts-editor-new-light") }
    @Test fun `edit contact`() { show(Look.Light, waitsFor = "Likes tea") { model -> ContactEditorScreen(model, "contact-sam", onBack = {}) }; composeRule.photograph("contacts-editor-edit-light") }
    @Test fun `edit contact dark`() { show(Look.Dark, waitsFor = "Likes tea") { model -> ContactEditorScreen(model, "contact-sam", onBack = {}) }; composeRule.photograph("contacts-editor-edit-dark") }

    @Test fun `account picker`() {
        show(Look.Light, waitsFor = "Choose who Pat Sample is") { model ->
            val contact = com.teanode.kit.contacts.ContactDetail(id = "contact-pat", name = "Pat Sample")
            ContactAccountPickerScreen(model, contact, onBack = {}, onLinked = {})
        }
        composeRule.photograph("contacts-account-picker-light")
    }

    @Test fun `contact picker`() {
        show(Look.Light, waitsFor = "Pat Sample") { model ->
            val person = com.teanode.kit.contacts.PersonCardMember(userId = "user-pat", username = "pat", name = "Pat Sample")
            PersonContactPickerScreen(model, person, onBack = {}, onLinked = {})
        }
        composeRule.photograph("contacts-contact-picker-light")
    }

    private fun importModel(model: ContactsModel, hasAccess: Boolean, found: List<PhoneContact> = emptyList()) =
        ContactImportModel(model, "book-own", { found }, { hasAccess })

    private val phoneContacts = listOf(
        PhoneContact("p1", "Ada Example", "ada@example.com", com.teanode.kit.contacts.ContactIdentity("Ada Example", listOf("ada@example.com"), emptyList()), null),
        PhoneContact("p2", "Sam Fixture", "sam@example.org", com.teanode.kit.contacts.ContactIdentity("Sam Fixture", listOf("sam@example.org"), listOf("+1 (555) 0142")), null),
        PhoneContact("p3", "Ben Sample", "555 010 1234", com.teanode.kit.contacts.ContactIdentity("Ben Sample", emptyList(), listOf("555 010 1234")), null),
    )

    @Composable
    private fun importing(model: ContactsModel, hasAccess: Boolean, found: List<PhoneContact>) {
        val import = androidx.compose.runtime.remember { importModel(model, hasAccess, found) }
        androidx.compose.runtime.LaunchedEffect(import) { import.load() }
        ContactImportContent(import, onBack = {}, onOpenSettings = {}, onImport = {})
    }

    @Test fun `import list`() { show(Look.Light, waitsFor = "Ada Example") { model -> importing(model, true, phoneContacts) }; composeRule.photograph("contacts-import-light") }
    @Test fun `import list dark`() { show(Look.Dark, waitsFor = "Already in TeaNode") { model -> importing(model, true, phoneContacts) }; composeRule.photograph("contacts-import-dark") }
    @Test fun `import denied`() { show(Look.Light, waitsFor = "Contacts access is off") { model -> importing(model, false, emptyList()) }; composeRule.photograph("contacts-import-denied-light") }
    @Test fun `import empty`() { show(Look.Dark, waitsFor = "No contacts on this phone") { model -> importing(model, true, emptyList()) }; composeRule.photograph("contacts-import-empty-dark") }

    @Test fun `visits`() { show(Look.Light, waitsFor = "Fixture Park") { _ -> PersonVisitsScreen(LocalConnection.current, "user-sam", onBack = {}) }; composeRule.photograph("contacts-visits-light") }
    @Test fun `visits dark`() { show(Look.Dark, waitsFor = "Corner Cafe") { _ -> PersonVisitsScreen(LocalConnection.current, "user-sam", onBack = {}) }; composeRule.photograph("contacts-visits-dark") }
}
