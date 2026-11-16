package com.teanode.kit.passwords

import com.teanode.kit.TeaNodeError

/**
 * FakePasswordsServer stands in for the server's Passwords half: in the kit's tests, and in demo mode, where the app
 * answers from it. It keeps what the real one keeps (sealed text, revisions, earlier versions, shares and their keys) and
 * refuses what the real one refuses, with the same words. It never holds a key, except the agent's, which it keeps the way
 * the real one does. Each person has a connection of their own ([api]); the server itself answers as `defaultUserId`,
 * a person that exists from the start, so a test with one person needs no more than `FakePasswordsServer()`.
 */
class FakePasswordsServer(private val defaultUserId: String = "me") : PasswordsApi {
    class Person(val userId: String, val name: String, val username: String) {
        var setup: PasswordsSetUpInput? = null
        var lockAfterMinutes = 15
    }

    class StoredShareKey(val itemKeyByRecipientKey: String, val recipientPublicKey: String)

    class StoredItem(
        val id: String,
        val ownerUserId: String,
        val itemKind: String,
        var encryptedFields: String,
        var itemKeyByAccountKey: String,
        var revision: Long,
        var isUsableByAgent: Boolean = false,
        var itemKeyByAgentKey: String? = null,
        var deletedAt: String? = null,
        /** Set when access narrowed while the item kept the key somebody held; cleared by a re-key. */
        var needsNewKey: Boolean = false,
        var grantees: List<PasswordShareGranteeInput> = emptyList(),
        /** Recipient user id to the key and the public key it was sealed for. */
        var keys: Map<String, StoredShareKey> = emptyMap(),
    )

    /** An earlier version of an item, kept when a save or a re-key replaced it. */
    private class StoredVersion(
        val versionId: String,
        val itemKind: String,
        val encryptedFields: String,
        val itemKeyByAccountKey: String,
        val isSavedByPerson: Boolean,
        val savedAt: String,
    )

    private val people = LinkedHashMap<String, Person>()
    private val items = LinkedHashMap<String, StoredItem>()

    /** The earlier versions of each item, oldest first. */
    private val versions = mutableMapOf<String, MutableList<StoredVersion>>()
    private val circles = mutableMapOf<String, List<String>>()
    private val circleNames = mutableMapOf<String, String>()
    private var revisionCounter = 0L
    private var versionCounter = 0

    /** Whether the next saves count as the person's own tap (a token's do not). */
    private var areSavesByPerson = true

    /** The server agent's key pair: the real server keeps the private half, and the tests open the agent's copy with it. */
    val agentKeyPair = PasswordsCrypto.makeKeyPair()

    /** When true, every call fails the way a missing connection does. */
    @Volatile var isOffline = false

    /** When true, every unlock is refused the way the server refuses after too many wrong tries. */
    @Volatile var isRateLimited = false

    /** The `sinceRevision` of every list call, in order. */
    val listCalls = mutableListOf<Long?>()
    private var listCallMark = 0

    /** People who have the most the server lets others share with them: it stores no new key for them. */
    private val tooMuchSharedUserIds = mutableSetOf<String>()

    /** When true, every waiting share it lists says that more are waiting. */
    private var hasMoreWaiting = false

    /** When set, the next re-key calls are refused with this sentence (until cleared). */
    var rekeyRefusal: String? = null
    var rekeyCallCount = 0
        private set

    init {
        addPerson(defaultUserId, defaultUserId)
    }

    /** One person's connection to this server. */
    fun api(userId: String): PasswordsApi = FakePasswordsApi(this, userId)

    /** Starts counting list calls from here ([listCallsSinceMark]). */
    fun markListCalls() {
        listCallMark = listCalls.size
    }

    fun listCallsSinceMark(): List<Long?> = listCalls.drop(listCallMark)

    fun addPerson(userId: String, name: String, username: String = name.lowercase()) {
        people[userId] = Person(userId, name, username)
    }

