package com.teanode.kit.passwords

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.passwords.SharingFixture.makeCache
import com.teanode.kit.passwords.SharingFixture.makeServer
import com.teanode.kit.passwords.SharingFixture.makeSession
import com.teanode.kit.passwords.SharingFixture.setUp
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.api.io.TempDir
import java.io.File

/** TestClock is a clock a test moves by hand. */
internal class TestClock {
    var epochMS = 1_800_000_000_000L

    fun advance(minutes: Long) {
        epochMS += minutes * 60_000
    }
}

/**
 * These check that the access record sealed in an item is the person's own (signed with the account key, not older than
 * one already seen), that a new key given without the person's say never reaches further than the old one did, and that
 * the share screen's split between what the record lists and what only the server lists is the same everywhere.
 */
class PasswordsRecordTest {
    private val adaPassphrase = "ada invented passphrase"

    private fun loginFields(name: String = "Example", password: String = "correct horse") = SharingFixture.loginFields(name, password)

    private fun grantee(userId: String) = PasswordShareGranteeInput(granteeUserId = userId)

    /** What a holder of the item key can do: open the item's key (with the account key, which the test has, standing in for the key a grantee was given) and seal other fields with it. */
    private fun replaceFields(itemId: String, server: FakePasswordsServer, accountKey: ByteArray, access: PasswordsItemAccess?, needsNewKey: Boolean = false) {
        val stored = server.item(itemId)!!
        val itemKey = PasswordsCrypto.open(
            PasswordsSealedValue.fromJsonText(stored.itemKeyByAccountKey), accountKey, PasswordsCrypto.itemKeyAssociatedData(itemId),
        )
        val kind = PasswordsItemKind.fromRawValue(stored.itemKind)!!
        val opened = PasswordsCrypto.openItemFields(PasswordsSealedValue.fromJsonText(stored.encryptedFields), itemKey, itemId, kind)
        val forged = PasswordsCrypto.sealItemFields(opened.withAccess(access), itemKey, itemId)
        server.replaceStoredFields(itemId, forged.jsonText, stored.itemKeyByAccountKey, needsNewKey)
    }

    /** A signature made with another account key, as a holder of the item key who is not the owner would have to. */
    private fun foreignMac(itemId: String, access: PasswordsItemAccess): String =
        PasswordsCrypto.macOfAccess(PasswordsCrypto.makeKey(), itemId, PasswordsItemKind.Login, access)

    /** Ada owns an item shared with Ben; both have set up Passwords. */
    private class Setup(
        val server: FakePasswordsServer,
        val flags: HookedServerFlags,
        val ada: PasswordsSession,
        val adaCache: PasswordsCache,
        val accountKey: ByteArray,
        val itemId: String,
    )

    private suspend fun makeSharedItem(
        extraPeople: List<Pair<String, String>> = emptyList(),
        sharedWith: List<String> = listOf("user-ben"),
        clock: TestClock? = null,
    ): Setup {
        val allPeople = listOf("user-ben" to "Ben", "user-mallory" to "Mallory") + extraPeople
        val server = makeServer(*(listOf("user-ada" to "Ada") + allPeople).toTypedArray())
        val flags = HookedServerFlags()
        val cache = makeCache()
        val ada = makeSession(server, "user-ada", flags, cache, if (clock != null) ({ clock.epochMS }) else System::currentTimeMillis)
        setUp(ada, adaPassphrase)
        for ((userId, name) in allPeople) setUp(makeSession(server, userId), "$name invented passphrase")
        val accountKey = makeSession(server, "user-ada").unlockWithPassphrase(adaPassphrase).accountKey
        val item = ada.save(loginFields())
        val recipients = mutableListOf<PasswordRecipient>()
        for (userId in sharedWith) recipients += ada.recipients(granteeUserId = userId)
        ada.setShares(item.id, sharedWith.map { userId -> grantee(userId) }, recipients)
        return Setup(server, flags, ada, cache, accountKey, item.id)
    }

    // A record the person did not sign

