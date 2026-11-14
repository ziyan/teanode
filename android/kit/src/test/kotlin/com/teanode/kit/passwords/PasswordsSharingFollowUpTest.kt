package com.teanode.kit.passwords

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.passwords.SharingFixture.loginFields
import com.teanode.kit.passwords.SharingFixture.makeServer
import com.teanode.kit.passwords.SharingFixture.makeSession
import com.teanode.kit.passwords.SharingFixture.setUp
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.api.io.TempDir
import java.io.File

/** Finishing a share leaves the copy current, the server's two warnings about sharing are read, and what a device remembers about an item does not depend on the user id the server names. */
class PasswordsSharingFollowUpTest {
    /** Ada shares an item with Grace before Grace has set up Passwords, so Grace is waiting. */
    private suspend fun shareWithWaitingGrace(ada: PasswordsSession): PasswordItem {
        val item = ada.save(loginFields())
        val recipients = ada.recipients(granteeUserId = "user-grace")
        return ada.setShares(item.id, listOf(PasswordShareGranteeInput(granteeUserId = "user-grace")), recipients)
    }

    // The copy after finishing

    @Test fun `finishing sharing reads the item again so the copy shows the key given`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        val ada = makeSession(server, "user-ada")
        setUp(ada, "ada invented passphrase")
        val item = shareWithWaitingGrace(ada)
        setUp(makeSession(server, "user-grace"), "grace invented passphrase")
        ada.sync()
        val before = ada.item(item.id)
        assertEquals(listOf(false), before.shares.flatMap { share -> share.recipients }.map { recipient -> recipient.hasKey })