    fun setHasTooMuchShared(userId: String, isTooMuch: Boolean) {
        if (isTooMuch) tooMuchSharedUserIds.add(userId) else tooMuchSharedUserIds.remove(userId)
    }

    fun setHasMoreWaiting(hasMore: Boolean) {
        hasMoreWaiting = hasMore
    }

    fun setSavesAreByPerson(isByPerson: Boolean) {
        areSavesByPerson = isByPerson
    }

    /**
     * Sets the members of a circle. Somebody an item no longer reaches loses its key, and the item is marked as needing a
     * new key, as the real server does when a member leaves.
     */
    fun setCircle(circleId: String, members: List<String>, name: String = circleId) {
        circleNames[circleId] = name
        val reachedBefore = items.mapValues { entry -> reached(entry.value).toSet() }
        circles[circleId] = members
        for ((itemId, item) in items) {
            val reachedNow = reached(item).toSet()
            if ((reachedBefore[itemId] ?: emptySet()).minus(reachedNow).isEmpty()) continue
            item.keys = item.keys.filterKeys { recipientUserId -> reachedNow.contains(recipientUserId) }
            item.needsNewKey = true
            item.revision = nextRevision()
        }
    }

    /** Changes an item's sealed fields as another device's save would (a new revision, the same key). */
    fun touch(itemId: String) {
        items[itemId]?.revision = nextRevision()
    }

    /** Puts an item back to sealed text captured earlier, as a server that kept an old copy would. */
    fun replaceStoredFields(itemId: String, encryptedFields: String, itemKeyByAccountKey: String, needsNewKey: Boolean = false) {
        val stored = items[itemId] ?: return
        stored.encryptedFields = encryptedFields
        stored.itemKeyByAccountKey = itemKeyByAccountKey
        stored.needsNewKey = needsNewKey
        stored.revision = nextRevision()
    }

    /** Marks an item as needing a new key without anything else changing, as the server does on its own word. */
    fun markNeedsNewKey(itemId: String) {
        val stored = items[itemId] ?: return
        stored.needsNewKey = true
        stored.revision = nextRevision()
    }

    /** Takes one person's key to an item away without anything else changing, so that person waits for a key. */
    fun removeKey(itemId: String, userId: String) {
        val stored = items[itemId] ?: return
        stored.keys = stored.keys - userId
        stored.revision = nextRevision()
    }

    /** Gives an item another revision without touching anything else, as if it had been changed earlier than the copies know of. */
    fun setRevision(itemId: String, revision: Long) {
        items[itemId]?.revision = revision
    }

    fun item(itemId: String): StoredItem? = items[itemId]

    /** The item as its owner sees it. */
    fun storedItem(itemId: String): PasswordItemView? = items[itemId]?.let { stored -> view(stored, stored.ownerUserId) }

    /** A person's public key as standard base64; empty before they set up. */
    fun publicKey(userId: String): String = people[userId]?.setup?.publicKey ?: ""

    /** Gives a person another public key without touching anything else, as the server would show it if the person's key changed (or a server lied about it). */
    fun replacePublicKey(userId: String, publicKey: String) {
        val person = people[userId] ?: return
        person.setup = person.setup?.copy(publicKey = publicKey)
    }

    /** Stores another sealed key for a recipient, for the public key that recipient has now. */
    fun replaceShareKey(itemId: String, recipientUserId: String, itemKeyByRecipientKey: String) {
        val stored = items[itemId] ?: return
        val publicKey = people[recipientUserId]?.setup?.publicKey ?: return
        stored.keys = stored.keys + (recipientUserId to StoredShareKey(itemKeyByRecipientKey, publicKey))
        stored.revision = nextRevision()
    }

    /** The sealed copy the server holds for the agent, by item id. */
    fun agentCopyOf(itemId: String): String? = items[itemId]?.itemKeyByAgentKey

    private fun requireOnline() {
        if (isOffline) throw TeaNodeError.Refused("The Internet connection appears to be offline.")
    }

    private fun nextRevision(): Long {
        revisionCounter += 1
        return revisionCounter
    }

