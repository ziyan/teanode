package com.teanode.kit

import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

@Serializable
private class Echo(val GetSession: Map<String, String>)

class SignInTest {
    private val server = ServerAddress.parse("mail.example.com")

    private fun signedIn(accessToken: String = "tnt", expiresInMS: Long = Long.MAX_VALUE / 4) =
        Account(server, "a-client", accessToken, "", expiresInMS, "robin")

    @Test
    fun signingInWithAPasswordAsksForTheWholeAccountAndExchangesTheCode() = runBlocking {
        val signIn = SignIn(server, FakeServer { 404 to "{}" })
        val address = signIn.authorizationAddress("a-client", "a-challenge", "a-state")
        val items = QueryString.parse(address.substringAfter('?'))
        assertTrue(address.startsWith("https://mail.example.com/oauth/authorize?"))
        assertEquals(appRedirectAddress, items["redirect_uri"])
        assertEquals("S256", items["code_challenge_method"])
        assertEquals("https://mail.example.com/api/v1", items["resource"])

        val returned = "com.teanode.app:/oauth?code=a-code&state=a-state"
        assertEquals("a-code", SignIn.authorizationCode(returned, "a-state"))
        assertNull(SignIn.authorizationCode(returned, "another-state"))

        val fake = FakeServer { 200 to """{"access_token":"tnt_access","refresh_token":"a-refresh","expires_in":2592000,"scope":"api"}""" }
        val account = SignIn(server, fake).exchangeAuthorizationCode("a-code", "a-verifier", "a-client")
        assertEquals("tnt_access", account.accessToken)
        val sent = formOf(fake.sent[0])
        assertEquals("authorization_code", sent["grant_type"])
        assertEquals("a-verifier", sent["code_verifier"])
        assertEquals(appRedirectAddress, sent["redirect_uri"])
    }

    @Test
    fun theAppRegistersAndExchangesItsCode() = runBlocking {
        val fake = FakeServer { request ->
            when {
                request.url.endsWith("/oauth/register") -> 201 to """{"client_id":"a-client"}"""
                request.url.endsWith("/oauth/token") -> 200 to """{"access_token":"tnt_access","refresh_token":"a-refresh","expires_in":2592000,"scope":"api"}"""
                else -> 404 to "{}"
            }
        }
        val signIn = SignIn(server, fake)
        val clientId = signIn.register()
        assertEquals("a-client", clientId)
        assertTrue(fake.sent[0].body!!.toString(Charsets.UTF_8).contains(""""redirect_uris":["com.teanode.app:/oauth"]"""))

        val account = signIn.exchangeConnectCode("abc123", clientId, "A test phone", nowEpochMS = 1_000_000L)
        assertEquals("tnt_access", account.accessToken)
        assertEquals("a-refresh", account.refreshToken)
        assertEquals(1_000_000L + 2_592_000_000L, account.expiresAtEpochMS)
        val exchange = formOf(fake.sent[1])
        assertEquals("urn:teanode:params:oauth:grant-type:connect-code", exchange["grant_type"])
        assertEquals("abc123", exchange["connect_code"])
        assertEquals("A test phone", exchange["device_name"])
    }

    @Test
    fun aSpentCodeSaysSo() {
        val fake = FakeServer { 400 to """{"error":"invalid_grant","error_description":"that connect code has been used"}""" }
        val failure = assertThrows(TeaNodeError::class.java) { runBlocking { SignIn(server, fake).exchangeConnectCode("used", "a-client", "") } }
        assertEquals(TeaNodeError.ConnectCodeSpent, failure)
    }

    @Test
    fun aTokenCloseToRunningOutIsRenewedFirst() = runBlocking {
        val fake = FakeServer { request ->
            if (request.url.endsWith("/oauth/token")) 200 to """{"access_token":"tnt_renewed","refresh_token":"another-refresh","expires_in":2592000}""" else 200 to SESSION_ANSWER
        }
        val account = Account(server, "a-client", "tnt_old", "a-refresh", System.currentTimeMillis() + 60_000L, "robin")
        val connection = Connection(account, fake)
        assertEquals("robin", connection.username())
        assertEquals(listOf("/oauth/token", "/api/v1/graphql"), fake.paths)
        assertEquals("Bearer tnt_renewed", fake.sent[1].header("Authorization"))
        assertEquals("another-refresh", connection.account.refreshToken)
    }

    @Test
    fun aCallSaysWhichLanguageAndTimeZoneTheAppIsShownIn() = runBlocking {
        val fake = FakeServer { 200 to SESSION_ANSWER }
        Connection(signedIn(), fake).username()
        assertEquals(Connection.shownLanguage(), fake.sent[0].header("X-Language"))
        assertEquals(Connection.shownTimeZone(), fake.sent[0].header("X-Timezone"))
        assertEquals("zh", Connection.shownLanguage(listOf("zh-Hans", "en")))
        assertEquals("zh", Connection.shownLanguage(listOf("zh-CN")))
        assertEquals("ja", Connection.shownLanguage(listOf("ja")))
        assertEquals("en", Connection.shownLanguage(listOf("en")))
        assertEquals("en", Connection.shownLanguage(listOf("fr")))
        assertEquals("en", Connection.shownLanguage(emptyList()))
    }