        ada.finishSharing(item.id, ada.finishPlan(listOf(item.id)))
        val after = ada.item(item.id)
        assertEquals(listOf(true), after.shares.flatMap { share -> share.recipients }.map { recipient -> recipient.hasKey }, "the copy was read again after the key was added")
    }

    @Test fun `an automatic completion reads the changed items again too`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val item = shareWithWaitingGrace(ada)
        setUp(makeSession(server, "user-grace"), "grace invented passphrase")
        // This session chose Grace itself, so it may complete the share by itself.
        val completed = ada.completeWaitingShares()
        assertEquals(1, completed.addedKeyCount)
        val after = ada.item(item.id)
        assertEquals(listOf(true), after.shares.flatMap { share -> share.recipients }.map { recipient -> recipient.hasKey })
        // Sharing again seals for nobody: the copy knows Grace has a key.
        val recipients = ada.recipients(granteeUserId = "user-grace")
        ada.setShares(item.id, listOf(PasswordShareGranteeInput(granteeUserId = "user-grace")), recipients)
        assertEquals(0, flags.lastShareKeys.size)
    }

    // Too much shared, and more waiting

    @Test fun `a waiting person with too much shared is skipped and named`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        val flags = HookedServerFlags()
        val ada = makeSession(server, "user-ada", flags)
        setUp(ada, "ada invented passphrase")
        val item = shareWithWaitingGrace(ada)
        setUp(makeSession(server, "user-grace"), "grace invented passphrase")
        server.setHasTooMuchShared("user-grace", true)

        val recipients = ada.recipients(granteeUserId = "user-grace")
        assertEquals(listOf(true), recipients.map { recipient -> recipient.hasTooMuchShared })
        val waiting = ada.waitingShares()
        assertEquals(listOf(true), waiting.map { share -> share.recipient.hasTooMuchShared })

        val completed = ada.completeWaitingShares()
        assertEquals(0, completed.addedKeyCount)
        assertEquals(listOf("Grace"), completed.tooMuchSharedNames)
        // She has no room, so the plan has nothing to give her and finishing is refused.
        val plan = ada.finishPlan(listOf(item.id))
        assertEquals(emptyList<PasswordsFinishPlan.Item>(), plan.items)
        val error = assertThrows<Throwable>("finished an item the plan did not name") { runBlocking { ada.finishSharing(item.id, plan) } }
        assertEquals(PasswordsError.ItemNotInSharingPlan, error)
        assertEquals(0, flags.addedShareKeyCalls.size, "no key is sealed for somebody the server stores none for")
        assertEquals(emptySet<String>(), ada.itemIdsNeedingYou())

        // Sharing again does not seal for her either.
        ada.setShares(item.id, listOf(PasswordShareGranteeInput(granteeUserId = "user-grace")), recipients)
        assertEquals(0, flags.lastShareKeys.size)

        // Once she has room again, the next completion seals for her.
        server.setHasTooMuchShared("user-grace", false)
        ada.sync(isFull = true)
        ada.finishSharing(item.id, ada.finishPlan(listOf(item.id)))
        assertEquals(1, flags.addedShareKeyCalls.size)
    }

    @Test fun `the completion says when more are waiting than the server listed`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        val ada = makeSession(server, "user-ada")
        setUp(ada, "ada invented passphrase")
        shareWithWaitingGrace(ada)
        setUp(makeSession(server, "user-grace"), "grace invented passphrase")
        val plain = ada.completeWaitingShares()
        assertFalse(plain.hasMoreWaiting)

        val second = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        val adaAgain = makeSession(second, "user-ada")
        setUp(adaAgain, "ada invented passphrase")
        shareWithWaitingGrace(adaAgain)
        setUp(makeSession(second, "user-grace"), "grace invented passphrase")
        second.setHasMoreWaiting(true)
        val waiting = adaAgain.waitingShares()
        assertEquals(listOf(true), waiting.map { share -> share.hasMoreWaiting })
        val completed = adaAgain.completeWaitingShares()
        assertTrue(completed.hasMoreWaiting)
    }

    @Test fun `the new fields are read from the servers answer and an older answer means false`() {
        val withFields = """{"itemId":"item-one","hasMoreWaiting":true,"recipient":{"userId":"user-grace","name":"Grace","username":"grace","publicKey":null,"hasKey":false,"hasTooMuchShared":true}}"""
        val decoded = TeaNodeJson.decodeFromString(PasswordWaitingShareView.serializer(), withFields)
        assertTrue(decoded.hasMoreWaiting)
        assertTrue(decoded.recipient.hasTooMuchShared)
        val without = """{"itemId":"item-one","recipient":{"userId":"user-grace","name":"Grace","username":"grace","publicKey":null,"hasKey":false}}"""
        val older = TeaNodeJson.decodeFromString(PasswordWaitingShareView.serializer(), without)
        assertFalse(older.hasMoreWaiting)
        assertFalse(older.recipient.hasTooMuchShared)
        assertTrue(PasswordsDocuments.WAITING_PASSWORD_SHARES.contains("hasMoreWaiting"))
        assertTrue(PasswordsDocuments.WAITING_PASSWORD_SHARES.contains("hasTooMuchShared"))
        assertTrue(PasswordsDocuments.PASSWORD_RECIPIENTS.contains("hasTooMuchShared"))
        assertTrue(PasswordsDocuments.PASSWORD_ITEMS.contains("hasTooMuchShared"))
    }

    // What a device remembers about an item

    private fun memory(version: Long) = PasswordsItemMemory(highestRecordVersion = version, highestRecordMac = "mac-$version")

    private fun assertMemoriesFollowTheItemAndNotTheUserId(cache: PasswordsCache) {
        cache.recordSeenRecord("user-ada", "item-one", 4, "mac-4")
        cache.recordKnownIfAbsent("user-ada", "user-grace", "grace-key")
        val named = cache.loadPins("user-ada")
        assertEquals(4L, named.itemMemories["item-one"]?.highestRecordVersion)

        // The server names another user id for the same sign-in: what was remembered about the item is still read, and the
        // keys known for other people are not carried over.
        val renamed = cache.loadPins("user-other")
        assertEquals(4L, renamed.itemMemories["item-one"]?.highestRecordVersion)
        assertEquals("mac-4", renamed.itemMemories["item-one"]?.highestRecordMac)
        assertEquals(emptyMap<String, String>(), renamed.knownPublicKeys)
        assertEquals("grace-key", named.knownPublicKeys["user-grace"])
    }

    @Test fun `the memory storage keeps item memories by item id`() {
        assertMemoriesFollowTheItemAndNotTheUserId(PasswordsCache(InMemoryPasswordsStorage()))
    }

    @Test fun `the file storage keeps item memories by item id`(@TempDir directory: File) {
        assertMemoriesFollowTheItemAndNotTheUserId(PasswordsCache(FilePasswordsStorage(directory)))
    }

    @Test fun `two copies of an item memory merge to the stricter one`() {
        val older = memory(2).copy(sealedForUserIds = listOf("user-ben"), didMakeAgentCopy = true)
        val newer = memory(5).copy(sealedForUserIds = listOf("user-grace"), chosenUserIds = listOf("user-grace"))
        val merged = older.merging(newer)
        assertEquals(5L, merged.highestRecordVersion)
        assertEquals("mac-5", merged.highestRecordMac)
        assertEquals(listOf("user-ben", "user-grace"), merged.sealedForUserIds)
        assertEquals(listOf("user-grace"), merged.chosenUserIds)
        assertFalse(merged.didMakeAgentCopy, "the agent copy counts only when both made it")
        assertEquals(merged, newer.merging(older))
    }
}