    private fun time(): String = "2026-10-20T10:00:00Z"

    private fun refuse(sentence: String): Nothing = throw TeaNodeError.Refused(sentence)

    private fun person(userId: String): Person = people[userId] ?: refuse("api: not found")

    // Setup

    internal fun readSetup(userId: String): PasswordsSetupView {
        requireOnline()
        val person = person(userId)
        val itemCount = items.values.count { stored -> stored.ownerUserId == userId && stored.deletedAt == null }
        val current = person.setup ?: return PasswordsSetupView(isSetUp = false, lockAfterMinutes = person.lockAfterMinutes, itemCount = 0)
        return PasswordsSetupView(
            isSetUp = true, passphraseSalt = current.passphraseSalt, passphraseIterationCount = current.passphraseIterationCount,
            publicKey = current.publicKey, lockAfterMinutes = person.lockAfterMinutes, itemCount = itemCount,
        )
    }

    internal fun setUp(userId: String, input: PasswordsSetUpInput): PasswordsSetupView {
        requireOnline()
        val person = person(userId)
        if (person.setup != null) refuse("api: already exists: Passwords is already set up.")
        person.setup = input
        return readSetup(userId)
    }

    private fun proofIsRight(proof: String, hash: String): Boolean {
        val bytes = runCatching { unbase64(proof) }.getOrNull() ?: return false
        return base64(PasswordsCrypto.proofHash(bytes)) == hash
    }

    internal fun unlock(userId: String, proof: String, isRecoveryKey: Boolean): PasswordsUnlockView {
        requireOnline()
        if (isRateLimited) refuse("api: too many attempts, try again later")
        val current = person(userId).setup ?: refuse("Passwords is not set up yet.")
        val hash = if (isRecoveryKey) current.recoveryKeyProofHash else current.passphraseProofHash
        if (!proofIsRight(proof, hash)) refuse(if (isRecoveryKey) "That recovery key is not right." else "That passphrase is not right.")
        return PasswordsUnlockView(
            accountKeyWrapped = if (isRecoveryKey) current.accountKeyByRecoveryKey else current.accountKeyByPassphrase,
            privateKeyByAccountKey = current.privateKeyByAccountKey, publicKey = current.publicKey,
        )
    }

    internal fun changePassphrase(userId: String, input: PasswordsChangePassphraseInput): PasswordsSetupView {
        requireOnline()
        val person = person(userId)
        val current = person.setup ?: refuse("Passwords is not set up yet.")
        val hash = if (input.isRecoveryKey) current.recoveryKeyProofHash else current.passphraseProofHash
        if (!proofIsRight(input.proof, hash)) refuse(if (input.isRecoveryKey) "That recovery key is not right." else "That passphrase is not right.")
        person.setup = current.copy(
            accountKeyByPassphrase = input.accountKeyByPassphrase, passphraseSalt = input.passphraseSalt,
            passphraseIterationCount = input.passphraseIterationCount, passphraseProofHash = input.passphraseProofHash,
        )
        return readSetup(userId)
    }

    internal fun replaceRecoveryKey(userId: String, input: PasswordsReplaceRecoveryKeyInput): PasswordsSetupView {
        requireOnline()
        val person = person(userId)
        val current = person.setup ?: refuse("Passwords is not set up yet.")
        if (!proofIsRight(input.proof, current.passphraseProofHash)) refuse("That passphrase is not right.")
        person.setup = current.copy(accountKeyByRecoveryKey = input.accountKeyByRecoveryKey, recoveryKeyProofHash = input.recoveryKeyProofHash)
        return readSetup(userId)
    }

    internal fun updateSettings(userId: String, lockAfterMinutes: Int): PasswordsSetupView {
        requireOnline()
        val person = person(userId)
        if (lockAfterMinutes !in 1..10080) refuse("The lock time is 1 minute to 7 days.")
        person.lockAfterMinutes = lockAfterMinutes
        return readSetup(userId)
    }

