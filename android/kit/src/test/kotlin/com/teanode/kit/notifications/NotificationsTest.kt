package com.teanode.kit.notifications

import com.teanode.kit.Account
import com.teanode.kit.AccountRing
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.MemorySecrets
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.Base64

class NotificationsTest {
    private fun bodyOf(fake: FakeServer): JsonObject = TeaNodeJson.parseToJsonElement(fake.sent.last().body!!.decodeToString()) as JsonObject

    @Test
    fun thePhoneRegistersWithTheRelay() = runBlocking {
        val fake = FakeServer { 200 to """{"pushRegistration":"a-sealed-registration"}""" }
        val sealed = PushRelay(fake).register("a-firebase-token")
        assertEquals("a-sealed-registration", sealed)
        assertEquals("https://mail.teanode.com/api/v1/push/registrations", fake.sent[0].url)
        val body = bodyOf(fake)
        // The token is Firebase's own text, not the hex of an Apple token.
        assertEquals(JsonPrimitive("a-firebase-token"), body["deviceToken"])
        assertEquals(JsonPrimitive("android"), body["platform"])
        assertEquals(JsonPrimitive("alert"), body["pushType"])
        assertEquals(JsonPrimitive(false), body["isSandbox"])
    }

    @Test
    fun theSameTokenRegistersAgainForRings() = runBlocking {
        val fake = FakeServer { 200 to """{"pushRegistration":"a-ring-registration"}""" }
        PushRelay(fake).register("a-firebase-token", pushType = "voip")
        assertEquals(JsonPrimitive("voip"), bodyOf(fake)["pushType"])
        assertEquals(JsonPrimitive("android"), bodyOf(fake)["platform"])
    }

    @Test
    fun aRefusalFromTheRelayIsSaidInItsWords() {
        val fake = FakeServer { 400 to """{"errorCode":"refused","errorMessage":"This relay has no key for Android."}""" }
        val failure = assertThrows(TeaNodeError.Refused::class.java) { runBlocking { PushRelay(fake).register("token") } }
        assertEquals("This relay has no key for Android.", failure.reason)
        val unreadable = FakeServer { 500 to "oops" }
        val other = assertThrows(TeaNodeError.Refused::class.java) { runBlocking { PushRelay(unreadable).register("token") } }
        assertEquals("The notification relay refused this device.", other.reason)
    }

    /** Sealed by the server's own code (push.EncryptContent) with the key 0, 1, 2 ... 31, so the two sides are checked against each other. */
    private val sealedByTheServer =
        "H8RRRtjjpug7CHBstkS2ic1gPC+8v2ETffkv/MTXRhaeqmtyQJlSIczFvR481L+D1BqhO2Yrfj7bLeBSFAqJ6salcR93DzAV0Hleqk+fuO6pBo99yOEC9Th0d3b6jj6DbnbFCqL2MZpVKCm4va3GD+K4WMEw53BqMqCutMuPKvhXNemi9I/xNGgsTk0DwtlofvJaDfsKruPrdHYcbh6GhJnlMoyNoFN89Jyh/44oVcTaSTfMrAwh6hSWET6IAKGuwhUaJl7bpmD3aJJMWjDthHDMClCZB9rbMhezpEl62YAnPDx1iknIqW/yjXFVsqXMv6bv8yMD2jIawXoa2PlPu3qpZXZY6GxXyvgRGazoTJREK2c9gW0="
    private val theServersKey: String = Base64.getEncoder().encodeToString(ByteArray(32) { index -> index.toByte() })

    @Test
    fun whatTheServerSealsThePhoneOpens() {
        val content = PayloadKey.open(sealedByTheServer, theServersKey)
        assertEquals("question", content.notificationKind)
        assertEquals("Which day suits you?", content.body)
        assertEquals(NotificationButton("Friday", "answer:1"), content.buttons?.first())
        assertThrows(Exception::class.java) { PayloadKey.open(sealedByTheServer, PayloadKey.make()) }
    }

