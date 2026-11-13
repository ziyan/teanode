package com.teanode.kit.passwords

import com.teanode.kit.passwords.SharingFixture.ITERATION_COUNT
import com.teanode.kit.passwords.SharingFixture.loginFields
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

/** Forwards to the fake server, and lets the "server" lie in two ways: it adds an item nobody shared, and it answers the setup read with another public key. */
internal class HostileServerFlags {
    var plantedItem: PasswordItemView? = null
    var setupPublicKeyOverride: String? = null
}

internal class HostileServerApi(private val inner: PasswordsApi, private val flags: HostileServerFlags) : PasswordsApi by inner {
    override suspend fun readPasswordsSetup(): PasswordsSetupView {
        val real = inner.readPasswordsSetup()
        val override = flags.setupPublicKeyOverride ?: return real
        return real.copy(publicKey = override)
    }

    override suspend fun listPasswordItems(sinceRevision: Long?): PasswordItemListView {
        val real = inner.listPasswordItems(sinceRevision)
        val planted = flags.plantedItem ?: return real
        return PasswordItemListView(real.items + planted, real.itemIds + planted.id, maxOf(real.revision, planted.revision))
    }
}

/** These are the checks that a server choosing keys cannot get an item key out of the phone for anybody the owner's record does not reach, and cannot make the phone forget whose key it knew. */
class PasswordsPinTest {
    private fun makeCache() = SharingFixture.makeCache()

    /**
     * An item the server plants in the list opens with nothing, so the key it carries is not recorded. A waiting member of
     * the circle the owner's record reaches is sealed for at once, with the key the server gives, and that key is recorded
     * as the known one.
     */
    @Test fun `a planted item records no key and a waiting member the record reaches is sealed for when the person finishes the sharing`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        server.addPerson("user-mallory", "Mallory")
        server.setCircle("circle-one", listOf("user-ada", "user-mallory"))
        val flags = HostileServerFlags()
        val adaCache = makeCache()
        val ada = PasswordsSession(HostileServerApi(server.api("user-ada"), flags), adaCache, "user-ada")
        ada.setUp("ada invented passphrase", ITERATION_COUNT)
        val item = ada.save(loginFields())
        // Ada shares with her circle. Mallory has not set up Passwords, so nothing is sealed for anybody.
        val recipients = ada.recipients(granteeCircleId = "circle-one")
        ada.setShares(item.id, listOf(PasswordShareGranteeInput(granteeCircleId = "circle-one")), recipients)
        val malloryDevice = PasswordsSession(server.api("user-mallory"), makeCache(), "user-mallory")
        malloryDevice.setUp("mallory invented passphrase", ITERATION_COUNT)
        val malloryKey = server.publicKey("user-mallory")

        // The server adds one made-up shared item to Ada's list, "from" Mallory, carrying her key.
        flags.plantedItem = PasswordItemView(
            id = "planted-1", ownerUserId = "user-mallory", ownerName = "Mallory", ownerPublicKey = malloryKey, itemKind = "login",
            encryptedFields = "{}", itemKey = "{}", isSharedWithMe = true, revision = 99999, isUsableByAgent = false,
            createdAt = "2026-10-20T10:00:00Z", updatedAt = "2026-10-20T10:00:00Z", shares = emptyList(),
        )
        ada.sync()
        assertNotNull(adaCache.load())
        assertNull(adaCache.loadPins("user-ada").knownPublicKeys["user-mallory"], "the planted item opens with nothing, so its key is not recorded")
        assertEquals(0, ada.keyChanges().size)

        // Ada never sealed this item for Mallory, so nothing is done by itself; the item waits for her.
        val byItself = ada.completeWaitingShares()
        assertEquals(0, byItself.addedKeyCount)
        assertEquals(setOf(item.id), ada.itemIdsNeedingYou())
        // The record reaches the circle, so when Ada finishes the sharing Mallory is sealed for the key the server gives.
        ada.finishSharing(item.id, ada.finishPlan(listOf(item.id)))
        assertEquals(malloryKey, adaCache.loadPins("user-ada").knownPublicKeys["user-mallory"])
        val stored = server.item(item.id)
        assertEquals(malloryKey, stored?.keys?.get("user-mallory")?.recipientPublicKey)