    internal fun erase(userId: String): Boolean {
        requireOnline()
        val person = person(userId)
        person.setup = null
        for (itemId in items.filterValues { stored -> stored.ownerUserId == userId }.keys.toList()) {
            items.remove(itemId)
            versions.remove(itemId)
        }
        for (stored in items.values) stored.keys = stored.keys - userId
        return true
    }

    // Items

    private fun reached(item: StoredItem): List<String> {
        val userIds = mutableListOf<String>()
        for (grantee in item.grantees) {
            grantee.granteeUserId?.let { granteeUserId -> userIds.add(granteeUserId) }
            grantee.granteeCircleId?.let { circleId -> userIds.addAll(circles[circleId] ?: emptyList()) }
        }
        return userIds.filter { reachedUserId -> reachedUserId != item.ownerUserId }.distinct()
    }

    private fun recipientView(userId: String, item: StoredItem?): PasswordRecipientView {
        val person = people[userId]
        val publicKey = person?.setup?.publicKey
        val hasKey = publicKey != null && item?.keys?.get(userId)?.recipientPublicKey == publicKey
        return PasswordRecipientView(
            userId = userId, name = person?.name ?: "", username = person?.username ?: "", publicKey = publicKey, hasKey = hasKey,
            hasTooMuchShared = !hasKey && tooMuchSharedUserIds.contains(userId),
        )
    }

    private fun view(item: StoredItem, viewerUserId: String): PasswordItemView? {
        val owner = people[item.ownerUserId]
        if (item.ownerUserId == viewerUserId) {
            val shares = item.grantees.mapIndexed { index, grantee ->
                val members = grantee.granteeUserId?.let { memberUserId -> listOf(memberUserId) } ?: (circles[grantee.granteeCircleId ?: ""] ?: emptyList())
                PasswordShareView(
                    id = "share-${item.id}-$index", granteeUserId = grantee.granteeUserId, granteeCircleId = grantee.granteeCircleId,
                    granteeName = grantee.granteeUserId?.let { granteeUserId -> people[granteeUserId]?.let { person -> person.name.ifEmpty { person.username } } }
                        ?: grantee.granteeCircleId?.let { circleId -> circleNames[circleId] ?: circleId } ?: "",
                    recipients = members.filter { memberUserId -> memberUserId != item.ownerUserId }.map { memberUserId -> recipientView(memberUserId, item) },
                )
            }
            return PasswordItemView(
                id = item.id, ownerUserId = item.ownerUserId, ownerName = owner?.name ?: "", ownerPublicKey = owner?.setup?.publicKey ?: "",
                itemKind = item.itemKind, encryptedFields = item.encryptedFields, itemKey = item.itemKeyByAccountKey, isSharedWithMe = false,
                revision = item.revision, isUsableByAgent = item.isUsableByAgent, deletedAt = item.deletedAt, createdAt = time(), updatedAt = time(),
                shares = shares, needsNewKey = item.needsNewKey,
            )
        }
        val key = item.keys[viewerUserId] ?: return null
        if (item.deletedAt != null || !reached(item).contains(viewerUserId) || key.recipientPublicKey != people[viewerUserId]?.setup?.publicKey) return null
        return PasswordItemView(
            id = item.id, ownerUserId = item.ownerUserId, ownerName = owner?.name ?: "", ownerPublicKey = owner?.setup?.publicKey ?: "",
            itemKind = item.itemKind, encryptedFields = item.encryptedFields, itemKey = key.itemKeyByRecipientKey, isSharedWithMe = true,
            revision = item.revision, isUsableByAgent = false, createdAt = time(), updatedAt = time(), shares = emptyList(),
        )
    }

    internal fun list(userId: String, sinceRevision: Long?): PasswordItemListView {
        requireOnline()
        listCalls.add(sinceRevision)
        val visible = items.values.mapNotNull { stored -> view(stored, userId) }
        val changed = visible.filter { item -> item.revision > (sinceRevision ?: 0) }.sortedBy { item -> item.revision }
        return PasswordItemListView(changed, visible.map { item -> item.id }.sorted(), visible.maxOfOrNull { item -> item.revision } ?: 0)
    }