    @Test
    fun aKeyThatIsNotAKeyIsRefused() {
        assertThrows(TeaNodeError.UnreadableAnswer::class.java) { PayloadKey.open(sealedByTheServer, "not base64!") }
        assertThrows(TeaNodeError.UnreadableAnswer::class.java) { PayloadKey.open(sealedByTheServer, Base64.getEncoder().encodeToString(ByteArray(16))) }
        assertThrows(TeaNodeError.UnreadableAnswer::class.java) { PayloadKey.open("not base64!", theServersKey) }
        assertThrows(TeaNodeError.UnreadableAnswer::class.java) { PayloadKey.open("AAAA", theServersKey) }
    }

    @Test
    fun whatThePhoneSealsItOpensAgain() {
        val key = PayloadKey.make()
        assertEquals(32, Base64.getDecoder().decode(key).size)
        val content = NotificationContent(notificationKind = "chat", conversationId = "c1", title = "Sam", body = "Hello", threadId = "chat:c1")
        val sealed = PayloadKey.seal(content, key)
        assertEquals(content, PayloadKey.open(sealed, key))
        // Each sealing has a nonce of its own.
        assertNotEquals(sealed, PayloadKey.seal(content, key))
    }

    private val server = ServerAddress.parse("mail.example.com")

    private fun account(username: String) = Account(server, "a-client", "a", "r", Long.MAX_VALUE / 4, username)

    @Test
    fun theKeyThatOpensANotificationNamesItsAccount() {
        val keys = listOf(
            AccountPayloadKey(account("robin").accountKey, PayloadKey.make()),
            AccountPayloadKey(account("sam").accountKey, theServersKey),
        )
        val opened = PayloadKey.open(sealedByTheServer, keys)
        assertEquals(account("sam").accountKey, opened?.accountKey)
        assertEquals("question", opened?.content?.notificationKind)
        assertNull(PayloadKey.open(sealedByTheServer, keys.take(1)))
    }

    @Test
    fun theSingleKeyStaysWithTheAccountInUse() {
        val secrets = MemorySecrets()
        secrets.write("payloadKey", "the-single-key".toByteArray())
        val keyring = PayloadKeyring(secrets)
        val ring = AccountRing(listOf(account("robin"), account("sam")), account("sam").accountKey)

        assertEquals(listOf(AccountPayloadKey(account("sam").accountKey, "the-single-key")), keyring.keys(ring))
        assertNull(secrets.read("payloadKey"), "the single key was kept on its own after it was moved")

        // The other account gets a key of its own, and the moved one stays.
        assertEquals("a-new-key", keyring.current(account("robin").accountKey) { "a-new-key" })
        assertEquals("the-single-key", keyring.current(account("sam").accountKey) { "never-made" })
        assertEquals(
            listOf(AccountPayloadKey(account("robin").accountKey, "a-new-key"), AccountPayloadKey(account("sam").accountKey, "the-single-key")),
            keyring.keys(ring),
        )

        keyring.forget(account("sam").accountKey)
        assertNull(keyring.stored(account("sam").accountKey))
        assertEquals(listOf(account("robin").accountKey), keyring.keys(ring).map { key -> key.accountKey })
    }

    @Test
    fun theSingleKeyNeverReplacesAnAccountsOwn() {
        val secrets = MemorySecrets()
        val keyring = PayloadKeyring(secrets)
        keyring.current(account("robin").accountKey) { "robins-key" }
        secrets.write("payloadKey", "the-single-key".toByteArray())
        keyring.moveSingleKey(account("robin").accountKey)
        assertEquals("robins-key", keyring.stored(account("robin").accountKey))
        assertNull(secrets.read("payloadKey"))
    }

    @Test
    fun theSingleKeyWaitsWhileNoAccountIsKept() {
        val secrets = MemorySecrets()
        secrets.write("payloadKey", "the-single-key".toByteArray())
        assertEquals(emptyList<AccountPayloadKey>(), PayloadKeyring(secrets).keys(AccountRing()))
        assertNotNull(secrets.read("payloadKey"))
    }

