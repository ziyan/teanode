package com.teanode.kit.passwords

import com.teanode.kit.passwords.SharingFixture.ITERATION_COUNT
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

/** A server that restores another version than the one asked for. */
internal class RestoreVersionSwap {
    var replacementVersionId: String? = null
}

internal class RestoreVersionSwapApi(private val inner: PasswordsApi, private val swap: RestoreVersionSwap) : PasswordsApi by inner {
    override suspend fun restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Long) =
        inner.restorePasswordItemVersion(itemId, swap.replacementVersionId ?: versionId, baseRevision)
}

/** Restoring an earlier version brings back the content only, and a confirmed Finish sharing plan is all that is sealed. */
class PasswordsRestoreAndPlanTest {
    private val passphrase = "ada invented passphrase"

    private fun makeSession(server: FakePasswordsServer, userId: String, swap: RestoreVersionSwap? = null): PasswordsSession {
        val api = server.api(userId)
        return PasswordsSession(if (swap != null) RestoreVersionSwapApi(api, swap) else api, SharingFixture.makeCache(), userId)
    }

    private fun loginFields(password: String = "correct horse") = SharingFixture.loginFields("Example", password)

    private fun grantee(userId: String) = PasswordShareGranteeInput(granteeUserId = userId)

    private suspend fun people(server: FakePasswordsServer) {
        for ((userId, name) in listOf("user-ada" to "Ada", "user-ben" to "Ben", "user-cora" to "Cora")) server.addPerson(userId, name)
        for ((userId, name) in listOf("user-ben" to "ben", "user-cora" to "cora")) makeSession(server, userId).setUp("$name invented passphrase", ITERATION_COUNT)
    }

    private class History(val item: PasswordItem, val plainVersionId: String, val wideVersionId: String)

    /**
     * Ada's item with a history: a plain save, then shared with Ben and the agent let in, then Ben taken off and the agent
     * turned off, then an edit. Returns the item and the ids of the versions that name nobody (`plainVersionId`) and that
     * name Ben with the agent on (`wideVersionId`).
     */
    private suspend fun itemWithHistory(ada: PasswordsSession): History {
        val item = ada.save(loginFields("first-password"))
        val recipients = ada.recipients(granteeUserId = "user-ben")
        ada.setShares(item.id, listOf(grantee("user-ben")), recipients)
        ada.setUsableByAgent(item.id, true)
        ada.save(loginFields("second-password"), item.id, null)
        ada.setShares(item.id, emptyList(), emptyList())
        ada.setUsableByAgent(item.id, false)
        ada.save(loginFields("third-password"), item.id, null)
        val opened = ada.versions(item.id)
        var plainVersionId: String? = null
        var wideVersionId: String? = null
        for (version in opened.versions) {
            val access = version.fields.access
            val isWide = access?.isUsableByAgent == true && access.grantees.isNotEmpty()
            val isPlain = (access?.grantees?.isEmpty() ?: true) && access?.isUsableByAgent != true
            if (isWide && wideVersionId == null) wideVersionId = version.id
            if (isPlain && plainVersionId == null) plainVersionId = version.id
        }
        return History(item, plainVersionId!!, wideVersionId!!)
    }

    // The answer to a restore is the version the person chose

    /**
     * The person looked at the plain version and taps Restore. The server restores the wide version (Ben and the agent on
     * its record) and lists Ben as a recipient. The phone refuses the answer and seals nothing for Ben or the agent.
     */
    @Test fun `restore answered with another old record is refused and seals nothing`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        people(server)
        val swap = RestoreVersionSwap()
        val ada = makeSession(server, "user-ada", swap)
        ada.setUp("ada invented passphrase", ITERATION_COUNT)
        val history = itemWithHistory(ada)
        val before = ada.item(history.item.id)
        assertEquals(emptyList<PasswordsGrantee>(), before.verifiedAccess?.grantees ?: emptyList<PasswordsGrantee>())
        assertEquals(false, before.verifiedAccess?.isUsableByAgent)