    internal fun recipients(userId: String, granteeUserId: String?, granteeCircleId: String?): List<PasswordRecipientView> {
        requireOnline()
        val members = granteeUserId?.let { memberUserId -> listOf(memberUserId) } ?: (circles[granteeCircleId ?: ""] ?: emptyList())
        return members.filter { memberUserId -> memberUserId != userId }.map { memberUserId -> recipientView(memberUserId, null) }
    }

    internal fun waiting(userId: String): List<PasswordWaitingShareView> {
        requireOnline()
        val waiting = mutableListOf<PasswordWaitingShareView>()
        for (item in items.values.sortedBy { stored -> stored.id }) {
            if (item.ownerUserId != userId || item.deletedAt != null) continue
            for (recipientId in reached(item)) {
                val recipient = recipientView(recipientId, item)
                if (recipient.publicKey != null && !recipient.hasKey) waiting.add(PasswordWaitingShareView(item.id, recipient, hasMoreWaiting))
            }
        }
        return waiting
    }

    private fun keepVersion(stored: StoredItem) {
        versionCounter += 1
        versions.getOrPut(stored.id) { mutableListOf() }.add(
            StoredVersion(
                "version-$versionCounter", stored.itemKind, stored.encryptedFields, stored.itemKeyByAccountKey, areSavesByPerson,
                "2026-10-20T10:%02d:00Z".format(versionCounter % 60),
            ),
        )
    }

    internal fun save(userId: String, input: PasswordItemSaveInput): PasswordItemView {
        requireOnline()
        val existing = items[input.id]
        val stored: StoredItem
        if (existing != null) {
            if (existing.ownerUserId != userId) refuse("api: not found")
            if (input.baseRevision != existing.revision) refuse("This item changed on another device. Open it again.")
            if (existing.itemKind != input.itemKind) refuse("An item's kind cannot change.")
            keepVersion(existing)
            existing.encryptedFields = input.encryptedFields
            existing.itemKeyByAccountKey = input.itemKey
            stored = existing
        } else {
            if (input.baseRevision != null) refuse("api: not found")
            stored = StoredItem(input.id, userId, input.itemKind, input.encryptedFields, input.itemKey, 0)
            items[stored.id] = stored
        }
        stored.revision = nextRevision()
        return view(stored, userId)!!
    }

    private fun ownedItem(itemId: String, userId: String): StoredItem {
        val stored = items[itemId]
        if (stored == null || stored.ownerUserId != userId) refuse("api: not found")
        return stored
    }

    internal fun delete(userId: String, itemId: String): PasswordItemView {
        requireOnline()
        val stored = ownedItem(itemId, userId)
        if (stored.keys.isNotEmpty() || stored.isUsableByAgent) stored.needsNewKey = true
        stored.deletedAt = time()
        stored.isUsableByAgent = false
        stored.itemKeyByAgentKey = null
        stored.keys = emptyMap()
        stored.revision = nextRevision()
        return view(stored, userId)!!
    }

    internal fun restore(userId: String, itemId: String): PasswordItemView {
        requireOnline()
        val stored = ownedItem(itemId, userId)
        stored.deletedAt = null
        stored.revision = nextRevision()
        return view(stored, userId)!!
    }

    /** Deletes a deleted item for good, as the real server does for a tap. */
    internal fun purge(userId: String, itemId: String): Boolean {
        requireOnline()
        val stored = ownedItem(itemId, userId)
        if (stored.deletedAt == null) refuse("api: not found")
        items.remove(itemId)
        versions.remove(itemId)
        return true
    }

