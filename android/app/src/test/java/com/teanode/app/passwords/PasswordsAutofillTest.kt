package com.teanode.app.passwords

import com.teanode.kit.passwords.PasswordItem
import com.teanode.kit.passwords.PasswordsItemFields
import com.teanode.kit.passwords.PasswordsLoginFields
import com.teanode.kit.passwords.PasswordsNoteFields
import com.teanode.kit.passwords.PasswordsSharingCheck
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class PasswordsAutofillTest {
    private fun item(name: String, fields: PasswordsItemFields, deletedAtEpochMS: Long? = null) = PasswordItem(
        id = name, fields = fields, ownerUserId = "owner", ownerName = "", isSharedWithMe = false, revision = 1, isUsableByAgent = false,
        lastUsedByAgentAtEpochMS = null, deletedAtEpochMS = deletedAtEpochMS, createdAtEpochMS = null, updatedAtEpochMS = null, shares = emptyList(),
        needsNewKey = false, sharingCheck = PasswordsSharingCheck.Verified,
    )

    private fun login(name: String, vararg sites: String, deletedAtEpochMS: Long? = null) =
        item(name, PasswordsItemFields.Login(PasswordsLoginFields(name = name, sites = sites.toList())), deletedAtEpochMS)

    @Test fun `the exact origin comes first then related hosts and never a lookalike`() {
        val items = listOf(
            login("Parent", "https://example.com"), login("Exact B", "https://login.example.com/sign-in"), login("Exact A", "login.example.com"),
            login("Lookalike", "https://login.example.com.evil.test"), login("Other port", "https://login.example.com:8443"), login("Other", "https://example.org"),
        )
        assertEquals(listOf("Exact A", "Exact B", "Parent"), loginsFor(items, "https://login.example.com").map { entry -> entry.name })
    }

    @Test fun `a deleted item a note and a native app get nothing`() {
        val note = item("Note", PasswordsItemFields.Note(PasswordsNoteFields(name = "Note")))
        val items = listOf(login("Deleted", "https://example.com", deletedAtEpochMS = 1L), note, login("Live", "https://example.com"))
        assertEquals(listOf("Live"), loginsFor(items, "https://example.com").map { entry -> entry.name })
        assertTrue(loginsFor(items, null).isEmpty())
    }
}
