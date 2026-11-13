package com.teanode.kit.passwords

import com.teanode.kit.TeaNodeError
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

/**
 * The session against a fake server that keeps only sealed text, so a pass here means two devices really can open each
 * other's work through nothing but what the server holds. (Sharing is not ported: the stand-in server keeps no shares.)
 */
class PasswordsSessionTest {
    /** The lowest count the format accepts, so the tests do not spend minutes in key derivation. */
    private val iterationCount = PasswordsCrypto.MINIMUM_PASSPHRASE_ITERATION_COUNT

    private class Device(val session: PasswordsSession, val cache: PasswordsCache, val storage: InMemoryPasswordsStorage)

    private fun makeDevice(server: FakePasswordsServer, userId: String = "user-ada", clock: () -> Long = System::currentTimeMillis): Device {
        val storage = InMemoryPasswordsStorage()
        val cache = PasswordsCache(storage)
        return Device(PasswordsSession(server, cache, userId, clock), cache, storage)
    }

    private fun loginFields(name: String = "Example", password: String = "correct horse") =
        PasswordsItemFields.Login(PasswordsLoginFields(name = name, userName = "ada@example.com", password = password, sites = listOf("https://example.com")))

    private fun failure(block: () -> Unit): Throwable = assertThrows<Throwable>(block)

    // Set up and unlock

