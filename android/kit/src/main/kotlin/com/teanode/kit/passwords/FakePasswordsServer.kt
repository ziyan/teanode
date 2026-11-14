package com.teanode.kit.passwords

import com.teanode.kit.TeaNodeError

/**
 * FakePasswordsServer stands in for the server's Passwords half for one person: in the kit's tests, and in demo mode,
 * where the app answers from it. It keeps what the real one keeps (sealed text, revisions, earlier versions) and refuses
 * what the real one refuses, with the same words. It never holds a key. Sharing is not kept: nobody else exists here.
 */
class FakePasswordsServer(private val userId: String = "me") : PasswordsApi {
    private class StoredItem(
        val id: String,
        val itemKind: String,
        var encryptedFields: String,
        var itemKeyByAccountKey: String,
        var revision: Long,
        var deletedAt: String? = null,
        var itemKeyByAgentKey: String? = null,
        var isUsableByAgent: Boolean = false,
        var needsNewKey: Boolean = false,
    )

    private class StoredVersion(
        val versionId: String,
        val itemKind: String,
        val encryptedFields: String,
        val itemKeyByAccountKey: String,
        val savedAt: String,
    )

    private var setup: PasswordsSetUpInput? = null
    private var lockAfterMinutes = 15
    private val items = LinkedHashMap<String, StoredItem>()
    private val versions = mutableMapOf<String, MutableList<StoredVersion>>()
    private var revisionCounter = 0L
    private var versionCounter = 0
    /** The server agent's key pair: the real server keeps the private half, and the tests open the agent's copy with it. */
    val agentKeyPair = PasswordsCrypto.makeKeyPair()

    /** When true, every call fails the way a missing connection does. */
    @Volatile var isOffline = false

    /** When true, every unlock is refused the way the server refuses after too many wrong tries. */
    @Volatile var isRateLimited = false

    /** The `sinceRevision` of every list call, in order. */
    val listCalls = mutableListOf<Long?>()

    /** Changes an item's sealed fields as another device's save would (a new revision, the same key). */
    @Synchronized
    fun touch(itemId: String) {
        items[itemId]?.revision = nextRevision()
    }

    /** Puts an item back to sealed text captured earlier, as a server that kept an old copy would. */
    @Synchronized
    fun replaceStoredFields(itemId: String, encryptedFields: String, itemKeyByAccountKey: String) {
        val stored = items[itemId] ?: return
        stored.encryptedFields = encryptedFields
        stored.itemKeyByAccountKey = itemKeyByAccountKey
        stored.revision = nextRevision()
    }

    /** Gives an item another revision without touching anything else, as if it had been changed earlier than the copies know of. */
    @Synchronized
    fun setRevision(itemId: String, revision: Long) {
        items[itemId]?.revision = revision
    }

    @Synchronized
    fun storedItem(itemId: String): PasswordItemView? = items[itemId]?.let { stored -> view(stored) }

    private fun requireOnline() {
        if (isOffline) throw TeaNodeError.Refused("The Internet connection appears to be offline.")
    }

    private fun nextRevision(): Long {
        revisionCounter += 1
        return revisionCounter
    }

    private fun time(): String = "2026-10-20T10:00:00Z"

    private fun refuse(sentence: String): Nothing = throw TeaNodeError.Refused(sentence)

    override suspend fun readPasswordsSetup(): PasswordsSetupView {
        requireOnline()
        val current = setup ?: return PasswordsSetupView(isSetUp = false, lockAfterMinutes = lockAfterMinutes, itemCount = 0)
        val itemCount = items.values.count { stored -> stored.deletedAt == null }
        return PasswordsSetupView(
            isSetUp = true, passphraseSalt = current.passphraseSalt, passphraseIterationCount = current.passphraseIterationCount,
            publicKey = current.publicKey, lockAfterMinutes = lockAfterMinutes, itemCount = itemCount,
        )
    }

    override suspend fun setUpPasswords(input: PasswordsSetUpInput): PasswordsSetupView {
        requireOnline()
        if (setup != null) refuse("api: already exists: Passwords is already set up.")
        setup = input
        return readPasswordsSetup()
    }

    private fun proofIsRight(proof: String, hash: String): Boolean {
        val bytes = runCatching { unbase64(proof) }.getOrNull() ?: return false
        return base64(PasswordsCrypto.proofHash(bytes)) == hash
    }

