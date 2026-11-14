package com.teanode.app.passwords

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoResponse
import com.teanode.kit.passwords.FakePasswordsServer
import com.teanode.kit.passwords.InMemoryPasswordsStorage
import com.teanode.kit.passwords.PasswordsCache
import com.teanode.kit.passwords.PasswordsCardFields
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

    @Volatile
    private var server = FakePasswordsServer(USER_ID)

    /** Whether the invented person has set Passwords up yet; a fresh demo is not set up until a screen asks for it. */
    @Volatile
    private var isSeeded = false

    /** Starts the invented Passwords over: set up and filled (`isSetUp`), or still to be set up. */
    @Synchronized
    fun reset(isSetUp: Boolean) {
        server = FakePasswordsServer(USER_ID)
        isSeeded = false
        if (isSetUp) seed()
    }

    private fun seed() = runBlocking {
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
        device.save(PasswordsItemFields.Login(PasswordsLoginFields(name = "Team Wiki", userName = "alex.example", password = "an-invented-wiki-password", sites = listOf("https://wiki.example.net"), folder = "Work")))
        cardId = device.save(
            PasswordsItemFields.Card(
                PasswordsCardFields(name = "Everyday card", cardholderName = "Alex Example", number = "4111111111114242", expiryMonth = "9", expiryYear = "2029", securityCode = "123", brand = "Visa"),
            ),
        ).id
        noteId = device.save(PasswordsItemFields.Note(PasswordsNoteFields(name = "Router", text = "The admin page is at 192.168.0.1.\nThe password is on the label under the router."))).id
        val gone = device.save(PasswordsItemFields.Login(PasswordsLoginFields(name = "Old Forum", userName = "alex", password = "retired", sites = listOf("https://forum.example.org"))))
        deletedItemId = gone.id
        device.delete(gone.id)
        isSeeded = true
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
