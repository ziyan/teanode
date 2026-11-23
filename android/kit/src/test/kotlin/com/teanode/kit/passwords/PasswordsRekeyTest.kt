package com.teanode.kit.passwords

import com.teanode.kit.passwords.SharingFixture.makeServer
import com.teanode.kit.passwords.SharingFixture.makeSession
import com.teanode.kit.passwords.SharingFixture.setUp
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

/** These check that an item gets a new item key when access to it narrows, so the old key (kept by the agent's copy, or by somebody who was removed) opens nothing saved later. */
class PasswordsRekeyTest {
    private fun loginFields(password: String = "correct horse") = SharingFixture.loginFields("Example", password)

    private fun grantee(userId: String) = PasswordShareGranteeInput(granteeUserId = userId)

    /** The item key an item's stored account copy holds. */
    private fun accountItemKey(stored: FakePasswordsServer.StoredItem, accountKey: ByteArray): ByteArray =
        PasswordsCrypto.open(PasswordsSealedValue.fromJsonText(stored.itemKeyByAccountKey), accountKey, PasswordsCrypto.itemKeyAssociatedData(stored.id))

    private fun fieldsOf(stored: FakePasswordsServer.StoredItem, itemKey: ByteArray): PasswordsItemFields =
        PasswordsCrypto.openItemFields(PasswordsSealedValue.fromJsonText(stored.encryptedFields), itemKey, stored.id, PasswordsItemKind.Login)

    @Test fun `turning the agent off gives the item a new key that the old agent copy cannot open`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada")
        val device = makeSession(server, "user-ada")
        setUp(device, "ada invented passphrase")
        val accountKey = makeSession(server, "user-ada").unlockWithPassphrase("ada invented passphrase").accountKey
        val item = device.save(loginFields())
        device.setUsableByAgent(item.id, true)

        // The agent's copy of the item key, as a copy of the database would hold it.
        val usable = server.item(item.id)!!
        val agentCopy = PasswordsKeyPairSealedValue.fromJsonText(usable.itemKeyByAgentKey!!)
        val ownerPublicKey = unbase64(server.publicKey("user-ada"))
        val oldItemKey = PasswordsCrypto.openForKeyPair(
            agentCopy, server.agentKeyPair.privateKey, ownerPublicKey, "user-ada", PasswordsCrypto.AGENT_RECIPIENT_ID, item.id,
        )
        assertEquals(loginFields(), fieldsOf(usable, oldItemKey).withAccess(null))

        val stopped = device.setUsableByAgent(item.id, false)
        assertFalse(stopped.isUsableByAgent)
        assertEquals(loginFields(), stopped.fields.withAccess(null))
        val stoppedStored = server.item(item.id)!!
        assertNull(stoppedStored.itemKeyByAgentKey)
        val newItemKey = accountItemKey(stoppedStored, accountKey)
        assertFalse(newItemKey.contentEquals(oldItemKey))