    override suspend fun unlockPasswords(proof: String, isRecoveryKey: Boolean): PasswordsUnlockView {
        requireOnline()
        if (isRateLimited) refuse("api: too many attempts, try again later")
        val current = setup ?: refuse("Passwords is not set up yet.")
        val hash = if (isRecoveryKey) current.recoveryKeyProofHash else current.passphraseProofHash
        if (!proofIsRight(proof, hash)) refuse(if (isRecoveryKey) "That recovery key is not right." else "That passphrase is not right.")
        return PasswordsUnlockView(
            accountKeyWrapped = if (isRecoveryKey) current.accountKeyByRecoveryKey else current.accountKeyByPassphrase,
            privateKeyByAccountKey = current.privateKeyByAccountKey, publicKey = current.publicKey,
        )
    }

    override suspend fun changePasswordsPassphrase(input: PasswordsChangePassphraseInput): PasswordsSetupView {
        requireOnline()
        val current = setup ?: refuse("Passwords is not set up yet.")
        val hash = if (input.isRecoveryKey) current.recoveryKeyProofHash else current.passphraseProofHash
        if (!proofIsRight(input.proof, hash)) refuse(if (input.isRecoveryKey) "That recovery key is not right." else "That passphrase is not right.")
        setup = current.copy(
            accountKeyByPassphrase = input.accountKeyByPassphrase, passphraseSalt = input.passphraseSalt,
            passphraseIterationCount = input.passphraseIterationCount, passphraseProofHash = input.passphraseProofHash,
        )
        return readPasswordsSetup()
    }

    override suspend fun replacePasswordsRecoveryKey(input: PasswordsReplaceRecoveryKeyInput): PasswordsSetupView {
        requireOnline()
        val current = setup ?: refuse("Passwords is not set up yet.")
        if (!proofIsRight(input.proof, current.passphraseProofHash)) refuse("That passphrase is not right.")
        setup = current.copy(accountKeyByRecoveryKey = input.accountKeyByRecoveryKey, recoveryKeyProofHash = input.recoveryKeyProofHash)
        return readPasswordsSetup()
    }

    override suspend fun updatePasswordsSettings(lockAfterMinutes: Int): PasswordsSetupView {
        requireOnline()
        if (lockAfterMinutes !in 1..10080) refuse("The lock time is 1 minute to 7 days.")
        this.lockAfterMinutes = lockAfterMinutes
        return readPasswordsSetup()
    }

    override suspend fun erasePasswords(): Boolean {
        requireOnline()
        setup = null
        items.clear()
        versions.clear()
        return true
    }

    private fun view(stored: StoredItem): PasswordItemView = PasswordItemView(
        id = stored.id, ownerUserId = userId, ownerName = "", ownerPublicKey = setup?.publicKey ?: "", itemKind = stored.itemKind,
        encryptedFields = stored.encryptedFields, itemKey = stored.itemKeyByAccountKey, isSharedWithMe = false, revision = stored.revision,
        isUsableByAgent = stored.isUsableByAgent, deletedAt = stored.deletedAt, createdAt = time(), updatedAt = time(), shares = emptyList(),
        needsNewKey = stored.needsNewKey,
    )

    override suspend fun listPasswordItems(sinceRevision: Long?): PasswordItemListView {
        requireOnline()
        listCalls.add(sinceRevision)
        val visible = items.values.map { stored -> view(stored) }
        val changed = visible.filter { item -> item.revision > (sinceRevision ?: 0) }.sortedBy { item -> item.revision }
        return PasswordItemListView(changed, visible.map { item -> item.id }.sorted(), visible.maxOfOrNull { item -> item.revision } ?: 0)
    }