    @Test
    fun aSignInAnotherProcessRenewedIsTakenUpInsteadOfRenewingAgain() = runBlocking {
        val fake = FakeServer { request ->
            if (request.url.endsWith("/oauth/token")) 400 to """{"error":"invalid_grant"}""" else 200 to SESSION_ANSWER
        }
        val stale = Account(server, "a-client", "tnt_old", "spent-refresh", System.currentTimeMillis() + 60_000L, "robin")
        val newer = Account(server, "a-client", "tnt_new", "new-refresh", System.currentTimeMillis() + 30 * 86_400_000L, "robin")
        val connection = Connection(stale, fake, MemoryAccountStore(listOf(newer)))
        connection.username()
        assertEquals(listOf("/api/v1/graphql"), fake.paths, "no renewal with a refresh token the other process spent")
        assertEquals("Bearer tnt_new", fake.sent[0].header("Authorization"))
    }

    @Test
    fun anErrorFromTheServerIsShownInItsWords() {
        val fake = FakeServer { 200 to """{"errors":[{"message":"no such conversation"}]}""" }
        val failure = assertThrows(TeaNodeError::class.java) { runBlocking { Connection(signedIn(), fake).username() } }
        assertEquals(TeaNodeError.Refused("no such conversation"), failure)
    }

    @Test
    fun aRefusedFieldIsShownInItsWordsNotAsUnreadable() {
        val fake = FakeServer { 200 to """{"errors":[{"message":"that question was already answered"}],"data":{"AnswerAgentHerdrQuestion":null}}""" }
        val failure = assertThrows(TeaNodeError::class.java) { runBlocking { Connection(signedIn(), fake).username() } }
        assertEquals(TeaNodeError.Refused("that question was already answered"), failure)
    }

    @Test
    fun anAnswerTheAppCannotReadSaysSo() {
        for (body in listOf("not json", """{"data":null}""", """{}""", """{"data":{"GetSession":"wrong shape"}}""")) {
            val fake = FakeServer { 200 to body }
            val failure = assertThrows(TeaNodeError::class.java, { runBlocking { Connection(signedIn(), fake).username() } }, body)
            assertEquals(TeaNodeError.UnreadableAnswer, failure, body)
        }
    }

    @Test
    fun variablesAndTheDocumentGoInTheBody() = runBlocking {
        val fake = FakeServer { 200 to SESSION_ANSWER }
        val echoed: Echo = Connection(signedIn(), fake).execute("query (\$id: String!) { GetSession { username } }", mapOf("id" to JsonPrimitive("x1")))
        assertEquals("robin", echoed.GetSession["username"])
        val body = fake.sent[0].body!!.toString(Charsets.UTF_8)
        assertTrue(body.contains(""""variables":{"id":"x1"}"""), body)
        assertEquals("application/json", fake.sent[0].header("Content-Type"))
    }

    @Test
    fun anUploadIsAMultipartBodyAndAnswersTheFilesId() = runBlocking {
        val fake = FakeServer { 200 to """{"attachments":[{"id":"file-1"}]}""" }
        val id = Connection(signedIn(), fake).upload("note \"one\".txt", "text/plain", "hello".toByteArray(), isTaxDocument = true)
        assertEquals("file-1", id)
        assertTrue(fake.sent[0].url.endsWith("/api/v1/agent/attachments?isTaxDocument=true"))
        val boundary = fake.sent[0].header("Content-Type")!!.substringAfter("boundary=")
        val body = fake.sent[0].body!!.toString(Charsets.UTF_8)
        assertTrue(body.startsWith("--$boundary\r\n"))
        assertTrue(body.contains("filename=\"note 'one'.txt\""), "a quote in the name cannot end the header early")
        assertTrue(body.endsWith("hello\r\n--$boundary--\r\n"))
        assertEquals("/api/v1/agent/attachments?isVoiceMessage=true", Connection.uploadPath(isTaxDocument = true, isVoiceMessage = true))
        assertEquals("/api/v1/agent/attachments", Connection.uploadPath(isTaxDocument = false, isVoiceMessage = false))
    }

    @Test
    fun aFileTooLargeForTheServerSaysSo() {
        val fake = FakeServer { 413 to "" }
        val failure = assertThrows(TeaNodeError::class.java) { runBlocking { Connection(signedIn(), fake).upload("big.bin", "application/octet-stream", ByteArray(1)) } }
        assertEquals(TeaNodeError.Refused("That file is larger than the server takes."), failure)
    }
}