    internal fun setShares(userId: String, itemId: String, grantees: List<PasswordShareGranteeInput>, keys: List<PasswordShareKeyInput>): PasswordItemView {
        requireOnline()
        val stored = ownedItem(itemId, userId)
        val reachedBefore = reached(stored).toSet()
        stored.grantees = grantees
        val reachedUserIds = reached(stored).toSet()
        if (reachedBefore.minus(reachedUserIds).isNotEmpty()) stored.needsNewKey = true
        stored.keys = stored.keys.filterKeys { recipientUserId -> reachedUserIds.contains(recipientUserId) }
        for (key in keys) {
            if (key.itemKey != stored.itemKeyByAccountKey) refuse("This item changed on another device. Open it again.")
            if (!reachedUserIds.contains(key.recipientUserId)) refuse("That person is not reached by this share.")
            if (people[key.recipientUserId]?.setup?.publicKey != key.recipientPublicKey) {
                refuse("api: invalid arguments: That person's key changed since it was read. Read their key again and seal the item for it.")
            }
            stored.keys = stored.keys + (key.recipientUserId to StoredShareKey(key.itemKeyByRecipientKey, key.recipientPublicKey))
        }
        stored.revision = nextRevision()
        return view(stored, userId)!!
    }

    internal fun addShareKeys(userId: String, keys: List<PasswordItemShareKeyInput>): Int {
        requireOnline()
        var storedCount = 0
        for (key in keys) {
            val stored = ownedItem(key.itemId, userId)
            if (key.itemKey != stored.itemKeyByAccountKey) refuse("This item changed on another device. Open it again.")
            if (!reached(stored).contains(key.recipientUserId) || people[key.recipientUserId]?.setup?.publicKey != key.recipientPublicKey) refuse("api: not found")
            // A person who has too much shared with them gets no key, and the call goes on for the others.
            if (tooMuchSharedUserIds.contains(key.recipientUserId)) continue
            stored.keys = stored.keys + (key.recipientUserId to StoredShareKey(key.itemKeyByRecipientKey, key.recipientPublicKey))
            stored.revision = nextRevision()
            storedCount += 1
        }
        return storedCount
    }

    internal fun setUsableByAgent(userId: String, itemId: String, isUsableByAgent: Boolean, itemKeyByAgentKey: String?): PasswordItemView {
        requireOnline()
        val stored = ownedItem(itemId, userId)
        if (stored.itemKind == "card" && isUsableByAgent) refuse("api: invalid arguments: A card cannot be used by the agent.")
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
        return view(stored, userId)!!
    }

    internal fun rekey(userId: String, input: PasswordItemRekeyInput): PasswordItemView {
        requireOnline()
        rekeyCallCount += 1
        rekeyRefusal?.let { sentence -> refuse(sentence) }
        val stored = ownedItem(input.id, userId)
        if (input.baseRevision != stored.revision) refuse("This item changed on another device. Open it again.")
        val reachedUserIds = reached(stored).toSet()
        val newKeys = mutableMapOf<String, StoredShareKey>()
        for (key in input.shareKeys) {
            if (key.itemId != stored.id || !reachedUserIds.contains(key.recipientUserId)) refuse("api: not found")
            if (people[key.recipientUserId]?.setup?.publicKey != key.recipientPublicKey) {
                refuse("api: invalid arguments: That person's key changed since it was read. Read their key again and seal the item for it.")
            }
            newKeys[key.recipientUserId] = StoredShareKey(key.itemKeyByRecipientKey, key.recipientPublicKey)
        }
        keepVersion(stored)
        val agentCopy = input.itemKeyByAgentKey
        if (agentCopy != null) {
            if (!stored.isUsableByAgent) refuse("api: invalid arguments: The agent cannot use this item.")
            stored.itemKeyByAgentKey = agentCopy
        } else {
            stored.isUsableByAgent = false
            stored.itemKeyByAgentKey = null
        }
        stored.encryptedFields = input.encryptedFields
        stored.itemKeyByAccountKey = input.itemKey
        stored.keys = if (stored.deletedAt == null) newKeys else emptyMap()
        stored.needsNewKey = false
        stored.revision = nextRevision()
        return view(stored, userId)!!
    }

