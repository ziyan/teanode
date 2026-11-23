package com.teanode.kit.passwords

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.boolean
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

class PasswordsSiteMatchTest {
    @Test fun `the same host matches whatever the scheme www port or path`() {
        for (site in listOf("https://example.com", "example.com", "http://example.com/sign-in?next=1", "https://www.example.com", "EXAMPLE.com.", "https://example.com:8443/", "https://ada@example.com/", "//example.com", " example.com/path ")) {
            assertTrue(PasswordsSiteMatch.isMatch(listOf(site), "example.com"), site)
            assertTrue(PasswordsSiteMatch.isMatch(listOf(site), "https://www.example.com/login"), site)
        }
    }

    @Test fun `another host does not match`() {
        val requested = "example.com"
        assertFalse(PasswordsSiteMatch.isMatch(listOf("https://login.example.com"), requested))
        assertFalse(PasswordsSiteMatch.isMatch(listOf("https://example.com.evil.test"), requested))
        assertFalse(PasswordsSiteMatch.isMatch(listOf("https://evilexample.test"), requested))
        assertFalse(PasswordsSiteMatch.isMatch(listOf("https://example.org"), requested))
        // A user name that looks like the host does not make it the host.
        assertFalse(PasswordsSiteMatch.isMatch(listOf("https://example.com@evil.test/"), requested))
        assertFalse(PasswordsSiteMatch.isMatch(emptyList(), requested))
        assertFalse(PasswordsSiteMatch.isMatch(listOf("ftp://example.com"), requested))
        assertFalse(PasswordsSiteMatch.isMatch(listOf(""), requested))
        assertFalse(PasswordsSiteMatch.isMatch(listOf("https://example.com"), ""))
    }

    @Test fun `related hosts are below the exact ones and lookalikes are nothing`() {
        assertEquals(PasswordsSiteMatchLevel.RelatedHost, PasswordsSiteMatch.level("https://example.com", "login.example.com"))
        assertEquals(PasswordsSiteMatchLevel.RelatedHost, PasswordsSiteMatch.level("https://login.example.com", "example.com"))
        assertEquals(PasswordsSiteMatchLevel.None, PasswordsSiteMatch.level("https://example.com", "example.com.evil.test"))
        assertEquals(PasswordsSiteMatchLevel.ExactHost, PasswordsSiteMatch.level("https://www.example.com", "example.com"))
        assertEquals(PasswordsSiteMatchLevel.None, PasswordsSiteMatch.level("localhost", "app.localhost"))
        assertEquals(PasswordsSiteMatchLevel.RelatedHost, PasswordsSiteMatch.level(listOf("https://example.org", "https://login.example.com"), "example.com"))
    }

    @Test fun `host parsing`() {
        assertEquals("example.com", PasswordsSiteMatch.normalizedHost("https://www.Example.COM:8080/a?b#c"))
        assertEquals("www.test", PasswordsSiteMatch.normalizedHost("www.test"))
        assertEquals("[::1]", PasswordsSiteMatch.normalizedHost("http://[::1]:3000/"))
        assertEquals("localhost", PasswordsSiteMatch.normalizedHost("localhost:3000"))
        assertNull(PasswordsSiteMatch.normalizedHost("androidapp://com.example.app"))
        assertNull(PasswordsSiteMatch.normalizedHost("https://"))
    }

    private data class Login(val name: String, val sites: List<String>)

    @Test fun `sorting puts the exact host first then related and keeps the order within a level`() {
        val logins = listOf(
            Login("Parent", listOf("https://example.com")), Login("Other", listOf("https://example.org")),
            Login("Exact B", listOf("https://login.example.com/sign-in")), Login("Exact A", listOf("login.example.com")),
            Login("Lookalike", listOf("https://login.example.com.evil.test")),
        )
        val sorted = PasswordsSiteMatch.sorted(logins, "https://login.example.com/") { login -> login.sites }
        assertEquals(listOf("Exact B", "Exact A", "Parent"), sorted.map { login -> login.name })
        val everything = PasswordsSiteMatch.sorted(logins, "login.example.com", isKeepingOthers = true) { login -> login.sites }
        assertEquals(listOf("Exact B", "Exact A", "Parent", "Other", "Lookalike"), everything.map { login -> login.name })
    }

    @Test fun `an origin needs the same port and an http page needs an http site`() {
        assertTrue(PasswordsSiteMatch.isOrigin(listOf("example.com"), "https://example.com/sign-in"))
        assertTrue(PasswordsSiteMatch.isOrigin(listOf("example.com"), "https://example.com:443/"))
        assertFalse(PasswordsSiteMatch.isOrigin(listOf("example.com"), "https://example.com:8443/"))
        assertTrue(PasswordsSiteMatch.isOrigin(listOf("https://example.com:8443"), "https://example.com:8443/x"))
        assertFalse(PasswordsSiteMatch.isOrigin(listOf("https://example.com:8443"), "https://example.com/"))
        assertFalse(PasswordsSiteMatch.isOrigin(listOf("https://example.com"), "http://example.com/"))
        assertTrue(PasswordsSiteMatch.isOrigin(listOf("http://example.com"), "http://example.com/"))
        assertFalse(PasswordsSiteMatch.isOrigin(listOf("http://example.com"), "https://example.com/"))
        assertFalse(PasswordsSiteMatch.isOrigin(listOf("https://example.com:0"), "https://example.com/"))
        assertFalse(PasswordsSiteMatch.isOrigin(emptyList(), "https://example.com/"))
        assertFalse(PasswordsSiteMatch.isOrigin(listOf("example.com"), "about:blank"))
    }