    // The same notification contents the iPhone app's other tests read: each part of the app names what its notification opens.

    @Test
    fun aCodingQuestionNotificationNamesItsSession() {
        val json = """{"notificationKind":"herdr_question","computer":"laptop","paneId":"p3","questionFingerprint":"fingerprint-1","title":"notes on laptop asks","body":"Run the tests?","buttons":[{"label":"Open","buttonAction":"open"}],"threadId":"herdr_question:laptop:p3"}"""
        val content = TeaNodeJson.decodeFromString(NotificationContent.serializer(), json)
        assertEquals("herdr_question", content.notificationKind)
        assertEquals("laptop", content.computer)
        assertEquals("p3", content.paneId)
        assertEquals("fingerprint-1", content.questionFingerprint)
        // Any other notification decodes as before, with no session.
        val other = TeaNodeJson.decodeFromString(NotificationContent.serializer(), """{"notificationKind":"message","title":"Tea","body":"Hello"}""")
        assertNull(other.paneId)
    }

    @Test
    fun theEveningQuestionCarriesItsDay() {
        val json = """{"notificationKind":"journalEveningQuestion","title":"How was today?","body":"One tap is enough.","journalDay":"2026-10-26"}"""
        val content = TeaNodeJson.decodeFromString(NotificationContent.serializer(), json)
        assertEquals("journalEveningQuestion", content.notificationKind)
        assertEquals("2026-10-26", content.journalDay)
        assertNull(content.conversationId)
        // And a notification of any other kind has no day.
        val other = """{"notificationKind":"message","title":"Hello","body":"","conversationId":"c1"}"""
        assertNull(TeaNodeJson.decodeFromString(NotificationContent.serializer(), other).journalDay)
        // It survives being written and read again.
        val again = TeaNodeJson.decodeFromString(NotificationContent.serializer(), TeaNodeJson.encodeToString(NotificationContent.serializer(), content))
        assertEquals(content, again)
    }

    @Test
    fun aShoppingNotificationCarriesTheStoreOrTheList() {
        val arrival = """{"notificationKind":"shopping","shoppingStoreName":"Example Market","title":"At Example Market","body":"4 things on your lists","threadId":"shopping"}"""
        val content = TeaNodeJson.decodeFromString(NotificationContent.serializer(), arrival)
        assertEquals("shopping", content.notificationKind)
        assertEquals("Example Market", content.shoppingStoreName)
        assertNull(content.shoppingListId)
        val started = """{"notificationKind":"shopping","shoppingListId":"list-1","title":"Sam started shopping Groceries","body":"","threadId":"shopping"}"""
        assertEquals("list-1", TeaNodeJson.decodeFromString(NotificationContent.serializer(), started).shoppingListId)
        assertTrue(AgentPhone.allNotificationKinds.contains("shopping"))
    }

    @Test
    fun aLocationNotificationCarriesTheUserWhoMoved() {
        val json = """{"notificationKind":"location","locationUserId":"user-2","title":"Person B arrived home","body":"","threadId":"location-user-2"}"""
        val content = TeaNodeJson.decodeFromString(NotificationContent.serializer(), json)
        assertEquals("user-2", content.locationUserId)
        assertTrue(AgentPhone.allNotificationKinds.contains("location"))
    }

    // MARK: Registering the phone

    private fun connectionAnswering(body: String): Pair<Connection, FakeServer> {
        val fake = FakeServer { 200 to body }
        return Connection(Account(server, "a-client", "tnt_a", "", System.currentTimeMillis() + 3_600_000, "mira"), fake) to fake
    }

    private fun variablesOf(fake: FakeServer): JsonObject = bodyOf(fake)["variables"] as JsonObject

