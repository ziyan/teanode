package com.teanode.kit.passwords

import com.teanode.kit.passwords.SharingFixture.ITERATION_COUNT
import com.teanode.kit.passwords.SharingFixture.loginFields
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.io.TempDir
import java.io.File

/**
 * A device that has never seen an item cannot tell an old signed record from the newest one, so it does nothing by itself
 * for that item; a record with the version it remembers and another mac is a replay too; and what the device remembers
 * comes only from what it verified and did itself.
 */
class PasswordsReplayTest {
    private val passphrase = "ada invented passphrase"

    private fun makeCache() = SharingFixture.makeCache()

    private fun makeSession(server: FakePasswordsServer, userId: String, cache: PasswordsCache = makeCache()) = PasswordsSession(server.api(userId), cache, userId)

    /** The sealed text and agent copy the server held at one moment. */
    private class Snapshot(val encryptedFields: String, val itemKeyByAccountKey: String, val itemKeyByAgentKey: String?)

    private class AgentTurnedOff(
        val server: FakePasswordsServer,
        val one: PasswordsSession,
        val oneCache: PasswordsCache,
        val itemId: String,
        val whileOn: Snapshot,
    )

    /** Ada's first device turned the agent on (record version 2) and then off (version 3). The server state while the agent was on is returned. */
    private suspend fun makeItemWhoseAgentWasTurnedOff(): AgentTurnedOff {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        val oneCache = makeCache()
        val one = makeSession(server, "user-ada", oneCache)
        one.setUp(passphrase, ITERATION_COUNT)
        val item = one.save(loginFields())
        val on = one.setUsableByAgent(item.id, true)
        assertEquals(2L, on.fields.access?.recordVersion)
        val stored = server.item(item.id)!!
        val whileOn = Snapshot(stored.encryptedFields, stored.itemKeyByAccountKey, stored.itemKeyByAgentKey)
        assertNotNull(whileOn.itemKeyByAgentKey)
        val off = one.setUsableByAgent(item.id, false)
        assertEquals(3L, off.fields.access?.recordVersion)
        assertFalse(off.isUsableByAgent)
        return AgentTurnedOff(server, one, oneCache, item.id, whileOn)
    }

    /**
     * The audit's proof: the server shows a second device that has nothing of the item the record from while the agent was
     * on. The device believes it (it cannot know better) but gives the item no new key by itself, so the agent gets no copy
     * of a new key; the person finishes it.
     */
    @Test fun `a server that shows an old record to a new device cannot make it give the agent a new key`() = runBlocking<Unit> {
        val world = makeItemWhoseAgentWasTurnedOff()
        val twoCache = makeCache()
        val two = makeSession(world.server, "user-ada", twoCache)
        two.unlockWithPassphrase(passphrase)
        world.server.replaceStoredFields(world.itemId, world.whileOn.encryptedFields, world.whileOn.itemKeyByAccountKey, needsNewKey = false)
        world.server.setUsableByAgent("user-ada", world.itemId, true, world.whileOn.itemKeyByAgentKey)
        two.sync(isFull = true)
        val seenOnTwo = two.item(world.itemId)
        assertEquals(PasswordsSharingCheck.Verified, seenOnTwo.sharingCheck, "no memory of version 3 on this device, so the old record is believed")

        world.server.markNeedsNewKey(world.itemId)
        two.sync()
        val rekeysBefore = world.server.rekeyCallCount
        val given = two.giveNewKeysToMarkedItems()
        assertEquals(emptyList<String>(), given)
        assertEquals(rekeysBefore, world.server.rekeyCallCount, "nothing was sent to the server")
        assertEquals(setOf(world.itemId), two.itemIdsNeedingYou())

        // The same device in a later session (the same pin store) remembers the item, but not that it ever sealed an agent copy.
        val laterSession = makeSession(world.server, "user-ada", twoCache)
        laterSession.unlockWithPassphrase(passphrase)
        laterSession.sync()
        val givenLater = laterSession.giveNewKeysToMarkedItems()
        assertEquals(listOf(world.itemId), givenLater, "an item remembered from an earlier session is given a key")
        assertNull(world.server.item(world.itemId)?.itemKeyByAgentKey, "this device never made an agent copy, so the new key has none")
    }