    override suspend fun listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?): List<PasswordRecipientView> {
        requireOnline()
        return emptyList()
    }

    override suspend fun listWaitingPasswordShares(): List<PasswordWaitingShareView> {
        requireOnline()
        return emptyList()
    }

    override suspend fun readPasswordsAgentKey(): String {
        requireOnline()
        return base64(agentKeyPair.publicKey)
    }

    private fun keepVersion(stored: StoredItem) {
        versionCounter += 1
        versions.getOrPut(stored.id) { mutableListOf() }.add(
            StoredVersion("version-$versionCounter", stored.itemKind, stored.encryptedFields, stored.itemKeyByAccountKey, "2026-10-20T10:%02d:00Z".format(versionCounter % 60)),
        )
    }

    override suspend fun savePasswordItem(input: PasswordItemSaveInput): PasswordItemView {
        requireOnline()
        val existing = items[input.id]
        val stored: StoredItem
        if (existing != null) {
            if (input.baseRevision != existing.revision) refuse("This item changed on another device. Open it again.")
            if (existing.itemKind != input.itemKind) refuse("An item's kind cannot change.")
            keepVersion(existing)
            existing.encryptedFields = input.encryptedFields
            existing.itemKeyByAccountKey = input.itemKey
            stored = existing
        } else {
            if (input.baseRevision != null) refuse("api: not found")
            stored = StoredItem(input.id, input.itemKind, input.encryptedFields, input.itemKey, 0)
            items[stored.id] = stored
        }
        stored.revision = nextRevision()
        return view(stored)
    }

    private fun owned(itemId: String): StoredItem = items[itemId] ?: refuse("api: not found")

    override suspend fun deletePasswordItem(id: String): PasswordItemView {
        requireOnline()
        val stored = owned(id)
        if (stored.isUsableByAgent) stored.needsNewKey = true
        stored.deletedAt = time()
        stored.isUsableByAgent = false
        stored.itemKeyByAgentKey = null
        stored.revision = nextRevision()
        return view(stored)
    }

    override suspend fun restorePasswordItem(id: String): PasswordItemView {
        requireOnline()
        val stored = owned(id)
        stored.deletedAt = null
        stored.revision = nextRevision()
        return view(stored)
    }

    override suspend fun purgePasswordItem(id: String): Boolean {
        requireOnline()
        val stored = owned(id)
        if (stored.deletedAt == null) refuse("api: not found")
        items.remove(id)
        versions.remove(id)
        return true
    }

    override suspend fun setPasswordItemShares(itemId: String, grantees: List<PasswordShareGranteeInput>, keys: List<PasswordShareKeyInput>): PasswordItemView =
        refuse("Sharing is not kept by this stand-in server.")

    override suspend fun addPasswordShareKeys(keys: List<PasswordItemShareKeyInput>): Int = refuse("Sharing is not kept by this stand-in server.")

    /** The sealed copy the server holds for the agent, by item id. */
    @Synchronized
    fun agentCopyOf(itemId: String): String? = items[itemId]?.itemKeyByAgentKey

    override suspend fun setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Boolean, itemKeyByAgentKey: String?): PasswordItemView {
        requireOnline()
        val stored = owned(itemId)
        if (stored.itemKind == "card") refuse("api: invalid arguments: A card cannot be used by the agent.")
        if (isUsableByAgent) {
            if (itemKeyByAgentKey == null) refuse("api: invalid arguments: The agent needs a copy of the item key.")
            stored.itemKeyByAgentKey = itemKeyByAgentKey
        } else {
            // Access narrowed while the item keeps the key the agent held: it needs a new one.
            if (stored.isUsableByAgent) stored.needsNewKey = true
            stored.itemKeyByAgentKey = null
        }
        stored.isUsableByAgent = isUsableByAgent
        stored.revision = nextRevision()
        return view(stored)
    }

    override suspend fun rekeyPasswordItem(input: PasswordItemRekeyInput): PasswordItemView {
        requireOnline()
        val stored = owned(input.id)
        if (input.baseRevision != stored.revision) refuse("This item changed on another device. Open it again.")
        keepVersion(stored)
        stored.encryptedFields = input.encryptedFields
        stored.itemKeyByAccountKey = input.itemKey
        stored.itemKeyByAgentKey = input.itemKeyByAgentKey
        stored.isUsableByAgent = input.itemKeyByAgentKey != null
        stored.needsNewKey = false
        stored.revision = nextRevision()
        return view(stored)
    }

    override suspend fun listPasswordItemVersions(itemId: String): List<PasswordItemVersionView> {
        requireOnline()
        owned(itemId)
        return versions[itemId].orEmpty().reversed().map { version ->
            PasswordItemVersionView(version.versionId, version.savedAt, true, version.encryptedFields, version.itemKeyByAccountKey, version.itemKind)
        }
    }

    override suspend fun restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Long): PasswordItemView {
        requireOnline()
        val stored = owned(itemId)
        if (stored.revision != baseRevision) refuse("This item changed on another device. Open it again.")
        val version = versions[itemId].orEmpty().firstOrNull { entry -> entry.versionId == versionId } ?: refuse("api: not found")
        keepVersion(stored)
        stored.encryptedFields = version.encryptedFields
        stored.itemKeyByAccountKey = version.itemKeyByAccountKey
        // The server drops the agent's copy with the restore and marks the item as needing a new key.
        if (stored.isUsableByAgent) stored.needsNewKey = true
        stored.isUsableByAgent = false
        stored.itemKeyByAgentKey = null
        stored.revision = nextRevision()
        return view(stored)
    }
}

/**
 * Answers a GraphQL document of Passwords the way the real server's schema does, from this stand-in, so demo mode (which
 * speaks GraphQL like the app does) reaches it. Null when the document is not one of Passwords'. A refusal is an `errors`
 * answer carrying the server's sentence.
 */