    internal fun listVersions(userId: String, itemId: String): List<PasswordItemVersionView> {
        requireOnline()
        val stored = ownedItem(itemId, userId)
        return versions[itemId].orEmpty().reversed().map { version ->
            PasswordItemVersionView(version.versionId, version.savedAt, version.isSavedByPerson, version.encryptedFields, version.itemKeyByAccountKey, stored.itemKind)
        }
    }

    /** Makes an earlier version current, drops the keys sealed for recipients and the agent's copy, and marks the item. */
    internal fun restoreVersion(userId: String, itemId: String, versionId: String, baseRevision: Long): PasswordItemView {
        requireOnline()
        val stored = ownedItem(itemId, userId)
        if (baseRevision != stored.revision) refuse("This item changed on another device. Open it again.")
        val version = versions[itemId].orEmpty().firstOrNull { entry -> entry.versionId == versionId } ?: refuse("api: not found")
        keepVersion(stored)
        stored.encryptedFields = version.encryptedFields
        stored.itemKeyByAccountKey = version.itemKeyByAccountKey
        stored.keys = emptyMap()
        stored.isUsableByAgent = false
        stored.itemKeyByAgentKey = null
        stored.needsNewKey = true
        stored.revision = nextRevision()
        return view(stored, userId)!!
    }

    internal fun readAgentKey(): String {
        requireOnline()
        return base64(agentKeyPair.publicKey)
    }

    // The default person's connection: the server answers as `defaultUserId`.
    override suspend fun readPasswordsSetup() = readSetup(defaultUserId)
    override suspend fun setUpPasswords(input: PasswordsSetUpInput) = setUp(defaultUserId, input)
    override suspend fun unlockPasswords(proof: String, isRecoveryKey: Boolean) = unlock(defaultUserId, proof, isRecoveryKey)
    override suspend fun changePasswordsPassphrase(input: PasswordsChangePassphraseInput) = changePassphrase(defaultUserId, input)
    override suspend fun replacePasswordsRecoveryKey(input: PasswordsReplaceRecoveryKeyInput) = replaceRecoveryKey(defaultUserId, input)
    override suspend fun updatePasswordsSettings(lockAfterMinutes: Int) = updateSettings(defaultUserId, lockAfterMinutes)
    override suspend fun erasePasswords() = erase(defaultUserId)
    override suspend fun listPasswordItems(sinceRevision: Long?) = list(defaultUserId, sinceRevision)
    override suspend fun listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?) = recipients(defaultUserId, granteeUserId, granteeCircleId)
    override suspend fun listWaitingPasswordShares() = waiting(defaultUserId)
    override suspend fun readPasswordsAgentKey() = readAgentKey()
    override suspend fun savePasswordItem(input: PasswordItemSaveInput) = save(defaultUserId, input)
    override suspend fun deletePasswordItem(id: String) = delete(defaultUserId, id)
    override suspend fun restorePasswordItem(id: String) = restore(defaultUserId, id)
    override suspend fun purgePasswordItem(id: String) = purge(defaultUserId, id)
    override suspend fun setPasswordItemShares(itemId: String, grantees: List<PasswordShareGranteeInput>, keys: List<PasswordShareKeyInput>) =
        setShares(defaultUserId, itemId, grantees, keys)
    override suspend fun addPasswordShareKeys(keys: List<PasswordItemShareKeyInput>) = addShareKeys(defaultUserId, keys)
    override suspend fun setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Boolean, itemKeyByAgentKey: String?) =
        setUsableByAgent(defaultUserId, itemId, isUsableByAgent, itemKeyByAgentKey)
    override suspend fun rekeyPasswordItem(input: PasswordItemRekeyInput) = rekey(defaultUserId, input)
    override suspend fun listPasswordItemVersions(itemId: String) = listVersions(defaultUserId, itemId)
    override suspend fun restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Long) =
        restoreVersion(defaultUserId, itemId, versionId, baseRevision)
}