        // A password saved afterwards is sealed by the new key; the old key opens nothing of it.
        device.save(loginFields(password = "changed afterwards"), item.id)
        val after = server.item(item.id)!!
        assertEquals(loginFields(password = "changed afterwards"), fieldsOf(after, newItemKey).withAccess(null))
        assertThrows<Exception> { fieldsOf(after, oldItemKey) }
        // The person's other devices read it as before.
        val other = makeSession(server, "user-ada")
        other.unlockWithPassphrase("ada invented passphrase")
        other.sync()
        val opened = other.item(item.id)
        assertEquals(loginFields(password = "changed afterwards"), opened.fields.withAccess(null))
    }

    @Test fun `turning the agent off on an item the agent does not use changes nothing`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada")
        val device = makeSession(server, "user-ada")
        setUp(device, "ada invented passphrase")
        val item = device.save(loginFields())
        device.setUsableByAgent(item.id, false)
        assertEquals(0, server.rekeyCallCount)
    }

    private class SharedWorld(val server: FakePasswordsServer, val ada: PasswordsSession, val people: Map<String, PasswordsSession>, val itemId: String)

    /** Ada has two devices. The first shares an item with Grace; the second shares it with Linus and Heidi, whose keys the first never saw. */
    private suspend fun makeSharedWorld(): SharedWorld {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace", "user-linus" to "Linus", "user-heidi" to "Heidi")
        val ada = makeSession(server, "user-ada")
        setUp(ada, "ada invented passphrase")
        val people = mutableMapOf<String, PasswordsSession>()
        for (userId in listOf("user-grace", "user-linus", "user-heidi")) {
            val session = makeSession(server, userId)
            setUp(session, "$userId invented passphrase")
            people[userId] = session
        }
        val item = ada.save(loginFields())
        val graceRecipients = ada.recipients(granteeUserId = "user-grace")
        ada.setShares(item.id, listOf(grantee("user-grace")), graceRecipients)

        val secondDevice = makeSession(server, "user-ada")
        secondDevice.unlockWithPassphrase("ada invented passphrase")
        secondDevice.sync()
        val linusAndHeidi = secondDevice.recipients(granteeUserId = "user-linus") + secondDevice.recipients(granteeUserId = "user-heidi")
        val everyone = listOf("user-grace", "user-linus", "user-heidi").map { userId -> grantee(userId) }
        secondDevice.setShares(item.id, everyone, linusAndHeidi)
        ada.sync()
        for (session in people.values) session.sync()
        return SharedWorld(server, ada, people, item.id)
    }

    @Test fun `removing somebody keeps everyone the record still reaches and drops the removed one`() = runBlocking<Unit> {
        val world = makeSharedWorld()
        for (userId in listOf("user-grace", "user-linus", "user-heidi")) {
            val opened = world.people.getValue(userId).item(world.itemId)
            assertEquals(loginFields(), opened.fields.withAccess(null))
        }
        val before = world.server.item(world.itemId)!!
        val graceKeyBefore = before.keys["user-grace"]!!.itemKeyByRecipientKey

        // Heidi is removed. Grace's key is known on this device; Linus's was first seen only on the other one.
        val narrower = listOf("user-grace", "user-linus").map { userId -> grantee(userId) }
        val result = world.ada.setShares(world.itemId, narrower, emptyList())
        assertEquals(loginFields(), result.fields.withAccess(null))

        val after = world.server.item(world.itemId)!!
        assertNotNull(after.keys["user-grace"])
        assertNotEquals(graceKeyBefore, after.keys["user-grace"]?.itemKeyByRecipientKey)
        assertNull(after.keys["user-heidi"])
        // The person chose who keeps access, so the new key is sealed at once for Linus too.
        assertNotNull(after.keys["user-linus"])
        assertFalse(world.ada.needsNewKey(world.itemId))

        // A password saved now is out of Heidi's reach: she sees the item go, and her old copy of the key opens nothing of it.
        world.ada.save(loginFields(password = "changed afterwards"), world.itemId)
        val grace = world.people.getValue("user-grace")
        grace.sync()
        val graceOpened = grace.item(world.itemId)
        assertEquals(loginFields(password = "changed afterwards"), graceOpened.fields.withAccess(null))
        val heidiResult = world.people.getValue("user-heidi").sync()
        assertEquals(listOf(world.itemId), heidiResult.removedItemIds)

        // Linus keeps access with the new key and nobody is left waiting.
        val linus = world.people.getValue("user-linus")
        linus.sync()
        val linusOpened = linus.item(world.itemId)
        assertEquals(loginFields(password = "changed afterwards"), linusOpened.fields.withAccess(null))
        assertEquals(emptyList<PasswordWaitingShare>(), world.ada.waitingShares())
    }

    @Test fun `a rekey that fails after the removal leaves the removal and can be retried`() = runBlocking<Unit> {
        val world = makeSharedWorld()
        world.server.rekeyRefusal = "api: internal error"
        val narrower = listOf("user-grace", "user-linus").map { userId -> grantee(userId) }
        val error = assertThrows<PasswordsError>("the re-key was refused") { runBlocking { world.ada.setShares(world.itemId, narrower, emptyList()) } }
        assertEquals(PasswordsError.ItemNeedsNewKey(world.itemId), error)
        // The removal stands, and the item is marked.
        assertEquals(2, world.server.item(world.itemId)!!.grantees.size)
        assertTrue(world.ada.needsNewKey(world.itemId))
        assertEquals(listOf(world.itemId), world.ada.itemIdsNeedingNewKey())

        world.server.rekeyRefusal = null
        val retried = world.ada.giveNewKey(world.itemId)
        assertEquals(loginFields(), retried.fields.withAccess(null))
        assertFalse(world.ada.needsNewKey(world.itemId))
    }

    @Test fun `the next save of a marked item gives it a new key and seals the new fields with it`() = runBlocking<Unit> {
        val world = makeSharedWorld()
        world.server.rekeyRefusal = "api: internal error"
        val narrower = listOf("user-grace", "user-linus").map { userId -> grantee(userId) }
        runCatching { world.ada.setShares(world.itemId, narrower, emptyList()) }
        world.server.rekeyRefusal = null
        val keyBefore = world.server.item(world.itemId)!!.itemKeyByAccountKey

        val saved = world.ada.save(loginFields(password = "changed afterwards"), world.itemId)
        assertEquals(loginFields(password = "changed afterwards"), saved.fields.withAccess(null))
        assertNotEquals(keyBefore, world.server.item(world.itemId)!!.itemKeyByAccountKey)
        assertFalse(world.ada.needsNewKey(world.itemId))
    }

    @Test fun `a change on another device is reloaded and the rekey tried once`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada")
        val first = makeSession(server, "user-ada")
        setUp(first, "ada invented passphrase")
        val item = first.save(loginFields())
        val second = makeSession(server, "user-ada")
        second.unlockWithPassphrase("ada invented passphrase")
        second.sync()
        // The second device changes the password; the first has not heard.
        second.save(loginFields(password = "changed elsewhere"), item.id)

        val given = first.giveNewKey(item.id)
        assertEquals(loginFields(password = "changed elsewhere"), given.fields.withAccess(null))
        assertEquals(2, server.rekeyCallCount)
    }
}
