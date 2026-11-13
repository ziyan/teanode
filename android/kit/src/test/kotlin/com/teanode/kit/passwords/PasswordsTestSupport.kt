package com.teanode.kit.passwords

import org.junit.jupiter.api.Assertions.assertNotNull

/** HookedServerFlags lets a test see the calls a session makes and lets the "server" lie. */
internal class HookedServerFlags {
    /** The names of the mutating calls, in order. */
    val calls = mutableListOf<String>()
    var lastRekeyInput: PasswordItemRekeyInput? = null

    /** The keys of the last `SetPasswordItemShares` call. */
    var lastShareKeys: List<PasswordShareKeyInput> = emptyList()
    val addedShareKeyCalls = mutableListOf<List<PasswordItemShareKeyInput>>()

    /** Item ids whose owner view claims the agent may use it and that it needs a new key. */
    var falselyAgentAndMarkedItemIds: Set<String> = emptySet()

    /** Waiting rows the server invents. */
    var forgedWaiting: List<PasswordWaitingShareView> = emptyList()

    /** Shares the server invents on an owner's item, by item id. */
    var forgedShares: Map<String, List<PasswordShareView>> = emptyMap()

    /** The answer to a change of shares is the item as it was before the change. */
    var isAnsweringSharesUnchanged = false

    /** Replaces the sealed fields of a shared item the server lists, by item id. */
    var damagedFieldsByItemId: Map<String, String> = emptyMap()
}

/** HookedApi forwards to the fake server and records or bends what the flags say. */
internal class HookedApi(private val inner: PasswordsApi, private val flags: HookedServerFlags) : PasswordsApi by inner {
    private fun bent(view: PasswordItemView): PasswordItemView {
        val isFalselyMarked = flags.falselyAgentAndMarkedItemIds.contains(view.id)
        return view.copy(
            encryptedFields = flags.damagedFieldsByItemId[view.id] ?: view.encryptedFields,
            isUsableByAgent = view.isUsableByAgent || isFalselyMarked,
            shares = view.shares + (flags.forgedShares[view.id] ?: emptyList()),
            needsNewKey = view.needsNewKey || isFalselyMarked,
        )
    }

    override suspend fun listPasswordItems(sinceRevision: Long?): PasswordItemListView {
        val real = inner.listPasswordItems(sinceRevision)
        return real.copy(items = real.items.map { item -> bent(item) })
    }

    override suspend fun listWaitingPasswordShares(): List<PasswordWaitingShareView> = inner.listWaitingPasswordShares() + flags.forgedWaiting

    override suspend fun savePasswordItem(input: PasswordItemSaveInput): PasswordItemView {
        flags.calls.add("savePasswordItem")
        return bent(inner.savePasswordItem(input))
    }

    override suspend fun deletePasswordItem(id: String): PasswordItemView {
        flags.calls.add("deletePasswordItem")
        return bent(inner.deletePasswordItem(id))
    }

    override suspend fun restorePasswordItem(id: String): PasswordItemView {
        flags.calls.add("restorePasswordItem")
        return bent(inner.restorePasswordItem(id))
    }

    override suspend fun purgePasswordItem(id: String): Boolean {
        flags.calls.add("purgePasswordItem")
        return inner.purgePasswordItem(id)
    }

    override suspend fun setPasswordItemShares(itemId: String, grantees: List<PasswordShareGranteeInput>, keys: List<PasswordShareKeyInput>): PasswordItemView {
        flags.calls.add("setPasswordItemShares")
        flags.lastShareKeys = keys
        val before = if (flags.isAnsweringSharesUnchanged) inner.listPasswordItems(null).items.firstOrNull { item -> item.id == itemId } else null
        val real = inner.setPasswordItemShares(itemId, grantees, keys)
        return bent(before ?: real)
    }

    override suspend fun addPasswordShareKeys(keys: List<PasswordItemShareKeyInput>): Int {
        flags.calls.add("addPasswordShareKeys")
        flags.addedShareKeyCalls.add(keys)
        return inner.addPasswordShareKeys(keys)
    }