/** FakePasswordsApi is one person's connection to the fake server. */
private class FakePasswordsApi(private val server: FakePasswordsServer, private val userId: String) : PasswordsApi {
    override suspend fun readPasswordsSetup() = server.readSetup(userId)
    override suspend fun setUpPasswords(input: PasswordsSetUpInput) = server.setUp(userId, input)
    override suspend fun unlockPasswords(proof: String, isRecoveryKey: Boolean) = server.unlock(userId, proof, isRecoveryKey)
    override suspend fun changePasswordsPassphrase(input: PasswordsChangePassphraseInput) = server.changePassphrase(userId, input)
    override suspend fun replacePasswordsRecoveryKey(input: PasswordsReplaceRecoveryKeyInput) = server.replaceRecoveryKey(userId, input)
    override suspend fun updatePasswordsSettings(lockAfterMinutes: Int) = server.updateSettings(userId, lockAfterMinutes)
    override suspend fun erasePasswords() = server.erase(userId)
    override suspend fun listPasswordItems(sinceRevision: Long?) = server.list(userId, sinceRevision)
    override suspend fun listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?) = server.recipients(userId, granteeUserId, granteeCircleId)
    override suspend fun listWaitingPasswordShares() = server.waiting(userId)
    override suspend fun readPasswordsAgentKey() = server.readAgentKey()
    override suspend fun savePasswordItem(input: PasswordItemSaveInput) = server.save(userId, input)
    override suspend fun deletePasswordItem(id: String) = server.delete(userId, id)
    override suspend fun restorePasswordItem(id: String) = server.restore(userId, id)
    override suspend fun purgePasswordItem(id: String) = server.purge(userId, id)
    override suspend fun setPasswordItemShares(itemId: String, grantees: List<PasswordShareGranteeInput>, keys: List<PasswordShareKeyInput>) =
        server.setShares(userId, itemId, grantees, keys)
    override suspend fun addPasswordShareKeys(keys: List<PasswordItemShareKeyInput>) = server.addShareKeys(userId, keys)
    override suspend fun setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Boolean, itemKeyByAgentKey: String?) =
        server.setUsableByAgent(userId, itemId, isUsableByAgent, itemKeyByAgentKey)
    override suspend fun rekeyPasswordItem(input: PasswordItemRekeyInput) = server.rekey(userId, input)
    override suspend fun listPasswordItemVersions(itemId: String) = server.listVersions(userId, itemId)
    override suspend fun restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Long) =
        server.restoreVersion(userId, itemId, versionId, baseRevision)
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
            asks("ListPasswordRecipients") -> reply("ListPasswordRecipients", kotlinx.serialization.builtins.ListSerializer(PasswordRecipientView.serializer()), listPasswordRecipients(optionalText("granteeUserId"), optionalText("granteeCircleId")))
            asks("ListWaitingPasswordShares") -> reply("ListWaitingPasswordShares", kotlinx.serialization.builtins.ListSerializer(PasswordWaitingShareView.serializer()), listWaitingPasswordShares())
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
            asks("SetPasswordItemShares") -> reply(
                "SetPasswordItemShares", PasswordItemView.serializer(),
                setPasswordItemShares(
                    text("itemId"),
                    json.decodeFromJsonElement(kotlinx.serialization.builtins.ListSerializer(PasswordShareGranteeInput.serializer()), variables["grantees"]!!),
                    json.decodeFromJsonElement(kotlinx.serialization.builtins.ListSerializer(PasswordShareKeyInput.serializer()), variables["keys"]!!),
                ),
            )
            asks("AddPasswordShareKeys") -> """{"data":{"AddPasswordShareKeys":${
                addPasswordShareKeys(json.decodeFromJsonElement(kotlinx.serialization.builtins.ListSerializer(PasswordItemShareKeyInput.serializer()), variables["keys"]!!))
            }}}"""
            else -> null
        }
    } catch (error: com.teanode.kit.TeaNodeError.Refused) {
        """{"errors":[{"message":${kotlinx.serialization.json.JsonPrimitive(error.reason)}}],"data":null}"""
    }
}