        // An item the record does not reach gets nothing: a second item Ada never shared.
        val unsharedItem = ada.save(loginFields())
        ada.sync()
        val again = ada.completeWaitingShares()
        assertEquals(0, again.addedKeyCount)
        assertNull(server.item(unsharedItem.id)?.keys?.get("user-mallory"))
    }

    /** The server makes the phone drop its copy by answering the setup read with another public key. The pins about other people stay, so a later change of an owner's key is still noticed. */
    @Test fun `a server answer that drops the copy leaves the pins and a later key change still warns`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        server.addPerson("user-grace", "Grace")
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        val ada = PasswordsSession(server.api("user-ada"), makeCache(), "user-ada")
        val flags = HostileServerFlags()
        val graceCache = makeCache()
        val grace = PasswordsSession(HostileServerApi(server.api("user-grace"), flags), graceCache, "user-grace")
        ada.setUp("ada invented passphrase", ITERATION_COUNT)
        grace.setUp("grace invented passphrase", ITERATION_COUNT)
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeUserId = "user-grace")
        ada.setShares(item.id, listOf(PasswordShareGranteeInput(granteeUserId = "user-grace")), recipients)
        grace.sync()
        val adaKey = server.publicKey("user-ada")
        assertEquals(mapOf("user-ada" to adaKey), graceCache.loadPins("user-grace").knownPublicKeys)

        // One lying answer to the setup read.
        flags.setupPublicKeyOverride = base64(PasswordsCrypto.makeKeyPair().publicKey)
        val error = assertThrows<Throwable>("expected needs passphrase") { runBlocking { grace.sync() } }
        assertEquals(PasswordsError.NeedsPassphrase, error)
        assertNull(graceCache.load()) // the copy is gone
        assertEquals(mapOf("user-ada" to adaKey), graceCache.loadPins("user-grace").knownPublicKeys) // the pins are not
        flags.setupPublicKeyOverride = null
        grace.unlockWithPassphrase("grace invented passphrase")
        assertNotNull(graceCache.load())
        assertEquals(mapOf("user-ada" to adaKey), graceCache.loadPins("user-grace").knownPublicKeys)

        // Now the server shows another key for Ada without sealing the item key for it: the item opens with nothing, so
        // nothing is recorded and the key that was known stays.
        val newOwnerKey = base64(PasswordsCrypto.makeKeyPair().publicKey)
        server.replacePublicKey("user-ada", newOwnerKey)
        server.setRevision(item.id, 500)
        grace.sync(isFull = true)
        assertEquals(0, grace.keyChanges().size)
        assertEquals(adaKey, graceCache.loadPins("user-grace").knownPublicKeys["user-ada"])
    }

    /** Erasing Passwords on purpose (the person asked to start over) does remove the pins. */
    @Test fun `erasing passwords on purpose removes the pins too`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        server.addPerson("user-grace", "Grace")
        val storage = InMemoryPasswordsStorage()
        val ada = PasswordsSession(server.api("user-ada"), makeCache(), "user-ada")
        val graceCache = PasswordsCache(storage)
        val grace = PasswordsSession(server.api("user-grace"), graceCache, "user-grace")
        ada.setUp("ada invented passphrase", ITERATION_COUNT)
        grace.setUp("grace invented passphrase", ITERATION_COUNT)
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeUserId = "user-grace")
        ada.setShares(item.id, listOf(PasswordShareGranteeInput(granteeUserId = "user-grace")), recipients)
        grace.sync()
        assertTrue(graceCache.loadPins("user-grace").knownPublicKeys.isNotEmpty())
        grace.erase()
        assertNull(graceCache.load())
        assertEquals(PasswordsPins(userId = "user-grace"), graceCache.loadPins("user-grace"))
        assertNull(storage.readPins())
    }
}
