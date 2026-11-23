package com.teanode.app.passwords

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoResponse
import com.teanode.kit.passwords.FakePasswordsServer
import com.teanode.kit.passwords.InMemoryPasswordsStorage
import com.teanode.kit.passwords.PasswordsCache
import com.teanode.kit.passwords.PasswordShareGranteeInput
import com.teanode.kit.passwords.PasswordsCardFields
import com.teanode.kit.passwords.PasswordsGrantee
import com.teanode.kit.passwords.PasswordsItemAccess
import com.teanode.kit.passwords.PasswordsSealedValue
import com.teanode.kit.passwords.PasswordsCircleShare
import com.teanode.kit.passwords.PasswordsSealedChange
import com.teanode.kit.passwords.PasswordsCrypto
import com.teanode.kit.passwords.PasswordsItemFields
import com.teanode.kit.passwords.PasswordsLoginFields
import com.teanode.kit.passwords.PasswordsNoteFields
import com.teanode.kit.passwords.PasswordsPasskeyFields
import com.teanode.kit.passwords.PasswordsSession
import com.teanode.kit.passwords.answerGraphQL

import kotlinx.coroutines.runBlocking

/**
 * The invented Passwords of demo mode: a stand-in server (the kit's FakePasswordsServer) that holds only sealed text, set
 * up by an invented person's other device with a passphrase everyone can read here, and filled with a few items. The
 * phone's own session then unlocks it for real, with the real encryption, as it would a server.
 */
object PasswordsDemoStore {
    /** The passphrase of the invented person's Passwords. */
    const val PASSPHRASE = "a long invented passphrase"

    /** The user id the demo account has (see DemoServer). */
    private const val USER_ID = "demo-user"

    /** The cheapest key derivation the format allows, so a screenshot test does not wait on it. */
    val iterationCount = PasswordsCrypto.MINIMUM_PASSPHRASE_ITERATION_COUNT

    /** The id of the first login, whose screen has the one-time code and the passkey. */
    var mailLoginId = ""
        private set
    var agentLoginId = ""
        private set
    var cardId = ""
        private set
    var noteId = ""
        private set
    var deletedItemId = ""
        private set

    /** The login shared with the Home circle and with somebody who set up Passwords afterwards, so somebody is waiting for it (a Sharing start). */
    var sharedLoginId = ""
        private set

    /** The login shared with Sam alone, who has its key (a Sharing start). */
    var wikiLoginId = ""
        private set

    /** A login whose sharing record nobody signed, listed as shared by the server (a Sharing start): its screens say sharing could not be checked. */
    var uncheckedLoginId = ""
        private set

    /** The login Sam shared with the invented person (a Sharing start). */
    var receivedLoginId = ""
        private set

    /** The people and the circle of the Sharing start: the same ids the other demo areas give. */
    const val SAM_ID = "demo-user-2"
    const val ALEX_ID = "demo-user-3"
    const val CIRCLE_ID = "demo-circle-1"

    @Volatile
    private var server = FakePasswordsServer(USER_ID)

    /** Whether the invented person has set Passwords up yet; a fresh demo is not set up until a screen asks for it. */
    @Volatile
    private var isSeeded = false

    /** Starts the invented Passwords over: set up and filled (`isSetUp`), or still to be set up. */
    @Synchronized
    fun reset(isSetUp: Boolean, isSharing: Boolean = false) {
        server = FakePasswordsServer(USER_ID).also { fresh -> fresh.addPerson(USER_ID, "Alex Example", "alex.example") }
        isSeeded = false
        if (isSetUp) seed(isSharing)
    }

    /** What the phone remembers when it starts in Sharing: Sam's key as it was before it changed (opening what Sam shared makes the notice), and Sam as somebody this phone sealed the shared login for through the circle. */
    internal fun seedPins(cache: PasswordsCache) {
        cache.recordKnownIfAbsent(USER_ID, SAM_ID, java.util.Base64.getEncoder().encodeToString(PasswordsCrypto.makeKeyPair().publicKey))
        cache.recordSealed(USER_ID, sharedLoginId, PasswordsSealedChange(listOf(SAM_ID), isReplacing = false, circleShares = listOf(PasswordsCircleShare(SAM_ID, "Sam Lee", "Home"))))
    }