suspend fun FakePasswordsServer.answerGraphQL(document: String, variables: kotlinx.serialization.json.JsonObject): String? {
    fun asks(name: String) = Regex("""\b$name\s*[({]""").containsMatchIn(document) || Regex("""\b$name\s*\}""").containsMatchIn(document)
    val json = com.teanode.kit.TeaNodeJson
    fun <Input> input(serializer: kotlinx.serialization.KSerializer<Input>): Input = json.decodeFromJsonElement(serializer, variables)
    fun text(key: String): String = (variables[key] as kotlinx.serialization.json.JsonPrimitive).content
    fun optionalText(key: String): String? = (variables[key] as? kotlinx.serialization.json.JsonPrimitive)?.takeIf { it !is kotlinx.serialization.json.JsonNull }?.content
    fun long(key: String): Long? = optionalText(key)?.toLongOrNull()
    fun <Answer> reply(name: String, serializer: kotlinx.serialization.KSerializer<Answer>, answer: Answer) =
        """{"data":{"$name":${json.encodeToString(serializer, answer)}}}"""
    return try {
        when {
            asks("ReadPasswordsSetup") -> reply("ReadPasswordsSetup", PasswordsSetupView.serializer(), readPasswordsSetup())
            asks("SetUpPasswords") -> reply("SetUpPasswords", PasswordsSetupView.serializer(), setUpPasswords(input(PasswordsSetUpInput.serializer())))
            asks("UnlockPasswords") -> reply("UnlockPasswords", PasswordsUnlockView.serializer(), unlockPasswords(text("proof"), optionalText("isRecoveryKey") == "true"))
            asks("ChangePasswordsPassphrase") -> reply("ChangePasswordsPassphrase", PasswordsSetupView.serializer(), changePasswordsPassphrase(input(PasswordsChangePassphraseInput.serializer())))
            asks("ReplacePasswordsRecoveryKey") -> reply("ReplacePasswordsRecoveryKey", PasswordsSetupView.serializer(), replacePasswordsRecoveryKey(input(PasswordsReplaceRecoveryKeyInput.serializer())))
            asks("UpdatePasswordsSettings") -> reply("UpdatePasswordsSettings", PasswordsSetupView.serializer(), updatePasswordsSettings(text("lockAfterMinutes").toInt()))
            asks("ErasePasswords") -> """{"data":{"ErasePasswords":${erasePasswords()}}}"""
            asks("ListPasswordItemVersions") -> reply("ListPasswordItemVersions", kotlinx.serialization.builtins.ListSerializer(PasswordItemVersionView.serializer()), listPasswordItemVersions(text("itemId")))
            asks("ListPasswordItems") -> reply("ListPasswordItems", PasswordItemListView.serializer(), listPasswordItems(long("sinceRevision")))
            asks("ListPasswordRecipients") -> reply("ListPasswordRecipients", kotlinx.serialization.builtins.ListSerializer(PasswordRecipientView.serializer()), emptyList())
            asks("ListWaitingPasswordShares") -> reply("ListWaitingPasswordShares", kotlinx.serialization.builtins.ListSerializer(PasswordWaitingShareView.serializer()), emptyList())
            asks("ReadPasswordsAgentKey") -> """{"data":{"ReadPasswordsAgentKey":"${base64(agentKeyPair.publicKey)}"}}"""
            asks("SavePasswordItem") -> reply("SavePasswordItem", PasswordItemView.serializer(), savePasswordItem(input(PasswordItemSaveInput.serializer())))
            asks("DeletePasswordItem") -> reply("DeletePasswordItem", PasswordItemView.serializer(), deletePasswordItem(text("id")))
            asks("RestorePasswordItemVersion") -> reply("RestorePasswordItemVersion", PasswordItemView.serializer(), restorePasswordItemVersion(text("itemId"), text("versionId"), long("baseRevision")!!))
            asks("RestorePasswordItem") -> reply("RestorePasswordItem", PasswordItemView.serializer(), restorePasswordItem(text("id")))
            asks("PurgePasswordItem") -> """{"data":{"PurgePasswordItem":${purgePasswordItem(text("id"))}}}"""
            asks("SetPasswordItemUsableByAgent") -> reply(
                "SetPasswordItemUsableByAgent", PasswordItemView.serializer(),
                setPasswordItemUsableByAgent(text("itemId"), optionalText("isUsableByAgent") == "true", optionalText("itemKeyByAgentKey")),
            )
            asks("RekeyPasswordItem") -> reply("RekeyPasswordItem", PasswordItemView.serializer(), rekeyPasswordItem(input(PasswordItemRekeyInput.serializer())))
            else -> null
        }
    } catch (error: com.teanode.kit.TeaNodeError.Refused) {
        """{"errors":[{"message":${kotlinx.serialization.json.JsonPrimitive(error.reason)}}],"data":null}"""
    }
}