    @Test fun `a record forged by an item key holder reaches nobody and lets the agent nowhere`() = runBlocking<Unit> {
        val setup = makeSharedItem()
        val forgedGrantees = listOf<PasswordsGrantee>(PasswordsGrantee.Person("user-ben"), PasswordsGrantee.Person("user-mallory"))
        val unsigned = PasswordsItemAccess(isUsableByAgent = true, grantees = forgedGrantees)
        val missingMac = PasswordsItemAccess(isUsableByAgent = true, grantees = forgedGrantees, recordVersion = 99)
        val wrongMacBase = PasswordsItemAccess(isUsableByAgent = true, grantees = forgedGrantees, recordVersion = 99)
        val wrongMac = wrongMacBase.copy(recordMac = foreignMac(setup.itemId, wrongMacBase))
        // A signature that was right for another version of the same record.
        val signed = PasswordsCrypto.signAccess(setup.accountKey, setup.itemId, PasswordsItemKind.Login, PasswordsItemAccess(isUsableByAgent = false, grantees = emptyList()), 40)
        val tampered = PasswordsItemAccess(isUsableByAgent = true, grantees = forgedGrantees, recordVersion = signed.recordVersion, recordMac = signed.recordMac)

        for (forged in listOf(unsigned, missingMac, wrongMac, tampered)) {
            replaceFields(setup.itemId, setup.server, setup.accountKey, forged, needsNewKey = true)
            // The server adds its own word to the forged consent: Mallory is a grantee, and the agent flag is on.
            setup.server.setShares("user-ada", setup.itemId, listOf(grantee("user-ben"), grantee("user-mallory")), emptyList())
            setup.server.setUsableByAgent("user-ada", setup.itemId, true, "forged agent copy")
            setup.ada.sync(isFull = true)

            val item = setup.ada.item(setup.itemId)
            assertEquals(PasswordsSharingCheck.Unverified, item.sharingCheck)
            assertNull(item.verifiedAccess)
            assertFalse(item.isUsableByAgent)
            assertTrue(item.recordedShares.isEmpty())
            assertEquals(setOf("user-ben", "user-mallory"), item.unrecordedShares.mapNotNull { share -> share.granteeUserId }.toSet())

            // Nothing is sealed for anybody from it, and nothing re-keys.
            setup.flags.calls.clear()
            val completed = setup.ada.completeWaitingShares()
            assertEquals(0, completed.addedKeyCount)
            assertEquals(emptyList<String>(), setup.flags.calls.filter { call -> call == "addPasswordShareKeys" })
            val given = setup.ada.giveNewKeysToMarkedItems()
            assertEquals(emptyList<String>(), given)
            assertFalse(setup.flags.calls.contains("rekeyPasswordItem"))
            val agentError = assertThrows<Throwable>("the agent must not be let in on a record that was not checked") {
                runBlocking { setup.ada.setUsableByAgent(setup.itemId, true) }
            }
            assertTrue(agentError is PasswordsError.SharingCouldNotBeChecked)
            val keyError = assertThrows<Throwable>("a new key must not be built on a record that was not checked") {
                runBlocking { setup.ada.giveNewKey(setup.itemId) }
            }
            assertTrue(keyError is PasswordsError.SharingCouldNotBeChecked)
            assertFalse(setup.flags.calls.contains("setPasswordItemUsableByAgent"))
        }
    }

    @Test fun `an edit of an item whose record was not checked keeps it unchecked`() = runBlocking<Unit> {
        val setup = makeSharedItem()
        replaceFields(
            setup.itemId, setup.server, setup.accountKey,
            PasswordsItemAccess(isUsableByAgent = true, grantees = listOf(PasswordsGrantee.Person("user-mallory"))),
        )
        setup.ada.sync(isFull = true)
        val edited = setup.ada.save(loginFields(password = "edited"), setup.itemId)
        assertEquals(PasswordsSharingCheck.Unverified, edited.sharingCheck)
        assertFalse(edited.isUsableByAgent)
    }

