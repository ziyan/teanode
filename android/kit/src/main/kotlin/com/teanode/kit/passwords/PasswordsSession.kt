package com.teanode.kit.passwords

import com.teanode.kit.TeaNodeError
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.time.Instant
import java.util.UUID

// The Passwords feature as the phone uses it: set up, unlock, read, save, delete, change the settings. This is the only
// place that joins the encryption (PasswordsCrypto.kt), the server (PasswordsApi.kt) and the phone's copy
// (PasswordsCache.kt). Nothing here draws anything; the screens and the autofill service are built on it.
//
// The account key is the one secret that opens everything. It is made on the device, sealed twice (by the passphrase and
// by the recovery key) and sent sealed; it comes back only to someone who proves one of the two. A session holds it in
// memory while unlocked, and the app keeps a copy behind the fingerprint (PasswordsKeyStore in the app).

/** PasswordsError is why a Passwords call failed, where the person has something to do about it. */
sealed class PasswordsError(message: String) : Exception(message) {
    /** The session has no account key: unlock first. */
    data object Locked : PasswordsError("Passwords is locked.")

    /** The cache or the kept key does not open Passwords any more: the passphrase is needed. */
    data object NeedsPassphrase : PasswordsError("Enter your Passwords passphrase.")

    data object NotSetUp : PasswordsError("Passwords is not set up.")

    data object AlreadySetUp : PasswordsError("Passwords is already set up.")

    data object WrongPassphrase : PasswordsError("That passphrase is not right.")

    data object WrongRecoveryKey : PasswordsError("That recovery key is not right.")

    /** The item changed since it was read: read it again, then redo the change. */
    data object ItemChangedOnAnotherDevice : PasswordsError("This item changed on another device. Open it again.")

    data object ItemNotFound : PasswordsError("That item is not here any more.")

    /** Only the person who owns an item changes it, shares it, or lets the agent use it. */
    data object OnlyOwnerCanChangeItem : PasswordsError("Only the person who owns an item can change it.")

    /** An item's kind is fixed when it is made. */
    data object ItemKindCannotChange : PasswordsError("An item cannot change from a login to a note or a card.")

    data object CardCannotBeUsedByAgent : PasswordsError("A card cannot be used by the agent.")

    data object RecipientKeyChangedSinceRead : PasswordsError("That person's key changed since it was read. Read their key again.")

    /** The server counted too many tries (wrong passphrases) and asks for a wait. */
    data object TooManyAttempts : PasswordsError("Too many attempts. Wait a while and try again.")

    data class UnknownItemKind(val itemKind: String) : PasswordsError("This item is of a kind this version cannot open. Update the app.")

    /** The server sent a key or a value that is not the shape it should be. */
    data object InvalidServerAnswer : PasswordsError("The server sent something this app cannot read.")

    /** The server answered a request about one item with another item (another id). Nothing is done with the answer. */
    data object AnsweredAboutAnotherItem : PasswordsError("The server answered about another item.")

    /** The server answered a restore of an earlier version with other sealed fields or another item key than the one listed. */
    data object RestoredAnotherVersion : PasswordsError("The server restored something else.")

    /** The agent was stopped or somebody was removed, but the item could not be given a new key afterwards. The change stands; [PasswordsSession.giveNewKey] tries again. */
    data class ItemNeedsNewKey(val itemId: String) : PasswordsError("Removed. The item could not be given a new key.")

    /** The item's access record is missing, not signed by the person's devices, or older than one this device has seen. */
    data class SharingCouldNotBeChecked(val itemId: String) : PasswordsError("Sharing could not be checked. Set sharing again.")

    companion object {
        /**
         * The sentences the server refuses with, which have a case of their own. The server sends no code beside its
         * sentence, so a refusal is told by the sentence, found anywhere in the reason.
         */
        private val serverSentences: List<Pair<String, PasswordsError>> = listOf(
            "That passphrase is not right." to WrongPassphrase,
            "That recovery key is not right." to WrongRecoveryKey,
            "This item changed on another device." to ItemChangedOnAnotherDevice,
            "That person's key changed since it was read." to RecipientKeyChangedSinceRead,
            "A card cannot be used by the agent." to CardCannotBeUsedByAgent,
            "Passwords is already set up." to AlreadySetUp,
            "Passwords is not set up yet." to NotSetUp,
            "too many attempts, try again later" to TooManyAttempts,
        )

        /** The server's refusals that have a case of their own; any other error is returned as it is. */
        fun mapped(error: Throwable): Throwable {
            if (error !is TeaNodeError.Refused) return error
            for ((sentence, known) in serverSentences) if (error.message?.contains(sentence) == true) return known
            return error
        }
    }
}

/** Whether the access record inside an item is the person's own. Only the person's devices can sign it. */
enum class PasswordsSharingCheck {
    /** The item belongs to somebody else: only its owner can check the record. */
    OwnedByAnotherPerson,

    /** Signed with the person's account key, and not older than the newest this device has seen. */
    Verified,

    /** Missing, or signed by nobody who holds the account key: it reaches nobody. */
    Unverified,

    /** Signed, but older than a record this device saw before, or the same version with another mac. */
    Replayed,
}

/** An item opened: its fields in the clear, and what the server says about it. Nothing here is sealed, so keep it in memory only. */
data class PasswordItem(
    val id: String,
    val fields: PasswordsItemFields,
    val ownerUserId: String,
    val ownerName: String,
    /** Whether somebody else owns it and shared it with the person. */
    val isSharedWithMe: Boolean,
    val revision: Long,
    /** The server says so AND the owner's own record sealed in the item's fields says so AND that record is signed and not an older one. */
    val isUsableByAgent: Boolean,
    val lastUsedByAgentAtEpochMS: Long?,
    /** When the owner deleted it. A deleted item is kept 30 days. */
    val deletedAtEpochMS: Long?,
    val createdAtEpochMS: Long?,
    val updatedAtEpochMS: Long?,
    /** The owner's shares; empty for a shared item. */
    val shares: List<PasswordShareView>,
    /** The server marks an item whose access narrowed while it still has the key somebody held. */
    val needsNewKey: Boolean,
    /** Whether the access record sealed in the fields is the person's own. */
    val sharingCheck: PasswordsSharingCheck,
) {
    /** The access record, when this device checked it. Null otherwise, and a null record reaches nobody. */
    val verifiedAccess: PasswordsItemAccess? get() = if (sharingCheck == PasswordsSharingCheck.Verified) fields.access else null

    /** The shares the server lists that the person's own record lists as well: the only ones that count as "shared with". */
    val recordedShares: List<PasswordShareView> get() = shares.filter { share -> share.isListedIn(verifiedAccess) }

    /** The shares the server lists that the person's own record does not list. */
    val unrecordedShares: List<PasswordShareView> get() = shares.filter { share -> !share.isListedIn(verifiedAccess) }

    val itemKind: PasswordsItemKind get() = fields.itemKind
    val name: String get() = fields.name
    val isDeleted: Boolean get() = deletedAtEpochMS != null
}

