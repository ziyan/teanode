package com.teanode.kit.passwords

import com.teanode.kit.passwords.SharingFixture.loginFields
import com.teanode.kit.passwords.SharingFixture.makeCache
import com.teanode.kit.passwords.SharingFixture.makeServer
import com.teanode.kit.passwords.SharingFixture.makeSession
import com.teanode.kit.passwords.SharingFixture.sealedAccess
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

/**
 * These check the rules of the access record: what is sealed inside the item decides who gets a key and whether the
 * agent may use it, in the order of calls that keeps it from ever being wider than what the person chose, whatever the
 * server says.
 */
class PasswordsAccessFlowTest {
    private fun person(userId: String) = PasswordShareGranteeInput(granteeUserId = userId)

    private fun circle(circleId: String) = PasswordShareGranteeInput(granteeCircleId = circleId)

    // The order of calls

    @Test fun `sharing saves the wider record before the server is told and unsharing tells the server before the rekey`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-ben" to "Ben")
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val accountKey = makeSession(server, "user-ada").unlockWithPassphrase("ada invented passphrase").accountKey
        val ben = makeSession(server, "user-ben")
        setUp(ben, "ben invented passphrase")
        val item = ada.save(loginFields())
        flags.calls.clear()

        val recipients = ada.recipients(granteeUserId = "user-ben")
        val shared = ada.setShares(item.id, listOf(person("user-ben")), recipients)
        assertEquals(listOf("savePasswordItem", "setPasswordItemShares"), flags.calls)
        assertEquals(PasswordsSharingCheck.Verified, shared.sharingCheck)
        assertEquals(listOf<PasswordsGrantee>(PasswordsGrantee.Person("user-ben")), shared.fields.access?.grantees)
        assertEquals(false, shared.fields.access?.isUsableByAgent)
        // The item started at version 1; sharing signed version 2.
        assertEquals(2L, shared.fields.access?.recordVersion)
        val storedAccess = sealedAccess(item.id, server, accountKey)
        assertEquals(listOf<PasswordsGrantee>(PasswordsGrantee.Person("user-ben")), storedAccess?.grantees)
        ben.sync()
        val benOpened = ben.item(item.id)
        assertEquals(loginFields(), benOpened.fields.withAccess(null))

