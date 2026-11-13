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
    private val agentKeyPair = PasswordsCrypto.makeKeyPair()

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
        isUsableByAgent = false, deletedAt = stored.deletedAt, createdAt = time(), updatedAt = time(), shares = emptyList(),
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
        stored.deletedAt = time()
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

    override suspend fun setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Boolean, itemKeyByAgentKey: String?): PasswordItemView =
        refuse("The agent is not kept by this stand-in server.")

    override suspend fun rekeyPasswordItem(input: PasswordItemRekeyInput): PasswordItemView = refuse("Re-keying is not kept by this stand-in server.")

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
        stored.revision = nextRevision()
        return view(stored)
    }
}