/** Whether the person's record lists this share's person or circle as a grantee. */
fun PasswordShareView.isListedIn(access: PasswordsItemAccess?): Boolean {
    if (access == null) return false
    granteeCircleId?.let { circleId -> return access.grantees.contains(PasswordsGrantee.Circle(circleId)) }
    granteeUserId?.let { userId -> return access.grantees.contains(PasswordsGrantee.Person(userId)) }
    return false
}

/** Every item that opened, and the ids of those that did not (damaged, or of a kind this build does not know). */
data class PasswordsOpenedItems(val items: List<PasswordItem>, val unreadableItemIds: List<String>)

/** An earlier version of an item the person owns, opened: its fields in the clear, for the person to read and to put back. */
data class PasswordItemVersion(
    val id: String,
    val savedAtEpochMS: Long?,
    /** Whether the person's own tap saved it, rather than another device or a program. */
    val isSavedByPerson: Boolean,
    val fields: PasswordsItemFields,
) {
    val name: String get() = fields.name
    val itemKind: PasswordsItemKind get() = fields.itemKind
}

/** The earlier versions of an item that opened, newest first, and how many did not. */
data class PasswordsOpenedVersions(val versions: List<PasswordItemVersion>, val unreadableVersionCount: Int)

/** What a right passphrase or recovery key opened. */
class PasswordsUnlocked(
    /** Keep this behind the fingerprint (PasswordsKeyStore) to open Passwords without the passphrase next time. */
    val accountKey: ByteArray,
    val keyPair: PasswordsKeyPair,
)

internal fun serverEpochMS(text: String?): Long? = text?.let { value -> runCatching { Instant.parse(value).toEpochMilli() }.getOrNull() }

/**
 * PasswordsOpener opens items of the cache with the account key. It does no slow key derivation and no networking, so
 * the autofill service can use it.
 */
class PasswordsOpener(
    val userId: String,
    val keyPair: PasswordsKeyPair,
    internal val accountKey: ByteArray,
    /** The one key this device knows for each person, by user id (standard base64). */
    var knownPublicKeys: Map<String, String> = emptyMap(),
    /** The key the person compared safety codes for, by user id. It gates nothing. */
    var comparedPublicKeys: Map<String, String> = emptyMap(),
    /** What this device remembers about each item the person owns. A lower version, or the same with another mac, is a replay. */
    var itemMemories: Map<String, PasswordsItemMemory> = emptyMap(),
) {
    /**
     * The item's key: opened with the account key for an item the person owns, with their key pair and the owner's public
     * key the item carries for one shared with them.
     */
    fun itemKey(view: PasswordItemView): ByteArray {
        if (view.isSharedWithMe) {
            val ownerPublicKey = ownerPublicKey(view)
            val sealed = PasswordsKeyPairSealedValue.fromJsonText(view.itemKey)
            return PasswordsCrypto.openForKeyPair(sealed, keyPair.privateKey, ownerPublicKey, view.ownerUserId, userId, view.id)
        }
        val sealed = PasswordsSealedValue.fromJsonText(view.itemKey)
        return PasswordsCrypto.open(sealed, accountKey, PasswordsCrypto.itemKeyAssociatedData(view.id))
    }

    internal fun ownerPublicKey(view: PasswordItemView): ByteArray {
        val data = runCatching { unbase64(view.ownerPublicKey) }.getOrNull()
        if (data == null || !PasswordsCrypto.isPublicKeyValid(data)) throw PasswordsError.InvalidServerAnswer
        return data
    }

    fun open(view: PasswordItemView): PasswordItem {
        val itemKind = PasswordsItemKind.fromRawValue(view.itemKind) ?: throw PasswordsError.UnknownItemKind(view.itemKind)
        val itemKey = itemKey(view)
        val sealedFields = PasswordsSealedValue.fromJsonText(view.encryptedFields)
        val fields = PasswordsCrypto.openItemFields(sealedFields, itemKey, view.id, itemKind)
        val sharingCheck = sharingCheck(view, fields, itemKind)
        // The agent can use an item only when the owner's own record says so as well, and the record is the person's: a
        // server that sets its flag on an item the owner never let the agent use changes nothing here.
        val isUsableByAgent = sharingCheck == PasswordsSharingCheck.Verified && view.isUsableByAgent && fields.access?.isUsableByAgent == true
        return PasswordItem(
            id = view.id, fields = fields, ownerUserId = view.ownerUserId, ownerName = view.ownerName, isSharedWithMe = view.isSharedWithMe,
            revision = view.revision, isUsableByAgent = isUsableByAgent, lastUsedByAgentAtEpochMS = serverEpochMS(view.lastUsedByAgentAt),
            deletedAtEpochMS = serverEpochMS(view.deletedAt), createdAtEpochMS = serverEpochMS(view.createdAt),
            updatedAtEpochMS = serverEpochMS(view.updatedAt), shares = view.shares, needsNewKey = view.needsNewKey, sharingCheck = sharingCheck,
        )
    }

    /** Whether the item's access record is the person's own. A shared item is never checked here: the key is the owner's. */
    private fun sharingCheck(view: PasswordItemView, fields: PasswordsItemFields, itemKind: PasswordsItemKind): PasswordsSharingCheck {
        if (view.isSharedWithMe) return PasswordsSharingCheck.OwnedByAnotherPerson
        val record = PasswordsCrypto.verifyAccess(accountKey, view.id, itemKind, fields)
        val version = record?.recordVersion ?: return PasswordsSharingCheck.Unverified
        return if (itemMemories[view.id]?.isReplay(version, record.recordMac) == true) PasswordsSharingCheck.Replayed else PasswordsSharingCheck.Verified
    }

    /** Opens an earlier version of an item the person owns. A version of another kind than the item, or sealed for another item, does not open. */
    fun open(version: PasswordItemVersionView, itemId: String, itemKind: PasswordsItemKind): PasswordItemVersion {
        if (version.itemKind != itemKind.rawValue) throw PasswordsError.UnknownItemKind(version.itemKind)
        val sealedKey = PasswordsSealedValue.fromJsonText(version.itemKey)
        val itemKey = PasswordsCrypto.open(sealedKey, accountKey, PasswordsCrypto.itemKeyAssociatedData(itemId))
        val sealedFields = PasswordsSealedValue.fromJsonText(version.encryptedFields)
        val fields = PasswordsCrypto.openItemFields(sealedFields, itemKey, itemId, itemKind)
        return PasswordItemVersion(version.versionId, serverEpochMS(version.savedAt), version.isSavedByPerson, fields)
    }

    /** Opens many; the ones that do not open are named, not hidden. */
    fun openAll(views: List<PasswordItemView>): PasswordsOpenedItems {
        val items = mutableListOf<PasswordItem>()
        val unreadableItemIds = mutableListOf<String>()
        for (view in views) {
            try {
                items.add(open(view))
            } catch (_: Exception) {
                unreadableItemIds.add(view.id)
            }
        }
        return PasswordsOpenedItems(items, unreadableItemIds)
    }

    companion object {
        /** Opens the person's key pair with the account key. A key pair that does not match `expectedPublicKey` is refused. */
        fun fromPrivateKey(
            accountKey: ByteArray,
            userId: String,
            privateKeyByAccountKey: String,
            expectedPublicKey: String? = null,
            pins: PasswordsPins? = null,
        ): PasswordsOpener {
            val sealed = PasswordsSealedValue.fromJsonText(privateKeyByAccountKey)
            val privateKey = PasswordsCrypto.open(sealed, accountKey, PasswordsCrypto.PRIVATE_KEY_ASSOCIATED_DATA)
            val keyPair = PasswordsCrypto.keyPair(privateKey)
            if (expectedPublicKey != null && base64(keyPair.publicKey) != expectedPublicKey) {
                throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyPair)
            }
            return PasswordsOpener(
                userId, keyPair, accountKey, pins?.knownPublicKeys ?: emptyMap(), pins?.comparedPublicKeys ?: emptyMap(),
                pins?.itemMemories ?: emptyMap(),
            )
        }

        /** The safety code of a person's public key given as standard base64; empty when it is not a key. */
        fun safetyCodeText(publicKeyText: String): String =
            runCatching { PasswordsCrypto.safetyCode(unbase64(publicKeyText)) }.getOrDefault("")
    }
}

