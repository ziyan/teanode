package com.teanode.kit.passwords

import com.teanode.kit.passwords.SharingFixture.ITERATION_COUNT
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

/** AnswerSwap makes a server answer a request about one item with another item the person owns, as a server that files its answer under another id would, and can inflate the count of keys it stored. */
internal class AnswerSwap {
    /** The item every swapped answer shows. */
    var shownItemId: String? = null

    /** The requests about these item ids are answered with `shownItemId`. */
    var swappedItemIds: Set<String> = emptySet()

    /** What `AddPasswordShareKeys` answers instead of the real count. */
    var addedKeyCountOverride: Int? = null
}

internal class SwappingApi(private val inner: PasswordsApi, private val swap: AnswerSwap) : PasswordsApi by inner {
    private suspend fun answered(real: PasswordItemView, askedItemId: String): PasswordItemView {
        val shown = swap.shownItemId
        if (!swap.swappedItemIds.contains(askedItemId) || shown == null) return real
        return inner.listPasswordItems(null).items.firstOrNull { item -> item.id == shown } ?: real
    }

    override suspend fun savePasswordItem(input: PasswordItemSaveInput) = answered(inner.savePasswordItem(input), input.id)

    override suspend fun deletePasswordItem(id: String) = answered(inner.deletePasswordItem(id), id)

    override suspend fun restorePasswordItem(id: String) = answered(inner.restorePasswordItem(id), id)

    override suspend fun setPasswordItemShares(itemId: String, grantees: List<PasswordShareGranteeInput>, keys: List<PasswordShareKeyInput>) =
        answered(inner.setPasswordItemShares(itemId, grantees, keys), itemId)

    override suspend fun addPasswordShareKeys(keys: List<PasswordItemShareKeyInput>): Int {
        val real = inner.addPasswordShareKeys(keys)
        return swap.addedKeyCountOverride ?: real
    }

    override suspend fun setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Boolean, itemKeyByAgentKey: String?) =
        answered(inner.setPasswordItemUsableByAgent(itemId, isUsableByAgent, itemKeyByAgentKey), itemId)

    override suspend fun rekeyPasswordItem(input: PasswordItemRekeyInput) = answered(inner.rekeyPasswordItem(input), input.id)

    override suspend fun restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Long) =
        answered(inner.restorePasswordItemVersion(itemId, versionId, baseRevision), itemId)
}

/**
 * An answer is about the item asked for; Finish sharing says what it will do and puts the agent back; restoring is
 * limited to what this device sealed; a first look at a deleted or marked item is not remembered; earlier versions of an
 * item can be read and put back.
 */
class PasswordsAnswerChecksTest {
    private val passphrase = "ada invented passphrase"

    private fun makeCache(pinStorage: PasswordsStorage = InMemoryPasswordsStorage()): PasswordsCache = PasswordsCache(PinSharingStorage(pinStorage))

    private fun makeSession(server: FakePasswordsServer, userId: String, cache: PasswordsCache = SharingFixture.makeCache(), swap: AnswerSwap? = null): PasswordsSession {
        val api = server.api(userId)
        return PasswordsSession(if (swap != null) SwappingApi(api, swap) else api, cache, userId)
    }

    private fun loginFields(name: String = "Example", password: String = "correct horse") = SharingFixture.loginFields(name, password)

    private fun ben() = PasswordShareGranteeInput(granteeUserId = "user-ben")

    private suspend fun adaAndBen(server: FakePasswordsServer) {
        server.addPerson("user-ada", "Ada")
        server.addPerson("user-ben", "Ben")
        makeSession(server, "user-ben").setUp("ben invented passphrase", ITERATION_COUNT)
    }

    private suspend fun expectRefusedAsAnotherItem(operation: suspend () -> Unit) {
        val error = assertThrows<Throwable>("the answer about another item was accepted") { runBlocking { operation() } }
        assertEquals(PasswordsError.AnsweredAboutAnotherItem, error)
    }

    // An answer about another item

    /** A phone with two items, Y and Z, and a server that answers every request about Y with Z. */
    private class YAndZ(
        val server: FakePasswordsServer,
        val phone: PasswordsSession,
        val cache: PasswordsCache,
        val swap: AnswerSwap,
        val yItem: PasswordItem,
        val zItem: PasswordItem,
    )