    @Test fun `setting sharing again repairs an item whose record was not checked and rekeys it`() = runBlocking<Unit> {
        val setup = makeSharedItem()
        // Mallory held the key and wrote a record that lets the agent in, and the server lists her.
        replaceFields(
            setup.itemId, setup.server, setup.accountKey,
            PasswordsItemAccess(isUsableByAgent = true, grantees = listOf(PasswordsGrantee.Person("user-mallory"))),
        )
        setup.server.setShares("user-ada", setup.itemId, listOf(grantee("user-ben"), grantee("user-mallory")), emptyList())
        setup.server.setUsableByAgent("user-ada", setup.itemId, true, "forged agent copy")
        setup.ada.sync(isFull = true)
        val keyBefore = setup.server.item(setup.itemId)?.itemKeyByAccountKey

        // The share screen starts from what the record lists, which is nobody; the person chooses Ben again.
        val recipients = setup.ada.recipients(granteeUserId = "user-ben")
        setup.flags.calls.clear()
        val repaired = setup.ada.setShares(setup.itemId, listOf(grantee("user-ben")), recipients)
        assertEquals(PasswordsSharingCheck.Verified, repaired.sharingCheck)
        assertEquals(listOf<PasswordsGrantee>(PasswordsGrantee.Person("user-ben")), repaired.fields.access?.grantees)
        assertEquals(false, repaired.fields.access?.isUsableByAgent)
        assertTrue(setup.flags.calls.contains("rekeyPasswordItem"))
        val stored = setup.server.item(setup.itemId)!!
        assertNotEquals(keyBefore, stored.itemKeyByAccountKey)
        assertNull(stored.itemKeyByAgentKey)
        assertEquals(false, stored.isUsableByAgent)
        assertEquals(listOf("user-ben"), stored.grantees.mapNotNull { entry -> entry.granteeUserId })
        assertEquals(false, stored.needsNewKey)
        // Ben, whom the person chose, keeps a key for the new item key.
        assertNotNull(stored.keys["user-ben"])
        assertNull(stored.keys["user-mallory"])
    }

    // An old record sent back

    @Test fun `an older record the server sends back is not acted on and is remembered`() = runBlocking<Unit> {
        val setup = makeSharedItem()
        // Version 2 shares with Ben. Capture what the server holds, then take Ben away (version 3, new key).
        val atShare = setup.server.item(setup.itemId)!!
        val sharedFields = atShare.encryptedFields
        val sharedKey = atShare.itemKeyByAccountKey
        setup.ada.setShares(setup.itemId, emptyList(), emptyList())
        val unshared = setup.ada.item(setup.itemId)
        assertEquals(3L, unshared.fields.access?.recordVersion)

        // The server puts version 2 back, asks for a new key, and lists Ben as a share again.
        setup.server.replaceStoredFields(setup.itemId, sharedFields, sharedKey, needsNewKey = true)
        setup.server.setShares("user-ada", setup.itemId, listOf(grantee("user-ben")), emptyList())
        setup.flags.calls.clear()
        setup.ada.sync(isFull = true)

        val replayed = setup.ada.item(setup.itemId)
        assertEquals(PasswordsSharingCheck.Replayed, replayed.sharingCheck)
        assertNull(replayed.verifiedAccess)
        assertFalse(replayed.isUsableByAgent)
        assertEquals(emptyList<String>(), setup.ada.giveNewKeysToMarkedItems())
        val completed = setup.ada.completeWaitingShares()
        assertEquals(0, completed.addedKeyCount)
        assertEquals(emptyList<String>(), setup.flags.calls.filter { call -> call == "rekeyPasswordItem" || call == "addPasswordShareKeys" })

        // A device that is opened again later (the same pin store) still remembers version 3.
        val reopened = makeSession(setup.server, "user-ada", cache = setup.adaCache)
        reopened.unlockWithPassphrase(adaPassphrase)
        val again = reopened.item(setup.itemId)
        assertEquals(PasswordsSharingCheck.Replayed, again.sharingCheck)

        // The person sets sharing again: the new record is built on the highest version seen (3), not on the replayed
        // one's (2), so every device that saw version 3 takes it as newer.
        val repaired = setup.ada.setShares(setup.itemId, emptyList(), emptyList())
        assertEquals(PasswordsSharingCheck.Verified, repaired.sharingCheck)
        assertTrue((repaired.fields.access?.recordVersion ?: 0) > 3)
    }