/** PasswordsSession is Passwords for one signed-in person on this phone. */
class PasswordsSession(
    private val api: PasswordsApi,
    private val cache: PasswordsCache,
    val userId: String,
    private val clock: () -> Long = System::currentTimeMillis,
) {
    private val mutex = Mutex()
    private var contents: PasswordsCacheContents? = null
    private var hasLoadedContents = false
    private var opener: PasswordsOpener? = null
    private var openedItems: MutableMap<String, PasswordItem> = mutableMapOf()

    /** The pins as they were the last time the store could be read, kept for when it cannot be. */
    private var lastReadPins: PasswordsPins? = null

    /** When Passwords was last used, for [isAskRequired]. */
    var lastUsedAtEpochMS: Long? = null
        private set

    /** The sealed fields and item key of each earlier version the server listed for an item, so a restore can check the server restored that and nothing else. */
    private val listedVersionTexts: MutableMap<String, Map<String, Pair<String, String>>> = mutableMapOf()

    val isUnlocked: Boolean get() = opener != null

    /** The lock time in minutes: the server's, as of the last unlock or change. */
    val lockAfterMinutes: Int get() = loadedContents()?.lockAfterMinutes ?: DEFAULT_LOCK_AFTER_MINUTES

    /** Whether Passwords must ask for the passphrase or the fingerprint again, by the lock time. */
    fun isAskRequired(nowEpochMS: Long = clock()): Boolean {
        if (opener == null) return true
        return PasswordsLockPolicy.isAskRequired(lockAfterMinutes, lastUsedAtEpochMS, nowEpochMS)
    }

    /** Forgets the account key and everything opened. The cache stays. */
    fun lock() {
        opener = null
        openedItems = mutableMapOf()
        listedVersionTexts.clear()
        lastUsedAtEpochMS = null
    }

    private fun touch() {
        lastUsedAtEpochMS = clock()
    }

    /** Notes that the person is using Passwords without asking it for anything (browsing, searching). Does nothing while locked. */
    fun noteUse() {
        if (opener != null) touch()
    }

    private fun requireOpener(): PasswordsOpener {
        val current = currentOpener() ?: throw PasswordsError.Locked
        touch()
        return current
    }

    private fun currentOpener(): PasswordsOpener? {
        val current = opener ?: return null
        val pins = currentPins()
        current.knownPublicKeys = pins.knownPublicKeys
        current.comparedPublicKeys = pins.comparedPublicKeys
        current.itemMemories = pins.itemMemories
        return current
    }

    /** The pins as they are now. When the store cannot be read, the pins as they last were; never none, unless none was ever read. */
    private fun currentPins(): PasswordsPins {
        runCatching { cache.loadPins(userId) }.getOrNull()?.let { read ->
            lastReadPins = read
            return read
        }
        return lastReadPins ?: PasswordsPins(userId = userId)
    }

    /** Opens an item. A shared item opens with the key its owner carries on it, which is recorded as the known one once it has opened. */
    private fun openItem(view: PasswordItemView, current: PasswordsOpener): PasswordItem {
        val opened = current.open(view)
        if (view.isSharedWithMe) noteKey(view.ownerUserId, view.ownerPublicKey, current)
        remember(opened)
        return opened
    }

    /** Records a key the server gave for a person and this device used: the first seen becomes the known key; another replaces it and leaves a notice. */
    private fun noteKey(otherUserId: String, publicKey: String, current: PasswordsOpener) {
        val known = current.knownPublicKeys[otherUserId]
        if (known == null) {
            if (runCatching { cache.recordKnownIfAbsent(userId, otherUserId, publicKey) }.isSuccess) {
                current.knownPublicKeys = current.knownPublicKeys + (otherUserId to publicKey)
            }
            return
        }
        if (known == publicKey) return
        if (runCatching { cache.recordChangedKey(userId, otherUserId, publicKey) }.isSuccess) {
            current.knownPublicKeys = current.knownPublicKeys + (otherUserId to publicKey)
        }
    }

    /**
     * Remembers the access record an opened item of the person's shows: its version and mac, once the signature is valid
     * and it is not a replay. The first look at an item that is deleted, or that the server marked as needing a new key,
     * is not remembered: the record on it may be one the person has since narrowed or ended.
     */
    private fun remember(item: PasswordItem) {
        val access = item.fields.access
        val version = access?.recordVersion
        if (item.sharingCheck != PasswordsSharingCheck.Verified || access == null || version == null) return
        val current = lastReadPins?.itemMemories?.get(item.id) ?: PasswordsItemMemory()
        if (current.highestRecordVersion == 0L && (item.isDeleted || item.needsNewKey)) return
        val seen = current.seeingRecord(version, access.recordMac)
        if (seen == current) return
        runCatching { cache.recordSeenRecord(userId, item.id, version, access.recordMac) }
        val pins = lastReadPins ?: return
        lastReadPins = pins.copy(itemMemories = pins.itemMemories + (item.id to seen))
    }

    /** The cache as read from the file once, or as kept in memory since. A file that is damaged, or is another person's, counts as no cache. */
    private fun loadedContents(): PasswordsCacheContents? {
        if (!hasLoadedContents) {
            hasLoadedContents = true
            val stored = runCatching { cache.load() }.getOrNull()
            if (stored != null && stored.userId == userId) contents = stored
        }
        return contents
    }

    private fun itemView(id: String): PasswordItemView? = loadedContents()?.item(id)

    private fun store(updated: PasswordsCacheContents) {
        contents = updated
        hasLoadedContents = true
        cache.save(updated)
    }

    private suspend fun <Answer> call(operation: suspend () -> Answer): Answer {
        try {
            return operation()
        } catch (error: Exception) {
            throw PasswordsError.mapped(error)
        }
    }

    // Setting up

    suspend fun readSetup(): PasswordsSetupView = call { api.readPasswordsSetup() }

    /**
     * Sets Passwords up with a passphrase: makes the account key, the key pair and a recovery key, sends them sealed, and
     * leaves the session unlocked. Returns the recovery key as the person should write it down; it is not kept anywhere
     * and cannot be shown again. `iterationCount` is the slow key derivation's cost; only a test lowers it.
     */
    suspend fun setUp(passphrase: String, iterationCount: Int = PasswordsCrypto.PASSPHRASE_ITERATION_COUNT): String = mutex.withLock {
        val current = readSetup()
        if (current.isSetUp) throw PasswordsError.AlreadySetUp

        val accountKey = PasswordsCrypto.makeKey()
        val keyPair = PasswordsCrypto.makeKeyPair()
        val salt = PasswordsCrypto.randomBytes(PasswordsCrypto.PASSPHRASE_SALT_BYTE_COUNT)
        val passphraseSecret = PasswordsCrypto.passphraseKey(passphrase, salt, iterationCount)
        val recoveryKey = PasswordsCrypto.makeRecoveryKey()
        val privateKeySealed = PasswordsCrypto.seal(keyPair.privateKey, accountKey, PasswordsCrypto.PRIVATE_KEY_ASSOCIATED_DATA)
        val input = PasswordsSetUpInput(
            accountKeyByPassphrase = sealAccountKey(accountKey, passphraseSecret),
            passphraseSalt = base64(salt),
            passphraseIterationCount = iterationCount,
            passphraseProofHash = proofHashText(passphraseSecret),
            accountKeyByRecoveryKey = sealAccountKey(accountKey, recoveryKey),
            recoveryKeyProofHash = proofHashText(recoveryKey),
            publicKey = base64(keyPair.publicKey),
            privateKeyByAccountKey = privateKeySealed.jsonText,
        )
        val setup = call { api.setUpPasswords(input) }
        store(PasswordsCacheContents(userId = userId, publicKey = input.publicKey, privateKeyByAccountKey = input.privateKeyByAccountKey, lockAfterMinutes = setup.lockAfterMinutes))
        opener = PasswordsOpener(userId, keyPair, accountKey)
        openedItems = mutableMapOf()
        touch()
        PasswordsCrypto.formatRecoveryKey(recoveryKey)
    }

    private fun sealAccountKey(accountKey: ByteArray, secret: ByteArray): String =
        PasswordsCrypto.seal(accountKey, PasswordsCrypto.wrapKey(secret), PasswordsCrypto.ACCOUNT_KEY_ASSOCIATED_DATA).jsonText

    private fun proofHashText(secret: ByteArray): String = base64(PasswordsCrypto.proofHash(PasswordsCrypto.proof(secret)))

    // Unlocking

    /** Unlocks with the passphrase. Slow on purpose (the key derivation), so the caller shows that it is working. */
    suspend fun unlockWithPassphrase(passphrase: String): PasswordsUnlocked = mutex.withLock {
        val setup = readSetup()
        if (!setup.isSetUp) throw PasswordsError.NotSetUp
        val saltText = setup.passphraseSalt
        val iterationCount = setup.passphraseIterationCount
        val salt = saltText?.let { text -> runCatching { unbase64(text) }.getOrNull() }
        if (salt == null || iterationCount == null) throw PasswordsError.InvalidServerAnswer
        val secret = PasswordsCrypto.passphraseKey(passphrase, salt, iterationCount)
        finishUnlock(secret, isRecoveryKey = false, setup = setup)
    }

    /**
     * Unlocks with the recovery key as the person wrote it down (spaces, hyphens and letter case do not matter). One that
     * is not a recovery key at all is [PasswordsCryptoFailure.InvalidRecoveryKey].
     */
    suspend fun unlockWithRecoveryKey(recoveryKey: String): PasswordsUnlocked = mutex.withLock {
        val secret = PasswordsCrypto.parseRecoveryKey(recoveryKey)
        val setup = readSetup()
        if (!setup.isSetUp) throw PasswordsError.NotSetUp
        finishUnlock(secret, isRecoveryKey = true, setup = setup)
    }

    private suspend fun finishUnlock(secret: ByteArray, isRecoveryKey: Boolean, setup: PasswordsSetupView): PasswordsUnlocked {
        val proof = base64(PasswordsCrypto.proof(secret))
        val view = call { api.unlockPasswords(proof, isRecoveryKey) }
        val sealedAccountKey = PasswordsSealedValue.fromJsonText(view.accountKeyWrapped)
        val accountKey = PasswordsCrypto.open(sealedAccountKey, PasswordsCrypto.wrapKey(secret), PasswordsCrypto.ACCOUNT_KEY_ASSOCIATED_DATA)
        if (accountKey.size != PasswordsCrypto.KEY_BYTE_COUNT) throw PasswordsError.InvalidServerAnswer
        val newOpener = PasswordsOpener.fromPrivateKey(accountKey, userId, view.privateKeyByAccountKey, view.publicKey)

        // Keep the items of a copy that is the same person's same setup; start over otherwise.
        var updated = PasswordsCacheContents(
            userId = userId, publicKey = view.publicKey, privateKeyByAccountKey = view.privateKeyByAccountKey, lockAfterMinutes = setup.lockAfterMinutes,
        )
        loadedContents()?.let { existing ->
            if (existing.publicKey == view.publicKey) updated = updated.copy(revision = existing.revision, items = existing.items)
        }
        store(updated)
        opener = newOpener
        openedItems = mutableMapOf()
        touch()
        return PasswordsUnlocked(accountKey, newOpener.keyPair)
    }

    /**
     * Unlocks with an account key kept from an earlier unlock (read from the key store behind the fingerprint), using the
     * phone's copy of the key pair. [PasswordsError.NeedsPassphrase] when there is no copy, it is another person's, or the
     * key does not open it.
     */
    fun open(accountKey: ByteArray): PasswordsUnlocked {
        val stored = loadedContents() ?: throw PasswordsError.NeedsPassphrase
        val newOpener = try {
            PasswordsOpener.fromPrivateKey(accountKey, stored.userId, stored.privateKeyByAccountKey, stored.publicKey, currentPins())
        } catch (_: Exception) {
            throw PasswordsError.NeedsPassphrase
        }
        opener = newOpener
        openedItems = mutableMapOf()
        touch()
        return PasswordsUnlocked(accountKey, newOpener.keyPair)
    }

    // Reading

    /**
     * Brings the phone's copy up to date with the server: fetches what changed since the copy's revision, replaces those
     * items and drops the ones the person can no longer reach. With `isFull` it fetches everything. If Passwords was
     * erased or set up again elsewhere, the copy is dropped (the pinned keys of other people stay), the session locks, and
     * this throws [PasswordsError.NotSetUp] or [PasswordsError.NeedsPassphrase].
     */
    suspend fun sync(isFull: Boolean = false): PasswordsMergeResult = mutex.withLock {
        val current = requireOpener()
        val setup = readSetup()
        if (!setup.isSetUp) {
            forgetEverything()
            throw PasswordsError.NotSetUp
        }
        val before = loadedContents() ?: throw PasswordsError.NeedsPassphrase
        if (setup.publicKey != before.publicKey) {
            forgetEverything()
            throw PasswordsError.NeedsPassphrase
        }
        var list = call { api.listPasswordItems(if (isFull) null else before.revision) }
        // The copy may have changed while the answer was on its way.
        var updated = loadedContents() ?: throw PasswordsError.NeedsPassphrase
        // An incremental page carries only items newer than the copy's revision. If the server lists an item the copy does
        // not hold and the page did not bring (the copy is ahead of it), the page is not enough: ask for everything, once.
        if (!isFull && isMissingItem(list.itemIds, updated, list.items)) {
            list = call { api.listPasswordItems(null) }
            updated = loadedContents() ?: throw PasswordsError.NeedsPassphrase
        }
        val (merged, result) = updated.copy(lockAfterMinutes = setup.lockAfterMinutes).merge(list)
        store(merged)
        for (itemId in result.changedItemIds + result.removedItemIds) openedItems.remove(itemId)
        recordOwnerKeys(current)
        result
    }

    private fun isMissingItem(listed: List<String>, contents: PasswordsCacheContents, brought: List<PasswordItemView>): Boolean {
        val heldItemIds = contents.items.map { item -> item.id }.toSet() + brought.map { item -> item.id }
        return listed.any { itemId -> !heldItemIds.contains(itemId) }
    }

    /** Opens one shared item of each owner whose key differs from the one this device knows, so that key is recorded once the item has opened. */
    private fun recordOwnerKeys(current: PasswordsOpener) {
        val stored = loadedContents() ?: return
        val checkedOwnerUserIds = mutableSetOf<String>()
        for (view in stored.items) {
            if (!view.isSharedWithMe || checkedOwnerUserIds.contains(view.ownerUserId)) continue
            if (current.knownPublicKeys[view.ownerUserId] == view.ownerPublicKey) continue
            if (runCatching { openItem(view, current) }.isSuccess) checkedOwnerUserIds.add(view.ownerUserId)
        }
    }

    /** Drops the copy and locks. The pinned keys of other people stay: a server's answer alone must not make the phone forget whose key it knew. */
    private fun forgetEverything(isKeepingPins: Boolean = true) {
        if (isKeepingPins) cache.eraseKeepingPins() else cache.erase()
        contents = null
        hasLoadedContents = true
        lock()
    }

    /** Every item in the phone's copy, opened (those deleted in the last 30 days included), sorted by name. */
    fun list(): PasswordsOpenedItems {
        val current = requireOpener()
        val stored = loadedContents() ?: return PasswordsOpenedItems(emptyList(), emptyList())
        val items = mutableListOf<PasswordItem>()
        val unreadableItemIds = mutableListOf<String>()
        for (view in stored.items) {
            val known = openedItems[view.id]
            if (known != null && known.revision == view.revision) {
                items.add(known)
                continue
            }
            try {
                val opened = openItem(view, current)
                openedItems[view.id] = opened
                items.add(opened)
            } catch (_: Exception) {
                unreadableItemIds.add(view.id)
            }
        }
        items.sortWith(compareBy<PasswordItem, String>(String.CASE_INSENSITIVE_ORDER) { item -> item.name }.thenBy { item -> item.id })
        return PasswordsOpenedItems(items, unreadableItemIds)
    }

    /** One item, opened. [PasswordsError.ItemNotFound] when the copy has no such item. */
    fun item(id: String): PasswordItem {
        val current = requireOpener()
        val view = itemView(id) ?: throw PasswordsError.ItemNotFound
        val known = openedItems[id]
        if (known != null && known.revision == view.revision) return known
        val opened = openItem(view, current)
        openedItems[id] = opened
        return opened
    }

    /** The person's own safety code, from the key pair this session opened at unlock, never from what the server reports. */
    fun ownSafetyCode(): String = PasswordsCrypto.safetyCode(requireOpener().keyPair.publicKey)

    // Changing items

    /**
     * Saves an item. Without `id` it is a new item with a new item key; with one it is a change to an item the person
     * owns, sealed with its existing key (so shares and the agent's use keep working), based on `baseRevision`, which is
     * the item's revision when the person opened it. If the server's revision has moved on, this throws
     * [PasswordsError.ItemChangedOnAnotherDevice]. The kind cannot change. An edit never changes who may have the item or
     * whether the agent may use it: the access record of the fields given is replaced by the item's own (a new item starts
     * with a signed record that grants nobody).
     */
    suspend fun save(fields: PasswordsItemFields, id: String? = null, baseRevision: Long? = null): PasswordItem = mutex.withLock {
        val current = requireOpener()
        val itemId: String
        val itemKey: ByteArray
        val sealedItemKey: String
        val revisionToBaseOn: Long?
        val fieldsToSeal: PasswordsItemFields
        if (id != null) {
            val view = itemView(id) ?: throw PasswordsError.ItemNotFound
            if (view.isSharedWithMe) throw PasswordsError.OnlyOwnerCanChangeItem
            if (view.itemKind != fields.itemKind.rawValue) throw PasswordsError.ItemKindCannotChange
            fieldsToSeal = fields.withAccess(current.open(view).fields.access)
            itemId = id
            itemKey = current.itemKey(view)
            sealedItemKey = view.itemKey
            revisionToBaseOn = baseRevision ?: view.revision
        } else {
            // Lower case, as the server writes it back: the id is part of what is encrypted.
            itemId = UUID.randomUUID().toString().lowercase()
            // A record that grants nobody, signed: an item with none could not be told from one whose record somebody removed.
            fieldsToSeal = PasswordsCrypto.withSignedAccess(current.accountKey, itemId, fields.itemKind, fields, null, 0)
            itemKey = PasswordsCrypto.makeKey()
            sealedItemKey = PasswordsCrypto.seal(itemKey, current.accountKey, PasswordsCrypto.itemKeyAssociatedData(itemId)).jsonText
            revisionToBaseOn = null
        }
        val sealedFields = PasswordsCrypto.sealItemFields(fieldsToSeal, itemKey, itemId)
        val input = PasswordItemSaveInput(itemId, fields.itemKind.rawValue, sealedFields.jsonText, sealedItemKey, revisionToBaseOn)
        val view = call { api.savePasswordItem(input) }
        keep(view, itemId, current)
    }

    /** Deletes an item the person owns. It stays 30 days ([PasswordItem.isDeleted]) and can be restored. */
    suspend fun delete(id: String): PasswordItem = mutex.withLock {
        val current = requireOpener()
        val view = call { api.deletePasswordItem(id) }
        keep(view, id, current)
    }

    /**
     * Deletes an item the person deleted earlier, and its old versions, for good. It cannot be brought back. The server
     * needs the person's own tap (the app's sign-in). [PasswordsError.ItemNotFound] for an item that is not in the
     * person's recently deleted ones.
     */
    suspend fun purge(id: String) = mutex.withLock {
        requireOpener()
        val view = ownedView(id)
        if (view.deletedAt == null) throw PasswordsError.ItemNotFound
        call { api.purgePasswordItem(id) }
        val updated = loadedContents() ?: return@withLock
        store(updated.copy(items = updated.items.filter { item -> item.id != id }))
        openedItems.remove(id)
    }

    /** Brings a deleted item back. */
    suspend fun restore(id: String): PasswordItem = mutex.withLock {
        val current = requireOpener()
        val view = call { api.restorePasswordItem(id) }
        keep(view, id, current)
    }

    // Earlier versions

    /** The earlier versions of an item the person owns, newest first, opened. The server keeps up to 30 days of them and needs the person's own tap. */
    suspend fun versions(itemId: String): PasswordsOpenedVersions = mutex.withLock {
        val current = requireOpener()
        val view = ownedView(itemId)
        val itemKind = PasswordsItemKind.fromRawValue(view.itemKind)
        if (view.deletedAt != null || itemKind == null) throw PasswordsError.ItemNotFound
        val listed = call { api.listPasswordItemVersions(itemId) }
        listedVersionTexts[itemId] = listed.associate { version -> version.versionId to (version.encryptedFields to version.itemKey) }
        val versions = mutableListOf<PasswordItemVersion>()
        var unreadableVersionCount = 0
        val seenVersionIds = mutableSetOf<String>()
        for (version in listed) {
            if (!seenVersionIds.add(version.versionId)) continue
            val opened = runCatching { current.open(version, itemId, itemKind) }.getOrNull()
            if (opened != null) versions.add(opened) else unreadableVersionCount++
        }
        versions.sortByDescending { version -> version.savedAtEpochMS ?: Long.MIN_VALUE }
        PasswordsOpenedVersions(versions, unreadableVersionCount)
    }

    /**
     * Puts an earlier version's content back as the item's own, on the person's tap, and only the content: who the item is
     * shared with and whether the agent may use it stay as they are now.
     *
     * - The version has to be one [versions] listed in this session, and the item's record has to be the person's own now
     *   ([PasswordsError.SharingCouldNotBeChecked]); with either missing nothing is sent.
     * - The server's answer has to carry exactly the sealed fields and item key that were listed; anything else is
     *   [PasswordsError.RestoredAnotherVersion] and nothing more is done with the answer.
     * - The restored fields are then written with the record this device held before the restore, signed one version above
     *   the highest this device has seen, and the item is given a new key at once. The restored version's own record is
     *   never used.
     *
     * If a step after the restore fails, the restored item is returned and [giveNewKey] or the next save tries again.
     */
    suspend fun restoreVersion(itemId: String, versionId: String): PasswordItem = mutex.withLock {
        val current = requireOpener()
        val view = ownedView(itemId)
        if (view.deletedAt != null) throw PasswordsError.ItemNotFound
        val listed = listedVersionTexts[itemId]?.get(versionId) ?: throw PasswordsError.ItemNotFound
        val before = current.open(view)
        val currentAccess = before.verifiedAccess
        if (before.sharingCheck != PasswordsSharingCheck.Verified || currentAccess == null) throw PasswordsError.SharingCouldNotBeChecked(itemId)
        val highestVersion = highestRecordVersion(before)
        val answered = call { api.restorePasswordItemVersion(itemId, versionId, view.revision) }
        if (answered.id != itemId) throw PasswordsError.AnsweredAboutAnotherItem
        if ((answered.encryptedFields to answered.itemKey) != listed) throw PasswordsError.RestoredAnotherVersion
        val kept = keep(answered, itemId, current)
        try {
            val signed = PasswordsCrypto.withSignedAccess(current.accountKey, itemId, kept.itemKind, kept.fields, currentAccess, highestVersion)
            saveFields(signed, ownedView(itemId), current)
            rekey(itemId, AccessEdit.None)
        } catch (error: PasswordsError.Locked) {
            throw error
        } catch (_: Exception) {
            // The restore stands; the item is marked and the person can finish it.
        }
        itemOrNull(itemId) ?: kept
    }

    private fun itemOrNull(id: String): PasswordItem? = runCatching { item(id) }.getOrNull()

    /** Puts a kept item the server answered with into the copy, opens it, and remembers its record. The answer must be about `itemId`. */
    private fun keep(view: PasswordItemView, itemId: String, current: PasswordsOpener): PasswordItem {
        if (view.id != itemId) throw PasswordsError.AnsweredAboutAnotherItem
        val updated = loadedContents() ?: throw PasswordsError.NeedsPassphrase
        store(updated.replacing(view))
        openedItems.remove(view.id)
        val opened = current.open(view)
        openedItems[view.id] = opened
        remember(opened)
        return opened
    }

    private fun ownedView(itemId: String): PasswordItemView {
        val view = itemView(itemId) ?: throw PasswordsError.ItemNotFound
        if (view.isSharedWithMe) throw PasswordsError.OnlyOwnerCanChangeItem
        return view
    }

    // The agent

    /**
     * Lets the agent use an item the person owns, or stops it. The owner's own record, sealed inside the item's fields and
     * signed with the account key, is the consent; the server's flag alone is not.
     *
     * - Turning it on saves the fields with the new record first, then seals the item key for the server's agent key and
     *   tells the server, so the server can open this item and nothing else.
     * - Turning it off tells the server first, which stops the agent at once, then gives the item a new item key whose
     *   fields carry the record with the agent off, so a copy of the old agent key opens nothing saved later. If that
     *   fails after the server was told, this throws [PasswordsError.ItemNeedsNewKey]; [giveNewKey] tries again.
     *
     * A card cannot be used by the agent, and only the owner can allow it. An item whose record was not checked cannot be
     * turned on: [PasswordsError.SharingCouldNotBeChecked].
     */
    suspend fun setUsableByAgent(itemId: String, isUsable: Boolean): PasswordItem = mutex.withLock {
        val current = requireOpener()
        var view = ownedView(itemId)
        // The server keeps no key for the agent on a deleted item; do not ask it to.
        if (view.deletedAt != null) throw PasswordsError.ItemNotFound
        val opened = current.open(view)
        val isRecordOn = opened.verifiedAccess?.isUsableByAgent == true
        if (!isUsable) {
            // Off. Nothing to do when neither the record nor the server says the agent may use it.
            if (!isRecordOn && !view.isUsableByAgent) return@withLock opened
            if (view.isUsableByAgent) {
                val answered = call { api.setPasswordItemUsableByAgent(itemId, false, null) }
                keep(answered, itemId, current)
            }
            return@withLock try {
                rekey(itemId, AccessEdit.AgentUse(false))
            } catch (error: Exception) {
                throw PasswordsError.ItemNeedsNewKey(itemId)
            }
        }
        if (view.itemKind == PasswordsItemKind.Card.rawValue) throw PasswordsError.CardCannotBeUsedByAgent
        // The record is the person's consent: one that was not checked cannot be built on.
        if (opened.sharingCheck != PasswordsSharingCheck.Verified) throw PasswordsError.SharingCouldNotBeChecked(itemId)
        if (!isRecordOn) {
            val newAccess = PasswordsItemAccess.accessWithAgentUse(opened.verifiedAccess, true)
            val signed = PasswordsCrypto.withSignedAccess(current.accountKey, itemId, opened.itemKind, opened.fields, newAccess, highestRecordVersion(opened))
            saveFields(signed, view, current)
            view = ownedView(itemId)
        }
        val sealedItemKey = agentCopy(current.itemKey(view), itemId, current)
        val answered = call { api.setPasswordItemUsableByAgent(itemId, true, sealedItemKey) }
        keep(answered, itemId, current)
    }

    /** The item key sealed for the server's agent key. */
    private suspend fun agentCopy(itemKey: ByteArray, itemId: String, current: PasswordsOpener): String {
        val agentKeyText = call { api.readPasswordsAgentKey() }
        val agentPublicKey = runCatching { unbase64(agentKeyText) }.getOrNull()
        if (agentPublicKey == null || agentPublicKey.size != PasswordsCrypto.PUBLIC_KEY_BYTE_COUNT) throw PasswordsError.InvalidServerAnswer
        return PasswordsCrypto.sealForKeyPair(itemKey, current.keyPair, userId, agentPublicKey, PasswordsCrypto.AGENT_RECIPIENT_ID, itemId).jsonText
    }

    /** The highest access record version this device has seen for an item with a valid signature, counting the record the item shows now. */
    private fun highestRecordVersion(item: PasswordItem): Long =
        maxOf(currentPins().itemMemories[item.id]?.highestRecordVersion ?: 0, item.verifiedAccess?.recordVersion ?: 0)

    /** What a new key changes in the item's access record, on top of the fields it re-seals. */
    private sealed interface AccessEdit {
        data object None : AccessEdit

        data class AgentUse(val isUsableByAgent: Boolean) : AccessEdit
    }

    /**
     * Gives the item a new item key now, because the person asked: the retry after the agent was stopped but the new key
     * could not be made. It seals for the agent too when the person's record says so. [PasswordsError.SharingCouldNotBeChecked]
     * when the item's record is not the person's own.
     */
    suspend fun giveNewKey(itemId: String): PasswordItem = mutex.withLock {
        ownedView(itemId)
        rekey(itemId, AccessEdit.None)
    }

    /** The person's own items the server marked as needing a new key, deleted ones not included. */
    fun itemIdsNeedingNewKey(): List<String> =
        loadedContents()?.items.orEmpty().filter { view -> !view.isSharedWithMe && view.needsNewKey && view.deletedAt == null }.map { view -> view.id }

    /**
     * Makes a new item key for an item the person owns and seals everything with it: the fields (with `accessEdit` applied
     * to their access record and signed again), the person's copy, and the agent's copy when the record says the agent
     * may use the item. Nobody is sealed for but the person and the agent: sharing is not part of this port.
     */
    private suspend fun rekey(itemId: String, accessEdit: AccessEdit): PasswordItem {
        val current = requireOpener()
        val view = ownedView(itemId)
        if (view.deletedAt != null) throw PasswordsError.ItemNotFound
        val opened = current.open(view)
        // A new key given without the person choosing who has access builds on the record, so the record has to be the person's own.
        if (accessEdit is AccessEdit.None && opened.sharingCheck != PasswordsSharingCheck.Verified) throw PasswordsError.SharingCouldNotBeChecked(itemId)
        var fields = opened.fields
        if (accessEdit is AccessEdit.AgentUse) {
            val edited = PasswordsItemAccess.accessWithAgentUse(opened.verifiedAccess, accessEdit.isUsableByAgent)
            fields = PasswordsCrypto.withSignedAccess(current.accountKey, itemId, opened.itemKind, fields, edited, highestRecordVersion(opened))
        }
        val newItemKey = PasswordsCrypto.makeKey()
        val sealedFields = PasswordsCrypto.sealItemFields(fields, newItemKey, itemId)
        val sealedItemKey = PasswordsCrypto.seal(newItemKey, current.accountKey, PasswordsCrypto.itemKeyAssociatedData(itemId))
        var agentCopyText: String? = null
        if (fields.access?.isUsableByAgent == true && view.isUsableByAgent && view.itemKind != PasswordsItemKind.Card.rawValue) {
            agentCopyText = agentCopy(newItemKey, itemId, current)
        }
        val input = PasswordItemRekeyInput(itemId, view.revision, sealedFields.jsonText, sealedItemKey.jsonText, emptyList(), agentCopyText)
        val answered = call { api.rekeyPasswordItem(input) }
        return keep(answered, itemId, current)
    }

    /** Saves fields over an owned item with the key it has, based on the revision the copy holds. */
    private suspend fun saveFields(fields: PasswordsItemFields, view: PasswordItemView, current: PasswordsOpener): PasswordItem {
        val sealedFields = PasswordsCrypto.sealItemFields(fields, current.itemKey(view), view.id)
        val input = PasswordItemSaveInput(view.id, view.itemKind, sealedFields.jsonText, view.itemKey, view.revision)
        val answered = call { api.savePasswordItem(input) }
        return keep(answered, view.id, current)
    }

    // Settings

    /** Changes the passphrase. The account key stays the same, so nothing else is re-sealed. The session must be unlocked. */
    suspend fun changePassphrase(
        currentPassphrase: String,
        newPassphrase: String,
        iterationCount: Int = PasswordsCrypto.PASSPHRASE_ITERATION_COUNT,
    ) = mutex.withLock {
        val current = requireOpener()
        val setup = readSetup()
        if (!setup.isSetUp) throw PasswordsError.NotSetUp
        val salt = setup.passphraseSalt?.let { text -> runCatching { unbase64(text) }.getOrNull() }
        val currentCount = setup.passphraseIterationCount
        if (salt == null || currentCount == null) throw PasswordsError.InvalidServerAnswer
        val currentSecret = PasswordsCrypto.passphraseKey(currentPassphrase, salt, currentCount)
        changePassphrase(currentSecret, false, newPassphrase, iterationCount, current.accountKey)
    }

    /** Changes the passphrase, proving it with the recovery key: for a person who forgot the old one. */
    suspend fun changePassphraseWithRecoveryKey(
        recoveryKey: String,
        newPassphrase: String,
        iterationCount: Int = PasswordsCrypto.PASSPHRASE_ITERATION_COUNT,
    ) = mutex.withLock {
        val current = requireOpener()
        changePassphrase(PasswordsCrypto.parseRecoveryKey(recoveryKey), true, newPassphrase, iterationCount, current.accountKey)
    }

    private suspend fun changePassphrase(proofSecret: ByteArray, isRecoveryKey: Boolean, newPassphrase: String, iterationCount: Int, accountKey: ByteArray) {
        val salt = PasswordsCrypto.randomBytes(PasswordsCrypto.PASSPHRASE_SALT_BYTE_COUNT)
        val newSecret = PasswordsCrypto.passphraseKey(newPassphrase, salt, iterationCount)
        val input = PasswordsChangePassphraseInput(
            proof = base64(PasswordsCrypto.proof(proofSecret)),
            isRecoveryKey = isRecoveryKey,
            accountKeyByPassphrase = sealAccountKey(accountKey, newSecret),
            passphraseSalt = base64(salt),
            passphraseIterationCount = iterationCount,
            passphraseProofHash = proofHashText(newSecret),
        )
        call { api.changePasswordsPassphrase(input) }
    }

    /** Makes a new recovery key (the old one stops working), proving the passphrase. Returns the new key as the person should write it down. */
    suspend fun replaceRecoveryKey(passphrase: String): String = mutex.withLock {
        val current = requireOpener()
        val setup = readSetup()
        if (!setup.isSetUp) throw PasswordsError.NotSetUp
        val salt = setup.passphraseSalt?.let { text -> runCatching { unbase64(text) }.getOrNull() }
        val iterationCount = setup.passphraseIterationCount
        if (salt == null || iterationCount == null) throw PasswordsError.InvalidServerAnswer
        val secret = PasswordsCrypto.passphraseKey(passphrase, salt, iterationCount)
        val recoveryKey = PasswordsCrypto.makeRecoveryKey()
        val input = PasswordsReplaceRecoveryKeyInput(
            proof = base64(PasswordsCrypto.proof(secret)),
            accountKeyByRecoveryKey = sealAccountKey(current.accountKey, recoveryKey),
            recoveryKeyProofHash = proofHashText(recoveryKey),
        )
        call { api.replacePasswordsRecoveryKey(input) }
        PasswordsCrypto.formatRecoveryKey(recoveryKey)
    }

    /** Sets the lock time (1 to 10080 minutes). Returns what the server kept. */
    suspend fun updateLockAfterMinutes(minutes: Int): Int = mutex.withLock {
        requireOpener()
        val setup = call { api.updatePasswordsSettings(minutes) }
        loadedContents()?.let { stored -> store(stored.copy(lockAfterMinutes = setup.lockAfterMinutes)) }
        setup.lockAfterMinutes
    }

    /**
     * Erases Passwords on the server (setup, items, shares, keys), then the phone's copy and the pinned keys (the person
     * asked to start over), and locks. The caller deletes the kept account key from the key store. This cannot be undone.
     */
    suspend fun erase() = mutex.withLock {
        call { api.erasePasswords() }
        forgetEverything(isKeepingPins = false)
    }

    companion object {
        /** The lock time a person starts with, until the server says otherwise. */
        const val DEFAULT_LOCK_AFTER_MINUTES = 15
    }
}
