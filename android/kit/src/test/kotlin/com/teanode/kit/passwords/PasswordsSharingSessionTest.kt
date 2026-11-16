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
import org.junit.jupiter.api.assertThrows

/** The sharing half of the session tests: two people through a server that keeps only sealed text, and keys sealed for the wrong person that must open nothing. */
class PasswordsSharingSessionTest {
    private class Device(val session: PasswordsSession, val cache: PasswordsCache)

    private fun makeDevice(server: FakePasswordsServer, userId: String): Device {
        val cache = SharingFixture.makeCache()
        return Device(PasswordsSession(server.api(userId), cache, userId), cache)
    }

    private fun makeServer(vararg people: Pair<String, String>): FakePasswordsServer {
        val server = FakePasswordsServer()
        for ((userId, name) in people) server.addPerson(userId, name)
        return server
    }

    private class World(val server: FakePasswordsServer, val ada: Device, val grace: Device, val itemId: String)

    private suspend fun makeSharingWorld(): World {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        val ada = makeDevice(server, "user-ada")
        val grace = makeDevice(server, "user-grace")
        ada.session.setUp("ada invented passphrase", ITERATION_COUNT)
        grace.session.setUp("grace invented passphrase", ITERATION_COUNT)
        val item = ada.session.save(loginFields())
        return World(server, ada, grace, item.id)
    }

    private fun person(userId: String) = PasswordShareGranteeInput(granteeUserId = userId)

    private suspend fun grantGrace(world: World): PasswordItem {
        val recipients = world.ada.session.recipients(granteeUserId = "user-grace")
        return world.ada.session.setShares(world.itemId, listOf(person("user-grace")), recipients)
    }

    @Test fun `sharing seals the key for the recipient who opens it with their own key pair`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        val recipients = world.ada.session.recipients(granteeCircleId = "circle-one")
        assertEquals(listOf("user-grace"), recipients.map { recipient -> recipient.userId })
        val grace = recipients.first()
        assertEquals(8, grace.safetyCode?.split(" ")?.size)
        assertFalse(grace.hasKey)
        assertEquals(PasswordsKeyTrust.New, grace.keyTrust)

        val shared = world.ada.session.setShares(world.itemId, listOf(PasswordShareGranteeInput(granteeCircleId = "circle-one")), recipients)
        assertEquals(1, shared.shares.size)
        assertEquals(listOf(true), shared.shares.first().recipients.map { recipient -> recipient.hasKey })

        val result = world.grace.session.sync()
        assertEquals(listOf(world.itemId), result.changedItemIds)
        val opened = world.grace.session.item(world.itemId)
        assertTrue(opened.isSharedWithMe)
        assertEquals("Ada", opened.ownerName)
        assertEquals(loginFields(), opened.fields.withAccess(null))
        assertEquals(0, opened.shares.size)

