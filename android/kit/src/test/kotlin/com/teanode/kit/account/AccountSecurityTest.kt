package com.teanode.kit.account

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant

class AccountSecurityTest {
    private fun connection(page: String): Pair<Connection, FakeServer> {
        val fake = FakeServer { _ -> 200 to page }
        val account = Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", Long.MAX_VALUE / 4, "robin")
        return Connection(account, fake) to fake
    }

    private fun sentVariables(fake: FakeServer, index: Int = 0): JsonObject =
        com.teanode.kit.TeaNodeJson.parseToJsonElement(fake.sent[index].body!!.toString(Charsets.UTF_8)).jsonObject["variables"]!!.jsonObject

    @Test
    fun theCurrentSessionIsListedFirstAndNeverRevoked() {
        val sessions = listOf(
            AccountSession("newest", created = "2030-01-03T00:00:00Z"),
            AccountSession("mine", current = true, created = "2030-01-02T00:00:00Z"),
            AccountSession("ended", created = "2030-01-01T00:00:00Z", revoked = "2030-01-04T00:00:00Z"),
        )
        val listed = AccountSession.listed(sessions)
        assertEquals(listOf("mine", "newest", "ended"), listed.map { session -> session.id })
        assertEquals(listOf(false, true, false), listed.map { session -> session.canRevoke })
    }

    @Test
    fun theBrowserIsNamedOnlyWhenRecognized() {
        fun name(agent: String?) = AccountSession("a", created = "2030-01-01T00:00:00Z", userAgent = agent).browserName
        assertNull(name(null))
        assertNull(name(""))
        assertEquals("Chrome · Windows NT 10.0", name("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"))
        assertEquals("Firefox · Macintosh", name("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Gecko/20100101 Firefox/140.0"))
        assertEquals("Edge", name("SomeTool/1.0 Edg/140.0"))
        assertEquals("an-unknown-client", name("an-unknown-client"))
        assertEquals("x".repeat(60) + "…", name("x".repeat(80)))
    }

    @Test
    fun sessionsAreReadAndSentWithTheirFlag() = runBlocking {
        val (connection, fake) = connection("""{"data":{"ListSessions":[{"id":"s1","current":false,"created":"2030-01-02T03:04:05.123456789Z","ip":"192.0.2.10"},null,{"id":"s2","current":true,"created":"2030-01-01T00:00:00Z","impersonatorUsername":"operator"}]}}""")
        val sessions = connection.accountSessions(includeRevoked = true)
        assertEquals(listOf("s1", "s2"), sessions.map { session -> session.id })
        assertEquals("operator", sessions[1].impersonatorUsername)
        assertEquals(JsonPrimitive(true), sentVariables(fake)["includeRevoked"])
    }

    @Test
    fun aTokenSaysWhenItExpires() {
        val now = Instant.parse("2030-02-01T00:00:00Z")
        fun expiry(expires: String?) = APIToken("t", "laptop", "robin", "2030-01-01T00:00:00Z", expires).expiry(now)
        assertEquals(APITokenExpiry.Never, expiry(null))
        assertEquals(APITokenExpiry.Expired, expiry("2030-01-15T00:00:00Z"))
        assertEquals(APITokenExpiry.ExpiresAt(Instant.parse("2030-03-01T00:00:00Z")), expiry("2030-03-01T00:00:00Z"))
    }

    @Test
    fun aTokenIsCreatedWithALifetimeOrNone() = runBlocking {
        val (connection, fake) = connection("""{"data":{"CreateToken":{"secret":"tnt_made_up","token":{"id":"tnt_made","name":"laptop","username":"robin","created":"2030-01-01T00:00:00Z"}}}}""")
        val created = connection.createApiToken("laptop", APITokenLifetime.ThirtyDays)
        assertEquals("tnt_made_up", created.secret)
        connection.createApiToken("laptop", APITokenLifetime.Never)
        assertEquals(JsonPrimitive("720h"), sentVariables(fake, 0)["lifetime"])
        assertEquals(JsonNull, sentVariables(fake, 1)["lifetime"])
    }

    @Test
    fun aTokenChangeSendsOnlyWhatChanged() {
        val token = APIToken("tnt_one", "laptop", "robin", "2030-01-01T00:00:00Z")
        assertFalse(APITokenChange.of(token, " laptop ", null).hasChanges)
        val renamed = APITokenChange.of(token, " desktop ", null)
        assertTrue(renamed.hasChanges)
        assertEquals(JsonPrimitive("desktop"), renamed.variables["name"])
        assertEquals(JsonNull, renamed.variables["lifetime"])
        assertEquals(JsonPrimitive("never"), APITokenChange.of(token, "laptop", APITokenLifetime.Never).variables["lifetime"])
        assertEquals(JsonNull, APITokenChange.of(token, "laptop", APITokenLifetime.Year).variables["name"])
    }

    @Test
    fun anAppIsNamedAsTheDashboardNamesIt() {
        assertEquals("My notes", ConnectedApp("one", "My notes", "Notes").displayName)
        assertEquals("Notes", ConnectedApp("one", "My notes", "Notes").differentRegisteredName)
        assertNull(ConnectedApp("one", "Notes", "Notes").differentRegisteredName)
        assertEquals("Notes", ConnectedApp("one", "", "Notes").displayName)
        assertNull(ConnectedApp("one", "", "").displayName)
    }
}
