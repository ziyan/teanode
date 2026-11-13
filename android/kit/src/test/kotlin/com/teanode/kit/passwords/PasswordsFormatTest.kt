package com.teanode.kit.passwords

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class PasswordsFormatTest {
    private fun item(fields: PasswordsItemFields, ownerName: String = "", isSharedWithMe: Boolean = false) = PasswordItem(
        id = "item", fields = fields, ownerUserId = "owner", ownerName = ownerName, isSharedWithMe = isSharedWithMe, revision = 1, isUsableByAgent = false,
        lastUsedByAgentAtEpochMS = null, deletedAtEpochMS = null, createdAtEpochMS = null, updatedAtEpochMS = null, shares = emptyList(), needsNewKey = false,
        sharingCheck = if (isSharedWithMe) PasswordsSharingCheck.OwnedByAnotherPerson else PasswordsSharingCheck.Verified,
    )

    private val login = item(PasswordsItemFields.Login(PasswordsLoginFields(name = "Example", userName = "ada@example.com", password = "secret value", sites = listOf("https://example.com"), folder = "Work")))
    private val card = item(PasswordsItemFields.Card(PasswordsCardFields(name = "Card", cardholderName = "Ada Example", number = "4111 1111 1111 4242", securityCode = "123", brand = "Visa")))
    private val note = item(PasswordsItemFields.Note(PasswordsNoteFields(name = "Router", text = "\nfirst line\nsecond line")))

    @Test fun `a list line is the user name the end of the card or the first line of the note`() {
        assertEquals("ada@example.com", login.subtitle)
        assertEquals("•••• 4242", card.subtitle)
        assertEquals("first line", note.subtitle)
    }

    @Test fun `a search matches names sites and holders but never a password or a card number`() {
        assertTrue(login.matches("exam"))
        assertTrue(login.matches("EXAMPLE.com"))
        assertTrue(login.matches("work"))
        assertFalse(login.matches("secret"))
        assertTrue(card.matches("visa"))
        assertFalse(card.matches("4242"))
        assertFalse(card.matches("123"))
        assertTrue(login.matches("   "))
        assertTrue(item(login.fields, ownerName = "Alex", isSharedWithMe = true).matches("alex"))
    }

    @Test fun `filters`() {
        assertTrue(PasswordsFilter.Everything.includes(note))
        assertTrue(PasswordsFilter.Kind(PasswordsItemKind.Login).includes(login))
        assertFalse(PasswordsFilter.Kind(PasswordsItemKind.Login).includes(card))
        assertTrue(PasswordsFilter.Folder("Work").includes(login))
        assertFalse(PasswordsFilter.Folder("Work").includes(note))
        assertFalse(PasswordsFilter.Shared.includes(login))
        assertTrue(PasswordsFilter.Shared.includes(item(login.fields, "Alex", true)))
        assertFalse(PasswordsFilter.UsableByAgent.includes(login))
        assertEquals(listOf("Work"), listOf(login, card, note).folders())
    }

    @Test fun `card numbers expiries and codes`() {
        assertEquals("•••• 4242", PasswordsFormat.maskedCardNumber("4111-1111-1111-4242"))
        assertEquals("••••", PasswordsFormat.maskedCardNumber("123"))
        assertEquals("", PasswordsFormat.maskedCardNumber("abc"))
        assertEquals("4111 1111 1111 4242", PasswordsFormat.groupedCardNumber("4111111111114242"))
        assertEquals("09/29", PasswordsFormat.expiryText("9", "2029"))
        assertEquals("12/30", PasswordsFormat.expiryText("12", "30"))
        assertEquals("", PasswordsFormat.expiryText(" ", ""))
        assertEquals("123 456", PasswordsFormat.groupedOneTimeCode("123456"))
        assertEquals("1234 5678", PasswordsFormat.groupedOneTimeCode("12345678"))
    }

    @Test fun `the recovery key in rows of four groups`() {
        val text = PasswordsCrypto.formatRecoveryKey(ByteArray(32))
        val lines = PasswordsFormat.recoveryKeyLines(text)
        assertEquals(listOf(4, 4, 4, 1), lines.map { line -> line.split(" ").size })
    }

    @Test fun `a site opens as a web address`() {
        assertEquals("https://example.com/sign-in", PasswordsFormat.urlOfSite("example.com/sign-in"))
        assertEquals("http://example.com", PasswordsFormat.urlOfSite(" http://example.com "))
        assertNull(PasswordsFormat.urlOfSite("ftp://example.com"))
        assertNull(PasswordsFormat.urlOfSite(""))
        assertEquals("mail.example.com", PasswordsFormat.shownSite("https://mail.example.com/"))
    }
}