    /**
     * Device one remembers version 3. A device that has never seen the item signs a record with the same version 3 (it was
     * shown version 2): the server then shows device one a signed record with the version it remembers and another mac,
     * which is a replay.
     */
    @Test fun `a record with the remembered version and another mac is a replay and signing goes past both`() = runBlocking<Unit> {
        val world = makeItemWhoseAgentWasTurnedOff()
        val remembered = world.one.item(world.itemId).fields.access!!
        assertEquals(3L, remembered.recordVersion)

        val two = makeSession(world.server, "user-ada")
        two.unlockWithPassphrase(passphrase)
        world.server.replaceStoredFields(world.itemId, world.whileOn.encryptedFields, world.whileOn.itemKeyByAccountKey, needsNewKey = false)
        two.sync(isFull = true)
        world.server.addPerson("user-ben", "Ben")
        makeSession(world.server, "user-ben").setUp("ben invented passphrase", ITERATION_COUNT)
        val benRecipients = two.recipients(granteeUserId = "user-ben")
        val signedByTwo = two.setShares(world.itemId, listOf(PasswordShareGranteeInput(granteeUserId = "user-ben")), benRecipients)
        assertEquals(3L, signedByTwo.fields.access?.recordVersion, "version 2 was all this device had seen")
        assertNotEquals(remembered.recordMac, signedByTwo.fields.access?.recordMac)

        world.one.sync(isFull = true)
        val onOne = world.one.item(world.itemId)
        assertEquals(PasswordsSharingCheck.Replayed, onOne.sharingCheck)
        assertFalse(onOne.isUsableByAgent)
        assertNull(onOne.verifiedAccess)
        assertEquals(emptyList<String>(), world.one.giveNewKeysToMarkedItems())

        // Setting sharing again goes past the remembered version and past the incoming one.
        val repaired = world.one.setShares(world.itemId, emptyList(), emptyList())
        assertEquals(PasswordsSharingCheck.Verified, repaired.sharingCheck)
        assertTrue((repaired.fields.access?.recordVersion ?: 0) > 3)
    }

    /** The remembered version and mac survive a new session on the same pin store, and a record that is the same one is not a replay. */
    @Test fun `the same record seen again is not a replay and the mac is remembered`() = runBlocking<Unit> {
        val world = makeItemWhoseAgentWasTurnedOff()
        val reopened = makeSession(world.server, "user-ada", world.oneCache)
        reopened.unlockWithPassphrase(passphrase)
        reopened.sync(isFull = true)
        val again = reopened.item(world.itemId)
        assertEquals(PasswordsSharingCheck.Verified, again.sharingCheck)
        val pins = world.oneCache.loadPins("user-ada")
        assertEquals(3L, pins.itemMemories[world.itemId]?.highestRecordVersion)
        assertEquals(again.fields.access?.recordMac, pins.itemMemories[world.itemId]?.highestRecordMac)
    }

    /** Nothing about who holds a key is taken from the server's lists: a memory made by looking at a shared item has no people sealed for and no agent copy. */
    @Test fun `looking at an item remembers no one as sealed for`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        server.addPerson("user-ben", "Ben")
        val ben = makeSession(server, "user-ben")
        ben.setUp("ben invented passphrase", ITERATION_COUNT)
        val oneCache = makeCache()
        val one = makeSession(server, "user-ada", oneCache)
        one.setUp(passphrase, ITERATION_COUNT)
        val item = one.save(loginFields())
        val recipients = one.recipients(granteeUserId = "user-ben")
        one.setShares(item.id, listOf(PasswordShareGranteeInput(granteeUserId = "user-ben")), recipients)
        one.setUsableByAgent(item.id, true)
        val own = oneCache.loadPins("user-ada").itemMemories[item.id]
        assertEquals(listOf("user-ben"), own?.sealedForUserIds)
        assertEquals(true, own?.didMakeAgentCopy)