    override suspend fun rekeyPasswordItem(input: PasswordItemRekeyInput): PasswordItemView {
        flags.calls.add("rekeyPasswordItem")
        flags.lastRekeyInput = input
        return bent(inner.rekeyPasswordItem(input))
    }

    override suspend fun setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Boolean, itemKeyByAgentKey: String?): PasswordItemView {
        flags.calls.add("setPasswordItemUsableByAgent")
        return bent(inner.setPasswordItemUsableByAgent(itemId, isUsableByAgent, itemKeyByAgentKey))
    }

    override suspend fun restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Long): PasswordItemView {
        flags.calls.add("restorePasswordItemVersion")
        return bent(inner.restorePasswordItemVersion(itemId, versionId, baseRevision))
    }
}

/** What the sharing tests set up the same way: people on one server, a session each, a cache each. */
internal object SharingFixture {
    /** The lowest count the format accepts, so the tests do not spend minutes in key derivation. */
    const val ITERATION_COUNT = PasswordsCrypto.MINIMUM_PASSPHRASE_ITERATION_COUNT

    fun makeCache(): PasswordsCache = PasswordsCache(InMemoryPasswordsStorage())

    fun makeSession(
        server: FakePasswordsServer,
        userId: String,
        flags: HookedServerFlags? = null,
        cache: PasswordsCache = makeCache(),
        clock: () -> Long = System::currentTimeMillis,
    ): PasswordsSession {
        val api = server.api(userId)
        return PasswordsSession(if (flags != null) HookedApi(api, flags) else api, cache, userId, clock)
    }

    fun makeServer(vararg people: Pair<String, String>): FakePasswordsServer {
        val server = FakePasswordsServer()
        for ((userId, name) in people) server.addPerson(userId, name)
        return server
    }

    fun loginFields(name: String = "Example", password: String = "correct horse"): PasswordsItemFields =
        PasswordsItemFields.Login(PasswordsLoginFields(name = name, userName = "ada@example.com", password = password, sites = listOf("https://example.com")))

    suspend fun setUp(session: PasswordsSession, passphrase: String) {
        session.setUp(passphrase, ITERATION_COUNT)
    }

    /** The access record the owner sealed in the item, read the way the server would have to: with the owner's account key. */
    fun sealedAccess(itemId: String, server: FakePasswordsServer, accountKey: ByteArray): PasswordsItemAccess? {
        val stored = server.item(itemId)
        assertNotNull(stored)
        val itemKey = PasswordsCrypto.open(
            PasswordsSealedValue.fromJsonText(stored!!.itemKeyByAccountKey), accountKey, PasswordsCrypto.itemKeyAssociatedData(itemId),
        )
        val kind = PasswordsItemKind.fromRawValue(stored.itemKind)!!
        return PasswordsCrypto.openItemFields(PasswordsSealedValue.fromJsonText(stored.encryptedFields), itemKey, itemId, kind).access
    }

    /** The recipient a view carries, as the owner's phone would list it. */
    fun recipientView(recipient: PasswordRecipient, hasKey: Boolean = false): PasswordRecipientView =
        PasswordRecipientView(recipient.userId, recipient.name, recipient.username, recipient.publicKey?.let { key -> base64(key) }, hasKey)
}

/** A storage with a cache of its own and pins shared with another: two phones' caches that read the same keychain. */
internal class PinSharingStorage(private val shared: PasswordsStorage) : PasswordsStorage {
    private val own = InMemoryPasswordsStorage()

    override fun readCache(): String? = own.readCache()

    override fun writeCache(text: String) = own.writeCache(text)

    override fun deleteCache() = own.deleteCache()

    override fun readPins(): String? = shared.readPins()

    override fun writePins(text: String) = shared.writePins(text)

    override fun deletePins() = shared.deletePins()
}
