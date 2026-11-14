package com.teanode.app

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.teanode.app.session.AppSession
import com.teanode.app.session.MemorySecretStore
import com.teanode.kit.AppSection
import com.teanode.kit.ConnectLink
import com.teanode.kit.HTTPRequest
import com.teanode.kit.HTTPResponse
import com.teanode.kit.HTTPTransport
import com.teanode.kit.Pkce
import com.teanode.kit.QueryString
import com.teanode.kit.ServerAddress
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.util.concurrent.CopyOnWriteArrayList

/**
 * A server that answers the sign-in and the questions the session asks, and keeps what it was sent. Whoever a code or
 * an authorization code names is who signs in: the code "robin" gives the token "access-robin" and the username robin.
 */
private class SigningServer : HTTPTransport {
    val sent = CopyOnWriteArrayList<HTTPRequest>()

    override suspend fun send(request: HTTPRequest): HTTPResponse {
        sent.add(request)
        val body = request.body?.toString(Charsets.UTF_8).orEmpty()
        val form = if (request.url.endsWith("/oauth/token")) QueryString.parse(body) else emptyMap()
        val who = form["connect_code"] ?: form["code"] ?: "unknown"
        return when {
            request.url.endsWith("/oauth/register") -> HTTPResponse(201, """{"client_id":"a-client"}""".toByteArray())
            request.url.endsWith("/oauth/token") -> HTTPResponse(200, """{"access_token":"access-$who","refresh_token":"refresh-$who","expires_in":2592000}""".toByteArray())
            request.url.endsWith("/oauth/revoke") -> HTTPResponse(200)
            body.contains("GetServiceCapabilities") -> HTTPResponse(
                200,
                """{"data":{"GetServiceCapabilities":{"isMailRoutingEnabled":true,"isMailboxEnabled":true,"isCalendarEnabled":true,"isRemindersEnabled":true,"isContactsEnabled":true,"isPhotosEnabled":true,"isAssistantEnabled":true,"hasPendingServiceRestart":false,"hasCompletedSetup":true}}}""".toByteArray(),
            )
            body.contains("GetSession") -> {
                val username = request.header("Authorization").orEmpty().removePrefix("Bearer access-")
                HTTPResponse(200, """{"data":{"GetSession":{"username":"$username","userId":"u1","permissions":{"everywhere":["journal:use"]}}}}""".toByteArray())
            }
            else -> HTTPResponse(404)
        }
    }