    @Test fun `every change of the record raises the version by one`() = runBlocking<Unit> {
        val setup = makeSharedItem()
        val shared = setup.ada.item(setup.itemId)
        assertEquals(2L, shared.fields.access?.recordVersion)
        val agentOn = setup.ada.setUsableByAgent(setup.itemId, true)
        assertEquals(3L, agentOn.fields.access?.recordVersion)
        val agentOff = setup.ada.setUsableByAgent(setup.itemId, false)
        assertEquals(4L, agentOff.fields.access?.recordVersion)
        assertEquals(PasswordsSharingCheck.Verified, agentOff.sharingCheck)
        // A save that changes only the fields keeps the record as it was.
        val edited = setup.ada.save(loginFields(password = "edited"), setup.itemId)
        assertEquals(4L, edited.fields.access?.recordVersion)
    }

    // A new key never reaches further than the old one did

    @Test fun `an automatic new key is sealed only for people who held a key and the agent copy only if it had one`() = runBlocking<Unit> {
        val setup = makeSharedItem(extraPeople = listOf("user-dan" to "Dan"), sharedWith = listOf("user-ben"))
        val ben = "user-ben"
        // A second device of Ada's adds Dan and the agent, and then the server asks for a new key before this device has
        // looked at the item again.
        val second = makeSession(setup.server, "user-ada")
        second.unlockWithPassphrase(adaPassphrase)
        second.sync()
        val recipients = second.recipients(granteeUserId = "user-ben") + second.recipients(granteeUserId = "user-dan")
        second.setShares(setup.itemId, listOf(grantee(ben), grantee("user-dan")), recipients)
        second.setUsableByAgent(setup.itemId, true)
        setup.server.markNeedsNewKey(setup.itemId)
        // This device already knows Dan's key, but never saw Dan or the agent hold a key for the item.
        val danKey = setup.server.publicKey("user-dan")
        setup.adaCache.recordKnownIfAbsent("user-ada", "user-dan", danKey)

        setup.ada.sync()
        val marked = setup.ada.item(setup.itemId)
        assertTrue(marked.needsNewKey)
        assertEquals(PasswordsSharingCheck.Verified, marked.sharingCheck)
        assertEquals(setOf<PasswordsGrantee>(PasswordsGrantee.Person(ben), PasswordsGrantee.Person("user-dan")), (marked.fields.access?.grantees ?: emptyList()).toSet())
        assertEquals(true, marked.fields.access?.isUsableByAgent)

        setup.flags.calls.clear()
        val given = setup.ada.giveNewKeysToMarkedItems()
        assertEquals(listOf(setup.itemId), given)
        val rekeyInput = setup.flags.lastRekeyInput!!
        assertEquals(listOf(ben), rekeyInput.shareKeys.map { key -> key.recipientUserId })
        assertNull(rekeyInput.itemKeyByAgentKey)
        // Dan is in the record but this device never saw him hold a key, so the automatic key leaves him for the waiting shares.
        val waiting = setup.ada.waitingShares()
        assertEquals(listOf("user-dan"), waiting.map { share -> share.recipient.userId })
        // The record stays what the person signed.
        val after = setup.ada.item(setup.itemId)
        assertEquals(marked.fields.access, after.fields.access)
    }

    @Test fun `an automatic new key keeps the agent copy that this device saw and the keys of whoever it saw hold one`() = runBlocking<Unit> {
        val setup = makeSharedItem()
        setup.ada.setUsableByAgent(setup.itemId, true)
        setup.ada.sync(isFull = true)
        setup.ada.list()
        setup.server.markNeedsNewKey(setup.itemId)
        setup.ada.sync()
        val given = setup.ada.giveNewKeysToMarkedItems()
        assertEquals(listOf(setup.itemId), given)
        val rekeyInput = setup.flags.lastRekeyInput!!
        assertEquals(listOf("user-ben"), rekeyInput.shareKeys.map { key -> key.recipientUserId })
        assertNotNull(rekeyInput.itemKeyByAgentKey)
        val item = setup.ada.item(setup.itemId)
        assertTrue(item.isUsableByAgent)
    }