    private fun seed(isSharing: Boolean) = runBlocking {
        // The phone that set it up, which keeps nothing: only what it sealed is on the server.
        val device = PasswordsSession(server, PasswordsCache(InMemoryPasswordsStorage()), USER_ID)
        device.setUp(PASSPHRASE, iterationCount)
        val mail = device.save(
            PasswordsItemFields.Login(
                PasswordsLoginFields(
                    name = "Example Mail", userName = "alex@example.com", password = "correct horse battery staple", sites = listOf("https://mail.example.com", "mail.example.org"),
                    oneTimeCodeUri = "otpauth://totp/Example%20Mail:alex@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example%20Mail", notes = "The recovery address is on the fridge.",
                    passkeys = listOf(PasswordsPasskeyFields(credentialId = "AQID", relyingPartyId = "mail.example.com", userHandle = "BAUG", userName = "alex", userDisplayName = "Alex", privateKey = "demo", createdAt = "2026-09-12T09:30:00Z")),
                    folder = "Personal",
                ),
            ),
        )
        mailLoginId = mail.id
        // An earlier version, so the versions screens have something to show.
        device.save(
            PasswordsItemFields.Login(
                PasswordsLoginFields(
                    name = "Example Mail", userName = "alex@example.com", password = "correct horse battery staple", sites = listOf("https://mail.example.com", "mail.example.org"),
                    oneTimeCodeUri = "otpauth://totp/Example%20Mail:alex@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example%20Mail", notes = "The recovery address is on the fridge.",
                    passkeys = (mail.fields as PasswordsItemFields.Login).fields.passkeys, folder = "Personal",
                ),
            ),
            id = mail.id,
        )
        val shop = device.save(PasswordsItemFields.Login(PasswordsLoginFields(name = "Tea Shop", userName = "alex", password = "oolong-and-sencha-2026", sites = listOf("https://shop.example.com"), folder = "Personal")))
        agentLoginId = shop.id
        device.setUsableByAgent(shop.id, true)
        val wiki = device.save(PasswordsItemFields.Login(PasswordsLoginFields(name = "Team Wiki", userName = "alex.example", password = "an-invented-wiki-password", sites = listOf("https://wiki.example.net"), folder = "Work")))
        cardId = device.save(
            PasswordsItemFields.Card(
                PasswordsCardFields(name = "Everyday card", cardholderName = "Alex Example", number = "4111111111114242", expiryMonth = "9", expiryYear = "2029", securityCode = "123", brand = "Visa"),
            ),
        ).id
        noteId = device.save(PasswordsItemFields.Note(PasswordsNoteFields(name = "Router", text = "The admin page is at 192.168.0.1.\nThe password is on the label under the router."))).id
        val gone = device.save(PasswordsItemFields.Login(PasswordsLoginFields(name = "Old Forum", userName = "alex", password = "retired", sites = listOf("https://forum.example.org"))))
        deletedItemId = gone.id
        device.delete(gone.id)
        if (isSharing) seedSharing(device, shop.id, wiki.id)
        isSeeded = true
    }

    /**
     * Sharing: Sam (who has set up Passwords) and somebody who has not yet, the Home circle, the Tea Shop login shared with
     * the circle and with the second person, the wiki login shared with Sam alone, and a login Sam shared with the
     * invented person. The second person then sets up Passwords, so somebody is waiting for a key.
     */
    private suspend fun seedSharing(device: PasswordsSession, shopId: String, wikiId: String) {
        server.addPerson(SAM_ID, "Sam Lee", "sam")
        server.addPerson(ALEX_ID, "", "alex")
        server.setCircle(CIRCLE_ID, listOf(USER_ID, SAM_ID), "Home")
        val sam = PasswordsSession(server.api(SAM_ID), PasswordsCache(InMemoryPasswordsStorage()), SAM_ID)
        sam.setUp(PASSPHRASE, iterationCount)
        val shopRecipients = device.recipients(granteeCircleId = CIRCLE_ID) + device.recipients(granteeUserId = ALEX_ID)
        device.setShares(shopId, listOf(PasswordShareGranteeInput(granteeCircleId = CIRCLE_ID), PasswordShareGranteeInput(granteeUserId = ALEX_ID)), shopRecipients)
        device.setShares(wikiId, listOf(PasswordShareGranteeInput(granteeUserId = SAM_ID)), device.recipients(granteeUserId = SAM_ID))
        sharedLoginId = shopId
        wikiLoginId = wikiId
        val wifi = sam.save(PasswordsItemFields.Login(PasswordsLoginFields(name = "Family Wi-Fi", userName = "home-network", password = "an-invented-wifi-phrase", sites = emptyList(), folder = "Home")))
        sam.setShares(wifi.id, listOf(PasswordShareGranteeInput(granteeUserId = USER_ID)), sam.recipients(granteeUserId = USER_ID))
        receivedLoginId = wifi.id
        PasswordsSession(server.api(ALEX_ID), PasswordsCache(InMemoryPasswordsStorage()), ALEX_ID).setUp(PASSPHRASE, iterationCount)

        // A login whose record somebody who held its key wrote without the person's signature, and which the server lists as shared with Sam.
        val guest = device.save(PasswordsItemFields.Login(PasswordsLoginFields(name = "Guest portal", userName = "guest", password = "an-invented-guest-phrase", sites = listOf("https://guest.example.org"), folder = "Home")))
        val accountKey = PasswordsSession(server.api(USER_ID), PasswordsCache(InMemoryPasswordsStorage()), USER_ID).unlockWithPassphrase(PASSPHRASE).accountKey
        val stored = server.item(guest.id)!!
        val itemKey = PasswordsCrypto.open(PasswordsSealedValue.fromJsonText(stored.itemKeyByAccountKey), accountKey, PasswordsCrypto.itemKeyAssociatedData(guest.id))
        val forgedFields = guest.fields.withAccess(PasswordsItemAccess(isUsableByAgent = false, grantees = listOf(PasswordsGrantee.Person(SAM_ID))))
        val forged = PasswordsCrypto.sealItemFields(forgedFields, itemKey, guest.id)
        server.replaceStoredFields(guest.id, forged.jsonText, stored.itemKeyByAccountKey)
        server.api(USER_ID).setPasswordItemShares(guest.id, listOf(PasswordShareGranteeInput(granteeUserId = SAM_ID)), emptyList())
        uncheckedLoginId = guest.id
    }

    /** Answers a Passwords document from the invented server. */
    internal fun answer(document: String, variables: kotlinx.serialization.json.JsonObject): String? = runBlocking { server.answerGraphQL(document, variables) }
}

/** What demo mode answers for Passwords: every document goes to the invented server above. */
val passwordsDemoAnswerers: List<DemoAnswerer> = listOf(
    DemoAnswerer { request ->
        if (request.document.isEmpty()) null else PasswordsDemoStore.answer(request.document, request.variables)?.let { answer -> DemoResponse(answer) }
    },
)