    fun forms(path: String): List<Map<String, String>> =
        sent.filter { request -> request.url.endsWith(path) }.map { request -> QueryString.parse(request.body!!.toString(Charsets.UTF_8)) }
}

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class SessionTest {
    private lateinit var context: Context
    private lateinit var server: SigningServer
    private lateinit var session: AppSession

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
        server = SigningServer()
        session = AppSession(context, MemorySecretStore(), server, null, context.getSharedPreferences("session-test", Context.MODE_PRIVATE).also { kept -> kept.edit().clear().apply() })
    }

    @Test
    fun aConnectCodeWaitsForTheConfirmationBeforeItIsKept() {
        val link = ConnectLink(ServerAddress.parse("mail.example.com"), "robin")
        session.connect(link.url)
        val pending = session.pendingConnection.value
        assertNotNull(pending)
        assertEquals("robin", pending!!.account.username)
        assertNull("nothing is kept until the person confirms", session.connection.value)
        assertEquals("robin", server.forms("/oauth/token").single()["connect_code"])

        session.confirm(pending)
        assertNull(session.pendingConnection.value)
        assertEquals("robin", session.connection.value?.account?.username)
        assertEquals(1, session.accounts.value.accounts.size)
    }

    @Test
    fun aLinkOpenedFromOutsideAsksNothingOfItsServerUntilTheDialogIsAnswered() {
        session.open(ConnectLink(ServerAddress.parse("elsewhere.example.net"), "robin"))
        assertEquals("elsewhere.example.net", session.openedLink.value?.server?.host)
        assertTrue("no request goes to the server the link names", server.sent.isEmpty())
        assertNull(session.pendingConnection.value)
    }

    @Test
    fun anAccountAddedBesideTheOneInUseDoesNotReplaceItWhenTheyChoseToKeepIt() {
        session.connect(ConnectLink(ServerAddress.parse("mail.example.com"), "robin").url)
        session.confirm(session.pendingConnection.value!!)
        session.connect(ConnectLink(ServerAddress.parse("mail.example.org"), "sam").url)
        session.confirm(session.pendingConnection.value!!, isSwitching = false)
        assertEquals("robin", session.connection.value?.account?.username)
        assertEquals(2, session.accounts.value.accounts.size)
        assertEquals("robin", session.accounts.value.active?.username)
    }

    @Test
    fun declining_aConnectCodeGivesTheSignInBack() {
        session.connect(ConnectLink(ServerAddress.parse("mail.example.com"), "robin").url)
        session.decline(session.pendingConnection.value!!)
        assertNull(session.pendingConnection.value)
        assertNull(session.connection.value)
        assertEquals(1, server.sent.count { request -> request.url.endsWith("/oauth/revoke") })
    }

    @Test
    fun aTextThatIsNotAConnectLinkSaysSo() {
        session.connect("hello")
        assertEquals(context.getString(R.string.signin_not_a_link), session.problemText.value)
        session.connect(ConnectLink(ServerAddress.parse("mail.example.com")).url)
        assertEquals(context.getString(R.string.signin_link_has_no_code), session.problemText.value)
    }

    @Test
    fun anAddressThatIsNotEncryptedIsRefusedUnderTheField() {
        session.startPasswordSignIn("http://mail.example.com")
        assertEquals(context.getString(R.string.signin_address_not_encrypted), session.problemText.value)
        session.startPasswordSignIn("https://someone:secret@mail.example.com")
        assertEquals(context.getString(R.string.signin_address_has_credentials), session.problemText.value)
        session.startPasswordSignIn("not a server")
        assertEquals(context.getString(R.string.signin_address_not_an_address), session.problemText.value)
    }

    @Test
    fun signingInWithAPasswordOpensTheServersPageAndCompletesWhenItReturns() {
        var opened: String? = null
        session.openSignInPage = { address -> opened = address }
        session.startPasswordSignIn("mail.example.com")
        val address = opened!!
        val items = QueryString.parse(address.substringAfter('?'))
        assertTrue(address.startsWith("https://mail.example.com/oauth/authorize?"))
        assertEquals("a-client", items["client_id"])
        assertEquals("com.teanode.app:/oauth", items["redirect_uri"])

        // The page returns with a code; the verifier whose hash the page was given redeems it.
        session.completeSignIn("com.teanode.app:/oauth?code=robin&state=${items["state"]}")
        val redeemed = server.forms("/oauth/token").single()
        assertEquals("robin", redeemed["code"])
        assertEquals(items["code_challenge"], Pkce.challenge(redeemed["code_verifier"]!!))
        assertEquals("robin", session.connection.value?.account?.username)
        assertFalse(session.isConnecting.value)
    }

    @Test
    fun aReturnForAnotherRequestSignsNobodyIn() {
        var opened: String? = null
        session.openSignInPage = { address -> opened = address }
        session.startPasswordSignIn("mail.example.com")
        session.completeSignIn("com.teanode.app:/oauth?code=robin&state=someone-elses")
        assertNull(session.connection.value)
        assertEquals(context.getString(R.string.signin_not_signed_in), session.problemText.value)
        assertTrue(opened != null)
    }

    @Test
    fun theServerOffersPartsAfterItIsAsked() {
        session.connect(ConnectLink(ServerAddress.parse("mail.example.com"), "robin").url)
        session.confirm(session.pendingConnection.value!!)
        assertTrue(session.hasReadFacts.value)
        assertTrue(session.facts.value.isAvailable(AppSection.Mail))
        assertTrue(session.facts.value.isAvailable(AppSection.Journal))
        assertFalse(session.facts.value.isAvailable(AppSection.Health))
    }

    @Test
    fun signingOutRevokesAndPutsTheNextAccountToUse() {
        session.connect(ConnectLink(ServerAddress.parse("mail.example.com"), "robin").url)
        session.confirm(session.pendingConnection.value!!)
        session.connect(ConnectLink(ServerAddress.parse("mail.example.org"), "sam").url)
        session.confirm(session.pendingConnection.value!!)
        assertEquals("sam", session.connection.value?.account?.username)
        assertEquals(2, session.accounts.value.accounts.size)

        session.signOut()
        assertEquals("robin", session.connection.value?.account?.username)
        assertEquals(1, server.sent.count { request -> request.url.endsWith("/oauth/revoke") })

        session.signOut()
        assertNull(session.connection.value)
        assertTrue(session.accounts.value.accounts.isEmpty())
    }

    @Test
    fun theTabBarKeepsWhatThePersonChose() {
        assertEquals(listOf(AppSection.Agent, AppSection.Mail, AppSection.Photos), session.tabBarSections.value)
        session.setTabBarSections(listOf(AppSection.Finance, AppSection.Settings, AppSection.Mail))
        assertEquals(listOf(AppSection.Finance, AppSection.Mail), session.tabBarSections.value)
        val again = AppSession(context, MemorySecretStore(), server, null, context.getSharedPreferences("session-test", Context.MODE_PRIVATE))
        assertEquals(listOf(AppSection.Finance, AppSection.Mail), again.tabBarSections.value)
    }

    @Test
    fun aCountWaitingInAPartIsShownAndClearedAtZero() {
        session.setBadgeCount(AppSection.Agent, 4)
        assertEquals(4, session.badgeCounts.value[AppSection.Agent])
        session.setBadgeCount(AppSection.Agent, 0)
        assertTrue(session.badgeCounts.value.isEmpty())
    }

    @Test
    fun theKeptSignInCameBackAfterARestart() = runBlocking {
        val secrets = MemorySecretStore()
        val first = AppSession(context, secrets, server, null, context.getSharedPreferences("session-test", Context.MODE_PRIVATE))
        first.connect(ConnectLink(ServerAddress.parse("mail.example.com"), "robin").url)
        first.confirm(first.pendingConnection.value!!)
        val restarted = AppSession(context, secrets, server, null, context.getSharedPreferences("session-test", Context.MODE_PRIVATE))
        assertNull(restarted.connection.value)
        restarted.restore()
        assertEquals("robin", restarted.connection.value?.account?.username)
    }
}