    @Test fun `an item gets an automatic new key once in ten minutes and a session gives at most twenty`() = runBlocking<Unit> {
        val clock = TestClock()
        val setup = makeSharedItem(clock = clock)
        setup.server.markNeedsNewKey(setup.itemId)
        setup.ada.sync()
        val first = setup.ada.giveNewKeysToMarkedItems()
        assertEquals(listOf(setup.itemId), first)

        // Marked again straight away: held back, and said to need attention.
        setup.server.markNeedsNewKey(setup.itemId)
        setup.ada.sync()
        clock.advance(9)
        val second = setup.ada.giveNewKeysToMarkedItems()
        assertEquals(emptyList<String>(), second)
        assertEquals(setOf(setup.itemId), setup.ada.itemIdsNeedingAttention())
        // Only the first, automatic one reached the server.
        assertEquals(1, setup.server.rekeyCallCount)
        // The person can still ask for it themselves.
        setup.ada.giveNewKey(setup.itemId)
        assertEquals(2, setup.server.rekeyCallCount)

        // After ten minutes it may again.
        setup.server.markNeedsNewKey(setup.itemId)
        setup.ada.sync()
        clock.advance(2)
        val third = setup.ada.giveNewKeysToMarkedItems()
        assertEquals(listOf(setup.itemId), third)
        assertTrue(setup.ada.itemIdsNeedingAttention().isEmpty())

        // The limit per session.
        val itemIds = mutableListOf<String>()
        for (index in 0 until PasswordsSession.MOST_AUTOMATIC_REKEYS_PER_SESSION + 1) {
            val item = setup.ada.save(loginFields(name = "Item $index"))
            itemIds.add(item.id)
            setup.server.markNeedsNewKey(item.id)
        }
        setup.ada.sync()
        val batch = setup.ada.giveNewKeysToMarkedItems()
        // Two keys were given already in this session (the first and the third above).
        assertEquals(PasswordsSession.MOST_AUTOMATIC_REKEYS_PER_SESSION - 2, batch.size)
        assertEquals(itemIds.size - batch.size, setup.ada.itemIdsNeedingAttention().size)
    }

    // Share keys carry the item key

    @Test fun `the keys of a share carry the item key as the item showed it`() = runBlocking<Unit> {
        val setup = makeSharedItem()
        assertFalse(setup.flags.lastShareKeys.isEmpty())
        // The item key has not changed since (no rekey), so the text the keys carry is the stored one.
        val stored = setup.server.item(setup.itemId)
        for (key in setup.flags.lastShareKeys) assertEquals(stored?.itemKeyByAccountKey, key.itemKey)
    }

    // One split of recorded and not recorded shares

    @Test fun `a share people see is one the record lists and other shares are named as unrecorded until sharing is saved`() = runBlocking<Unit> {
        val setup = makeSharedItem()
        // The server lists Mallory too; the person never shared the item with her.
        setup.server.setShares("user-ada", setup.itemId, listOf(grantee("user-ben"), grantee("user-mallory")), emptyList())
        setup.ada.sync(isFull = true)
        val item = setup.ada.item(setup.itemId)
        assertEquals(PasswordsSharingCheck.Verified, item.sharingCheck)
        assertEquals(listOf("user-ben"), item.recordedShares.mapNotNull { share -> share.granteeUserId })
        assertEquals(listOf("user-mallory"), item.unrecordedShares.mapNotNull { share -> share.granteeUserId })

        // Saving sharing with what the record lists removes the server's extra share and gives the item a new key.
        val recipients = setup.ada.recipients(granteeUserId = "user-ben")
        val keyBefore = setup.server.item(setup.itemId)?.itemKeyByAccountKey
        setup.flags.calls.clear()
        val saved = setup.ada.setShares(setup.itemId, listOf(grantee("user-ben")), recipients)
        assertTrue(saved.unrecordedShares.isEmpty())
        assertEquals(listOf("user-ben"), saved.recordedShares.mapNotNull { share -> share.granteeUserId })
        assertTrue(setup.flags.calls.contains("rekeyPasswordItem"))
        assertNotEquals(keyBefore, setup.server.item(setup.itemId)?.itemKeyByAccountKey)
    }

    // Deleting for good

