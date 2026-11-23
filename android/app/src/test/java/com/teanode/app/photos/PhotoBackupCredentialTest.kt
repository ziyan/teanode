package com.teanode.app.photos

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.teanode.app.session.MemorySecretStore
import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.HTTPResponse
import com.teanode.kit.HTTPTransport
import com.teanode.kit.ServerAddress
import com.teanode.kit.photos.PhotoBackupCredential
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.IOException

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class PhotoBackupCredentialTest {
    private val context: Context = ApplicationProvider.getApplicationContext()
    private val account = Account(ServerAddress.parse("https://mail.example.com"), "client", "tnt_backup", "", System.currentTimeMillis() + 3_600_000, "alex")

    /** What the server answers to taking back each password id: "taken", "gone" (it has no such password) or "offline". */
    private val serverAnswers = mutableMapOf<String, String>()
    private val revokedCredentialIds = mutableListOf<String>()

    private val server = object : HTTPTransport {
        override suspend fun send(request: HTTPRequest): HTTPResponse {
            val credentialId = Regex("\"credentialId\":\"([^\"]+)\"").find(request.body!!.decodeToString())!!.groupValues[1]
            return when (serverAnswers[credentialId] ?: "taken") {
                "offline" -> throw IOException("no network")
                "gone" -> HTTPResponse(200, """{"data":null,"errors":[{"message":"not found"}]}""".toByteArray())
                else -> HTTPResponse(200, """{"data":{"RevokeSyncCredential":true}}""".toByteArray()).also { revokedCredentialIds.add(credentialId) }
            }
        }
    }

    private fun settings() = PhotoBackupSettings(context, account.accountKey, MemorySecretStore())

    @Test fun `a password let go of is taken back on the server`() = runBlocking {
        val settings = settings()
        settings.credentialId = "credential1"
        settings.saveCredential(PhotoBackupCredential("credential1", "Android phone", "device", "secret"))
        settings.forgetCredential()
        assertNull(settings.credential())
        assertNull(settings.credentialId)
        assertEquals(setOf("credential1"), settings.unrevokedCredentialIds)
        settings.revokeForgottenCredentials(Connection(account, server))
        assertEquals(listOf("credential1"), revokedCredentialIds)
        assertEquals(emptySet<String>(), settings.unrevokedCredentialIds)
    }

    @Test fun `a password the phone can no longer read is still taken back by its id`() = runBlocking {
        val settings = settings()
        // The server made it, but the sealed copy is missing or cannot be opened.
        settings.credentialId = "credential2"
        assertNull(settings.credential())
        settings.forgetCredential()
        settings.revokeForgottenCredentials(Connection(account, server))
        assertEquals(listOf("credential2"), revokedCredentialIds)
    }

    @Test fun `only a network failure leaves a password to take back later`() = runBlocking {
        val settings = settings()
        serverAnswers["credential3"] = "gone"
        serverAnswers["credential4"] = "offline"
        settings.unrevokedCredentialIds = setOf("credential3", "credential4")
        settings.revokeForgottenCredentials(Connection(account, server))
        assertEquals(setOf("credential4"), settings.unrevokedCredentialIds)
        serverAnswers.remove("credential4")
        settings.revokeForgottenCredentials(Connection(account, server))
        assertEquals(listOf("credential4"), revokedCredentialIds)
        assertEquals(emptySet<String>(), settings.unrevokedCredentialIds)
    }
}