    @Test fun `set up returns a recovery key of thirteen groups and leaves the session unlocked`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        val recoveryKey = device.session.setUp("a long invented passphrase", iterationCount)
        assertEquals(13, recoveryKey.split(" ").size)
        assertTrue(device.session.isUnlocked)
        val setup = device.session.readSetup()
        assertTrue(setup.isSetUp)
        assertEquals(iterationCount, setup.passphraseIterationCount)
        val contents = device.cache.load()!!
        assertEquals("user-ada", contents.userId)
        assertEquals(setup.publicKey, contents.publicKey)
    }

    @Test fun `setting up twice is refused`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("a long invented passphrase", iterationCount)
        val error = assertThrows<PasswordsError> { runBlocking { device.session.setUp("another one", iterationCount) } }
        assertEquals(PasswordsError.AlreadySetUp, error)
    }

    @Test fun `another device unlocks with the passphrase and the recovery key and reads what the first saved`() = runBlocking {
        val server = FakePasswordsServer()
        val first = makeDevice(server)
        val recoveryKey = first.session.setUp("a long invented passphrase", iterationCount)
        val saved = first.session.save(loginFields())

        val second = makeDevice(server)
        val unlocked = second.session.unlockWithPassphrase("a long invented passphrase")
        assertEquals(32, unlocked.accountKey.size)
        second.session.sync()
        assertEquals(loginFields(), second.session.item(saved.id).fields.withAccess(null))

        val third = makeDevice(server)
        third.session.unlockWithRecoveryKey(recoveryKey.lowercase().replace(" ", "-"))
        third.session.sync()
        assertEquals(loginFields(), third.session.item(saved.id).fields.withAccess(null))
    }

    @Test fun `a wrong passphrase and a wrong recovery key are typed`() = runBlocking {
        val server = FakePasswordsServer()
        makeDevice(server).session.setUp("a long invented passphrase", iterationCount)
        val second = makeDevice(server)
        assertEquals(PasswordsError.WrongPassphrase, assertThrows<PasswordsError> { runBlocking { second.session.unlockWithPassphrase("not the passphrase") } })
        val wrongKey = PasswordsCrypto.formatRecoveryKey(PasswordsCrypto.makeRecoveryKey())
        assertEquals(PasswordsError.WrongRecoveryKey, assertThrows<PasswordsError> { runBlocking { second.session.unlockWithRecoveryKey(wrongKey) } })
        assertFalse(second.session.isUnlocked)
    }

    @Test fun `unlocking before set up says so`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        assertEquals(PasswordsError.NotSetUp, assertThrows<PasswordsError> { runBlocking { device.session.unlockWithPassphrase("anything") } })
    }

    @Test fun `the kept account key opens the session again without the network`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        device.session.setUp("a long invented passphrase", iterationCount)
        val saved = device.session.save(loginFields())
        val unlocked = device.session.unlockWithPassphrase("a long invented passphrase")
        device.session.lock()
        assertFalse(device.session.isUnlocked)

        server.isOffline = true
        device.session.open(unlocked.accountKey)
        assertEquals(loginFields(), device.session.item(saved.id).fields.withAccess(null))

        // A different key, or no copy at all, asks for the passphrase.
        device.session.lock()
        assertEquals(PasswordsError.NeedsPassphrase, assertThrows<PasswordsError> { device.session.open(PasswordsCrypto.makeKey()) })
        val stranger = makeDevice(server)
        assertEquals(PasswordsError.NeedsPassphrase, assertThrows<PasswordsError> { stranger.session.open(unlocked.accountKey) })
    }

    @Test fun `the opener reads the cache without a session`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        device.session.setUp("a long invented passphrase", iterationCount)
        val saved = device.session.save(loginFields())
        val unlocked = device.session.unlockWithPassphrase("a long invented passphrase")

        // What the autofill service does: the copy, the account key, nothing else.
        val contents = device.cache.load()!!
        val opener = PasswordsOpener.fromPrivateKey(unlocked.accountKey, contents.userId, contents.privateKeyByAccountKey, contents.publicKey)
        val opened = opener.openAll(contents.items)
        assertEquals(listOf(saved.id), opened.items.map { item -> item.id })
        assertEquals(loginFields(), opened.items.first().fields.withAccess(null))
        assertEquals(emptyList<String>(), opened.unreadableItemIds)
        assertThrows<PasswordsCryptoException> { PasswordsOpener.fromPrivateKey(PasswordsCrypto.makeKey(), contents.userId, contents.privateKeyByAccountKey, contents.publicKey) }
    }

    // Saving

    @Test fun `save then sync on another device and an edit keeps the item key`() = runBlocking {
        val server = FakePasswordsServer()
        val first = makeDevice(server)
        first.session.setUp("a long invented passphrase", iterationCount)
        val created = first.session.save(loginFields())
        assertFalse(created.isSharedWithMe)
        assertEquals(created.id, created.id.lowercase())
        val storedBefore = server.storedItem(created.id)

        val edited = first.session.save(loginFields(password = "new password"), id = created.id)
        assertTrue(edited.revision > created.revision)
        assertEquals(storedBefore?.itemKey, server.storedItem(created.id)?.itemKey)

        val second = makeDevice(server)
        second.session.unlockWithPassphrase("a long invented passphrase")
        val result = second.session.sync()
        assertEquals(listOf(created.id), result.changedItemIds)
        val list = second.session.list()
        assertEquals(1, list.items.size)
        assertEquals(loginFields(password = "new password"), list.items.first().fields.withAccess(null))
    }

    @Test fun `a second sync asks only for what changed`() = runBlocking {
        val server = FakePasswordsServer()
        val first = makeDevice(server)
        first.session.setUp("a long invented passphrase", iterationCount)
        val created = first.session.save(loginFields())
        first.session.sync()
        first.session.save(PasswordsItemFields.Note(PasswordsNoteFields(name = "Router", text = "invented")))
        val result = first.session.sync()
        assertEquals(1, result.changedItemIds.size)
        assertNotEquals(created.id, result.changedItemIds.first())
        assertEquals(listOf<Long?>(0L, created.revision), server.listCalls)
        assertEquals(listOf("Example", "Router"), first.session.list().items.map { item -> item.name })
    }

    @Test fun `a sync asks for everything once when the server lists an item the copy missed`() = runBlocking {
        val server = FakePasswordsServer()
        val first = makeDevice(server)
        first.session.setUp("a long invented passphrase", iterationCount)
        first.session.save(loginFields())
        val second = makeDevice(server)
        second.session.unlockWithPassphrase("a long invented passphrase")
        second.session.sync()
        // An item the second copy has not seen, numbered below the copy's revision.
        val late = first.session.save(loginFields(name = "Late"))
        val stored = server.storedItem(late.id)!!
        server.replaceStoredFields(late.id, stored.encryptedFields, stored.itemKey)
        server.setRevision(late.id, 1)
        val result = second.session.sync()
        assertTrue(result.changedItemIds.contains(late.id))
        assertEquals(listOf<Long?>(0L, 1L, null), server.listCalls)
        assertEquals(listOf("Example", "Late"), second.session.list().items.map { item -> item.name })
        // Nothing is missing now, so the next sync is one incremental read again.
        second.session.sync()
        assertEquals(listOf<Long?>(0L, 1L, null, 1L), server.listCalls)
    }

    @Test fun `a stale base revision is a typed error`() = runBlocking {
        val server = FakePasswordsServer()
        val first = makeDevice(server)
        first.session.setUp("a long invented passphrase", iterationCount)
        val created = first.session.save(loginFields())
        val second = makeDevice(server)
        second.session.unlockWithPassphrase("a long invented passphrase")
        second.session.sync()

        first.session.save(loginFields(password = "changed on the first device"), id = created.id)
        val error = assertThrows<PasswordsError> {
            runBlocking { second.session.save(loginFields(password = "changed on the second"), id = created.id, baseRevision = created.revision) }
        }
        assertEquals(PasswordsError.ItemChangedOnAnotherDevice, error)
        // After reading it again the change goes through.
        second.session.sync()
        val current = second.session.item(created.id)
        second.session.save(loginFields(password = "changed on the second"), id = created.id, baseRevision = current.revision)
    }

    @Test fun `an item cannot change kind and a session that is locked refuses`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("a long invented passphrase", iterationCount)
        val created = device.session.save(loginFields())
        assertEquals(
            PasswordsError.ItemKindCannotChange,
            assertThrows<PasswordsError> { runBlocking { device.session.save(PasswordsItemFields.Note(PasswordsNoteFields(name = "x")), id = created.id) } },
        )
        device.session.lock()
        assertEquals(PasswordsError.Locked, assertThrows<PasswordsError> { runBlocking { device.session.save(loginFields()) } })
    }

    @Test fun `delete and restore`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("a long invented passphrase", iterationCount)
        val created = device.session.save(loginFields())
        val deleted = device.session.delete(created.id)
        assertTrue(deleted.isDeleted)
        assertTrue(device.session.list().items.first().isDeleted)
        val restored = device.session.restore(created.id)
        assertFalse(restored.isDeleted)
        assertTrue(restored.revision > deleted.revision)
    }

    @Test fun `purging a deleted item removes it everywhere and refuses an item that was not deleted`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        device.session.setUp("a long invented passphrase", iterationCount)
        val created = device.session.save(loginFields())
        assertEquals(PasswordsError.ItemNotFound, assertThrows<PasswordsError> { runBlocking { device.session.purge(created.id) } })
        device.session.delete(created.id)
        device.session.purge(created.id)
        assertEquals(emptyList<String>(), device.session.list().items.map { item -> item.id })
        assertNull(server.storedItem(created.id))
        assertEquals(PasswordsError.ItemNotFound, assertThrows<PasswordsError> { device.session.item(created.id) })
        assertNull(device.cache.load()!!.item(created.id))
    }

    @Test fun `earlier versions are listed newest first and open`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("a long invented passphrase", iterationCount)
        val created = device.session.save(loginFields(password = "first"))
        device.session.save(loginFields(password = "second"), id = created.id)
        device.session.save(loginFields(password = "third"), id = created.id)
        val opened = device.session.versions(created.id)
        assertEquals(0, opened.unreadableVersionCount)
        assertEquals(2, opened.versions.size)
        val passwords = opened.versions.map { version -> ((version.fields as PasswordsItemFields.Login).fields.password) }
        assertEquals(setOf("first", "second"), passwords.toSet())
        assertTrue(opened.versions.all { version -> version.isSavedByPerson })
    }

    // Settings

    @Test fun `changing the passphrase keeps the items and the recovery key`() = runBlocking {
        val server = FakePasswordsServer()
        val first = makeDevice(server)
        val recoveryKey = first.session.setUp("the old passphrase", iterationCount)
        val saved = first.session.save(loginFields())
        first.session.changePassphrase("the old passphrase", "the new passphrase", iterationCount)

        val second = makeDevice(server)
        assertEquals(PasswordsError.WrongPassphrase, assertThrows<PasswordsError> { runBlocking { second.session.unlockWithPassphrase("the old passphrase") } })
        second.session.unlockWithPassphrase("the new passphrase")
        second.session.sync()
        assertEquals(loginFields(), second.session.item(saved.id).fields.withAccess(null))
        makeDevice(server).session.unlockWithRecoveryKey(recoveryKey)
    }

    @Test fun `changing the passphrase with the recovery key`() = runBlocking {
        val server = FakePasswordsServer()
        val recoveryKey = makeDevice(server).session.setUp("forgotten passphrase", iterationCount)
        val second = makeDevice(server)
        second.session.unlockWithRecoveryKey(recoveryKey)
        second.session.changePassphraseWithRecoveryKey(recoveryKey, "a fresh passphrase", iterationCount)
        makeDevice(server).session.unlockWithPassphrase("a fresh passphrase")
    }

    @Test fun `replacing the recovery key stops the old one`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        val oldKey = device.session.setUp("the passphrase", iterationCount)
        val newKey = device.session.replaceRecoveryKey("the passphrase")
        assertNotEquals(oldKey, newKey)
        assertEquals(PasswordsError.WrongRecoveryKey, assertThrows<PasswordsError> { runBlocking { makeDevice(server).session.unlockWithRecoveryKey(oldKey) } })
        makeDevice(server).session.unlockWithRecoveryKey(newKey)
    }

    @Test fun `the lock time is kept on the server and in the cache`() = runBlocking {
        var now = 1_800_000_000_000L
        val device = makeDevice(FakePasswordsServer(), clock = { now })
        device.session.setUp("the passphrase", iterationCount)
        assertEquals(60, device.session.updateLockAfterMinutes(60))
        assertEquals(60, device.session.lockAfterMinutes)
        assertEquals(60, device.cache.load()?.lockAfterMinutes)
        assertFalse(device.session.isAskRequired(now + 59 * 60_000))
        assertTrue(device.session.isAskRequired(now + 61 * 60_000))
        now += 1
    }

    @Test fun `noting use moves the lock time on and does nothing while locked`() = runBlocking {
        var now = 1_800_000_000_000L
        val device = makeDevice(FakePasswordsServer(), clock = { now })
        device.session.noteUse()
        assertNull(device.session.lastUsedAtEpochMS)
        device.session.setUp("the passphrase", iterationCount)
        val firstUse = device.session.lastUsedAtEpochMS!!
        now += 20
        device.session.noteUse()
        assertTrue(device.session.lastUsedAtEpochMS!! > firstUse)
        device.session.lock()
        device.session.noteUse()
        assertNull(device.session.lastUsedAtEpochMS)
    }

    @Test fun `erasing removes the servers copy and the phones`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("the passphrase", iterationCount)
        device.session.save(loginFields())
        device.session.erase()
        assertFalse(device.session.isUnlocked)
        assertNull(device.cache.load())
        assertFalse(device.session.readSetup().isSetUp)
    }

    @Test fun `a sync after passwords was erased and set up again elsewhere asks for the passphrase`() = runBlocking {
        val server = FakePasswordsServer()
        val phone = makeDevice(server)
        phone.session.setUp("the passphrase", iterationCount)
        val other = makeDevice(server)
        other.session.erase()
        other.session.setUp("a new passphrase", iterationCount)
        assertEquals(PasswordsError.NeedsPassphrase, assertThrows<PasswordsError> { runBlocking { phone.session.sync() } })
        assertNull(phone.cache.load())
    }

    @Test fun `identity records come from the opened logins`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("the passphrase", iterationCount)
        val login = device.session.save(loginFields())
        device.session.save(PasswordsItemFields.Note(PasswordsNoteFields(name = "Router", text = "invented")))
        val records = PasswordsIdentityRecords.records(device.session.list().items)
        assertEquals(listOf(PasswordsPasswordIdentity(login.id, "example.com", "ada@example.com")), records.passwordIdentities)
    }

    // What the server says

    @Test fun `the servers sentences have a case of their own`() {
        val sentences = listOf(
            "That passphrase is not right." to PasswordsError.WrongPassphrase,
            "api: invalid credential: That recovery key is not right." to PasswordsError.WrongRecoveryKey,
            "api: conflict: This item changed on another device. Open it again." to PasswordsError.ItemChangedOnAnotherDevice,
            "api: invalid arguments: That person's key changed since it was read. Read their key again and seal the item for it." to PasswordsError.RecipientKeyChangedSinceRead,
            "api: invalid arguments: A card cannot be used by the agent." to PasswordsError.CardCannotBeUsedByAgent,
            "api: already exists: Passwords is already set up." to PasswordsError.AlreadySetUp,
            "api: not found: Passwords is not set up yet." to PasswordsError.NotSetUp,
            "api: too many attempts, try again later" to PasswordsError.TooManyAttempts,
        )
        for ((sentence, expected) in sentences) assertEquals(expected, PasswordsError.mapped(TeaNodeError.Refused(sentence)), sentence)
        val other = TeaNodeError.Refused("api: not found")
        assertEquals(other, PasswordsError.mapped(other))
    }

    @Test fun `too many wrong tries come back typed`() = runBlocking {
        val server = FakePasswordsServer()
        makeDevice(server).session.setUp("a long invented passphrase", iterationCount)
        server.isRateLimited = true
        val other = makeDevice(server)
        assertEquals(PasswordsError.TooManyAttempts, assertThrows<PasswordsError> { runBlocking { other.session.unlockWithPassphrase("a long invented passphrase") } })
    }

    // The record inside an item

    @Test fun `a new item carries a signed record that grants nobody and the agent nowhere`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("the passphrase", iterationCount)
        val created = device.session.save(loginFields())
        assertEquals(PasswordsSharingCheck.Verified, created.sharingCheck)
        assertEquals(1L, created.verifiedAccess?.recordVersion)
        assertEquals(emptyList<PasswordsGrantee>(), created.verifiedAccess?.grantees)
        assertFalse(created.isUsableByAgent)
    }

    @Test fun `an edit keeps the record the item had`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("the passphrase", iterationCount)
        val created = device.session.save(loginFields())
        val edited = device.session.save(loginFields(password = "changed").withAccess(PasswordsItemAccess(isUsableByAgent = true, grantees = listOf(PasswordsGrantee.Person("someone")))), id = created.id)
        assertEquals(created.fields.access, edited.fields.access)
    }

    @Test fun `an older record the server sends back is not trusted and is remembered`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        device.session.setUp("the passphrase", iterationCount)
        val created = device.session.save(loginFields())
        val sealedAtVersionOne = server.storedItem(created.id)!!
        // The owner's device changes the record twice more: version 3 is the newest this device has seen.
        val unlocked = device.session.unlockWithPassphrase("the passphrase")
        val itemKey = PasswordsSealedValue.fromJsonText(sealedAtVersionOne.itemKey)
            .let { sealed -> PasswordsCrypto.open(sealed, unlocked.accountKey, PasswordsCrypto.itemKeyAssociatedData(created.id)) }
        val newer = PasswordsCrypto.withSignedAccess(unlocked.accountKey, created.id, PasswordsItemKind.Login, created.fields, PasswordsItemAccess(), 2)
        server.replaceStoredFields(created.id, PasswordsCrypto.sealItemFields(newer, itemKey, created.id).jsonText, sealedAtVersionOne.itemKey)
        device.session.sync()
        assertEquals(PasswordsSharingCheck.Verified, device.session.item(created.id).sharingCheck)
        // The server then shows the version 1 record again.
        server.replaceStoredFields(created.id, sealedAtVersionOne.encryptedFields, sealedAtVersionOne.itemKey)
        device.session.sync()
        val shown = device.session.item(created.id)
        assertEquals(PasswordsSharingCheck.Replayed, shown.sharingCheck)
        assertNull(shown.verifiedAccess)
        assertFalse(shown.isUsableByAgent)
    }

    @Test fun `what a device remembers about an item only goes up and survives a new cache instance`() {
        val storage = InMemoryPasswordsStorage()
        val cache = PasswordsCache(storage)
        cache.recordSeenRecord("user-ada", "item-1", 3, "mac-3")
        cache.recordSeenRecord("user-ada", "item-1", 2, "mac-2")
        assertEquals(3L, PasswordsCache(storage).loadPins("user-ada").itemMemories["item-1"]?.highestRecordVersion)
        assertEquals("mac-3", PasswordsCache(storage).loadPins("user-ada").itemMemories["item-1"]?.highestRecordMac)
        // Another person's pins are not read as this one's.
        assertEquals(PasswordsPins(userId = "user-grace"), PasswordsCache(storage).loadPins("user-grace"))
        val memory = PasswordsItemMemory(highestRecordVersion = 3, highestRecordMac = "mac-3")
        assertTrue(memory.isReplay(2, "mac-2"))
        assertTrue(memory.isReplay(3, "other-mac"))
        assertFalse(memory.isReplay(3, "mac-3"))
        assertFalse(memory.isReplay(4, "mac-4"))
        assertEquals("mac-3", PasswordsItemMemory(highestRecordVersion = 3).seeingRecord(3, "mac-3").highestRecordMac)
    }

    @Test fun `a server answer that drops the copy leaves the pins`() = runBlocking {
        val storage = InMemoryPasswordsStorage()
        val cache = PasswordsCache(storage)
        cache.recordKnownIfAbsent("user-ada", "user-grace", "key-one")
        val contents = PasswordsCacheContents(userId = "user-ada", publicKey = "p", privateKeyByAccountKey = "k", lockAfterMinutes = 15)
        cache.save(contents)
        cache.eraseKeepingPins()
        assertNull(cache.load())
        assertEquals("key-one", cache.loadPins("user-ada").knownPublicKeys["user-grace"])
        cache.erase()
        assertEquals(emptyMap<String, String>(), cache.loadPins("user-ada").knownPublicKeys)
    }

    @Test fun `the cache merges pages and never moves the revision back`() {
        fun view(id: String, revision: Long) = PasswordItemView(
            id = id, ownerUserId = "u", ownerName = "", itemKind = "note", encryptedFields = "{}", itemKey = "{}", isSharedWithMe = false,
            revision = revision, isUsableByAgent = false, createdAt = "", updatedAt = "", shares = emptyList(),
        )
        val contents = PasswordsCacheContents(userId = "u", publicKey = "p", privateKeyByAccountKey = "k", lockAfterMinutes = 15, revision = 5, items = listOf(view("a", 3), view("b", 5)))
        val (merged, result) = contents.merge(PasswordItemListView(listOf(view("a", 7), view("c", 6)), listOf("a", "c"), 7))
        assertEquals(listOf("a", "c"), merged.items.map { item -> item.id })
        assertEquals(listOf("a", "c"), result.changedItemIds)
        assertEquals(listOf("b"), result.removedItemIds)
        assertEquals(7L, merged.revision)
        val (same, none) = merged.merge(PasswordItemListView(emptyList(), listOf("a", "c"), 2))
        assertEquals(7L, same.revision)
        assertEquals(PasswordsMergeResult(), none)
        assertEquals(7L, merged.replacing(view("a", 1)).revision)
    }

    @Test fun `a damaged or newer cache is typed`() {
        val storage = InMemoryPasswordsStorage()
        storage.writeCache("not json")
        assertEquals(PasswordsCacheFailure.Unreadable, assertThrows<PasswordsCacheException> { PasswordsCache(storage).load() }.failure)
        storage.writeCache("""{"formatVersion":99,"userId":"u","publicKey":"p","privateKeyByAccountKey":"k","lockAfterMinutes":15}""")
        assertEquals(PasswordsCacheFailure.UnsupportedVersion, assertThrows<PasswordsCacheException> { PasswordsCache(storage).load() }.failure)
        assertArrayEquals(byteArrayOf(), byteArrayOf())
    }

    // The agent

    @Test fun `the agent seal opens with the agent private key and only that item key is in it`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        device.session.setUp("the passphrase", iterationCount)
        val created = device.session.save(loginFields())
        val other = device.session.save(loginFields(name = "Other"))
        val allowed = device.session.setUsableByAgent(created.id, true)
        assertTrue(allowed.isUsableByAgent)
        assertEquals(2L, allowed.verifiedAccess?.recordVersion)
        assertTrue(allowed.verifiedAccess!!.isUsableByAgent)
        val unlocked = device.session.unlockWithPassphrase("the passphrase")
        val storedKey = PasswordsCrypto.open(PasswordsSealedValue.fromJsonText(server.storedItem(created.id)!!.itemKey), unlocked.accountKey, PasswordsCrypto.itemKeyAssociatedData(created.id))
        val seal = PasswordsKeyPairSealedValue.fromJsonText(server.agentCopyOf(created.id)!!)
        val agentOpened = PasswordsCrypto.openForKeyPair(seal, server.agentKeyPair.privateKey, PasswordsCrypto.keyPair(unlocked.keyPair.privateKey).publicKey, "user-ada", PasswordsCrypto.AGENT_RECIPIENT_ID, created.id)
        assertArrayEquals(storedKey, agentOpened)
        assertNull(server.agentCopyOf(other.id))
    }

    @Test fun `turning the agent off stops it at once and gives the item a new key`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        device.session.setUp("the passphrase", iterationCount)
        val created = device.session.save(loginFields())
        device.session.setUsableByAgent(created.id, true)
        val keyBefore = server.storedItem(created.id)!!.itemKey
        val stopped = device.session.setUsableByAgent(created.id, false)
        assertFalse(stopped.isUsableByAgent)
        assertFalse(stopped.verifiedAccess!!.isUsableByAgent)
        assertNull(server.agentCopyOf(created.id))
        assertNotEquals(keyBefore, server.storedItem(created.id)!!.itemKey)
        assertFalse(server.storedItem(created.id)!!.needsNewKey)
        assertEquals(emptyList<String>(), device.session.itemIdsNeedingNewKey())
    }

    @Test fun `a card cannot be used by the agent`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("the passphrase", iterationCount)
        val card = device.session.save(PasswordsItemFields.Card(PasswordsCardFields(name = "Card", number = "4111111111111111")))
        assertEquals(PasswordsError.CardCannotBeUsedByAgent, assertThrows<PasswordsError> { runBlocking { device.session.setUsableByAgent(card.id, true) } })
    }

    @Test fun `a deleted item cannot be given to the agent or taken back`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("the passphrase", iterationCount)
        val created = device.session.save(loginFields())
        device.session.delete(created.id)
        assertEquals(PasswordsError.ItemNotFound, assertThrows<PasswordsError> { runBlocking { device.session.setUsableByAgent(created.id, true) } })
        assertEquals(PasswordsError.ItemNotFound, assertThrows<PasswordsError> { runBlocking { device.session.setUsableByAgent(created.id, false) } })
    }

    @Test fun `a record the person did not sign cannot be built on for the agent`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        device.session.setUp("the passphrase", iterationCount)
        val created = device.session.save(loginFields())
        // The server hands back the fields without any record: the version one record is gone.
        val unlocked = device.session.unlockWithPassphrase("the passphrase")
        val itemKey = PasswordsCrypto.open(PasswordsSealedValue.fromJsonText(server.storedItem(created.id)!!.itemKey), unlocked.accountKey, PasswordsCrypto.itemKeyAssociatedData(created.id))
        val bare = loginFields()
        server.replaceStoredFields(created.id, PasswordsCrypto.sealItemFields(bare, itemKey, created.id).jsonText, server.storedItem(created.id)!!.itemKey)
        device.session.sync()
        assertEquals(PasswordsSharingCheck.Unverified, device.session.item(created.id).sharingCheck)
        assertEquals(PasswordsError.SharingCouldNotBeChecked(created.id), assertThrows<PasswordsError> { runBlocking { device.session.setUsableByAgent(created.id, true) } })
    }

    // Restoring an earlier version

    @Test fun `restoring an old version puts its content back and keeps the sharing record as it is now`() = runBlocking {
        val server = FakePasswordsServer()
        val device = makeDevice(server)
        device.session.setUp("the passphrase", iterationCount)
        val created = device.session.save(loginFields(password = "first"))
        device.session.save(loginFields(password = "second"), id = created.id)
        val versions = device.session.versions(created.id)
        val firstVersion = versions.versions.first { version -> (version.fields as PasswordsItemFields.Login).fields.password == "first" }
        val restored = device.session.restoreVersion(created.id, firstVersion.id)
        assertEquals("first", (restored.fields as PasswordsItemFields.Login).fields.password)
        // The record was written again one version above the highest this device had seen, and the item has a new key.
        assertEquals(PasswordsSharingCheck.Verified, restored.sharingCheck)
        assertTrue(restored.verifiedAccess!!.recordVersion!! > 1L)
        assertFalse(restored.needsNewKey)
        // What it was before the restore stays as an earlier version.
        assertTrue(device.session.versions(created.id).versions.any { version -> (version.fields as PasswordsItemFields.Login).fields.password == "second" })
    }

    @Test fun `restoring a version not listed in this session is refused`() = runBlocking {
        val device = makeDevice(FakePasswordsServer())
        device.session.setUp("the passphrase", iterationCount)
        val created = device.session.save(loginFields())
        device.session.save(loginFields(password = "second"), id = created.id)
        assertEquals(PasswordsError.ItemNotFound, assertThrows<PasswordsError> { runBlocking { device.session.restoreVersion(created.id, "version-1") } })
    }
}
