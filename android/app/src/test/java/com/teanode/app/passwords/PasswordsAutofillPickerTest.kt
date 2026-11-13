package com.teanode.app.passwords

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class PasswordsAutofillPickerTest {
    private val chrome = "com.android.chrome"
    private fun field(id: String, kind: FieldKind, webAddress: String? = "https://example.com", isVisible: Boolean = true) = FoundField(id, kind, webAddress, isVisible)

    @Test fun `a login form in a trusted browser is picked whole`() {
        val picked = AutofillFieldPicker.pick(listOf(field("user", FieldKind.UserName), field("secret", FieldKind.Password)), chrome)
        assertEquals(listOf("user"), picked.userNameIds)
        assertEquals(listOf("secret"), picked.passwordIds)
        assertEquals("https://example.com", picked.webAddress)
    }

    @Test fun `an app that is not a known browser gets nothing even when it claims a web domain`() {
        val picked = AutofillFieldPicker.pick(listOf(field("secret", FieldKind.Password, "https://bank.example")), "com.example.game")
        assertTrue(picked.passwordIds.isEmpty())
        assertNull(picked.webAddress)
        assertFalse(TrustedBrowsers.isTrusted(null))
    }

    @Test fun `an invisible field is not filled`() {
        val picked = AutofillFieldPicker.pick(listOf(field("secret", FieldKind.Password), field("trap", FieldKind.Password, isVisible = false)), chrome)
        assertEquals(listOf("secret"), picked.passwordIds)
    }

    @Test fun `a field of another frame is left alone and the address is the password field's own`() {
        val picked = AutofillFieldPicker.pick(
            listOf(
                field("secret", FieldKind.Password, "https://login.example.com"),
                field("embedded user", FieldKind.UserName, "https://ads.example.net"),
                field("embedded secret", FieldKind.Password, "https://ads.example.net"),
            ),
            chrome,
        )
        assertEquals("https://login.example.com", picked.webAddress)
        assertEquals(listOf("secret"), picked.passwordIds)
        assertTrue(picked.userNameIds.isEmpty())
    }

    @Test fun `a form with only a user name is not offered`() {
        assertNull(AutofillFieldPicker.pick(listOf(field("user", FieldKind.UserName)), chrome).webAddress)
    }

    @Test fun `a field with no page address is not filled`() {
        assertNull(AutofillFieldPicker.pick(listOf(field("secret", FieldKind.Password, null)), chrome).webAddress)
    }
}