    @Test fun `purging a deleted item removes it everywhere and refuses an item that was not deleted`() = runBlocking<Unit> {
        val setup = makeSharedItem()
        val notDeleted = assertThrows<Throwable>("an item that was not deleted cannot be purged") { runBlocking { setup.ada.purge(setup.itemId) } }
        assertEquals(PasswordsError.ItemNotFound, notDeleted)
        setup.ada.delete(setup.itemId)
        val listedBefore = setup.ada.list()
        assertEquals(listOf(setup.itemId), listedBefore.items.filter { item -> item.isDeleted }.map { item -> item.id })
        setup.ada.purge(setup.itemId)
        assertNull(setup.server.item(setup.itemId))
        assertTrue(setup.ada.list().items.isEmpty())
        assertEquals(true, setup.adaCache.load()?.items?.isEmpty())
        val gone = assertThrows<Throwable>("the item is gone") { setup.ada.item(setup.itemId) }
        assertEquals(PasswordsError.ItemNotFound, gone)
    }

    // What the device remembers

    @Test fun `what a device remembers about an item only goes up and survives a new storage instance`(@TempDir directory: File) {
        val first = PasswordsCache(FilePasswordsStorage(directory))
        first.recordSeenRecord("user-ada", "item-1", 5, "mac-five")
        first.recordSeenRecord("user-ada", "item-1", 3, "mac-three")
        first.recordSeenRecord("user-ada", "item-1", 5, "mac-other")
        first.recordSealed("user-ada", "item-1", PasswordsSealedChange(listOf("user-ben", "user-ben", "user-carol"), isReplacing = false, didMakeAgentCopy = true))
        first.recordSeenRecord("user-ada", "item-1", 4, "mac-four")
        val second = PasswordsCache(FilePasswordsStorage(directory))
        val pins = second.loadPins("user-ada")
        assertEquals(
            PasswordsItemMemory(highestRecordVersion = 5, highestRecordMac = "mac-five", sealedForUserIds = listOf("user-ben", "user-carol"), didMakeAgentCopy = true),
            pins.itemMemories["item-1"],
        )
        // What is remembered about an item follows the item id, not the user id the server names.
        assertEquals(5L, second.loadPins("user-grace").itemMemories["item-1"]?.highestRecordVersion)

        val memory = PasswordsCache(InMemoryPasswordsStorage())
        memory.recordSeenRecord("user-ada", "item-1", 2, null)
        memory.recordSeenRecord("user-ada", "item-1", 1, "mac-one")
        assertEquals(2L, memory.loadPins("user-ada").itemMemories["item-1"]?.highestRecordVersion)
        // A version remembered without its mac takes the mac of the next record seen at that version.
        memory.recordSeenRecord("user-ada", "item-1", 2, "mac-two")
        assertEquals("mac-two", memory.loadPins("user-ada").itemMemories["item-1"]?.highestRecordMac)
        memory.erase()
        assertTrue(memory.loadPins("user-ada").itemMemories.isEmpty())
    }

    @Test fun `a record with the remembered version and another mac is a replay and a memory of an earlier build still reads`() {
        val memory = PasswordsItemMemory(highestRecordVersion = 3, highestRecordMac = "mac-three")
        assertFalse(memory.isReplay(3, "mac-three"))
        assertTrue(memory.isReplay(3, "mac-other"))
        assertTrue(memory.isReplay(2, "mac-three"))
        assertFalse(memory.isReplay(4, "mac-four"))
        // The first build kept only a version and the people it saw holding a key.
        val earlier = """{"highestRecordVersion":4,"keyHolderUserIds":["user-ben"],"hadAgentCopy":true}"""
        val decoded = TeaNodeJson.decodeFromString(PasswordsItemMemory.serializer(), earlier)
        assertEquals(PasswordsItemMemory(highestRecordVersion = 4), decoded)
    }

    @Test fun `pins written before items had a memory still read`() {
        val text = """{"formatVersion":1,"userId":"user-ada","seenPublicKeys":{},"confirmedPublicKeys":{}}""" // the first version's shape
        val pins = TeaNodeJson.decodeFromString(PasswordsPins.serializer(), text)
        assertTrue(pins.itemMemories.isEmpty())
    }
}
