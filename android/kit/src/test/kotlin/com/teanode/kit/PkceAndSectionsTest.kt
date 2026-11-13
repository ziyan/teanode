package com.teanode.kit

import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class PkceTest {
    @Test
    fun theChallengeIsTheHashTheRfcGives() {
        // The example in RFC 7636, appendix B.
        assertEquals("E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", Pkce.challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
    }

    @Test
    fun randomTextIsUrlSafeAndNeverTheSameTwice() {
        val first = Pkce.randomText()
        assertEquals(43, first.length)
        assertTrue(first.all { character -> character.isLetterOrDigit() || character == '-' || character == '_' })
        assertNotEquals(first, Pkce.randomText())
    }
}

class QueryStringTest {
    @Test
    fun aFormIsEncodedInAStableOrder() {
        assertEquals("a=1&b=x%20y%26z&c=%E4%B8%AD", QueryString.form(mapOf("c" to "中", "b" to "x y&z", "a" to "1")))
    }

    @Test
    fun whatIsEncodedIsDecoded() {
        assertEquals(mapOf("server" to "https://mail.example.com:10443", "code" to "a b+c"), QueryString.parse("server=https%3A%2F%2Fmail.example.com%3A10443&code=a%20b%2Bc"))
        assertEquals(mapOf("flag" to ""), QueryString.parse("flag"))
        assertEquals("100%", QueryString.decode("100%"), "a lone percent sign is kept")
    }
}

class SectionFactsTest {
    private val offered = ServiceCapabilities.legacy.copy(isPhotosEnabled = true)

    private fun session(vararg permissions: String) = AccessSession("user-1", AccessSession.AccessSessionPermissions(permissions.toList()))

    @Test
    fun theTabBarKeepsKnownPartsOnceAndThreeAtMost() {
        assertEquals(listOf(AppSection.Agent, AppSection.Mail, AppSection.Photos), TabBarChoice.sections(TabBarChoice.defaultText))
        assertEquals(
            listOf(AppSection.Mail, AppSection.Finance, AppSection.Photos),
            TabBarChoice.sections("mail,mail,settings,nonsense,finance,photos,agent"),
        )
        assertEquals("agent,mail", TabBarChoice.text(listOf(AppSection.Agent, AppSection.Mail)))
    }

    @Test
    fun everyPartIsInOneGroupOrIsSettings() {
        val grouped = AppSectionGroup.entries.flatMap { group -> group.sections }
        assertEquals(AppSection.entries.filter { section -> section != AppSection.Settings }.toSet(), grouped.toSet())
        assertEquals(grouped.size, grouped.toSet().size)
    }

    @Test
    fun beforeTheServerHasSaidOnlySettingsIsOffered() {
        val facts = SectionFacts()
        assertEquals(listOf(AppSection.Settings), AppSection.entries.filter { section -> facts.isAvailable(section) })
    }

    @Test
    fun aPartTheServerOrThePermissionsTurnOffIsLeftOut() {
        val facts = SectionFacts(services = offered, session = session("location:use", "photos:use"))
        assertTrue(facts.isAvailable(AppSection.Mail))
        assertTrue(facts.isAvailable(AppSection.Location))
        assertFalse(facts.isAvailable(AppSection.Health))
        assertFalse(facts.isAvailable(AppSection.Coding), "no computer runs coding sessions")
        assertTrue(facts.copy(hasCodingComputers = true).isAvailable(AppSection.Coding))
        assertFalse(facts.copy(services = offered.copy(isPhotosEnabled = false)).isAvailable(AppSection.Photos))
        assertFalse(facts.copy(session = session()).isAvailable(AppSection.Photos), "someone who may not use Photos")
        assertTrue(facts.copy(session = AccessSession()).isAvailable(AppSection.Photos), "permissions that could not be read leave it offered")
        assertFalse(facts.copy(services = offered.copy(isAssistantEnabled = false)).isAvailable(AppSection.Goals))
        assertFalse(facts.copy(services = offered.copy(isAssistantEnabled = false)).isAvailable(AppSection.Agent))
        assertTrue(facts.copy(services = offered.copy(isAssistantEnabled = false), session = session("circle:use")).isAvailable(AppSection.Agent))
    }

    @Test
    fun aServerThatPredatesTheCapabilitiesQuestionOffersEverything() = runBlocking {
        val account = Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt", "", Long.MAX_VALUE / 4, "robin")
        val old = FakeServer { 200 to """{"errors":[{"message":"Cannot query field \"GetServiceCapabilities\" on type \"Query\"."}]}""" }
        assertEquals(ServiceCapabilities.legacy, Connection(account, old).serviceCapabilities())

        val beforePhotos = FakeServer { request ->
            if (request.body!!.toString(Charsets.UTF_8).contains("isPhotosEnabled")) {
                200 to """{"errors":[{"message":"Cannot query field \"isPhotosEnabled\" on type \"ServiceCapabilities\"."}]}"""
            } else {
                200 to """{"data":{"GetServiceCapabilities":{"isMailRoutingEnabled":true,"isMailboxEnabled":true,"isCalendarEnabled":false,"isRemindersEnabled":false,"isContactsEnabled":true,"isAssistantEnabled":true,"hasPendingServiceRestart":false,"hasCompletedSetup":true}}}"""
            }
        }
        val capabilities = Connection(account, beforePhotos).serviceCapabilities()
        assertFalse(capabilities.isPhotosEnabled)
        assertFalse(capabilities.isCalendarEnabled)
        assertEquals(2, beforePhotos.sent.size)
    }
}