        // A recipient cannot change it.
        val error = assertThrows<Throwable>("only the owner changes an item") {
            runBlocking { world.grace.session.save(loginFields(password = "graces change"), world.itemId) }
        }
        assertEquals(PasswordsError.OnlyOwnerCanChangeItem, error)
        // The owner's later edit reaches the recipient with the same item key.
        world.ada.session.save(loginFields(password = "new password"), world.itemId)
        world.grace.session.sync()
        val updated = world.grace.session.item(world.itemId)
        assertEquals(loginFields(password = "new password"), updated.fields.withAccess(null))
    }

    @Test fun `unsharing drops the item from the recipients copy`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        val recipients = world.ada.session.recipients(granteeUserId = "user-grace")
        world.ada.session.setShares(world.itemId, listOf(person("user-grace")), recipients)
        world.grace.session.sync()
        assertEquals(1, world.grace.session.list().items.size)

        world.ada.session.setShares(world.itemId, emptyList(), emptyList())
        val result = world.grace.session.sync()
        assertEquals(listOf(world.itemId), result.removedItemIds)
        assertEquals(0, world.grace.session.list().items.size)
    }

    @Test fun `a recipient who sets up later is sealed for at once when the owners device completes the waiting shares`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        val ada = makeDevice(server, "user-ada")
        ada.session.setUp("ada invented passphrase", ITERATION_COUNT)
        val item = ada.session.save(loginFields())

        // Grace has not set up Passwords: she has no key, so the share is made without one.
        val recipients = ada.session.recipients(granteeCircleId = "circle-one")
        assertNull(recipients.first().publicKey)
        assertNull(recipients.first().safetyCode)
        ada.session.setShares(item.id, listOf(PasswordShareGranteeInput(granteeCircleId = "circle-one")), recipients)
        assertEquals(0, ada.session.waitingShares().size)

        val grace = makeDevice(server, "user-grace")
        grace.session.setUp("grace invented passphrase", ITERATION_COUNT)
        val waiting = ada.session.waitingShares()
        assertEquals(listOf(item.id), waiting.map { share -> share.itemId })
        assertEquals("Example", waiting.first().itemName)
        assertEquals("user-grace", waiting.first().recipient.userId)
        assertEquals(PasswordsKeyTrust.New, waiting.first().recipient.keyTrust)

        // Ada's device has never seen Grace's key. By itself it seals for nobody it never sealed for; when Ada finishes
        // the sharing of the item it seals for the key the server gives, without asking.
        val byItself = ada.session.completeWaitingShares()
        assertEquals(0, byItself.addedKeyCount)
        assertEquals(setOf(item.id), ada.session.itemIdsNeedingYou())
        ada.session.sync(isFull = true)
        ada.session.finishSharing(item.id, ada.session.finishPlan(listOf(item.id)))
        assertNotNull(server.item(item.id)?.keys?.get("user-grace"))
        assertEquals(mapOf("user-grace" to server.publicKey("user-grace")), ada.cache.loadPins("user-ada").knownPublicKeys)
        assertEquals(0, ada.session.waitingShares().size)
        grace.session.sync()
        val opened = grace.session.item(item.id)
        assertEquals(loginFields(), opened.fields.withAccess(null))
    }

    // Known keys

    @Test fun `sharing with a first seen person seals at once and records their key as known`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        val recipients = world.ada.session.recipients(granteeUserId = "user-grace")
        assertEquals(PasswordsKeyTrust.New, recipients.first().keyTrust)
        assertEquals(false, recipients.first().isCompared)

        world.ada.session.setShares(world.itemId, listOf(person("user-grace")), recipients)
        assertNotNull(world.server.item(world.itemId)?.keys?.get("user-grace"))
        val graceKey = base64(recipients.first().publicKey!!)
        val pins = world.ada.cache.loadPins("user-ada")
        assertEquals(mapOf("user-grace" to graceKey), pins.knownPublicKeys)
        assertEquals(emptyMap<String, String>(), pins.changedPublicKeys)
        assertEquals(0, world.ada.session.keyChanges().size)
        val again = world.ada.session.recipients(granteeUserId = "user-grace")
        assertEquals(PasswordsKeyTrust.Known, again.first().keyTrust)

        // Grace opens it without anything having been confirmed.
        world.grace.session.sync()
        val opened = world.grace.session.item(world.itemId)
        assertEquals(loginFields(), opened.fields.withAccess(null))
    }

    @Test fun `a known recipient is sealed on another item and existing recipients are not sealed again`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        grantGrace(world)
        val firstKey = world.server.item(world.itemId)?.keys?.get("user-grace")?.itemKeyByRecipientKey

        // A second item: Grace's key is known now, and it is sealed for.
        val second = world.ada.session.save(loginFields(name = "Second"))
        val recipients = world.ada.session.recipients(granteeUserId = "user-grace")
        assertEquals(PasswordsKeyTrust.Known, recipients.first().keyTrust)
        world.ada.session.setShares(second.id, listOf(person("user-grace")), recipients)
        assertNotNull(world.server.item(second.id)?.keys?.get("user-grace"))

        // Sharing the first item again (a second grantee) does not seal Grace's key again.
        world.server.addPerson("user-linus", "Linus")
        world.ada.session.setShares(world.itemId, listOf(person("user-grace"), person("user-linus")), recipients)
        assertEquals(firstKey, world.server.item(world.itemId)?.keys?.get("user-grace")?.itemKeyByRecipientKey)
    }

    @Test fun `a changed recipient key is sealed for at once and leaves a notice that can be dismissed`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        grantGrace(world)
        val oldKey = world.server.publicKey("user-grace")
        val newKeyPair = PasswordsCrypto.makeKeyPair()
        val newKey = base64(newKeyPair.publicKey)
        world.server.replacePublicKey("user-grace", newKey)

        val recipients = world.ada.session.recipients(granteeUserId = "user-grace")
        val grace = recipients.first()
        assertEquals(PasswordsKeyTrust.Changed, grace.keyTrust)
        assertEquals(PasswordsOpener.safetyCodeText(oldKey), grace.knownSafetyCode)
        assertEquals(PasswordsCrypto.safetyCode(newKeyPair.publicKey), grace.safetyCode)
        assertNotEquals(grace.knownSafetyCode, grace.safetyCode)

        // Nothing is asked: on an item Grace has no key for yet, the new key is sealed for and recorded, with a notice.
        val second = world.ada.session.save(loginFields(name = "Second"))
        world.ada.session.setShares(second.id, listOf(person("user-grace")), recipients)
        assertEquals(newKey, world.server.item(second.id)?.keys?.get("user-grace")?.recipientPublicKey)
        val pins = world.ada.cache.loadPins("user-ada")
        assertEquals(newKey, pins.knownPublicKeys["user-grace"])
        assertEquals(oldKey, pins.changedPublicKeys["user-grace"])
        assertEquals(
            listOf(PasswordsKeyChange("user-grace", "Grace", PasswordsOpener.safetyCodeText(oldKey), PasswordsCrypto.safetyCode(newKeyPair.publicKey), newKey)),
            world.ada.session.keyChanges(),
        )

        // The first item, sealed for her old key, is waiting for the new one: completing seals it at once.
        val waiting = world.ada.session.waitingShares()
        assertEquals(listOf(world.itemId), waiting.map { share -> share.itemId })
        val completed = world.ada.session.completeWaitingShares()
        assertEquals(1, completed.addedKeyCount)
        assertEquals(newKey, world.server.item(world.itemId)?.keys?.get("user-grace")?.recipientPublicKey)

        // The notice stays until it is dismissed, and dismissing it does not undo the new key.
        assertEquals(1, world.ada.session.keyChanges().size)
        world.ada.session.dismissKeyChange("user-grace")
        assertEquals(emptyList<PasswordsKeyChange>(), world.ada.session.keyChanges())
        assertEquals(newKey, world.ada.cache.loadPins("user-ada").knownPublicKeys["user-grace"])
    }

    @Test fun `comparing codes marks the key compared and gates nothing`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        grantGrace(world)
        val graceKey = world.server.publicKey("user-grace")
        val before = world.ada.session.recipients(granteeUserId = "user-grace")
        assertEquals(false, before.first().isCompared)

        world.ada.session.markCompared("user-grace", graceKey)
        val after = world.ada.session.recipients(granteeUserId = "user-grace")
        assertEquals(PasswordsKeyTrust.Known, after.first().keyTrust)
        assertEquals(true, after.first().isCompared)

        // A new key is accepted as before, and it is no longer shown as compared.
        val newKey = base64(PasswordsCrypto.makeKeyPair().publicKey)
        world.server.replacePublicKey("user-grace", newKey)
        val changed = world.ada.session.recipients(granteeUserId = "user-grace")
        assertEquals(PasswordsKeyTrust.Changed, changed.first().keyTrust)
        assertEquals(false, changed.first().isCompared)
        val second = world.ada.session.save(loginFields(name = "Second"))
        world.ada.session.setShares(second.id, listOf(person("user-grace")), changed)
        assertEquals(newKey, world.server.item(second.id)?.keys?.get("user-grace")?.recipientPublicKey)
        assertEquals(listOf("user-grace"), world.ada.session.keyChanges().map { change -> change.userId })

        // Comparing the new codes clears the notice and shows "compared" again.
        world.ada.session.markCompared("user-grace", newKey)
        assertEquals(emptyList<PasswordsKeyChange>(), world.ada.session.keyChanges())
        val compared = world.ada.session.recipients(granteeUserId = "user-grace")
        assertEquals(true, compared.first().isCompared)
    }

    @Test fun `completing waiting shares seals every item of a recipient with no confirmation`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace")
        server.setCircle("circle-one", listOf("user-ada", "user-grace"))
        val ada = makeDevice(server, "user-ada")
        ada.session.setUp("ada invented passphrase", ITERATION_COUNT)
        val first = ada.session.save(loginFields(name = "First"))
        val second = ada.session.save(loginFields(name = "Second"))
        val grantees = listOf(PasswordShareGranteeInput(granteeCircleId = "circle-one"))
        ada.session.setShares(first.id, grantees, emptyList())
        ada.session.setShares(second.id, grantees, emptyList())
        val grace = makeDevice(server, "user-grace")
        grace.session.setUp("grace invented passphrase", ITERATION_COUNT)

        val byItself = ada.session.completeWaitingShares()
        assertEquals(0, byItself.addedKeyCount)
        assertEquals(setOf(first.id, second.id), ada.session.itemIdsNeedingYou())
        ada.session.sync(isFull = true)
        ada.session.finishSharing(first.id, ada.session.finishPlan(listOf(first.id)))
        ada.session.finishSharing(second.id, ada.session.finishPlan(listOf(second.id)))
        grace.session.sync()
        val opened = grace.session.list()
        assertEquals(listOf("First", "Second"), opened.items.map { item -> item.name })
    }

    @Test fun `waiting shares of known keys are sealed by complete waiting shares`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        grantGrace(world)
        // Another item is shared with Grace while her key is known but before the key is sealed for it: the share is made
        // without a key because Ada's call listed no recipients.
        val second = world.ada.session.save(loginFields(name = "Second"))
        world.ada.session.setShares(second.id, listOf(person("user-grace")), emptyList())
        // Ada named Grace for the item herself on this device, so the waiting share completes by itself.
        val completed = world.ada.session.completeWaitingShares()
        assertEquals(1, completed.addedKeyCount)
        assertEquals(emptySet<String>(), world.ada.session.itemIdsNeedingYou())
        world.grace.session.sync()
        val opened = world.grace.session.item(second.id)
        assertEquals(loginFields(name = "Second"), opened.fields.withAccess(null))
    }

    @Test fun `a recipient records the owners key on first sight with no notice`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        grantGrace(world)
        world.grace.session.sync()
        val adaKey = world.server.publicKey("user-ada")
        val pins = world.grace.cache.loadPins("user-grace")
        assertEquals(mapOf("user-ada" to adaKey), pins.knownPublicKeys)
        assertEquals(emptyMap<String, String>(), pins.changedPublicKeys)
        assertEquals(0, world.grace.session.keyChanges().size)
        val opened = world.grace.session.item(world.itemId)
        assertEquals(loginFields(), opened.fields.withAccess(null))
    }

    @Test fun `a changed owner key is accepted the item keeps opening and a notice stays until dismissed`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        grantGrace(world)
        world.grace.session.sync()
        val oldKey = world.server.publicKey("user-ada")

        // Ada's key changes, and her new key seals the item key again for Grace.
        val adaUnlocked = makeDevice(world.server, "user-ada").session.unlockWithPassphrase("ada invented passphrase")
        val storedItem = world.server.item(world.itemId)!!
        val itemKey = PasswordsCrypto.open(
            PasswordsSealedValue.fromJsonText(storedItem.itemKeyByAccountKey), adaUnlocked.accountKey, PasswordsCrypto.itemKeyAssociatedData(world.itemId),
        )
        val newOwner = PasswordsCrypto.makeKeyPair()
        val newOwnerKey = base64(newOwner.publicKey)
        val gracePublicKey = unbase64(world.server.publicKey("user-grace"))
        val resealed = PasswordsCrypto.sealForKeyPair(itemKey, newOwner, "user-ada", gracePublicKey, "user-grace", world.itemId)
        world.server.replacePublicKey("user-ada", newOwnerKey)
        world.server.replaceShareKey(world.itemId, "user-grace", resealed.jsonText)
        world.grace.session.sync()

        // The item opens with the new key, nothing waits on the person, and the notice carries both codes.
        val listed = world.grace.session.list()
        assertEquals(listOf(world.itemId), listed.items.map { item -> item.id })
        assertEquals(emptyList<String>(), listed.unreadableItemIds)
        val opened = world.grace.session.item(world.itemId)
        assertEquals(loginFields(), opened.fields.withAccess(null))
        assertEquals(
            listOf(PasswordsKeyChange("user-ada", "Ada", PasswordsOpener.safetyCodeText(oldKey), PasswordsCrypto.safetyCode(newOwner.publicKey), newOwnerKey)),
            world.grace.session.keyChanges(),
        )
        // The pin store holds the new key, which the autofill service reads.
        assertEquals(newOwnerKey, world.grace.cache.loadPins("user-grace").knownPublicKeys["user-ada"])

        // A later sync and a relaunch keep the notice; dismissing it removes only the notice.
        world.grace.session.sync()
        assertEquals(1, world.grace.session.keyChanges().size)
        world.grace.session.dismissKeyChange("user-ada")
        assertEquals(emptyList<PasswordsKeyChange>(), world.grace.session.keyChanges())
        val stillOpens = world.grace.session.item(world.itemId)
        assertEquals(loginFields(), stillOpens.fields.withAccess(null))
    }

    @Test fun `an item sealed by the server for a key it calls the owners does not open`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        grantGrace(world)
        // The server seals a made-up item key as if Ada had, with its own key pair standing in for the owner's.
        val forged = PasswordsCrypto.makeKeyPair()
        val gracePublicKey = unbase64(world.server.publicKey("user-grace"))
        val sealed = PasswordsCrypto.sealForKeyPair(PasswordsCrypto.makeKey(), forged, "user-ada", gracePublicKey, "user-grace", world.itemId)
        world.server.replaceShareKey(world.itemId, "user-grace", sealed.jsonText)
        world.grace.session.sync()
        val listed = world.grace.session.list()
        assertEquals(0, listed.items.size)
        assertEquals(listOf(world.itemId), listed.unreadableItemIds)
        // A key that opens nothing is not recorded as a change.
        assertEquals(0, world.grace.session.keyChanges().size)
    }

    @Test fun `the opener for the autofill service opens shared items whatever keys it knows`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        grantGrace(world)
        val unlocked = world.grace.session.unlockWithPassphrase("grace invented passphrase")
        world.grace.session.sync()
        val contents = world.grace.cache.load()!!
        val view = contents.item(world.itemId)!!

        val knowing = PasswordsOpener.fromPrivateKey(unlocked.accountKey, contents.userId, contents.privateKeyByAccountKey, contents.publicKey, world.grace.cache.loadPins("user-grace"))
        assertEquals(loginFields(), knowing.open(view).fields.withAccess(null))

        val unaware = PasswordsOpener.fromPrivateKey(unlocked.accountKey, contents.userId, contents.privateKeyByAccountKey, contents.publicKey, PasswordsPins(userId = "user-grace"))
        assertEquals(loginFields(), unaware.open(view).fields.withAccess(null))

        val mistakenPins = PasswordsPins(userId = "user-grace", knownPublicKeys = mapOf("user-ada" to base64(PasswordsCrypto.makeKeyPair().publicKey)))
        val mistaken = PasswordsOpener.fromPrivateKey(unlocked.accountKey, contents.userId, contents.privateKeyByAccountKey, contents.publicKey, mistakenPins)
        assertEquals(listOf(world.itemId), mistaken.openAll(listOf(view)).items.map { item -> item.id })
    }

    @Test fun `a key that changed while sharing comes back typed`() = runBlocking<Unit> {
        val world = makeSharingWorld()
        val recipients = world.ada.session.recipients(granteeUserId = "user-grace")
        // Grace's key changes after Ada's device read it and before it sealed for it.
        world.server.replacePublicKey("user-grace", base64(PasswordsCrypto.makeKeyPair().publicKey))
        val error = assertThrows<Throwable>("the key was stale") {
            runBlocking { world.ada.session.setShares(world.itemId, listOf(person("user-grace")), recipients) }
        }
        assertEquals(PasswordsError.RecipientKeyChangedSinceRead, error)
    }

    // A key sealed for the wrong person must fail

    /** The server hands Ben the key it holds for Grace (it stores a key for Ben's current public key, sealed for Grace): Ben's key pair opens nothing of it. */
    @Test fun `a key sealed for another person does not open for the person it is handed to`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-grace" to "Grace", "user-ben" to "Ben")
        val ada = makeDevice(server, "user-ada")
        val grace = makeDevice(server, "user-grace")
        val ben = makeDevice(server, "user-ben")
        ada.session.setUp("ada invented passphrase", ITERATION_COUNT)
        grace.session.setUp("grace invented passphrase", ITERATION_COUNT)
        ben.session.setUp("ben invented passphrase", ITERATION_COUNT)
        val item = ada.session.save(loginFields())
        ada.session.setShares(
            item.id, listOf(person("user-grace"), person("user-ben")),
            ada.session.recipients(granteeUserId = "user-grace") + ada.session.recipients(granteeUserId = "user-ben"),
        )
        grace.session.sync()
        assertEquals(loginFields(), grace.session.item(item.id).fields.withAccess(null))
        ben.session.sync()
        assertEquals(loginFields(), ben.session.item(item.id).fields.withAccess(null))

        // Now the server shows Ben the key sealed for Grace.
        val keyForGrace = server.item(item.id)!!.keys.getValue("user-grace").itemKeyByRecipientKey
        server.replaceShareKey(item.id, "user-ben", keyForGrace)
        ben.session.sync(isFull = true)
        val listed = ben.session.list()
        assertEquals(0, listed.items.size)
        assertEquals(listOf(item.id), listed.unreadableItemIds)
    }

    /** A key sealed for Ben is bound to Ben's id and the item: opened as somebody else, or for another item, it fails. */
    @Test fun `a key sealed for one recipient fails for another recipient id and another item`() = runBlocking<Unit> {
        val server = makeServer("user-ada" to "Ada", "user-ben" to "Ben")
        val ada = makeDevice(server, "user-ada")
        val ben = makeDevice(server, "user-ben")
        ada.session.setUp("ada invented passphrase", ITERATION_COUNT)
        val benUnlocked = run {
            ben.session.setUp("ben invented passphrase", ITERATION_COUNT)
            makeDevice(server, "user-ben").session.unlockWithPassphrase("ben invented passphrase")
        }
        val item = ada.session.save(loginFields())
        ada.session.setShares(item.id, listOf(person("user-ben")), ada.session.recipients(granteeUserId = "user-ben"))
        val sealed = PasswordsKeyPairSealedValue.fromJsonText(server.item(item.id)!!.keys.getValue("user-ben").itemKeyByRecipientKey)
        val ownerPublicKey = unbase64(server.publicKey("user-ada"))
        assertNotNull(PasswordsCrypto.openForKeyPair(sealed, benUnlocked.keyPair.privateKey, ownerPublicKey, "user-ada", "user-ben", item.id))
        assertThrows<Exception> { PasswordsCrypto.openForKeyPair(sealed, benUnlocked.keyPair.privateKey, ownerPublicKey, "user-ada", "user-grace", item.id) }
        assertThrows<Exception> { PasswordsCrypto.openForKeyPair(sealed, benUnlocked.keyPair.privateKey, ownerPublicKey, "user-ada", "user-ben", "another-item") }
        val strangerKeyPair = PasswordsCrypto.makeKeyPair()
        assertThrows<Exception> { PasswordsCrypto.openForKeyPair(sealed, strangerKeyPair.privateKey, ownerPublicKey, "user-ada", "user-ben", item.id) }
    }
}
