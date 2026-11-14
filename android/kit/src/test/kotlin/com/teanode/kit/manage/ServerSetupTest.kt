package com.teanode.kit.manage

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ServerSetupTest {
    @Test
    fun theSubmissionAddressFollowsTheServerWhenEmpty() {
        val draft = SubmissionAccessDraft(SubmissionAccess(host = null, port = 0, effectiveHost = "mail.example.com", effectivePort = "587"))
        assertEquals("", draft.host)
        assertEquals("", draft.portText)
        assertEquals(JsonPrimitive(0), draft.variables["port"])
        val set = SubmissionAccessDraft("mail.example.net ", "5-8-7x")
        assertEquals(JsonPrimitive("mail.example.net"), set.variables["host"])
        assertEquals(JsonPrimitive(587), set.variables["port"])
    }

    @Test
    fun theMailProgramAddressHasAPlainAndATlsPort() {
        val draft = MailProgramAccessDraft(MailProgramAccess(host = "imap.example.com", port = 143, tlsPort = 993, effectiveHost = "imap.example.com", effectivePort = "143", effectiveTlsPort = "993"))
        assertEquals("143", draft.portText)
        assertEquals("993", draft.tlsPortText)
        assertEquals(JsonPrimitive(993), draft.variables["tlsPort"])
        assertFalse(MailProgramAccess(effectiveHost = "x").isListening)
        assertTrue(MailProgramAccess(effectiveTlsPort = "993").isListening)
    }

    @Test
    fun anAddressIsKnownWhenEitherKindIsGiven() {
        assertTrue(ServerAddresses(ipv4 = "192.0.2.10").isKnown)
        assertFalse(ServerAddresses(errorMessage = "no route").isKnown)
    }

    @Test
    fun aReverseNameProblemIsToldApart() {
        assertEquals(null, ServerSetupAdvice.reverseProblem(OutgoingIdentity(isConfirmed = true)))
        assertEquals(ServerSetupAdvice.ReverseProblem.NoReverseName, ServerSetupAdvice.reverseProblem(OutgoingIdentity(reverseName = "")))
        assertEquals(ServerSetupAdvice.ReverseProblem.DoesNotResolveBack, ServerSetupAdvice.reverseProblem(OutgoingIdentity(reverseName = "host.example.com")))
        assertEquals(null, ServerSetupAdvice.resolves(emptyList()))
        assertEquals("192.0.2.1, 192.0.2.2", ServerSetupAdvice.resolves(listOf("192.0.2.1", "192.0.2.2")))
    }

    private val section = TeaNodeJson.parseToJsonElement(
        """{"providers":[{"id":"idp","name":"Example sign-in","issuer":"https://login.example.net/","clientId":"abc","hasClientSecret":true,"groupsClaim":null,"createUsers":true}]}""",
    ).jsonObject

    @Test
    fun aProviderIsReadWithoutItsSecretAndKeepsItWhenNothingIsTyped() {
        val providers = SsoProviders.drafts(section)
        assertEquals(1, providers.size)
        assertTrue(providers[0].hasClientSecret)
        assertEquals("", providers[0].clientSecret)
        assertEquals(JsonNull, providers[0].parameters["clientSecret"])
        assertEquals(JsonPrimitive("new"), providers[0].copy(clientSecret = "new").parameters["clientSecret"])
        assertEquals(JsonNull, providers[0].parameters["groupsClaim"])
        assertEquals(JsonPrimitive(true), providers[0].parameters["createUsers"])
    }

    @Test
    fun aProviderNeedsItsFieldsAndTellsWhereToRegisterTheRedirect() {
        val empty = SsoProviderDraft(key = 1)
        assertFalse(empty.isComplete)
        assertFalse(SsoProviders.canSave(listOf(empty)))
        assertEquals("https://mail.example.com/api/v1/sso/id/callback", empty.redirectAddress("https://mail.example.com"))
        val filled = empty.copy(providerId = " idp ", name = "Example", issuer = "https://login.example.net/", clientId = "abc")
        assertTrue(filled.isComplete)
        assertEquals("https://mail.example.com/api/v1/sso/idp/callback", filled.redirectAddress("https://mail.example.com"))
        val sent = SsoProviders.changes(listOf(filled))["providers"] as JsonArray
        assertEquals(JsonPrimitive("idp"), (sent[0] as JsonObject)["id"])
    }
}
