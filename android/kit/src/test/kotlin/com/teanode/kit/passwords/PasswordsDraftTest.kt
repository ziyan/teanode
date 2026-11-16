package com.teanode.kit.passwords

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class PasswordsDraftTest {
    @Test fun `a login draft is trimmed and its sites are one a line`() {
        val draft = PasswordsDraft(
            name = "  Example ", userName = " ada@example.com ", password = " keep spaces ", sitesText = "https://example.com\n\n  shop.example.org  \n", folder = " Work ",
        )
        val login = (draft.fields(PasswordsItemKind.Login) as PasswordsItemFields.Login).fields
        assertEquals("Example", login.name)
        assertEquals("ada@example.com", login.userName)
        assertEquals(" keep spaces ", login.password)
        assertEquals(listOf("https://example.com", "shop.example.org"), login.sites)
        assertEquals("Work", login.folder)
    }

    @Test fun `a card keeps only digits and spaces of its number`() {
        val card = (PasswordsDraft(name = "Card", number = " 4111-1111 1111 4242 ").fields(PasswordsItemKind.Card) as PasswordsItemFields.Card).fields
        assertEquals("41111111 1111 4242", card.number)
    }

    @Test fun `a one time code is an address or a secret made into one`() {
        assertEquals("", PasswordsDraft.oneTimeCodeUri("  ", "Example"))
        assertEquals("otpauth://totp/Example?secret=JBSWY3DPEHPK3PXP", PasswordsDraft.oneTimeCodeUri("otpauth://totp/Example?secret=JBSWY3DPEHPK3PXP", "Example"))
        assertEquals("otpauth://totp/Example%20Site?secret=JBSWY3DPEHPK3PXP", PasswordsDraft.oneTimeCodeUri("jbsw y3dp-ehpk 3pxp", "Example Site"))
        assertThrows<PasswordsDraftException> { PasswordsDraft.oneTimeCodeUri("not a secret!", "Example") }
        assertThrows<PasswordsDraftException> { PasswordsDraft.oneTimeCodeUri("otpauth://hotp/a?secret=JBSWY3DPEHPK3PXP", "Example") }
    }

    @Test fun `an edit is rebased onto a fresher copy and keeps only what the person changed`() {
        val original = PasswordsDraft(name = "Example", userName = "ada", password = "old")
        val mine = original.copy(password = "my new password")
        val fresh = PasswordsDraft(name = "Example renamed on another device", userName = "ada", password = "old")
        val rebased = mine.rebased(original, fresh)
        assertEquals("Example renamed on another device", rebased.name)
        assertEquals("my new password", rebased.password)
    }
}