    private suspend fun yAndZ(isSharedWithBen: Boolean = false): YAndZ {
        val server = FakePasswordsServer()
        adaAndBen(server)
        val swap = AnswerSwap()
        val cache = SharingFixture.makeCache()
        val phone = makeSession(server, "user-ada", cache, swap)
        phone.setUp(passphrase, ITERATION_COUNT)
        val yItem = phone.save(loginFields("Y"))
        val zItem = phone.save(loginFields("Z"))
        if (isSharedWithBen) {
            val recipients = phone.recipients(granteeUserId = "user-ben")
            phone.setShares(yItem.id, listOf(ben()), recipients)
        }
        swap.shownItemId = zItem.id
        swap.swappedItemIds = setOf(yItem.id)
        return YAndZ(server, phone, cache, swap, yItem, zItem)
    }

    private fun expectZUntouched(setup: YAndZ) {
        val zNow = setup.phone.item(setup.zItem.id)
        assertEquals(setup.zItem, zNow, "Z in the copy is as it was")
        val pins = setup.cache.loadPins("user-ada")
        assertEquals(emptyList<String>(), pins.itemMemories[setup.zItem.id]?.sealedForUserIds ?: emptyList<String>(), "nothing was credited to Z")
        assertEquals(false, pins.itemMemories[setup.zItem.id]?.didMakeAgentCopy ?: false)
        assertEquals(false, pins.itemMemories[setup.yItem.id]?.didMakeAgentCopy ?: false, "nothing was credited to Y either")
    }

    @Test fun `a save answered with another item is refused and keeps nothing`() = runBlocking<Unit> {
        val setup = yAndZ()
        expectRefusedAsAnotherItem { setup.phone.save(loginFields("Y changed"), setup.yItem.id, setup.yItem.revision) }
        expectZUntouched(setup)
        val yNow = setup.phone.item(setup.yItem.id)
        assertEquals("Y", yNow.name, "the copy of Y did not take the answer")
    }

    @Test fun `a delete answered with another item is refused and keeps nothing`() = runBlocking<Unit> {
        val setup = yAndZ()
        expectRefusedAsAnotherItem { setup.phone.delete(setup.yItem.id) }
        expectZUntouched(setup)
        val yNow = setup.phone.item(setup.yItem.id)
        assertFalse(yNow.isDeleted, "the copy of Y is not marked deleted by an answer about Z")
    }

    @Test fun `a restore answered with another item is refused and rekeys nothing`() = runBlocking<Unit> {
        val setup = yAndZ(isSharedWithBen = true)
        setup.swap.swappedItemIds = emptySet()
        setup.phone.delete(setup.yItem.id)
        setup.swap.swappedItemIds = setOf(setup.yItem.id)
        val rekeyCountBefore = setup.server.rekeyCallCount
        expectRefusedAsAnotherItem { setup.phone.restore(setup.yItem.id) }
        assertEquals(rekeyCountBefore, setup.server.rekeyCallCount, "no new key was made on the strength of the answer")
        val zNow = setup.phone.item(setup.zItem.id)
        assertEquals(setup.zItem, zNow)
    }

    @Test fun `the agent switch answered with another item is refused and credits no agent copy`() = runBlocking<Unit> {
        val setup = yAndZ()
        expectRefusedAsAnotherItem { setup.phone.setUsableByAgent(setup.yItem.id, true) }
        expectZUntouched(setup)
    }

    /** The phone files what it sealed under the id it asked about, not the id the server answers with. */
    @Test fun `a rekey answered with another item plants no memory`() = runBlocking<Unit> {
        val setup = yAndZ(isSharedWithBen = true)
        expectRefusedAsAnotherItem { setup.phone.giveNewKey(setup.yItem.id) }
        val pins = setup.cache.loadPins("user-ada")
        assertEquals(emptyList<String>(), pins.itemMemories[setup.zItem.id]?.sealedForUserIds ?: emptyList<String>(), "nothing was credited to Z")
        assertEquals(listOf("user-ben"), pins.itemMemories[setup.yItem.id]?.sealedForUserIds, "Y keeps what the share sealed, no more")
    }

