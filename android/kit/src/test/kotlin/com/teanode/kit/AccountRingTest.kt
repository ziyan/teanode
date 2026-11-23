package com.teanode.kit

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class AccountRingTest {
    private fun account(host: String, username: String, token: String = "tnt_a") = Account(
        server = ServerAddress.parse(host), clientId = "client-$host-$username", accessToken = token, refreshToken = "",
        expiresAtEpochMS = System.currentTimeMillis() + 86_400_000L, username = username,
    )

    @Test
    fun theFirstKeptIsInUseAndAddingAnotherKeepsIt() {
        val ring = AccountRing().put(account("mail.example.com", "robin")).put(account("mail.example.org", "sam"))
        assertEquals("robin", ring.active?.username)
        assertEquals(2, ring.accounts.size)
    }

    @Test
    fun signingInAgainReplacesTheSameSignIn() {
        val ring = AccountRing().put(account("mail.example.com", "robin", token = "tnt_old")).put(account("mail.example.com", "Robin", token = "tnt_new"))
        assertEquals(1, ring.accounts.size)
        assertEquals("tnt_new", ring.active?.accessToken)
    }

    @Test
    fun switchingAndForgettingMoveTheOneInUse() {
        val first = account("mail.example.com", "robin")
        val second = account("mail.example.org", "sam")
        var ring = AccountRing().put(first).put(second)
        ring = ring.activate(second.accountKey)
        assertEquals("sam", ring.active?.username)
        ring = ring.activate("nobody")
        assertEquals("sam", ring.active?.username)
        ring = ring.remove(second.accountKey)
        assertEquals("robin", ring.active?.username)
        ring = ring.remove(first.accountKey)
        assertNull(ring.active)
    }

    @Test
    fun theSamePersonOnTwoServersIsTwoSignIns() {
        val ring = AccountRing().put(account("mail.example.com", "robin")).put(account("mail.example.org", "robin"))
        assertEquals(2, ring.accounts.size)
    }

    @Test
    fun theStoreKeepsEveryServerAndTheOneInUse() {
        val secrets = MemorySecrets()
        val store = SecretAccountStore(secrets)
        val robin = account("mail.example.com", "robin")
        val sam = account("mail.example.org", "sam")
        store.add(robin)
        store.add(sam)
        assertEquals("sam", SecretAccountStore(secrets).load()?.username, "kept across a restart")
        store.activate(robin.accountKey)
        assertEquals("robin", store.load()?.username)
        store.save(sam.copy(accessToken = "tnt_renewed"))
        assertEquals("robin", store.load()?.username, "a renewal does not change the one in use")
        assertEquals("tnt_renewed", store.load(sam.accountKey)?.accessToken)
        store.delete()
        assertEquals("sam", store.load()?.username)
        store.delete()
        assertNull(store.load())
    }
}

/** MemorySecrets keeps named secrets the way the phone's encrypted file does. */
class MemorySecrets : SecretStore {
    private val secrets = HashMap<String, ByteArray>()

    @Synchronized
    override fun read(name: String): ByteArray? = secrets[name]

    @Synchronized
    override fun write(name: String, secret: ByteArray) {
        secrets[name] = secret
    }

    @Synchronized
    override fun delete(name: String) {
        secrets.remove(name)
    }
}