class RenewalTest {
    private val server = ServerAddress.parse("mail.example.com")

    /** A sign-in an hour from running out, so the next token is renewed. */
    private fun expiring(accessToken: String = "access-1", refreshToken: String = "refresh-1") =
        Account(server, "a-client", accessToken, refreshToken, System.currentTimeMillis() + 3_600_000L, "robin")

    private fun held() = Account(server, "a-client", "access-1", "refresh-1", System.currentTimeMillis() + 10 * 86_400_000L, "robin")

    @Test
    fun callersAtTheSameTimeShareOneRenewal() = runBlocking {
        val rotating = RotatingServer("refresh-1")
        val store = MemoryAccountStore(listOf(expiring()))
        val connection = Connection(expiring(), rotating, store)
        val tokens = (0 until 5).map { async(kotlinx.coroutines.Dispatchers.Default) { connection.accessToken() } }.awaitAll()
        assertEquals(List(5) { "access-2" }, tokens)
        assertEquals(1, rotating.renewalCount, "each caller renewed with the same refresh token")
        assertEquals("refresh-2", store.load(expiring().accountKey)?.refreshToken)
        assertFalse(connection.isSignedOut)
    }

    @Test
    fun requestsAtTheSameTimeShareOneRenewal() = runBlocking {
        val rotating = RotatingServer("refresh-1")
        val connection = Connection(expiring(), rotating, MemoryAccountStore(listOf(expiring())))
        (0 until 3).map { async(kotlinx.coroutines.Dispatchers.Default) { connection.username() } }.awaitAll()
        assertEquals(1, rotating.renewalCount)
        assertEquals(3, rotating.graphQLCount)
    }

    /** Another process renews with the same refresh token a moment first: the server refuses this one, and what the other stored is used. */
    @Test
    fun aRefusedRenewalTakesUpWhatAnotherProcessStored() = runBlocking {
        val store = MemoryAccountStore(listOf(expiring()))
        val elsewhere = Account(server, "a-client", "access-elsewhere", "refresh-elsewhere", System.currentTimeMillis() + 30 * 86_400_000L, "robin")
        val rotating = RotatingServer("refresh-elsewhere", onRefresh = { store.save(elsewhere) })
        val connection = Connection(expiring(), rotating, store)
        assertEquals("access-elsewhere", connection.accessToken())
        assertFalse(connection.isSignedOut, "a renewal another process made signed this one out")
        assertEquals("refresh-elsewhere", connection.account.refreshToken)
    }

    @Test
    fun aRefusedRenewalWithNothingNewerStoredHasEnded() = runBlocking {
        val rotating = RotatingServer("refresh-retired-by-the-server")
        val connection = Connection(expiring(), rotating, MemoryAccountStore(listOf(expiring())))
        val failure = assertThrows(TeaNodeError::class.java) { runBlocking { connection.accessToken() } }
        assertEquals(TeaNodeError.SignedOut, failure)
        assertTrue(connection.isSignedOut)
    }

    /** The server refuses a token another process's renewal retired; the request goes again with the token that process stored. */
    @Test
    fun a401IsTriedAgainWithTheStoredToken() = runBlocking {
        val stored = held().copy(accessToken = "access-2", refreshToken = "refresh-2", expiresAtEpochMS = System.currentTimeMillis() + 30 * 86_400_000L)
        val rotating = RotatingServer("refresh-2", answerGraphQL = { token -> if (token == "access-2") 200 to SESSION_ANSWER else 401 to "" })
        val connection = Connection(held(), rotating, MemoryAccountStore(listOf(stored)))
        connection.username()
        assertEquals(2, rotating.graphQLCount)
        assertEquals(0, rotating.renewalCount)
        assertFalse(connection.isSignedOut)
    }

    /** With nothing newer stored, a 401 renews the sign-in and tries again. */
    @Test
    fun a401WithNothingStoredRenewsAndTriesAgain() = runBlocking {
        val rotating = RotatingServer("refresh-1", answerGraphQL = { token -> if (token == "access-2") 200 to SESSION_ANSWER else 401 to "" })
        val connection = Connection(held(), rotating, MemoryAccountStore(listOf(held())))
        connection.username()
        assertEquals(1, rotating.renewalCount)
        assertEquals(2, rotating.graphQLCount)
    }

    @Test
    fun a401AgainAfterAFreshTokenHasEnded() = runBlocking {
        val rotating = RotatingServer("refresh-1", answerGraphQL = { _ -> 401 to "" })
        val connection = Connection(held(), rotating)
        val failure = assertThrows(TeaNodeError::class.java) { runBlocking { connection.username() } }
        assertEquals(TeaNodeError.SignedOut, failure)
        assertEquals(2, rotating.graphQLCount, "a 401 was tried more than once more")
        assertTrue(connection.isSignedOut)
    }
}