        flags.calls.clear()
        ada.setShares(item.id, emptyList(), emptyList())
        assertEquals(listOf("setPasswordItemShares", "rekeyPasswordItem"), flags.calls)
        val rekeyed = sealedAccess(item.id, server, accountKey)
        assertEquals(emptyList<PasswordsGrantee>(), rekeyed?.grantees)
        assertEquals(false, rekeyed?.isUsableByAgent)
        assertEquals(3L, rekeyed?.recordVersion)
        assertTrue(PasswordsCrypto.isAccessVerified(accountKey, item.id, PasswordsItemKind.Login, rekeyed))
        assertFalse(ada.needsNewKey(item.id))
    }

    @Test fun `turning the agent on saves the record first and turning it off tells the server before the rekey`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada")
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val item = ada.save(loginFields())
        flags.calls.clear()

        val on = ada.setUsableByAgent(item.id, true)
        assertTrue(on.isUsableByAgent)
        assertEquals(listOf("savePasswordItem", "setPasswordItemUsableByAgent"), flags.calls)
        assertEquals(true, on.fields.access?.isUsableByAgent)

        flags.calls.clear()
        val off = ada.setUsableByAgent(item.id, false)
        assertFalse(off.isUsableByAgent)
        assertEquals(listOf("setPasswordItemUsableByAgent", "rekeyPasswordItem"), flags.calls)
        assertNull(flags.lastRekeyInput?.itemKeyByAgentKey)
        assertEquals(false, off.fields.access?.isUsableByAgent)
        val stored = server.item(item.id)
        assertNull(stored?.itemKeyByAgentKey)
        assertEquals(false, stored?.needsNewKey)
    }

    @Test fun `an edit never changes the access record`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-ben" to "Ben")
        val ada = makeSession(server, "user-ada")
        setUp(ada, "ada invented passphrase")
        setUp(makeSession(server, "user-ben"), "ben invented passphrase")
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeUserId = "user-ben")
        ada.setShares(item.id, listOf(person("user-ben")), recipients)
        ada.setUsableByAgent(item.id, true)
        val agentOnItem = ada.item(item.id)
        val access = agentOnItem.fields.access!!
        assertEquals(PasswordsSharingCheck.Verified, agentOnItem.sharingCheck)
        assertEquals(true, access.isUsableByAgent)
        assertEquals(listOf<PasswordsGrantee>(PasswordsGrantee.Person("user-ben")), access.grantees)

        // An editor that built its fields without the record, or with a wider one, changes nothing.
        val withoutRecord = loginFields(password = "edited once")
        val widerRecord = loginFields(password = "edited twice").withAccess(
            PasswordsItemAccess(isUsableByAgent = true, grantees = listOf(PasswordsGrantee.Person("user-ben"), PasswordsGrantee.Person("user-mallory"))),
        )
        for (fields in listOf(withoutRecord, widerRecord)) {
            val saved = ada.save(fields, item.id)
            assertEquals(access, saved.fields.access)
        }
        // A new item starts with a signed record that grants nobody, whatever the fields carry.
        val fresh = ada.save(loginFields(name = "Fresh").withAccess(access))
        assertEquals(PasswordsSharingCheck.Verified, fresh.sharingCheck)
        assertEquals(emptyList<PasswordsGrantee>(), fresh.fields.access?.grantees)
        assertEquals(false, fresh.fields.access?.isUsableByAgent)
        assertEquals(1L, fresh.fields.access?.recordVersion)
    }

    // What the server says does not decide

    @Test fun `a server that answers with the shares unchanged after a narrowing still gets the rekey`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-ben" to "Ben")
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val ben = makeSession(server, "user-ben")
        setUp(ben, "ben invented passphrase")
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeUserId = "user-ben")
        ada.setShares(item.id, listOf(person("user-ben")), recipients)
        val keyBefore = server.item(item.id)?.itemKeyByAccountKey

        flags.isAnsweringSharesUnchanged = true
        flags.calls.clear()
        ada.setShares(item.id, emptyList(), emptyList())
        // The answer was the old item, so the first re-key is based on a revision the server has left behind; it is
        // refused, the copy is read again, and the second one goes through.
        assertEquals("setPasswordItemShares", flags.calls.first())
        assertEquals("rekeyPasswordItem", flags.calls.last())
        assertTrue(flags.calls.count { call -> call == "rekeyPasswordItem" } >= 1)
        val stored = server.item(item.id)
        assertNotEquals(keyBefore, stored?.itemKeyByAccountKey)
        assertNull(stored?.keys?.get("user-ben"))
        assertEquals(false, stored?.needsNewKey)
    }

    @Test fun `a forged waiting share and a forged share never get a key`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-mallory" to "Mallory")
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val mallory = makeSession(server, "user-mallory")
        setUp(mallory, "mallory invented passphrase")
        // Ada knows Mallory's key from sharing another item with her.
        val shared = ada.save(loginFields(name = "Shared"))
        val recipients = ada.recipients(granteeUserId = "user-mallory")
        ada.setShares(shared.id, listOf(person("user-mallory")), recipients)
        val secret = ada.save(loginFields(name = "Secret"))
        val malloryView = SharingFixture.recipientView(recipients.first())

        // The server lists Mallory as waiting for the secret item and invents a share that reaches her.
        flags.forgedWaiting = listOf(PasswordWaitingShareView(secret.id, malloryView))
        flags.forgedShares = mapOf(
            secret.id to listOf(PasswordShareView("forged", "user-mallory", null, "Mallory", listOf(malloryView))),
        )
        ada.sync(isFull = true)
        flags.calls.clear()
        val completed = ada.completeWaitingShares()
        assertEquals(0, completed.addedKeyCount)
        assertTrue(flags.addedShareKeyCalls.isEmpty())

        // A new key for the secret item is not sealed for her either.
        ada.giveNewKey(secret.id)
        assertEquals(0, flags.lastRekeyInput?.shareKeys?.size)
        val stored = server.item(secret.id)
        assertTrue(stored?.keys?.isEmpty() ?: false)
    }

    @Test fun `a rekey seals the agent copy only when the owners record says the agent may use the item`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada")
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val item = ada.save(loginFields())
        // The server claims the agent may use the item and that it needs a new key.
        flags.falselyAgentAndMarkedItemIds = setOf(item.id)
        ada.sync(isFull = true)
        val listed = ada.list()
        assertEquals(1, listed.items.size)
        assertFalse(listed.items[0].isUsableByAgent, "the server's flag alone is not enough")
        assertTrue(listed.items[0].needsNewKey)

        val given = ada.giveNewKeysToMarkedItems()
        assertEquals(listOf(item.id), given)
        assertNull(flags.lastRekeyInput?.itemKeyByAgentKey)
        assertNull(server.item(item.id)?.itemKeyByAgentKey)
    }

    @Test fun `a rekey keeps the agent copy when the record says so`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada")
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val item = ada.save(loginFields())
        ada.setUsableByAgent(item.id, true)
        ada.giveNewKey(item.id)
        assertNotNull(flags.lastRekeyInput?.itemKeyByAgentKey)
        val opened = ada.item(item.id)
        assertTrue(opened.isUsableByAgent)
    }

    // The mark the server sets

    @Test fun `a member leaving a circle marks the item and the next open gives it a new key before a save`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace", "user-linus" to "Linus")
        server.setCircle("circle-one", listOf("user-ada", "user-grace", "user-linus"))
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val grace = makeSession(server, "user-grace")
        val linus = makeSession(server, "user-linus")
        setUp(grace, "grace invented passphrase")
        setUp(linus, "linus invented passphrase")
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeCircleId = "circle-one")
        ada.setShares(item.id, listOf(circle("circle-one")), recipients)
        linus.sync()
        val linusOpened = linus.item(item.id)
        assertEquals(loginFields(), linusOpened.fields.withAccess(null))

        // Linus leaves the circle. The server marks the item and drops his key.
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        ada.sync()
        assertEquals(listOf(item.id), ada.itemIdsNeedingNewKey())
        flags.calls.clear()
        val saved = ada.save(loginFields(password = "changed afterwards"), item.id)
        assertEquals(listOf("rekeyPasswordItem"), flags.calls, "a marked item is given a new key instead of being saved with the old one")
        assertEquals(loginFields(password = "changed afterwards"), saved.fields.withAccess(null))
        assertFalse(saved.needsNewKey)
        assertEquals(emptyList<String>(), ada.itemIdsNeedingNewKey())

        // Grace keeps her access (she held a key when this device last saw the item); Linus is gone.
        grace.sync()
        val graceOpened = grace.item(item.id)
        assertEquals(loginFields(password = "changed afterwards"), graceOpened.fields.withAccess(null))
        val linusResult = linus.sync()
        assertEquals(listOf(item.id), linusResult.removedItemIds)
    }

    /**
     * A process that stops between the removal and the re-key (killed, suspended for good) leaves nothing on the phone to
     * remember it by; the server's mark on the item is what the next launch finds.
     */
    @Test fun `the servers mark survives a process that stopped between the removal and the rekey`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-ben" to "Ben")
        val firstCache = makeCache()
        val first = makeSession(server, "user-ada", cache = firstCache)
        setUp(first, "ada invented passphrase")
        setUp(makeSession(server, "user-ben"), "ben invented passphrase")
        val item = first.save(loginFields())
        val recipients = first.recipients(granteeUserId = "user-ben")
        first.setShares(item.id, listOf(person("user-ben")), recipients)
        server.rekeyRefusal = "api: internal error"
        val error = assertThrows<PasswordsError> { runBlocking { first.setShares(item.id, emptyList(), emptyList()) } }
        assertEquals(PasswordsError.ItemNeedsNewKey(item.id), error)
        server.rekeyRefusal = null

        // The phone starts again: the session is new, the pin store is the same, so the memory of the item from the first session is still there.
        val relaunched = makeSession(server, "user-ada", cache = firstCache)
        relaunched.unlockWithPassphrase("ada invented passphrase")
        relaunched.sync()
        assertEquals(listOf(item.id), relaunched.itemIdsNeedingNewKey())
        val given = relaunched.giveNewKeysToMarkedItems()
        assertEquals(listOf(item.id), given)
        assertEquals(emptyList<String>(), relaunched.itemIdsNeedingNewKey())
    }

    /**
     * A device that has nothing of the item (another phone, or one reinstalled) cannot tell an old signed record from the
     * newest one, so it leaves the marked item for the person, who finishes it.
     */
    @Test fun `a marked item a device has no memory of waits for the person who finishes it`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-ben" to "Ben")
        val first = makeSession(server, "user-ada")
        setUp(first, "ada invented passphrase")
        setUp(makeSession(server, "user-ben"), "ben invented passphrase")
        val item = first.save(loginFields())
        val recipients = first.recipients(granteeUserId = "user-ben")
        first.setShares(item.id, listOf(person("user-ben")), recipients)
        server.markNeedsNewKey(item.id)

        val other = makeSession(server, "user-ada")
        other.unlockWithPassphrase("ada invented passphrase")
        other.sync()
        other.item(item.id)
        val given = other.giveNewKeysToMarkedItems()
        assertEquals(emptyList<String>(), given, "looking at the item in this session is not a memory of it")
        assertEquals(setOf(item.id), other.itemIdsNeedingYou())
        assertEquals(listOf(item.id), other.itemIdsNeedingNewKey())

        other.finishSharing(item.id, other.finishPlan(listOf(item.id)))
        assertEquals(emptyList<String>(), other.itemIdsNeedingNewKey())
        assertEquals(emptySet<String>(), other.itemIdsNeedingYou())
        val stored = server.item(item.id)
        assertEquals(setOf("user-ben"), stored?.keys?.keys, "the person's own action seals for whom the record reaches")
    }

    @Test fun `a device gives a marked item a new key only for people it sealed for and the rest wait for the person`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace", "user-linus" to "Linus")
        server.setCircle("circle-one", listOf("user-ada", "user-grace", "user-linus"))
        val firstCache = makeCache()
        val first = makeSession(server, "user-ada", cache = firstCache)
        setUp(first, "ada invented passphrase")
        for (userId in listOf("user-grace", "user-linus")) setUp(makeSession(server, userId), "$userId invented passphrase")
        val item = first.save(loginFields())
        val recipients = first.recipients(granteeCircleId = "circle-one")
        first.setShares(item.id, listOf(circle("circle-one")), recipients)
        // Grace is removed from the circle and added again: the item is marked and she has no key.
        server.setCircle("circle-one", listOf("user-ada", "user-linus"))
        server.setCircle("circle-one", listOf("user-ada", "user-grace", "user-linus"))

        // The same phone in a later session (same pin store) remembers the item and sealing for both.
        val later = makeSession(server, "user-ada", cache = firstCache)
        later.unlockWithPassphrase("ada invented passphrase")
        later.sync()
        val given = later.giveNewKeysToMarkedItems()
        assertEquals(listOf(item.id), given)
        val afterRekey = server.item(item.id)
        assertEquals(setOf("user-grace", "user-linus"), afterRekey?.keys?.keys)
    }

    @Test fun `waiting shares of an item a device has no memory of are finished by the person with a notice for the circle members`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        val first = makeSession(server, "user-ada")
        setUp(first, "ada invented passphrase")
        val item = first.save(loginFields())
        val early = first.recipients(granteeCircleId = "circle-one")
        first.setShares(item.id, listOf(circle("circle-one")), early)
        // Grace sets up Passwords afterwards: a second phone of Ada's, which never saw the item, is not to seal for her.
        setUp(makeSession(server, "user-grace"), "grace invented passphrase")
        val second = makeSession(server, "user-ada")
        second.unlockWithPassphrase("ada invented passphrase")
        second.sync()
        val completed = second.completeWaitingShares()
        assertEquals(0, completed.addedKeyCount)
        assertEquals(setOf(item.id), second.itemIdsNeedingYou())
        assertEquals(emptyList<PasswordsCircleShareNotice>(), second.circleShareNotices())

        // The person opens the item and presses the button.
        second.finishSharing(item.id, second.finishPlan(listOf(item.id)))
        val stored = server.item(item.id)
        assertNotNull(stored?.keys?.get("user-grace"))
        assertEquals(emptySet<String>(), second.itemIdsNeedingYou())
        assertEquals(
            listOf(PasswordsCircleShareNotice(item.id, "Example", "user-grace", "Grace", "circle-one")),
            second.circleShareNotices(),
        )
        second.dismissCircleShareNotice(item.id, "user-grace")
        assertEquals(emptyList<PasswordsCircleShareNotice>(), second.circleShareNotices())
    }

    /** Sealing for a circle's member the device sealed for before shows no notice; a person the record names itself never does. */
    @Test fun `only a circle member the device never sealed for gets a notice`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-ben" to "Ben", "user-grace" to "Grace")
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        val ada = makeSession(server, "user-ada")
        setUp(ada, "ada invented passphrase")
        for (userId in listOf("user-ben", "user-grace")) setUp(makeSession(server, userId), "$userId invented passphrase")
        val item = ada.save(loginFields())
        val benRecipients = ada.recipients(granteeUserId = "user-ben")
        val circleRecipients = ada.recipients(granteeCircleId = "circle-one")
        ada.setShares(item.id, listOf(person("user-ben"), circle("circle-one")), benRecipients + circleRecipients)
        val notices = ada.circleShareNotices()
        assertEquals(listOf("user-grace"), notices.map { notice -> notice.recipientUserId })
        assertEquals("circle-one", notices.first().circleName)
        // Sharing again seals for nobody new: no second notice.
        ada.dismissCircleShareNotice(item.id, "user-grace")
        ada.setShares(item.id, listOf(person("user-ben"), circle("circle-one")), benRecipients + circleRecipients)
        assertEquals(emptyList<PasswordsCircleShareNotice>(), ada.circleShareNotices())
    }

    // Share keys name the item key

    @Test fun `a share key for a waiting recipient names the item key it was sealed from`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val item = ada.save(loginFields())
        // Grace has not set up Passwords: nothing is sealed yet.
        val early = ada.recipients(granteeCircleId = "circle-one")
        ada.setShares(item.id, listOf(circle("circle-one")), early)
        val grace = makeSession(server, "user-grace")
        setUp(grace, "grace invented passphrase")
        // Ada seals for Grace's key when she finishes the sharing of the item.
        ada.sync(isFull = true)
        ada.finishSharing(item.id, ada.finishPlan(listOf(item.id)))
        val stored = server.item(item.id)
        assertEquals(1, flags.addedShareKeyCalls.size)
        assertEquals(stored?.itemKeyByAccountKey, flags.addedShareKeyCalls.first().first().itemKey)
        grace.sync()
        val opened = grace.item(item.id)
        assertEquals(loginFields(), opened.fields.withAccess(null))
    }

    @Test fun `the copy is read in full once when the server names somebody the copy does not reach`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        server.setCircle("circle-one", listOf("user-ada"))
        val ada = makeSession(server, "user-ada")
        setUp(ada, "ada invented passphrase")
        val grace = makeSession(server, "user-grace")
        setUp(grace, "grace invented passphrase")
        // Ada knows Grace's key from sharing another item with her.
        val other = ada.save(loginFields(name = "Other"))
        val graceRecipients = ada.recipients(granteeUserId = "user-grace")
        ada.setShares(other.id, listOf(person("user-grace")), graceRecipients)
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeCircleId = "circle-one")
        ada.setShares(item.id, listOf(circle("circle-one")), recipients)
        ada.sync()

        // Grace joins the circle later: the server lists her as waiting; the copy's shares do not list her yet.
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        server.markListCalls()
        ada.sync(isFull = true)
        ada.finishSharing(item.id, ada.finishPlan(listOf(item.id)))
        val listCalls = server.listCallsSinceMark()
        assertEquals(1, listCalls.count { revision -> revision == null }, "one full read")
        grace.sync()
        val opened = grace.item(item.id)
        assertEquals(loginFields(), opened.fields.withAccess(null))
    }

    // Keys and safety codes

    @Test fun `the own safety code comes from the opened key pair`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada")
        val ada = makeSession(server, "user-ada")
        setUp(ada, "ada invented passphrase")
        val code = ada.ownSafetyCode()
        assertEquals(8, code.split(" ").size)
        val publicKey = unbase64(server.publicKey("user-ada"))
        assertEquals(PasswordsCrypto.safetyCode(publicKey), code)
        ada.lock()
        val error = assertThrows<PasswordsError> { ada.ownSafetyCode() }
        assertEquals(PasswordsError.Locked, error)
    }

    @Test fun `an owners key is recorded as known only after an item of theirs opened`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada")
        setUp(ada, "ada invented passphrase")
        val graceCache = makeCache()
        val grace = makeSession(server, "user-grace", flags, graceCache)
        setUp(grace, "grace invented passphrase")
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeUserId = "user-grace")
        ada.setShares(item.id, listOf(person("user-grace")), recipients)

        // The server hands Grace the item with its sealed fields damaged: it does not open, so nothing is recorded.
        flags.damagedFieldsByItemId = mapOf(item.id to "{}")
        grace.sync()
        val damaged = grace.list()
        assertEquals(0, damaged.items.size)
        assertEquals(listOf(item.id), damaged.unreadableItemIds)
        assertEquals(PasswordsPins(userId = "user-grace"), graceCache.loadPins("user-grace"))

        // The real item opens, and then the key is recorded.
        flags.damagedFieldsByItemId = emptyMap()
        grace.sync(isFull = true)
        val opened = grace.item(item.id)
        assertEquals(loginFields(), opened.fields.withAccess(null))
        val adaKey = server.publicKey("user-ada")
        assertEquals(mapOf("user-ada" to adaKey), graceCache.loadPins("user-grace").knownPublicKeys)
        assertEquals(emptyMap<String, String>(), graceCache.loadPins("user-grace").comparedPublicKeys)
    }

    @Test fun `a changed owner key that opens nothing is not recorded and comparing codes marks the new key compared`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        val ada = makeSession(server, "user-ada")
        setUp(ada, "ada invented passphrase")
        val graceCache = makeCache()
        val grace = makeSession(server, "user-grace", cache = graceCache)
        setUp(grace, "grace invented passphrase")
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeUserId = "user-grace")
        ada.setShares(item.id, listOf(person("user-grace")), recipients)
        grace.sync()
        val oldKey = server.publicKey("user-ada")
        // The server shows another key for Ada without sealing the item key for it: the item opens with nothing, so the
        // key is not recorded and there is no notice.
        val newKey = base64(PasswordsCrypto.makeKeyPair().publicKey)
        server.replacePublicKey("user-ada", newKey)
        server.setRevision(item.id, 900)
        grace.sync(isFull = true)
        val listed = grace.list()
        assertEquals(listOf(item.id), listed.unreadableItemIds)
        assertEquals(0, grace.keyChanges().size)
        assertEquals(oldKey, graceCache.loadPins("user-grace").knownPublicKeys["user-ada"])

        // Comparing codes with the key the person was shown records it, marked as compared.
        grace.markCompared("user-ada", newKey)
        val pins = graceCache.loadPins("user-grace")
        assertEquals(newKey, pins.knownPublicKeys["user-ada"])
        assertEquals(newKey, pins.comparedPublicKeys["user-ada"])
        assertEquals(emptyMap<String, String>(), pins.changedPublicKeys)
        val recipientsOfAda = grace.recipients(listOf(PasswordRecipientView("user-ada", "Ada", "ada", newKey, false)))
        assertEquals(PasswordsKeyTrust.Known, recipientsOfAda.first().keyTrust)
        assertEquals(true, recipientsOfAda.first().isCompared)
    }
}