        val twoCache = makeCache()
        val two = makeSession(server, "user-ada", twoCache)
        two.unlockWithPassphrase(passphrase)
        two.sync(isFull = true)
        two.list()
        val seen = twoCache.loadPins("user-ada").itemMemories[item.id]
        assertEquals(3L, seen?.highestRecordVersion)
        assertEquals(emptyList<String>(), seen?.sealedForUserIds)
        assertEquals(false, seen?.didMakeAgentCopy)
    }

    // What the phone remembers survives the server ending the sign-in

    @Test fun `only a person removing the account makes the phone forget keys and item memories`() = runBlocking<Unit> {
        assertTrue(PasswordsSignInEnd.RemovedByPerson.isForgettingKeysAndItemMemories)
        assertFalse(PasswordsSignInEnd.EndedByServer.isForgettingKeysAndItemMemories)

        val world = makeItemWhoseAgentWasTurnedOff()
        // The copy of the items goes when the sign-in ends; the item memory stays.
        world.oneCache.eraseKeepingPins()
        val kept = world.oneCache.loadPins("user-ada").itemMemories[world.itemId]
        assertEquals(3L, kept?.highestRecordVersion)
        assertNotNull(kept?.highestRecordMac)

        // Signing in again as the same person: the session finds the memory and still calls the old record a replay.
        val again = makeSession(world.server, "user-ada", world.oneCache)
        again.unlockWithPassphrase(passphrase)
        world.server.replaceStoredFields(world.itemId, world.whileOn.encryptedFields, world.whileOn.itemKeyByAccountKey, needsNewKey = false)
        again.sync(isFull = true)
        val replayed = again.item(world.itemId)
        assertEquals(PasswordsSharingCheck.Replayed, replayed.sharingCheck)

        // Removing the account from the phone clears it.
        world.oneCache.erase()
        assertTrue(world.oneCache.loadPins("user-ada").itemMemories.isEmpty())
    }

    @Test fun `a file of pins is kept when the server ended the sign-in and removed when the person removed the account`(@TempDir directory: File) {
        val cache = PasswordsCache(FilePasswordsStorage(directory))
        cache.recordSeenRecord("user-ada", "item-1", 2, "mac-two")
        cache.eraseKeepingPins()
        assertEquals(2L, cache.loadPins("user-ada").itemMemories["item-1"]?.highestRecordVersion)
        cache.erase()
        assertTrue(cache.loadPins("user-ada").itemMemories.isEmpty())
    }

    // People the person named on this device

    /**
     * A person Ada named directly on this device is hers to complete by itself later; a person named only by a circle is
     * not (who is in a circle is the server's word); and a device that never saw the item does not, whoever Ada named
     * elsewhere.
     */
    @Test fun `a waiting share completes by itself only for a person named directly on this device`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        for ((userId, name) in listOf("user-ada" to "Ada", "user-ben" to "Ben", "user-grace" to "Grace")) server.addPerson(userId, name)
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        val oneCache = makeCache()
        val one = makeSession(server, "user-ada", oneCache)
        one.setUp(passphrase, ITERATION_COUNT)
        val item = one.save(loginFields())
        // Ben and Grace have not set up Passwords: Ben is named, Grace only through the circle.
        one.setShares(
            item.id,
            listOf(PasswordShareGranteeInput(granteeUserId = "user-ben"), PasswordShareGranteeInput(granteeCircleId = "circle-one")),
            emptyList(),
        )
        assertEquals(listOf("user-ben"), oneCache.loadPins("user-ada").itemMemories[item.id]?.chosenUserIds)
        for ((userId, name) in listOf("user-ben" to "ben", "user-grace" to "grace")) {
            makeSession(server, userId).setUp("$name invented passphrase", ITERATION_COUNT)
        }

        // A later session on the same device (the memory is from before it) seals for Ben only.
        val later = makeSession(server, "user-ada", oneCache)
        later.unlockWithPassphrase(passphrase)
        later.sync()
        val completed = later.completeWaitingShares()
        assertEquals(1, completed.addedKeyCount)
        val stored = server.item(item.id)
        assertNotNull(stored?.keys?.get("user-ben"))
        assertNull(stored?.keys?.get("user-grace"))
        assertEquals(setOf(item.id), later.itemIdsNeedingYou(), "Grace joined through the circle and waits for the person")

        // A device that never saw the item does nothing by itself, whoever was named elsewhere.
        val other = makeSession(server, "user-ada")
        other.unlockWithPassphrase(passphrase)
        other.sync()
        val byOther = other.completeWaitingShares()
        assertEquals(0, byOther.addedKeyCount)

        // Taking Ben off the list forgets the choice.
        later.setShares(item.id, listOf(PasswordShareGranteeInput(granteeCircleId = "circle-one")), emptyList())
        assertEquals(emptyList<String>(), oneCache.loadPins("user-ada").itemMemories[item.id]?.chosenUserIds)
    }
}