    @Test fun `a full address demotes the same host on another port or scheme but a bare domain does not`() {
        val site = "https://example.com"
        assertEquals(PasswordsSiteMatchLevel.ExactHost, PasswordsSiteMatch.level(site, "https://example.com/", true))
        assertEquals(PasswordsSiteMatchLevel.None, PasswordsSiteMatch.level(site, "https://example.com:8443/", true))
        assertEquals(PasswordsSiteMatchLevel.None, PasswordsSiteMatch.level(site, "http://example.com/", true))
        // A domain identifier carries a host and nothing else, so only the host is compared.
        assertEquals(PasswordsSiteMatchLevel.ExactHost, PasswordsSiteMatch.level(site, "example.com", false))
        // A related host stays related.
        assertEquals(PasswordsSiteMatchLevel.RelatedHost, PasswordsSiteMatch.level(site, "https://login.example.com/", true))
        val logins = listOf("Other port" to listOf("https://example.com:8443"), "Exact" to listOf("example.com"), "Parent" to listOf("example.org", "https://example.com"))
        val sorted = PasswordsSiteMatch.sorted(logins, "https://example.com/", isPageAddress = true) { entry -> entry.second }
        assertEquals(listOf("Exact", "Parent"), sorted.map { entry -> entry.first })
    }

    /** internal/passwords/testdata/sitematch.json, which the Go package and the extension read too: all must answer every case the same way. */
    @Test fun `the shared site match cases`() {
        val file = File("../../internal/passwords/testdata/sitematch.json")
        assertTrue(file.exists(), "sitematch.json is not at ${file.absolutePath}")
        val cases = Json.parseToJsonElement(file.readText()) as JsonObject
        val hostCases = cases["normalizedHostCases"] as JsonArray
        val originCases = cases["originCases"] as JsonArray
        assertFalse(hostCases.isEmpty())
        assertFalse(originCases.isEmpty())
        for (element in hostCases) {
            val each = element as JsonObject
            val address = (each["site"] as JsonPrimitive).content
            assertEquals((each["expectedHost"] as JsonPrimitive).content, PasswordsSiteMatch.normalizedHost(address) ?: "", "normalizedHost of $address")
        }
        for (element in originCases) {
            val each = element as JsonObject
            val sites = (each["sites"] as JsonArray).map { site -> (site as JsonPrimitive).content }
            assertEquals(
                (each["isMatch"] as JsonPrimitive).boolean,
                PasswordsSiteMatch.isOrigin(sites, (each["page"] as JsonPrimitive).content),
                (each["name"] as JsonPrimitive).content,
            )
        }
    }

    @Test fun `identity records of a login`() {
        val login = PasswordsLoginFields(
            name = "Example", userName = "ada@example.com", password = "invented",
            sites = listOf("https://www.example.com", "example.com/other", "https://shop.example.org"),
            passkeys = listOf(
                PasswordsPasskeyFields(
                    credentialId = "AQID", relyingPartyId = "example.com", userHandle = "BAUG", userName = "ada", userDisplayName = "Ada",
                    privateKey = "key", createdAt = "2026-10-20T10:00:00Z",
                ),
                PasswordsPasskeyFields(credentialId = "not base64!", relyingPartyId = "example.com"),
            ),
        )
        val records = PasswordsIdentityRecords.records("item-one", login)
        assertEquals(listOf("example.com", "shop.example.org"), records.passwordIdentities.map { identity -> identity.serviceDomain })
        assertEquals(setOf("item-one"), records.passwordIdentities.map { identity -> identity.itemId }.toSet())
        assertEquals("ada@example.com", records.passwordIdentities.first().userName)
        assertEquals(1, records.passkeyIdentities.size)
        assertArrayEquals(byteArrayOf(1, 2, 3), records.passkeyIdentities.first().credentialId)
        assertArrayEquals(byteArrayOf(4, 5, 6), records.passkeyIdentities.first().userHandle)
        assertEquals("example.com", records.passkeyIdentities.first().relyingPartyId)
        assertEquals("ada", records.passkeyIdentities.first().userName)
    }

    @Test fun `a login without a password adds no password identity`() {
        val login = PasswordsLoginFields(name = "Example", userName = "ada", sites = listOf("https://example.com"))
        assertEquals(emptyList<PasswordsPasswordIdentity>(), PasswordsIdentityRecords.records("item-one", login).passwordIdentities)
    }
}