    /**
     * Removing a person from Y while the server answers the change of shares with Z: the change is refused, nothing is
     * sealed or re-keyed on the answer, and Z is left as it was.
     */
    @Test fun `narrowing answered with another item is refused and touches nothing else`() = runBlocking<Unit> {
        val setup = yAndZ(isSharedWithBen = true)
        val zBefore = setup.server.item(setup.zItem.id)!!
        val zFieldsBefore = zBefore.encryptedFields
        val rekeyCountBefore = setup.server.rekeyCallCount
        expectRefusedAsAnotherItem { setup.phone.setShares(setup.yItem.id, emptyList(), emptyList()) }
        assertEquals(rekeyCountBefore, setup.server.rekeyCallCount, "no re-key was made from the answer")
        val zAfter = setup.server.item(setup.zItem.id)!!
        assertEquals(zFieldsBefore, zAfter.encryptedFields, "Z was not rewritten")
        assertNull(zAfter.itemKeyByAgentKey)
        val pins = setup.cache.loadPins("user-ada")
        assertEquals(emptyList<String>(), pins.itemMemories[setup.zItem.id]?.sealedForUserIds ?: emptyList<String>())
    }

    @Test fun `a key count past the keys sent is refused and credits nobody`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        server.addPerson("user-ben", "Ben")
        val swap = AnswerSwap()
        val cache = SharingFixture.makeCache()
        val phone = makeSession(server, "user-ada", cache, swap)
        phone.setUp(passphrase, ITERATION_COUNT)
        val item = phone.save(loginFields())
        val noKeyRecipients = phone.recipients(granteeUserId = "user-ben")
        phone.setShares(item.id, listOf(ben()), noKeyRecipients)
        // Ben sets up afterwards: his key is waiting for the person.
        makeSession(server, "user-ben").setUp("ben invented passphrase", ITERATION_COUNT)