        // The server lists Ben, which the person's own record does not.
        server.setShares("user-ada", history.item.id, listOf(grantee("user-ben")), emptyList())
        ada.sync(isFull = true)
        swap.replacementVersionId = history.wideVersionId
        val error = assertThrows<Throwable>("a restore answered with another version went through") {
            runBlocking { ada.restoreVersion(history.item.id, history.plainVersionId) }
        }
        assertEquals(PasswordsError.RestoredAnotherVersion, error)
        val stored = server.item(history.item.id)!!
        assertNull(stored.keys["user-ben"], "Ben got no key")
        assertFalse(stored.isUsableByAgent, "the agent was not turned on")
        assertNull(stored.itemKeyByAgentKey)
        val held = ada.item(history.item.id)
        assertEquals(emptyList<PasswordsGrantee>(), held.verifiedAccess?.grantees ?: emptyList<PasswordsGrantee>(), "nothing was kept from the answer")
    }

    /** A version the server did not list in this session cannot be restored, and the server is not asked. */
    @Test fun `restore of a version not listed in this session is refused`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        people(server)
        val ada = makeSession(server, "user-ada")
        ada.setUp("ada invented passphrase", ITERATION_COUNT)
        val history = itemWithHistory(ada)
        val other = makeSession(server, "user-ada")
        other.unlockWithPassphrase("ada invented passphrase")
        val revisionBefore = server.item(history.item.id)?.revision
        val error = assertThrows<Throwable>("a restore of a version nobody listed went through") {
            runBlocking { other.restoreVersion(history.item.id, history.plainVersionId) }
        }
        assertEquals(PasswordsError.ItemNotFound, error)
        assertEquals(revisionBefore, server.item(history.item.id)?.revision)
    }

    // Sharing stays as it is now

    /**
     * An honest restore of the version in which the agent was on and Ben was shared: the content comes back, Cora (shared
     * with since) keeps her key, Ben (only in the old record) gets none, and the agent stays off.
     */
    @Test fun `restore of an old version keeps the sharing as it is now`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        people(server)
        val ada = makeSession(server, "user-ada")
        ada.setUp("ada invented passphrase", ITERATION_COUNT)
        val history = itemWithHistory(ada)
        // Cora is shared with after all of that.
        val coraRecipients = ada.recipients(granteeUserId = "user-cora")
        ada.setShares(history.item.id, listOf(grantee("user-cora")), coraRecipients)
        val before = server.item(history.item.id)!!
        assertNotNull(before.keys["user-cora"])
        assertFalse(before.isUsableByAgent)

        val restored = ada.restoreVersion(history.item.id, history.wideVersionId)
        assertEquals("second-password", (restored.fields as PasswordsItemFields.Login).fields.password)
        val after = server.item(history.item.id)!!
        assertFalse(after.isUsableByAgent, "the restore does not turn the agent on")
        assertNull(after.itemKeyByAgentKey)
        assertNotNull(after.keys["user-cora"], "Cora, shared with now, has the new key")
        assertNull(after.keys["user-ben"], "Ben was only in the old version's record")
        assertFalse(after.needsNewKey)
        assertEquals(listOf<PasswordsGrantee>(PasswordsGrantee.Person("user-cora")), restored.verifiedAccess?.grantees)
        assertEquals(false, restored.verifiedAccess?.isUsableByAgent)
        assertFalse(ada.itemIdsNeedingYou().contains(history.item.id))
    }

    // The plan is what is sealed

    /** The plan names Ben. Before the person confirms, the phone reads the server again and a second member is in the circle. Finishing seals for Ben alone. */
    @Test fun `finish sharing seals only for the people the plan named`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        people(server)
        server.setCircle("circle-one", listOf("user-ada", "user-ben"))
        val one = makeSession(server, "user-ada")
        one.setUp(passphrase, ITERATION_COUNT)
        val item = one.save(loginFields())
        val circleRecipients = one.recipients(granteeCircleId = "circle-one")
        one.setShares(item.id, listOf(PasswordShareGranteeInput(granteeCircleId = "circle-one")), circleRecipients)
        // A second phone with no memory of the item: it is marked, so this phone leaves it for the person.
        val two = makeSession(server, "user-ada")
        two.unlockWithPassphrase(passphrase)
        server.markNeedsNewKey(item.id)
        two.sync(isFull = true)
        val plan = two.finishPlan(listOf(item.id))
        assertEquals(listOf("user-ben"), plan.people.map { person -> person.userId })
        assertEquals(listOf(listOf("user-ben")), plan.items.map { planItem -> planItem.recipientUserIds })

        // The dialog stays up; the server adds Cora to the circle; the phone reads again.
        server.setCircle("circle-one", listOf("user-ada", "user-ben", "user-cora"))
        two.sync(isFull = true)
        two.finishSharing(item.id, plan)
        val stored = server.item(item.id)!!
        assertNotNull(stored.keys["user-ben"])
        assertNull(stored.keys["user-cora"], "Cora was not in the plan the person confirmed")
    }

    /** The same for an item that was not marked: only the waiting people the plan names get a key. */
    @Test fun `finish sharing of an item not marked leaves a waiting person the plan did not name without a key`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        people(server)
        server.setCircle("circle-one", listOf("user-ada", "user-ben"))
        val one = makeSession(server, "user-ada")
        one.setUp(passphrase, ITERATION_COUNT)
        val item = one.save(loginFields())
        val circleRecipients = one.recipients(granteeCircleId = "circle-one")
        one.setShares(item.id, listOf(PasswordShareGranteeInput(granteeCircleId = "circle-one")), circleRecipients)
        // A second phone with no memory of the item leaves Ben's key for the person: the first phone sealed for him, so
        // take his key away to make him wait.
        server.removeKey(item.id, "user-ben")
        val two = makeSession(server, "user-ada")
        two.unlockWithPassphrase(passphrase)
        two.sync(isFull = true)
        val plan = two.finishPlan(listOf(item.id))
        assertEquals(listOf(listOf("user-ben")), plan.items.map { planItem -> planItem.recipientUserIds })
        server.setCircle("circle-one", listOf("user-ada", "user-ben", "user-cora"))
        two.sync(isFull = true)
        two.finishSharing(item.id, plan)
        val stored = server.item(item.id)!!
        assertNotNull(stored.keys["user-ben"])
        assertNull(stored.keys["user-cora"])
    }

    @Test fun `finish sharing of an item the confirmed plan does not name is refused`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        people(server)
        val ada = makeSession(server, "user-ada")
        ada.setUp("ada invented passphrase", ITERATION_COUNT)
        val item = ada.save(loginFields())
        val plan = ada.finishPlan(emptyList())
        val error = assertThrows<Throwable>("finished an item the plan did not name") { runBlocking { ada.finishSharing(item.id, plan) } }
        assertEquals(PasswordsError.ItemNotInSharingPlan, error)
    }

    // A phone with no memory of the item

    private suspend fun shareItem(session: PasswordsSession, item: PasswordItem, userIds: List<String>) {
        val recipients = mutableListOf<PasswordRecipient>()
        for (userId in userIds) recipients += session.recipients(granteeUserId = userId)
        session.setShares(item.id, userIds.map { userId -> grantee(userId) }, recipients)
    }

    /**
     * The owner shared the item with Ben, then took Ben off on the first phone. The server puts the old record back and
     * lists Ben again. Returns the item, with the first phone's session.
     */
    private suspend fun itemWithAnOldRecordBroughtBack(server: FakePasswordsServer): Pair<PasswordItem, PasswordsSession> {
        people(server)
        val one = makeSession(server, "user-ada")
        one.setUp(passphrase, ITERATION_COUNT)
        val item = one.save(loginFields("first-password"))
        shareItem(one, item, listOf("user-ben"))
        val stale = server.item(item.id)!!
        val staleFields = stale.encryptedFields
        val staleKey = stale.itemKeyByAccountKey
        one.save(loginFields("second-password"), item.id, null)
        one.setShares(item.id, emptyList(), emptyList())
        server.setShares("user-ada", item.id, listOf(grantee("user-ben")), emptyList())
        server.replaceStoredFields(item.id, staleFields, staleKey, needsNewKey = false)
        return item to one
    }

    /** A second phone has never seen the item and is shown the old record as the item's. Restoring an earlier version there seals the new key for nobody the old record names, and makes no agent copy. */
    @Test fun `restore on a phone that never saw the item seals for nobody an old record names`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        val (item, _) = itemWithAnOldRecordBroughtBack(server)
        val two = makeSession(server, "user-ada")
        two.unlockWithPassphrase(passphrase)
        two.sync(isFull = true)
        val opened = two.versions(item.id)
        val versionId = opened.versions.first().id
        two.restoreVersion(item.id, versionId)

        val stored = server.item(item.id)!!
        assertNull(stored.keys["user-ben"], "Ben was named by a record this phone could not tell from an old one")
        assertNull(stored.itemKeyByAgentKey)
        assertFalse(stored.needsNewKey)
        // Ben is left for Finish sharing, which shows the plan first.
        val plan = two.finishPlan(listOf(item.id))
        assertEquals(listOf(listOf("user-ben")), plan.items.map { planItem -> planItem.recipientUserIds })
    }

    /** The same restore on the first phone, which remembers the newer record, is refused. */
    @Test fun `restore on a phone that remembers the newer record is refused`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        val (item, one) = itemWithAnOldRecordBroughtBack(server)
        one.sync(isFull = true)
        val opened = one.versions(item.id)
        val versionId = opened.versions.first().id
        val error = assertThrows<Throwable>("the restore went ahead") { runBlocking { one.restoreVersion(item.id, versionId) } }
        assertTrue(error is PasswordsError.SharingCouldNotBeChecked)
        assertNull(server.item(item.id)!!.keys["user-ben"])
    }

    /**
     * The plan names Cora, who waits (Ben holds a key). Before the person confirms, the server marks the item as needing a
     * new key and the phone reads again. A new key sealed for Cora alone would take Ben's key away, so finishing stops,
     * seals nothing, and a new plan names both.
     */
    @Test fun `finish sharing stops when the server marks the item after the plan was made`() = runBlocking<Unit> {
        val server = FakePasswordsServer()
        people(server)
        val one = makeSession(server, "user-ada")
        one.setUp(passphrase, ITERATION_COUNT)
        val item = one.save(loginFields())
        shareItem(one, item, listOf("user-ben", "user-cora"))
        server.removeKey(item.id, "user-cora")

        val two = makeSession(server, "user-ada")
        two.unlockWithPassphrase(passphrase)
        two.sync(isFull = true)
        val plan = two.finishPlan(listOf(item.id))
        assertEquals(listOf(listOf("user-cora")), plan.items.map { planItem -> planItem.recipientUserIds })

        server.markNeedsNewKey(item.id)
        two.sync(isFull = true)
        val error = assertThrows<Throwable>("finishing went ahead with a plan that drops Ben's key") { runBlocking { two.finishSharing(item.id, plan) } }
        assertEquals(PasswordsError.SharingPlanOutOfDate, error)
        val stored = server.item(item.id)!!
        assertNotNull(stored.keys["user-ben"])
        assertNull(stored.keys["user-cora"])
        val newPlan = two.finishPlan(listOf(item.id))
        assertEquals(listOf(listOf("user-ben", "user-cora")), newPlan.items.map { planItem -> planItem.recipientUserIds })
        two.finishSharing(item.id, newPlan)
        val finished = server.item(item.id)!!
        assertNotNull(finished.keys["user-ben"])
        assertNotNull(finished.keys["user-cora"])
    }
}
