package com.teanode.kit.passwords

import org.junit.jupiter.api.Test
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue

class PasswordsAutofillIndexTest {
    private fun login(id: String, vararg sites: String, userName: String = "", hasPassword: Boolean = true, hasOneTimeCode: Boolean = false) =
        PasswordsAutofillLogin(id, id, userName, sites.toList(), hasPassword, hasOneTimeCode, null)

    private val page = "https://login.example.com"
    private val logins = listOf(
        login("Exact", "https://login.example.com"),
        login("Parent", "https://example.com"),
        login("Elsewhere", "https://example.org", userName = "alex"),
        login("Lookalike", "https://login.example.com.evil.test"),
        login("No password", "https://login.example.com", hasPassword = false),
    )

    @Test
    fun `the exact origin is suggested, a related host sits under its own heading and the rest follow`() {
        val sections = PasswordsAutofillIndex.sections(logins, PasswordsAutofillIndex.levels(logins, page), "", false)
        assertEquals(listOf(PasswordsAutofillSectionKind.Suggested, PasswordsAutofillSectionKind.Related, PasswordsAutofillSectionKind.All), sections.map { section -> section.kind })
        assertEquals(listOf("Exact"), sections[0].logins.map { login -> login.itemId })
        assertEquals(listOf("Parent"), sections[1].logins.map { login -> login.itemId })
        assertEquals(listOf("Elsewhere", "Lookalike"), sections[2].logins.map { login -> login.itemId })
        assertTrue(sections[2].hasHeading)
    }

    @Test
    fun `an app names no site, so everything is under one list with no heading and nothing is asked`() {
        val levels = PasswordsAutofillIndex.levels(logins, null)
        assertTrue(levels.isEmpty())
        val sections = PasswordsAutofillIndex.sections(logins, levels, "", false)
        assertEquals(1, sections.size)
        assertFalse(sections[0].hasHeading)
        assertEquals(4, sections[0].logins.size)
        assertFalse(PasswordsAutofillIndex.isSiteMismatch(PasswordsSiteMatchLevel.None, null))
    }

    @Test
    fun `a search looks across every site and suggests nothing`() {
        val levels = PasswordsAutofillIndex.levels(logins, page)
        val byUser = PasswordsAutofillIndex.sections(logins, levels, " ALEX ", false)
        assertEquals(listOf("Elsewhere"), byUser.single().logins.map { login -> login.itemId })
        assertFalse(byUser.single().hasHeading)
        val bySite = PasswordsAutofillIndex.sections(logins, levels, "example.com", false)
        assertEquals(listOf("Exact", "Lookalike", "Parent"), bySite.single().logins.map { login -> login.itemId }.sorted())
        assertTrue(PasswordsAutofillIndex.sections(logins, levels, "nothing like it", false).isEmpty())
    }

    @Test
    fun `a one-time code list holds only the logins that have a code`() {
        val withCode = listOf(login("Has code", "https://example.com", hasOneTimeCode = true), login("No code", "https://example.com"))
        val sections = PasswordsAutofillIndex.sections(withCode, PasswordsAutofillIndex.levels(withCode, "https://example.com"), "", true)
        assertEquals(listOf("Has code"), sections.single().logins.map { login -> login.itemId })
    }

    @Test
    fun `only an exact origin is filled without asking`() {
        assertFalse(PasswordsAutofillIndex.isSiteMismatch(PasswordsSiteMatchLevel.ExactHost, page))
        assertTrue(PasswordsAutofillIndex.isSiteMismatch(PasswordsSiteMatchLevel.RelatedHost, page))
        assertTrue(PasswordsAutofillIndex.isSiteMismatch(PasswordsSiteMatchLevel.None, page))
        assertEquals("login.example.com", PasswordsAutofillIndex.requestedSiteText("https://login.example.com:8443/in"))
        assertNull(PasswordsAutofillIndex.requestedSiteText(null))
        assertEquals("example.com, example.org", PasswordsAutofillIndex.savedSitesText(login("Two", "https://www.example.com", "example.org/x")))
        assertEquals("", PasswordsAutofillIndex.savedSitesText(login("None")))
    }
}