        swap.addedKeyCountOverride = 7
        phone.sync(isFull = true)
        val error = assertThrows<Throwable>("a count of seven keys for one key sent was accepted") {
            runBlocking { phone.finishSharing(item.id, phone.finishPlan(listOf(item.id))) }
        }
        assertEquals(PasswordsError.InvalidServerAnswer, error)
        val pins = cache.loadPins("user-ada")
        assertEquals(emptyList<String>(), pins.itemMemories[item.id]?.sealedForUserIds ?: emptyList<String>(), "Ben was not credited")
    }

    // Restore

    /**
     * The owner took Ben off an item and deleted it. A device with nothing stored is shown the item as it was while Ben
     * had it. Restoring it (the person's tap on Restore, nothing else) gives Ben no key: only people this device sealed
     * for get one, the rest is left to Finish sharing.
     */
    @Test fun `restore on a device that never saw the item seals for nobody it did not seal for`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        adaAndBen(server)
        val one = makeSession(server, "user-ada")
        one.setUp(passphrase, ITERATION_COUNT)
        val item = one.save(loginFields())
        val recipients = one.recipients(granteeUserId = "user-ben")
        one.setShares(item.id, listOf(ben()), recipients)
        val snapshot = server.item(item.id)!!
        val snapshotFields = snapshot.encryptedFields
        val snapshotKey = snapshot.itemKeyByAccountKey
        one.setShares(item.id, emptyList(), emptyList())
        one.delete(item.id)

        val two = makeSession(server, "user-ada")
        two.unlockWithPassphrase(passphrase)
        server.replaceStoredFields(item.id, snapshotFields, snapshotKey, needsNewKey = true)
        server.setShares("user-ada", item.id, listOf(ben()), emptyList())
        two.sync(isFull = true)
        val seen = two.item(item.id)
        assertEquals(PasswordsSharingCheck.Verified, seen.sharingCheck)
        assertTrue(seen.isDeleted)

        two.restore(item.id)
        val after = server.item(item.id)!!
        assertNull(after.keys["user-ben"], "Ben has no key for the restored item's new key")
        assertFalse(after.needsNewKey, "the restored item was given a new key")

        // Nothing automatic gives Ben one either; the item waits for the person.
        two.completeWaitingShares()
        assertNull(server.item(item.id)?.keys?.get("user-ben"))
        assertTrue(two.itemIdsNeedingYou().contains(item.id))

        // The person finishes it: Ben gets his key.
        two.finishSharing(item.id, two.finishPlan(listOf(item.id)))
        assertNotNull(server.item(item.id)?.keys?.get("user-ben"))
    }

    /** On the device that sealed for Ben itself, restoring gives him the new key at once. */
    @Test fun `restore on the device that sealed for somebody gives them the new key`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        adaAndBen(server)
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        val item = phone.save(loginFields())
        val recipients = phone.recipients(granteeUserId = "user-ben")
        phone.setShares(item.id, listOf(ben()), recipients)
        phone.delete(item.id)
        phone.restore(item.id)
        assertNotNull(server.item(item.id)?.keys?.get("user-ben"))
    }

    // A first look

    @Test fun `a first look at a deleted item is not remembered`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        val one = makeSession(server, "user-ada")
        one.setUp(passphrase, ITERATION_COUNT)
        val item = one.save(loginFields())
        one.delete(item.id)

        val cache = SharingFixture.makeCache()
        val two = makeSession(server, "user-ada", cache)
        two.unlockWithPassphrase(passphrase)
        two.sync(isFull = true)
        val seen = two.item(item.id)
        assertTrue(seen.isDeleted)
        assertEquals(PasswordsSharingCheck.Verified, seen.sharingCheck)
        val pins = cache.loadPins("user-ada")
        assertEquals(0L, pins.itemMemories[item.id]?.highestRecordVersion ?: 0, "the record of a deleted first look is not remembered")
    }

    @Test fun `a first look at a marked item is not remembered but an unmarked one is`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        val one = makeSession(server, "user-ada")
        one.setUp(passphrase, ITERATION_COUNT)
        val markedItem = one.save(loginFields("Marked"))
        val plainItem = one.save(loginFields("Plain"))
        server.markNeedsNewKey(markedItem.id)

        val cache = SharingFixture.makeCache()
        val two = makeSession(server, "user-ada", cache)
        two.unlockWithPassphrase(passphrase)
        two.sync(isFull = true)
        two.list()
        val pins = cache.loadPins("user-ada")
        assertEquals(0L, pins.itemMemories[markedItem.id]?.highestRecordVersion ?: 0, "the record of a marked first look is not remembered")
        assertTrue((pins.itemMemories[plainItem.id]?.highestRecordVersion ?: 0) > 0, "an unmarked first look is remembered")
    }

    // The agent after another device's automatic re-key

    /**
     * A device that only looked at an item gives it a new key when the server marks it; the agent gets no copy (this
     * device made none), the server clears its flag, and the record still says "on". The item is listed for Finish
     * sharing, and finishing it puts the agent back.
     */
    @Test fun `a lost agent copy is listed for finish sharing and finishing puts it back`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        val maker = makeSession(server, "user-ada")
        maker.setUp(passphrase, ITERATION_COUNT)
        val item = maker.save(loginFields())
        maker.setUsableByAgent(item.id, true)

        val pinStorage = InMemoryPasswordsStorage()
        val firstLook = makeSession(server, "user-ada", makeCache(pinStorage))
        firstLook.unlockWithPassphrase(passphrase)
        firstLook.sync(isFull = true)
        firstLook.item(item.id)

        server.markNeedsNewKey(item.id)
        val second = makeSession(server, "user-ada", makeCache(pinStorage))
        second.unlockWithPassphrase(passphrase)
        second.sync(isFull = true)
        val given = second.giveNewKeysToMarkedItems()
        assertEquals(listOf(item.id), given)
        second.sync(isFull = true)
        val after = second.item(item.id)
        assertEquals(true, after.fields.access?.isUsableByAgent, "the record still says the agent may use it")
        assertFalse(after.isUsableByAgent, "the server holds no copy for the agent")
        assertTrue(second.itemIdsNeedingYou().contains(item.id), "the person is told")

        val plan = second.finishPlan(listOf(item.id))
        assertEquals(1, plan.itemCount)
        assertEquals(1, plan.agentItemCount)
        assertEquals(0, plan.newKeyItemCount)
        assertTrue(plan.people.isEmpty())

        second.sync(isFull = true)
        second.finishSharing(item.id, second.finishPlan(listOf(item.id)))
        second.sync(isFull = true)
        val finished = second.item(item.id)
        assertTrue(finished.isUsableByAgent, "Finish sharing put the agent back")
        assertFalse(second.itemIdsNeedingYou().contains(item.id))
    }

    @Test fun `a card is never listed for an agent copy`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        phone.save(PasswordsItemFields.Card(PasswordsCardFields(name = "Card", cardholderName = "Ada", number = "4111111111111111")))
        assertTrue(phone.itemIdsWithLostAgentCopy().isEmpty())
    }

    // What Finish sharing will do

    @Test fun `the finish plan names who gets keys for how many items and seals nothing`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        adaAndBen(server)
        server.addPerson("user-cora", "Cora")
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        val first = phone.save(loginFields("First"))
        val second = phone.save(loginFields("Second"))
        val third = phone.save(loginFields("Third"))
        val bensGrantee = PasswordShareGranteeInput(granteeUserId = "user-ben")
        val corasGrantee = PasswordShareGranteeInput(granteeUserId = "user-cora")
        val benAndCora = phone.recipients(granteeUserId = "user-ben") + phone.recipients(granteeUserId = "user-cora")
        phone.setShares(first.id, listOf(bensGrantee, corasGrantee), benAndCora)
        phone.setShares(second.id, listOf(corasGrantee), benAndCora)
        phone.setShares(third.id, listOf(bensGrantee), benAndCora)
        // Cora sets up Passwords afterwards: she is waiting for First and Second.
        makeSession(server, "user-cora").setUp("cora invented passphrase", ITERATION_COUNT)
        phone.sync(isFull = true)

        val plan = phone.finishPlan(listOf(first.id, second.id, third.id, first.id))
        assertEquals(2, plan.itemCount, "Third has nothing waiting, and a repeated id counts once")
        assertEquals(listOf("Cora"), plan.people.map { person -> person.name })
        assertEquals(2, plan.people.first().itemCount)
        assertEquals(0, plan.newKeyItemCount)
        assertEquals(0, plan.agentItemCount)
        assertNull(server.item(first.id)?.keys?.get("user-cora"), "the plan sealed nothing")

        val onlySecond = phone.finishPlan(listOf(second.id))
        assertEquals(1, onlySecond.itemCount)
        assertEquals(1, onlySecond.people.first().itemCount)
    }

    @Test fun `the finish plan says a new key goes to everyone the record reaches`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        adaAndBen(server)
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        val item = phone.save(loginFields())
        val recipients = phone.recipients(granteeUserId = "user-ben")
        phone.setShares(item.id, listOf(ben()), recipients)
        server.markNeedsNewKey(item.id)
        phone.sync(isFull = true)
        val plan = phone.finishPlan(listOf(item.id))
        assertEquals(1, plan.newKeyItemCount)
        assertEquals(listOf("Ben"), plan.people.map { person -> person.name })
    }

    // Earlier versions

    @Test fun `earlier versions are listed newest first opened and said who saved them`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        val first = phone.save(loginFields("Example", "first password"))
        val second = phone.save(loginFields("Example", "second password"), first.id, first.revision)
        server.setSavesAreByPerson(false)
        phone.save(loginFields("Example", "third password"), first.id, second.revision)

        val opened = phone.versions(first.id)
        assertEquals(0, opened.unreadableVersionCount)
        assertEquals(2, opened.versions.size)
        val newest = (opened.versions.first().fields as PasswordsItemFields.Login).fields
        val oldest = (opened.versions.last().fields as PasswordsItemFields.Login).fields
        assertEquals("second password", newest.password)
        assertEquals("first password", oldest.password)
        assertEquals(false, opened.versions.first().isSavedByPerson, "the third save came from a program")
        assertEquals(true, opened.versions.last().isSavedByPerson)
        assertNotNull(opened.versions.first().savedAtEpochMS)
    }

    @Test fun `restoring a version puts its fields back and gives the item a new key at once`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        adaAndBen(server)
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        val created = phone.save(loginFields("Example", "first password"))
        val recipients = phone.recipients(granteeUserId = "user-ben")
        val shared = phone.setShares(created.id, listOf(ben()), recipients)
        val edited = phone.save(loginFields("Example", "second password"), created.id, shared.revision)
        val keyBefore = server.item(created.id)?.itemKeyByAccountKey

        // The version saved just before the edit: the first password, with Ben named in its record.
        val opened = phone.versions(created.id)
        val version = opened.versions.first()
        assertEquals("first password", (version.fields as PasswordsItemFields.Login).fields.password)

        val restored = phone.restoreVersion(created.id, version.id)
        assertEquals("first password", (restored.fields as PasswordsItemFields.Login).fields.password)
        assertTrue(restored.revision > edited.revision)
        val storedItem = server.item(created.id)!!
        assertFalse(storedItem.needsNewKey, "the restore was followed by a new key")
        assertNotEquals(keyBefore, storedItem.itemKeyByAccountKey, "it is not the key of the version any more")
        assertNotNull(storedItem.keys["user-ben"], "the record held before the restore names Ben, so he gets the new key")
        // The replaced content stays as a version of its own.
        val versionsAfter = phone.versions(created.id)
        assertTrue(versionsAfter.versions.size > opened.versions.size)
    }

    /**
     * The restore brings back the content only: the record this device held before is written into the restored fields,
     * signed above the highest seen, so who the item is shared with stays as it is now, it is not a replay and nothing
     * waits for the person.
     */
    @Test fun `restoring a version with an older sharing record keeps the record held before signed above the highest seen`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        adaAndBen(server)
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        val created = phone.save(loginFields("Example", "first password"))
        val recipients = phone.recipients(granteeUserId = "user-ben")
        val shared = phone.setShares(created.id, listOf(ben()), recipients)
        val highestBefore = shared.fields.access!!.recordVersion!!
        // The oldest version holds the first signed record, which names nobody: older than the one now.
        val opened = phone.versions(created.id)
        val oldest = opened.versions.last()
        val oldestVersion = oldest.fields.access!!.recordVersion!!
        assertTrue(oldestVersion < highestBefore)

        val restored = phone.restoreVersion(created.id, oldest.id)
        assertEquals(PasswordsSharingCheck.Verified, restored.sharingCheck, "a restore is not a replay")
        assertEquals(highestBefore + 1, restored.fields.access?.recordVersion)
        assertEquals(listOf<PasswordsGrantee>(PasswordsGrantee.Person("user-ben")), restored.fields.access?.grantees, "the restored record named nobody, the one held before names Ben")
        val stored = server.item(created.id)
        assertNotNull(stored?.keys?.get("user-ben"), "Ben is shared with now, so he gets the new key")
        assertEquals(false, stored?.needsNewKey, "the item was given a new key at once")
    }

    /**
     * The server ended the agent's use with the restore, so the agent's copy is not made again by the restore, whatever
     * the restored version's record said. The record held before still allows it, so "Finish sharing" offers it back.
     */
    @Test fun `restoring a version does not turn the agent on and offers the copy on finish sharing`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        adaAndBen(server)
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        val created = phone.save(loginFields("Example", "first password"))
        val recipients = phone.recipients(granteeUserId = "user-ben")
        phone.setShares(created.id, listOf(ben()), recipients)
        val agentOn = phone.setUsableByAgent(created.id, true)
        val edited = phone.save(loginFields("Example", "second password"), created.id, agentOn.revision)
        val opened = phone.versions(created.id)
        val version = opened.versions.first { candidate -> candidate.fields.access?.isUsableByAgent == true }
        assertTrue(edited.revision > agentOn.revision)

        val restored = phone.restoreVersion(created.id, version.id)
        assertEquals(PasswordsSharingCheck.Verified, restored.sharingCheck)
        assertFalse(restored.isUsableByAgent, "the restore does not turn the agent on")
        val stored = server.item(created.id)
        assertNull(stored?.itemKeyByAgentKey)
        assertNotNull(stored?.keys?.get("user-ben"))
        assertEquals(false, stored?.needsNewKey)
        assertTrue(phone.itemIdsWithLostAgentCopy().contains(created.id), "the record still allows the agent, so Finish sharing offers it")
        val plan = phone.finishPlan(listOf(created.id))
        assertEquals(listOf(true), plan.items.map { item -> item.isAgentIncluded })
        phone.finishSharing(created.id, plan)
        assertEquals(true, server.item(created.id)?.isUsableByAgent)
    }

    /** The record the item has now has to be the person's own: with one that does not verify, a restore is refused before the server is asked, and nothing changes. */
    @Test fun `restoring a version of an item whose current record does not verify is refused before asking the server`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        adaAndBen(server)
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        val created = phone.save(loginFields("Example", "first password"))
        val second = makeSession(server, "user-ada")
        val unlocked = second.unlockWithPassphrase(passphrase)
        // The server holds a record naming Ben with a signature nobody made.
        val stored = server.item(created.id)!!
        val itemKey = PasswordsCrypto.open(
            PasswordsSealedValue.fromJsonText(stored.itemKeyByAccountKey), unlocked.accountKey, PasswordsCrypto.itemKeyAssociatedData(created.id),
        )
        val forgedAccess = PasswordsItemAccess(
            isUsableByAgent = false, grantees = listOf(PasswordsGrantee.Person("user-ben")), recordVersion = 9,
            recordMac = base64(ByteArray(PasswordsCrypto.KEY_BYTE_COUNT)),
        )
        val forged = PasswordsCrypto.sealItemFields(loginFields("Example").withAccess(forgedAccess), itemKey, created.id)
        server.replaceStoredFields(created.id, forged.jsonText, stored.itemKeyByAccountKey, needsNewKey = false)
        phone.sync(isFull = true)
        val current = phone.item(created.id)
        phone.save(loginFields("Example", "later"), created.id, current.revision)
        val opened = phone.versions(created.id)
        val version = opened.versions.first()
        val revisionBefore = server.item(created.id)?.revision

        val error = assertThrows<Throwable>("a restore on a record that cannot be checked went through") {
            runBlocking { phone.restoreVersion(created.id, version.id) }
        }
        assertTrue(error is PasswordsError.SharingCouldNotBeChecked)
        val after = server.item(created.id)
        assertEquals(revisionBefore, after?.revision, "the server was not asked")
        assertNull(after?.keys?.get("user-ben"))
    }

    @Test fun `a version restore answered with another item is refused`() = runBlocking<Unit> {
        val setup = yAndZ()
        setup.swap.swappedItemIds = emptySet()
        setup.phone.save(loginFields("Y changed"), setup.yItem.id, setup.yItem.revision)
        setup.swap.swappedItemIds = setOf(setup.yItem.id)
        val opened = setup.phone.versions(setup.yItem.id)
        val version = opened.versions.first()
        expectRefusedAsAnotherItem { setup.phone.restoreVersion(setup.yItem.id, version.id) }
        val zNow = setup.phone.item(setup.zItem.id)
        assertEquals(setup.zItem, zNow)
    }

    @Test fun `a version restore on a revision that moved on is refused as changed on another device`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        server.addPerson("user-ada", "Ada")
        val phone = makeSession(server, "user-ada")
        phone.setUp(passphrase, ITERATION_COUNT)
        val created = phone.save(loginFields("Example", "first password"))
        phone.save(loginFields("Example", "second password"), created.id, created.revision)
        val opened = phone.versions(created.id)
        val version = opened.versions.first()
        server.touch(created.id)
        val error = assertThrows<Throwable>("a restore on an old revision went through") {
            runBlocking { phone.restoreVersion(created.id, version.id) }
        }
        assertEquals(PasswordsError.ItemChangedOnAnotherDevice, error)
    }

    @Test fun `versions of an item shared with the person are refused`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        adaAndBen(server)
        val ada = makeSession(server, "user-ada")
        ada.setUp(passphrase, ITERATION_COUNT)
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeUserId = "user-ben")
        ada.setShares(item.id, listOf(ben()), recipients)
        val ben = makeSession(server, "user-ben")
        ben.unlockWithPassphrase("ben invented passphrase")
        ben.sync(isFull = true)
        val error = assertThrows<Throwable>("a recipient listed the owner's versions") { runBlocking { ben.versions(item.id) } }
        assertEquals(PasswordsError.OnlyOwnerCanChangeItem, error)
    }
}