    @Test
    fun thePhoneIsRegisteredAsAnAndroidPhoneWithBothRegistrations() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"RegisterAgentPhone":{"id":"phone-1","notificationKinds":[],"canBeNotified":true}}}""")
        val phone = connection.registerPhone("Mira's phone", "Pixel 9", pushRegistration = "alert-registration", voipPushRegistration = "voip-registration", payloadKey = "a-key")
        assertEquals("phone-1", phone.id)
        val variables = variablesOf(fake)
        assertEquals(JsonPrimitive("android"), variables["phonePlatform"])
        assertEquals(JsonPrimitive("alert-registration"), variables["pushRegistration"])
        assertEquals(JsonPrimitive("voip-registration"), variables["voipPushRegistration"])
        assertEquals(JsonPrimitive("a-key"), variables["payloadKey"])
        val document = (bodyOf(fake)["query"] as JsonPrimitive).content
        assertTrue(document.contains("voipPushRegistration: \$voipPushRegistration"))
        assertTrue(document.contains("phonePlatform: \$phonePlatform"))
    }

    @Test
    fun aRegistrationWithoutRingingSendsNone() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"RegisterAgentPhone":{"id":"phone-1","notificationKinds":[],"canBeNotified":true}}}""")
        connection.registerPhone("Mira's phone", "Pixel 9")
        assertNull(variablesOf(fake)["voipPushRegistration"])
        assertNull(variablesOf(fake)["pushRegistration"])
    }

    @Test
    fun theKindsAPhoneIsToldAreChosen() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"UpdateAgentPhone":{"id":"phone-1","notificationKinds":["chat"],"canBeNotified":true}}}""")
        val phone = connection.updatePhone("phone-1", listOf("chat"))
        assertEquals(listOf("chat"), phone.notificationKinds)
        assertEquals(JsonPrimitive("phone-1"), variablesOf(fake)["phoneId"])
    }
}

class AppUpdateTest {
    private fun update(version: String, build: String) =
        AppUpdate("android", version, build, "https://appdistribution.firebase.dev/i/example")

    @Test
    fun aNewerBuildOfTheSameVersionIsNewer() {
        assertTrue(update("1.2.0", "101").isNewer("1.2.0", "100"))
        assertFalse(update("1.2.0", "100").isNewer("1.2.0", "100"))
        assertFalse(update("1.2.0", "99").isNewer("1.2.0", "100"))
    }

    @Test
    fun theVersionComesBeforeTheBuild() {
        assertTrue(update("1.10.0", "5").isNewer("1.9.0", "100"))
        assertFalse(update("1.9.0", "500").isNewer("1.10.0", "100"))
        assertFalse(update("1.2", "100").isNewer("1.2.0", "100"))
    }

    @Test
    fun aVersionAloneIsComparedByVersionAlone() {
        val noBuild = AppUpdate("android", "1.2.0", "", "https://play.google.com/store/apps/details?id=example")
        assertTrue(noBuild.isNewer("1.1.0", "900"))
        assertFalse(noBuild.isNewer("1.2.0", "1"))
        assertEquals("1.2.0", noBuild.versionText)
    }

    @Test
    fun aDismissalIsOfThatUpdateAlone() {
        assertEquals("1.2.0 (101)", update("1.2.0", "101").versionText)
        assertNotEquals(update("1.2.0", "101").dismissalKey, update("1.2.0", "102").dismissalKey)
    }

    @Test
    fun theServersAnswerIsReadForTheAndroidChannel() = runBlocking {
        val fake = FakeServer { 200 to """{"data":{"GetAppUpdate":{"updateChannel":"android","version":"1.4.0","buildNumber":"42","installUrl":"https://appdistribution.firebase.dev/i/example"}}}""" }
        val connection = Connection(Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", System.currentTimeMillis() + 3_600_000, "mira"), fake)
        val found = connection.appUpdate()
        assertEquals("1.4.0 (42)", found?.versionText)
        val variables = (TeaNodeJson.parseToJsonElement(fake.sent[0].body!!.decodeToString()) as JsonObject)["variables"] as JsonObject
        assertEquals(JsonPrimitive("android"), variables["updateChannel"])
        val none = Connection(Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", System.currentTimeMillis() + 3_600_000, "mira"), FakeServer { 200 to """{"data":{"GetAppUpdate":null}}""" })
        assertNull(none.appUpdate())
    }
}
