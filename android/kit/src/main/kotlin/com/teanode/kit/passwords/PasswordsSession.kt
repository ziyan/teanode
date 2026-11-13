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

    /** A new key is wanted but was given a moment ago, or this session already gave the most it gives by itself. The item is listed in [PasswordsSession.itemIdsNeedingAttention]. */
    data class ItemNeedsAttention(val itemId: String) : PasswordsError("This item needs a new key, but one was given a moment ago.")

    /** This device has no memory of the item from before this session or from something it did itself, so it does nothing to it by itself: the person opens the item and finishes the sharing. */
    data class ItemNeedsYou(val itemId: String) : PasswordsError("Open this item to finish sharing it.")

    /** The item is not in the sharing plan the person confirmed. */
    data object ItemNotInSharingPlan : PasswordsError("That item was not in what you confirmed.")

    /** The server marked the item for a new key after the person confirmed a plan made while it was not marked: nothing is sealed and the person confirms a new plan. */
    data object SharingPlanOutOfDate : PasswordsError("Sharing for that item changed after you confirmed. Review who gets it and finish again.")

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

    /** When this session last gave each item a new key by itself, by item id. */
    private val lastAutomaticRekeyAtEpochMS: MutableMap<String, Long> = mutableMapOf()
    private var automaticRekeyCount = 0
    private val heldBackItemIds: MutableSet<String> = mutableSetOf()

    /**
     * The items this device had a memory of when the session read its pins for the first time: from an earlier session. A
     * memory made by looking at an item during this session does not count, because a device that has just met an item
     * cannot tell an old signed record from the newest one.
     */
    private var itemIdsRememberedBefore: Set<String>? = null

    /** The items the person changed or shared on this device during this session: this device's own action is a memory too. */
    private val itemIdsActedOn: MutableSet<String> = mutableSetOf()

    /** The marked items an automatic new key left for the person, because this device has no memory of them. */
    private var keyItemIdsLeftForPerson: MutableSet<String> = mutableSetOf()

    /** The items with a waiting share that completing by itself would have sealed for somebody this device never sealed for, or for an item it has no memory of. */
    private var shareItemIdsLeftForPerson: MutableSet<String> = mutableSetOf()

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
            if (itemIdsRememberedBefore == null) {
                itemIdsRememberedBefore = read.itemMemories.filter { entry -> entry.value.highestRecordVersion > 0 }.keys.toSet()
            }
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

    /**
     * Whether this device may act on an item by itself (a new key given because the server marked it, a waiting share
     * completed): it remembered the item before this session, or the person changed or shared it on this device in this session.
     */
    private fun isRememberedForAutomaticAction(itemId: String): Boolean {
        currentPins()
        return itemIdsActedOn.contains(itemId) || itemIdsRememberedBefore?.contains(itemId) == true
    }

    /** Records what this device sealed for an item itself. Best effort, after the server took the keys: a store that cannot be written makes a later automatic action stricter, never looser. */
    private fun recordSealed(itemId: String, change: PasswordsSealedChange) {
        runCatching { cache.recordSealed(userId, itemId, change) }
        val pins = lastReadPins ?: return
        lastReadPins = pins.copy(itemMemories = pins.itemMemories + (itemId to (pins.itemMemories[itemId] ?: PasswordsItemMemory()).sealing(change)))
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
    suspend fun sync(isFull: Boolean = false): PasswordsMergeResult = mutex.withLock { syncLocked(isFull) }

    private suspend fun syncLocked(isFull: Boolean = false): PasswordsMergeResult {
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
        return result
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
            // The key this item has was known to somebody who lost access to it: this save is the chance to give it a new one,
            // with the new fields sealed by it, as far as a new key given by itself may go. When it may not, the fields are
            // sealed with the key the item has.
            if (view.needsNewKey) {
                try {
                    return@withLock rekey(id, fieldsToSeal, baseRevision ?: view.revision, AccessEdit.None, RekeyOrigin.Automatic)
                } catch (error: PasswordsError.ItemNeedsAttention) {
                    // Saved below, with the key the item has.
                } catch (error: PasswordsError.SharingCouldNotBeChecked) {
                    // Saved below.
                } catch (error: PasswordsError.ItemNeedsYou) {
                    // Saved below.
                }
            }
            itemId = id
            itemKey = current.itemKey(view)
            sealedItemKey = view.itemKey
            revisionToBaseOn = baseRevision ?: view.revision
        } else {
            // Lower case, as the server writes it back: the id is part of what is encrypted.
            itemId = UUID.randomUUID().toString().lowercase()
            itemIdsActedOn.add(itemId)
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

    /**
     * Brings a deleted item back. The server keeps its mark when the item had to be given a new key (it had shares or an
     * agent copy when it was deleted), and the item is given one now, sealed only for people (and the agent) this device
     * itself sealed it for before: a device that never saw the item may be looking at a record from before the person
     * narrowed it, so the rest waits for "Finish sharing". If the new key fails the restored item is returned and
     * [giveNewKey] or the next save tries again.
     */
    suspend fun restore(id: String): PasswordItem = mutex.withLock {
        val current = requireOpener()
        val view = call { api.restorePasswordItem(id) }
        val kept = keep(view, id, current)
        if (!view.needsNewKey || view.isSharedWithMe) return@withLock kept
        try {
            rekey(id, null, null, AccessEdit.None, RekeyOrigin.Restore)
        } catch (_: Exception) {
            kept
        }
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
        // Asked before the restore, which makes this device remember the item.
        val isRemembered = isRememberedForAutomaticAction(itemId)
        val answered = call { api.restorePasswordItemVersion(itemId, versionId, view.revision) }
        if (answered.id != itemId) throw PasswordsError.AnsweredAboutAnotherItem
        if ((answered.encryptedFields to answered.itemKey) != listed) throw PasswordsError.RestoredAnotherVersion
        val kept = keep(answered, itemId, current)
        if (isRemembered) itemIdsActedOn.add(itemId)
        try {
            val signed = PasswordsCrypto.withSignedAccess(current.accountKey, itemId, kept.itemKind, kept.fields, currentAccess, highestVersion)
            saveFields(signed, ownedView(itemId), current)
            rekey(itemId, null, null, AccessEdit.None, if (isRemembered) RekeyOrigin.Request else RekeyOrigin.Restore)
        } catch (error: PasswordsError.Locked) {
            throw error
        } catch (_: Exception) {
            // The restore stands; the item is marked and the person can finish it.
        }
        item(itemId)
    }

    /** Puts a kept item the server answered with into the copy, opens it, and remembers its record. The answer must be about `itemId`. */
    private fun keep(view: PasswordItemView, itemId: String, current: PasswordsOpener, isRemembering: Boolean = true): PasswordItem {
        if (view.id != itemId) throw PasswordsError.AnsweredAboutAnotherItem
        val updated = loadedContents() ?: throw PasswordsError.NeedsPassphrase
        store(updated.replacing(view))
        openedItems.remove(view.id)
        val opened = current.open(view)
        openedItems[view.id] = opened
        if (isRemembering) remember(opened)
        return opened
    }

    private fun ownedView(itemId: String): PasswordItemView {
        val view = itemView(itemId) ?: throw PasswordsError.ItemNotFound
        if (view.isSharedWithMe) throw PasswordsError.OnlyOwnerCanChangeItem
        return view
    }

    // Sharing

    /**
     * The people a share with this person or circle would reach, with the safety code of each one's public key, whether it
     * is the key this device knows ([PasswordRecipient.keyTrust]) and whether the person compared codes. A person who has
     * not set up Passwords has no key yet.
     */
    suspend fun recipients(granteeUserId: String? = null, granteeCircleId: String? = null): List<PasswordRecipient> = mutex.withLock {
        requireOpener()
        val views = call { api.listPasswordRecipients(granteeUserId, granteeCircleId) }
        makeRecipients(views, granteeCircleId?.let { circleId -> listOf(circleId) } ?: emptyList())
    }

    /** The same recipients as [recipients] for views the server already sent (the owner's `shares` of an item); `circleId` is the circle of the share they came from, if it is a circle's. */
    fun recipients(views: List<PasswordRecipientView>, circleId: String? = null): List<PasswordRecipient> =
        makeRecipients(views, circleId?.let { listOf(it) } ?: emptyList())

    /** Recipients marked with what this device knows of each key. */
    private fun makeRecipients(views: List<PasswordRecipientView>, circleIds: List<String>): List<PasswordRecipient> {
        val pins = currentPins()
        return views.map { view ->
            PasswordRecipient(view, pins.knownPublicKeys[view.userId], pins.comparedPublicKeys[view.userId], circleIds)
        }
    }

    /** A recipient marked with what the opener knows of their key. */
    private fun recipientOf(view: PasswordRecipientView, current: PasswordsOpener): PasswordRecipient =
        PasswordRecipient(view, current.knownPublicKeys[view.userId], current.comparedPublicKeys[view.userId])

    /**
     * Replaces who the item is shared with by `grantees`. The owner's own record decides who may have the item: the list of
     * grantees is sealed inside the item's fields (`access`, signed with the account key), and a key is sealed for a person
     * only if that record reaches them and they have a key. The calls go in the order that keeps the sealed record from
     * ever being wider than what the person chose:
     *
     * - When somebody is added, the fields are saved with the new, signed record first, then the server is told with the
     *   keys sealed for the new recipients.
     * - When somebody is removed (the record before has a grantee the record now lacks, or the server lists a share the new
     *   list does not have, whatever the server answers), the server is told, then the item is given a new item key whose
     *   fields carry the narrower record, so the removed person's old copy opens nothing saved later. If that fails after
     *   the removal went through, this throws [PasswordsError.ItemNeedsNewKey]: the removal stands, the server marks the
     *   item, and [giveNewKey] tries again.
     *
     * An item whose record was not checked is treated as shared with nobody: this call writes a fresh signed record with the
     * grantees given, which is how the person sets sharing again, and gives the item a new key when the server lists any
     * share or agent copy for it, because whoever wrote the record may hold the old key.
     *
     * `recipients` are the people the grantees reach (from [recipients]). Each one the record reaches who has a key gets a
     * key sealed for the public key the server gives, at once: a first-seen key is recorded as the known one, and a key that
     * differs from the known one is accepted and recorded with a notice ([keyChanges]). A recipient that already has a key
     * is not sealed for again. A recipient without a public key is left out and shows up in [waitingShares]. Only the owner
     * shares; a recipient never shares onward.
     */
    suspend fun setShares(itemId: String, grantees: List<PasswordShareGranteeInput>, recipients: List<PasswordRecipient>): PasswordItem = mutex.withLock {
        var current = requireOpener()
        var view = ownedView(itemId)
        if (view.deletedAt != null) throw PasswordsError.ItemNotFound
        itemIdsActedOn.add(itemId)
        val previouslySealedUserIds = currentPins().itemMemories[itemId]?.sealedForUserIds?.toSet() ?: emptySet()
        // An item that still has the key somebody lost access to is given a new one before anything else, as far as one given by itself may go.
        if (view.needsNewKey && rekeyIfAllowed(itemId)) {
            current = requireOpener()
            view = ownedView(itemId)
        }
        val opened = current.open(view)
        val isRecordChecked = opened.sharingCheck == PasswordsSharingCheck.Verified
        val oldGrantees = opened.verifiedAccess?.grantees ?: emptyList()
        val newGrantees = mutableListOf<PasswordsGrantee>()
        val chosenInputs = mutableListOf<PasswordShareGranteeInput>()
        for (input in grantees) {
            val grantee = granteeOf(input) ?: continue
            if (newGrantees.contains(grantee)) continue
            newGrantees.add(grantee)
            chosenInputs.add(input)
        }
        val newAccess = PasswordsItemAccess.accessWithGrantees(opened.verifiedAccess, newGrantees)
        val isNarrowing = oldGrantees.any { grantee -> !newGrantees.contains(grantee) }
        val isWidening = newGrantees.any { grantee -> !oldGrantees.contains(grantee) }
        // The server lists somebody the new list leaves out, whatever the record said.
        val isRemovingListedShare = view.shares.any { share ->
            val grantee = granteeOf(share)
            grantee != null && !newGrantees.contains(grantee) && share.recipients.isNotEmpty()
        }
        // A record that was not checked may have been written by somebody who holds the item key.
        val isReplacingUntrustedRecord = !isRecordChecked && (view.shares.isNotEmpty() || view.isUsableByAgent)

        if (isWidening || !isRecordChecked) {
            val signed = PasswordsCrypto.withSignedAccess(current.accountKey, itemId, opened.itemKind, opened.fields, newAccess, highestRecordVersion(opened))
            saveFields(signed, view, current)
            view = ownedView(itemId)
        }

        val itemKey = current.itemKey(view)
        val alreadyKeyedUserIds = view.shares.flatMap { share -> share.recipients }.filter { recipient -> recipient.hasKey }.map { recipient -> recipient.userId }.toSet()
        val circleIdsByUserId = mutableMapOf<String, List<String>>()
        for (recipient in recipients) circleIdsByUserId[recipient.userId] = (circleIdsByUserId[recipient.userId] ?: emptyList()) + recipient.circleIds
        val keys = mutableListOf<PasswordShareKeyInput>()
        val sealedKeys = linkedMapOf<String, String>()
        val sealedUserIds = mutableSetOf<String>()
        for (recipient in recipients) {
            if (recipient.userId == userId || alreadyKeyedUserIds.contains(recipient.userId)) continue
            val publicKey = recipient.publicKey ?: continue
            if (recipient.hasTooMuchShared) continue
            if (!sealedUserIds.add(recipient.userId)) continue
            // The record the owner sealed decides, not the server's list of shares.
            val circleIds = (circleIdsByUserId[recipient.userId] ?: emptyList()) + circleIdsListing(recipient.userId, view)
            if (!PasswordsItemAccess.isReachedByAccess(newAccess, recipient.userId, circleIds)) continue
            keys.add(shareKey(itemKey, itemId, current.keyPair, recipient.userId, publicKey, view.itemKey))
            sealedKeys[recipient.userId] = base64(publicKey)
        }
        // The keys sealed for are recorded before the server is told, so a pin store that cannot be written stops the share before anything is sealed for them.
        recordSealedKeys(sealedKeys)
        val answered = call { api.setPasswordItemShares(itemId, chosenInputs, keys) }
        val kept = keep(answered, itemId, current)
        recordSealed(
            itemId,
            PasswordsSealedChange(
                userIds = sealedKeys.keys.toList(),
                isReplacing = false,
                chosenUserIds = newGrantees.filterIsInstance<PasswordsGrantee.Person>().map { grantee -> grantee.userId },
                circleShares = circleSharesFor(sealedKeys.keys.toList(), previouslySealedUserIds, answered, newAccess),
            ),
        )
        // Somebody lost access, and they may hold the item key: it gets replaced. The person's own record says so; a server
        // that answers with the shares unchanged cannot prevent it. A mark the server set on its own is followed as well,
        // which only ever re-keys more.
        if (!(isNarrowing || isRemovingListedShare || isReplacingUntrustedRecord || answered.needsNewKey)) return@withLock kept
        try {
            rekey(itemId, null, null, AccessEdit.Grantees(newGrantees), RekeyOrigin.Choice)
        } catch (_: Exception) {
            throw PasswordsError.ItemNeedsNewKey(itemId)
        }
    }

    /** The grantee of the access record that a share target is. */
    private fun granteeOf(input: PasswordShareGranteeInput): PasswordsGrantee? {
        val userIdText = input.granteeUserId
        val circleIdText = input.granteeCircleId
        return when {
            userIdText != null && circleIdText == null && userIdText.isNotEmpty() -> PasswordsGrantee.Person(userIdText)
            userIdText == null && circleIdText != null && circleIdText.isNotEmpty() -> PasswordsGrantee.Circle(circleIdText)
            else -> null
        }
    }

    /** The grantee of a share the server lists. */
    private fun granteeOf(share: PasswordShareView): PasswordsGrantee? {
        share.granteeCircleId?.let { circleId -> return PasswordsGrantee.Circle(circleId) }
        share.granteeUserId?.let { shareUserId -> return PasswordsGrantee.Person(shareUserId) }
        return null
    }

    /** The circles the server lists a person as a member of among the shares the copy holds of an item. */
    private fun circleIdsListing(recipientUserId: String, view: PasswordItemView): List<String> =
        view.shares.mapNotNull { share ->
            val circleId = share.granteeCircleId ?: return@mapNotNull null
            if (share.recipients.any { member -> member.userId == recipientUserId }) circleId else null
        }

    /**
     * The people among `userIds` who are reached only through a circle of the record and whom this device never sealed the
     * item for before: the ones the person is told about. Who is in a circle is the server's word, so this is the one place
     * the person is shown where it was relied on. A person the record names itself is not listed.
     */
    private fun circleSharesFor(
        userIds: List<String>,
        previouslySealedUserIds: Set<String>,
        view: PasswordItemView,
        access: PasswordsItemAccess?,
    ): List<PasswordsCircleShare> {
        if (access == null) return emptyList()
        val circleShares = mutableListOf<PasswordsCircleShare>()
        for (recipientUserId in userIds.sorted()) {
            if (previouslySealedUserIds.contains(recipientUserId)) continue
            if (access.grantees.contains(PasswordsGrantee.Person(recipientUserId))) continue
            for (share in view.shares) {
                val circleId = share.granteeCircleId ?: continue
                if (!access.grantees.contains(PasswordsGrantee.Circle(circleId))) continue
                val member = share.recipients.firstOrNull { recipient -> recipient.userId == recipientUserId } ?: continue
                circleShares.add(PasswordsCircleShare(recipientUserId, member.name, share.granteeName))
                break
            }
        }
        return circleShares
    }

    /** The highest access record version this device has seen for an item with a valid signature, counting the record the item shows now. */
    private fun highestRecordVersion(item: PasswordItem): Long =
        maxOf(currentPins().itemMemories[item.id]?.highestRecordVersion ?: 0, item.verifiedAccess?.recordVersion ?: 0)

    private fun sealedForRecipient(itemKey: ByteArray, itemId: String, ownerKeyPair: PasswordsKeyPair, recipientUserId: String, recipientPublicKey: ByteArray): String =
        PasswordsCrypto.sealForKeyPair(itemKey, ownerKeyPair, userId, recipientPublicKey, recipientUserId, itemId).jsonText

    /**
     * The key for `setPasswordItemShares`. `accountSealedItemKey` is the item key sealed with the account key, exactly as
     * the item showed it when this device opened it to seal this key: the server refuses the key when the item has another by now.
     */
    private fun shareKey(
        itemKey: ByteArray,
        itemId: String,
        ownerKeyPair: PasswordsKeyPair,
        recipientUserId: String,
        recipientPublicKey: ByteArray,
        accountSealedItemKey: String,
    ): PasswordShareKeyInput = PasswordShareKeyInput(
        recipientUserId = recipientUserId,
        itemKeyByRecipientKey = sealedForRecipient(itemKey, itemId, ownerKeyPair, recipientUserId, recipientPublicKey),
        recipientPublicKey = base64(recipientPublicKey),
        itemKey = accountSealedItemKey,
    )

    private fun itemShareKey(
        itemKey: ByteArray,
        itemId: String,
        ownerKeyPair: PasswordsKeyPair,
        recipient: PasswordRecipient,
        recipientPublicKey: ByteArray,
        accountSealedItemKey: String? = null,
    ): PasswordItemShareKeyInput = PasswordItemShareKeyInput(
        itemId = itemId,
        recipientUserId = recipient.userId,
        itemKeyByRecipientKey = sealedForRecipient(itemKey, itemId, ownerKeyPair, recipient.userId, recipientPublicKey),
        recipientPublicKey = base64(recipientPublicKey),
        itemKey = accountSealedItemKey,
    )

    /**
     * The people whose key changed from the one this device knew (as an item's owner, or as a recipient the person shared
     * with), each once, until the person dismisses the notice. The new key was accepted: nothing waits on the notice.
     */
    fun keyChanges(): List<PasswordsKeyChange> {
        val pins = currentPins()
        val names = mutableMapOf<String, String>()
        for (view in loadedContents()?.items.orEmpty()) {
            if (view.isSharedWithMe) names[view.ownerUserId] = view.ownerName
            for (recipient in view.shares.flatMap { share -> share.recipients }) names[recipient.userId] = recipient.name
        }
        val changes = mutableListOf<PasswordsKeyChange>()
        for ((otherUserId, previousPublicKey) in pins.changedPublicKeys) {
            val currentPublicKey = pins.knownPublicKeys[otherUserId] ?: continue
            changes.add(
                PasswordsKeyChange(
                    userId = otherUserId,
                    name = names[otherUserId] ?: "",
                    oldSafetyCode = PasswordsOpener.safetyCodeText(previousPublicKey),
                    newSafetyCode = PasswordsOpener.safetyCodeText(currentPublicKey),
                    newPublicKey = currentPublicKey,
                ),
            )
        }
        return changes.sortedWith(compareBy<PasswordsKeyChange> { change -> change.name }.thenBy { change -> change.userId })
    }

    /** The people this device sealed an item for through a circle for the first time, each until the person dismisses the notice. An item that is not in the copy any more is left out. */
    fun circleShareNotices(): List<PasswordsCircleShareNotice> {
        val pins = currentPins()
        val current = opener ?: return emptyList()
        val held = loadedContents() ?: return emptyList()
        val notices = mutableListOf<PasswordsCircleShareNotice>()
        for ((itemId, memory) in pins.itemMemories) {
            if (memory.circleShares.isEmpty()) continue
            val view = held.item(itemId) ?: continue
            if (view.deletedAt != null) continue
            val opened = runCatching { current.open(view) }.getOrNull() ?: continue
            for (share in memory.circleShares) {
                notices.add(PasswordsCircleShareNotice(itemId, opened.name, share.recipientUserId, share.recipientName, share.circleName))
            }
        }
        return notices.sortedWith(
            compareBy<PasswordsCircleShareNotice> { notice -> notice.recipientName }.thenBy { notice -> notice.itemName }.thenBy { notice -> notice.id },
        )
    }

    /** Dismisses the notice that somebody got an item through a circle. */
    fun dismissCircleShareNotice(itemId: String, recipientUserId: String) {
        requireOpener()
        cache.dismissCircleShare(userId, itemId, recipientUserId)
        val pins = lastReadPins ?: return
        val memory = pins.itemMemories[itemId] ?: return
        lastReadPins = pins.copy(
            itemMemories = pins.itemMemories + (itemId to memory.copy(circleShares = memory.circleShares.filter { share -> share.recipientUserId != recipientUserId })),
        )
    }

    /** Dismisses the notice of a changed key. The key stays accepted. */
    fun dismissKeyChange(otherUserId: String) {
        requireOpener()
        cache.dismissChangedKey(userId, otherUserId)
    }

    /**
     * Marks the key as compared after the person read the safety codes with its owner. `publicKey` is the key whose code the
     * person was shown (standard base64); it is recorded as the known key when the device does not know it yet or knew
     * another, and the person's changed-key notice is cleared. It never gates sharing or opening.
     */
    fun markCompared(otherUserId: String, publicKey: String) {
        requireOpener()
        val data = runCatching { unbase64(publicKey) }.getOrNull()
        if (data == null || !PasswordsCrypto.isPublicKeyValid(data) || otherUserId == userId) throw PasswordsError.InvalidServerAnswer
        val known = currentPins().knownPublicKeys[otherUserId]
        if (known != null && known != publicKey) cache.recordChangedKey(userId, otherUserId, publicKey)
        cache.recordCompared(userId, otherUserId, publicKey)
        openedItems = mutableMapOf()
    }

    /**
     * Records the keys (user id to standard base64) this device is about to seal item keys for: the first one seen for a
     * person becomes their known key, and one that differs from the known key replaces it and leaves a notice. Nothing is asked.
     */
    private fun recordSealedKeys(keys: Map<String, String>) {
        val pins = currentPins()
        for ((otherUserId, publicKey) in keys.toSortedMap()) {
            val known = pins.knownPublicKeys[otherUserId]
            if (known == null) {
                cache.recordKnownIfAbsent(userId, otherUserId, publicKey)
                continue
            }
            if (known != publicKey) cache.recordChangedKey(userId, otherUserId, publicKey)
        }
    }

    /** The recipients of the person's items who cannot open them yet, each with what the device knows of their key. */
    suspend fun waitingShares(): List<PasswordWaitingShare> = mutex.withLock {
        val current = requireOpener()
        val views = call { api.listWaitingPasswordShares() }
        views.map { view ->
            val name = itemView(view.itemId)?.let { held -> runCatching { current.open(held).name }.getOrNull() }
            PasswordWaitingShare(view.itemId, name, recipientOf(view.recipient, current), view.hasMoreWaiting)
        }
    }

    /** What [completeWaitingShares] works out from the server's list of waiting recipients and the copy. */
    private class WaitingPlan {
        val keys = mutableListOf<PasswordItemShareKeyInput>()

        /** The key each recipient sealed for was given, user id to standard base64. */
        val sealedKeys = linkedMapOf<String, String>()

        /** The people sealed for in each item, by item id. */
        val sealedUserIdsByItemId = linkedMapOf<String, MutableList<String>>()

        /** The people sealed for through a circle for the first time, by item id. */
        val circleSharesByItemId = linkedMapOf<String, MutableList<PasswordsCircleShare>>()

        /** The items with somebody waiting that this plan did not seal for because it is not the person's own action and this device cannot vouch for it. */
        val itemIdsLeftForPerson = mutableSetOf<String>()

        /** The server names an item the copy does not hold, or a person the owner's record would reach but the copy's shares do not list. */
        var isCopyBehind = false

        /** The names of waiting people who have too much shared with them already: skipped. */
        val tooMuchSharedNames = mutableSetOf<String>()
    }

    /**
     * `isByPerson` is whether the person started this (they opened the item and pressed the button): then every waiting
     * recipient the owner's verified record reaches is sealed for. Otherwise the device acts by itself and seals only for an
     * item it remembers and for people it sealed that item for before; the rest is left for the person.
     */
    private fun waitingPlan(waiting: List<PasswordWaitingShareView>, current: PasswordsOpener, isByPerson: Boolean): WaitingPlan {
        val plan = WaitingPlan()
        val memories = currentPins().itemMemories
        for (entry in waiting) {
            val recipient = recipientOf(entry.recipient, current)
            val publicKey = recipient.publicKey ?: continue
            if (recipient.userId == userId) continue
            val view = itemView(entry.itemId)
            if (view == null) {
                plan.isCopyBehind = true
                continue
            }
            if (view.isSharedWithMe || view.deletedAt != null) continue
            val opened = runCatching { current.open(view) }.getOrNull() ?: continue
            // A record that is not the person's own (missing, not signed by them, or an older one the server sent back) reaches nobody: nothing is sealed from it.
            val access = opened.verifiedAccess ?: continue
            val circleIds = circleIdsListing(recipient.userId, view)
            if (!PasswordsItemAccess.isReachedByAccess(access, recipient.userId, circleIds)) {
                // The server lists somebody the owner's record does not reach: nothing is sealed. But if the record would
                // reach them as a member of one of its circles, the copy of the circle may be old.
                val circleIdsOfRecord = access.grantees.filterIsInstance<PasswordsGrantee.Circle>().map { grantee -> grantee.circleId }
                if (PasswordsItemAccess.isReachedByAccess(access, recipient.userId, circleIdsOfRecord)) plan.isCopyBehind = true
                continue
            }
            val itemKey = runCatching { current.itemKey(view) }.getOrNull() ?: continue
            // The server stores no key for somebody who has too much shared with them: sealing one would only be tried again on every pass.
            if (recipient.hasTooMuchShared) {
                plan.tooMuchSharedNames.add(recipient.name)
                continue
            }
            val memory = memories[entry.itemId] ?: PasswordsItemMemory()
            if (!isByPerson && !(isRememberedForAutomaticAction(entry.itemId) && (memory.sealedForUserIds.contains(recipient.userId) || memory.chosenUserIds.contains(recipient.userId)))) {
                plan.itemIdsLeftForPerson.add(entry.itemId)
                continue
            }
            plan.sealedKeys[recipient.userId] = base64(publicKey)
            plan.sealedUserIdsByItemId.getOrPut(entry.itemId) { mutableListOf() }.add(recipient.userId)
            plan.circleSharesByItemId.getOrPut(entry.itemId) { mutableListOf() }
                .addAll(circleSharesFor(listOf(recipient.userId), memory.sealedForUserIds.toSet(), view, access))
            plan.keys.add(itemShareKey(itemKey, entry.itemId, current.keyPair, recipient, publicKey, view.itemKey))
        }
        return plan
    }

    /**
     * Seals the keys of items whose recipients were waiting (they set up Passwords, or set it up again with a new key, after
     * the share was made) for their current public keys, at once, but only when the owner's sealed record reaches the
     * recipient (a person grantee by user id, a circle grantee when the server lists them as a member). The key given for a
     * first-seen person is recorded as their known key; one that differs from the known key replaces it and leaves a notice.
     *
     * Done by itself, this seals only for an item the device remembers from before this session or from something it did
     * itself, and only for people it sealed that item for before or that the person named directly when they set who it is
     * shared with on this device (a circle's members are the server's word, so a member who joined later is not one of
     * them). Everything else is left for the person ([itemIdsNeedingYou], [finishSharing]): a device that has never seen an
     * item cannot tell an old signed record from the newest one, and a person added to an old record is exactly what an old
     * record would bring back. When the server names an item the copy does not hold, or somebody the record would reach but
     * the copy's shares do not list, the copy is read in full once and the list checked again.
     */
    suspend fun completeWaitingShares(): PasswordsCompletedShares = mutex.withLock {
        completeWaitingSharesLocked(null, null, isByPerson = false)
    }

    /** The same for one item, started by the person: every waiting recipient the owner's verified record reaches gets a key, whether or not this device sealed for them before. */
    suspend fun completeWaitingShares(onlyItemId: String): PasswordsCompletedShares = mutex.withLock {
        itemIdsActedOn.add(onlyItemId)
        completeWaitingSharesLocked(onlyItemId, null, isByPerson = true)
    }

    private suspend fun completeWaitingSharesLocked(onlyItemId: String?, onlyUserIds: Set<String>?, isByPerson: Boolean): PasswordsCompletedShares {
        var isFullReadDone = false
        var isRetriedAfterChange = false
        while (true) {
            val current = requireOpener()
            var waiting = call { api.listWaitingPasswordShares() }
            val hasMoreWaiting = waiting.any { entry -> entry.hasMoreWaiting }
            if (onlyItemId != null) waiting = waiting.filter { entry -> entry.itemId == onlyItemId }
            if (onlyUserIds != null) waiting = waiting.filter { entry -> onlyUserIds.contains(entry.recipient.userId) }
            val plan = waitingPlan(waiting, current, isByPerson)
            val tooMuchSharedNames = plan.tooMuchSharedNames.sorted()
            if (plan.isCopyBehind && !isFullReadDone) {
                isFullReadDone = true
                syncLocked(isFull = true)
                continue
            }
            if (onlyItemId != null) shareItemIdsLeftForPerson.remove(onlyItemId) else {
                shareItemIdsLeftForPerson = plan.itemIdsLeftForPerson.toMutableSet()
            }
            if (plan.keys.isEmpty()) return PasswordsCompletedShares(0, tooMuchSharedNames, hasMoreWaiting)
            recordSealedKeys(plan.sealedKeys)
            try {
                val addedKeyCount = call { api.addPasswordShareKeys(plan.keys) }
                // The server stores at most the keys it was given: a count past that is not about them.
                if (addedKeyCount < 0 || addedKeyCount > plan.keys.size) throw PasswordsError.InvalidServerAnswer
                for ((sealedItemId, sealedUserIds) in plan.sealedUserIdsByItemId) {
                    recordSealed(sealedItemId, PasswordsSealedChange(sealedUserIds, isReplacing = false, circleShares = plan.circleSharesByItemId[sealedItemId] ?: emptyList()))
                }
                // The copy's shares still say these people have no key: read the changed items again.
                if (addedKeyCount > 0) runCatching { syncLocked() }
                return PasswordsCompletedShares(addedKeyCount, tooMuchSharedNames, hasMoreWaiting)
            } catch (error: PasswordsError.ItemChangedOnAnotherDevice) {
                if (isRetriedAfterChange) throw error
                // An item got a new key since the copy was made: read it again, then seal from that.
                isRetriedAfterChange = true
                syncLocked()
            }
        }
    }

    // The agent

    /**
     * Lets the agent use one item, or stops it. The owner's record sealed in the item's fields (`access.isUsableByAgent`) is
     * what says so; the server's flag follows it, and the agent's opener refuses an item whose record says no. The calls go
     * in the order that keeps the record from ever being wider than what the person chose:
     *
     * - Turning it on saves the fields with the new record first, then seals the item key for the server's agent key and
     *   tells the server, so the server can open this item and nothing else.
     * - Turning it off tells the server first, which stops the agent at once, then gives the item a new item key whose
     *   fields carry the record with the agent off, so a copy of the old agent key opens nothing saved later. If that
     *   fails after the server was told, this throws [PasswordsError.ItemNeedsNewKey]; [giveNewKey] tries again.
     *
     * A card cannot be used by the agent, and only the owner can allow it. An item whose record was not checked cannot be
     * turned on: [PasswordsError.SharingCouldNotBeChecked], until the person sets sharing again with [setShares].
     */
    suspend fun setUsableByAgent(itemId: String, isUsable: Boolean): PasswordItem = mutex.withLock {
        setUsableByAgentLocked(itemId, isUsable)
    }

    private suspend fun setUsableByAgentLocked(itemId: String, isUsable: Boolean): PasswordItem {
        var current = requireOpener()
        var view = ownedView(itemId)
        // The server keeps no key for the agent on a deleted item; do not ask it to.
        if (view.deletedAt != null) throw PasswordsError.ItemNotFound
        itemIdsActedOn.add(itemId)
        if (view.needsNewKey && rekeyIfAllowed(itemId)) {
            current = requireOpener()
            view = ownedView(itemId)
        }
        val opened = current.open(view)
        val isRecordOn = opened.verifiedAccess?.isUsableByAgent == true
        if (!isUsable) {
            // Off. Nothing to do when neither the record nor the server says the agent may use it.
            if (!isRecordOn && !view.isUsableByAgent) return opened
            if (view.isUsableByAgent) {
                val answered = call { api.setPasswordItemUsableByAgent(itemId, false, null) }
                keep(answered, itemId, current)
            }
            return try {
                rekey(itemId, null, null, AccessEdit.AgentUse(false), RekeyOrigin.Choice)
            } catch (_: Exception) {
                throw PasswordsError.ItemNeedsNewKey(itemId)
            }
        }
        if (view.itemKind == PasswordsItemKind.Card.rawValue) throw PasswordsError.CardCannotBeUsedByAgent
        // The record is the person's consent: one that was not checked cannot be built on. The person sets sharing again first, which writes a record that can.
        if (opened.sharingCheck != PasswordsSharingCheck.Verified) throw PasswordsError.SharingCouldNotBeChecked(itemId)
        if (!isRecordOn) {
            val newAccess = PasswordsItemAccess.accessWithAgentUse(opened.verifiedAccess, true)
            val signed = PasswordsCrypto.withSignedAccess(current.accountKey, itemId, opened.itemKind, opened.fields, newAccess, highestRecordVersion(opened))
            saveFields(signed, view, current)
            view = ownedView(itemId)
        }
        val sealedItemKey = agentCopy(current.itemKey(view), itemId, current)
        val answered = call { api.setPasswordItemUsableByAgent(itemId, true, sealedItemKey) }
        val kept = keep(answered, itemId, current)
        recordSealed(itemId, PasswordsSealedChange(userIds = emptyList(), isReplacing = false, didMakeAgentCopy = true))
        return kept
    }

    /** The item key sealed for the server's agent key. */
    private suspend fun agentCopy(itemKey: ByteArray, itemId: String, current: PasswordsOpener): String {
        val agentKeyText = call { api.readPasswordsAgentKey() }
        val agentPublicKey = runCatching { unbase64(agentKeyText) }.getOrNull()
        if (agentPublicKey == null || agentPublicKey.size != PasswordsCrypto.PUBLIC_KEY_BYTE_COUNT) throw PasswordsError.InvalidServerAnswer
        return PasswordsCrypto.sealForKeyPair(itemKey, current.keyPair, userId, agentPublicKey, PasswordsCrypto.AGENT_RECIPIENT_ID, itemId).jsonText
    }

    // A new item key

    /**
     * The items that still need a new key but were held back by the 10 minute wait or the limit per session. The person can
     * ask for the key ([giveNewKey]), which is not held back.
     */
    fun itemIdsNeedingAttention(): Set<String> = heldBackItemIds.intersect(itemIdsNeedingNewKey().toSet())

    /** Whether the server marked the item as left with an old key that somebody who lost access may still hold. */
    fun needsNewKey(itemId: String): Boolean {
        val view = itemView(itemId) ?: return false
        return !view.isSharedWithMe && view.needsNewKey
    }

    /** The person's own items the server marked as needing a new key ([giveNewKey]), deleted ones not included. */
    fun itemIdsNeedingNewKey(): List<String> =
        loadedContents()?.items.orEmpty().filter { view -> !view.isSharedWithMe && view.needsNewKey && view.deletedAt == null }.map { view -> view.id }

    /**
     * Gives the item a new item key now, because the person asked: the retry after a removal whose re-key failed, or the
     * button on an item this device would not give a key by itself ([itemIdsNeedingYou]). It is the person's own action, so
     * it seals for everyone the owner's verified record reaches who has a key, and for the agent if the record says so.
     * [PasswordsError.SharingCouldNotBeChecked] when the item's record is not the person's own.
     */
    suspend fun giveNewKey(itemId: String): PasswordItem = mutex.withLock {
        ownedView(itemId)
        rekey(itemId, null, null, AccessEdit.None, RekeyOrigin.Request)
    }

    /** The same as part of finishing a plan the person confirmed: sealed only for the people the plan names for the item, and the agent's copy only if it says so. */
    private suspend fun giveNewKey(itemId: String, plan: PasswordsFinishPlan.Item) {
        ownedView(itemId)
        rekey(itemId, null, null, AccessEdit.None, RekeyOrigin.Request, plan)
    }

    /**
     * Finishes the sharing of one item the person opened: gives it a new key when the server marked it, a key to everybody
     * waiting whom the owner's verified record reaches, and the agent's copy again when the record says the agent may use
     * the item and the server has none. The screen shows [finishPlan] and asks first, and passes that plan here: keys are
     * sealed only for the people it names for this item (whoever the server lists by now), and the agent's copy is made only
     * if it says so. An item the plan does not name is refused ([PasswordsError.ItemNotInSharingPlan]). When the server
     * marked the item after the plan was made and the new key would take the key from people who hold one and are not in the
     * plan, nothing is sealed ([PasswordsError.SharingPlanOutOfDate]) and the person confirms a new plan.
     */
    suspend fun finishSharing(itemId: String, plan: PasswordsFinishPlan) = mutex.withLock {
        val itemPlan = plan.items.firstOrNull { item -> item.itemId == itemId } ?: throw PasswordsError.ItemNotInSharingPlan
        val view = ownedView(itemId)
        if (view.deletedAt != null) throw PasswordsError.ItemNotFound
        if (view.needsNewKey) {
            // The new key replaces every key the server holds. A plan made before the server marked the item names only the
            // people who were waiting, so sealing it now would take the key from everyone else who holds one and the record
            // still reaches. The person confirms again instead.
            if (sealingPlanWouldDropKeyHolders(view, itemPlan)) throw PasswordsError.SharingPlanOutOfDate
            giveNewKey(itemId, itemPlan)
        }
        itemIdsActedOn.add(itemId)
        completeWaitingSharesLocked(itemId, itemPlan.recipientUserIds.toSet(), isByPerson = true)
        if (itemPlan.isAgentIncluded && itemIdsWithLostAgentCopy().contains(itemId)) setUsableByAgentLocked(itemId, true)
        keyItemIdsLeftForPerson.remove(itemId)
        shareItemIdsLeftForPerson.remove(itemId)
    }

    /**
     * Whether a new key sealed for exactly this plan would take the key from somebody who holds one now and whom the item's
     * record still reaches, or the agent's copy when the record lets the agent use the item. False when the record is not
     * the person's own: the new key is refused then.
     */
    private fun sealingPlanWouldDropKeyHolders(view: PasswordItemView, plan: PasswordsFinishPlan.Item): Boolean {
        val opened = requireOpener().open(view)
        val access = opened.verifiedAccess
        if (opened.sharingCheck != PasswordsSharingCheck.Verified || access == null) return false
        for (recipient in view.shares.flatMap { share -> share.recipients }) {
            if (recipient.userId == userId || !recipient.hasKey || recipient.hasTooMuchShared || plan.recipientUserIds.contains(recipient.userId)) continue
            val publicKeyData = recipient.publicKey?.let { text -> runCatching { unbase64(text) }.getOrNull() }
            if (publicKeyData == null || !PasswordsCrypto.isPublicKeyValid(publicKeyData)) continue
            if (PasswordsItemAccess.isReachedByAccess(access, recipient.userId, circleIdsListing(recipient.userId, view))) return true
        }
        return access.isUsableByAgent && view.isUsableByAgent && view.itemKind != PasswordsItemKind.Card.rawValue && !plan.isAgentIncluded
    }

    /**
     * What [finishSharing] would do for these items, from the phone's copy: for each owned item whose record is the person's
     * own, the people it would seal a key for, whether it would give the item a new key, and whether it would make the
     * agent's copy again. An item with nothing to do is left out of the count. Nothing is sealed or sent.
     */
    fun finishPlan(itemIds: List<String>): PasswordsFinishPlan {
        val current = currentOpener() ?: return PasswordsFinishPlan(0, emptyList(), 0, 0)
        val held = loadedContents() ?: return PasswordsFinishPlan(0, emptyList(), 0, 0)
        val lostAgentCopyItemIds = itemIdsWithLostAgentCopy()
        var itemCount = 0
        var newKeyItemCount = 0
        var agentItemCount = 0
        val planItems = mutableListOf<PasswordsFinishPlan.Item>()
        val countsByUserId = mutableMapOf<String, Int>()
        val namesByUserId = mutableMapOf<String, String>()
        val seenItemIds = mutableSetOf<String>()
        for (itemId in itemIds) {
            if (!seenItemIds.add(itemId)) continue
            val view = held.item(itemId) ?: continue
            if (view.isSharedWithMe || view.deletedAt != null) continue
            val opened = runCatching { current.open(view) }.getOrNull() ?: continue
            val access = opened.verifiedAccess ?: continue
            val recipientUserIds = mutableSetOf<String>()
            for (recipient in view.shares.flatMap { share -> share.recipients }) {
                if (recipient.userId == userId) continue
                val publicKeyData = recipient.publicKey?.let { text -> runCatching { unbase64(text) }.getOrNull() }
                if (publicKeyData == null || !PasswordsCrypto.isPublicKeyValid(publicKeyData)) continue
                // A new key is sealed for everyone the record reaches who has a key; otherwise only for those waiting without
                // one (and not past what the server lets others share with them).
                if (!(view.needsNewKey || (!recipient.hasKey && !recipient.hasTooMuchShared))) continue
                if (!PasswordsItemAccess.isReachedByAccess(access, recipient.userId, circleIdsListing(recipient.userId, view))) continue
                recipientUserIds.add(recipient.userId)
                namesByUserId[recipient.userId] = recipient.name.ifEmpty { recipient.username }
            }
            val isAgentCopyLost = lostAgentCopyItemIds.contains(itemId)
            if (recipientUserIds.isEmpty() && !view.needsNewKey && !isAgentCopyLost) continue
            itemCount++
            if (view.needsNewKey) newKeyItemCount++
            if (isAgentCopyLost) agentItemCount++
            for (recipientUserId in recipientUserIds) countsByUserId[recipientUserId] = (countsByUserId[recipientUserId] ?: 0) + 1
            // The new key keeps the agent's copy of an item the agent already uses, so that copy is in the plan too.
            val isAgentCopyKept = view.needsNewKey && view.isUsableByAgent && access.isUsableByAgent && view.itemKind != PasswordsItemKind.Card.rawValue
            planItems.add(PasswordsFinishPlan.Item(itemId, recipientUserIds.sorted(), isAgentCopyLost || isAgentCopyKept))
        }
        val people = countsByUserId.map { (personUserId, count) -> PasswordsFinishPlan.Person(personUserId, namesByUserId[personUserId] ?: "", count) }
            .sortedWith(compareBy<PasswordsFinishPlan.Person> { person -> person.name.lowercase() }.thenBy { person -> person.userId })
        return PasswordsFinishPlan(itemCount, people, newKeyItemCount, agentItemCount, planItems)
    }

    /**
     * The items an automatic action left for the person, because this device has no memory of them from before this session
     * or from something it did itself, or because a waiting person is somebody it never sealed for: the screen says "Open
     * this item to finish sharing it" for each, and [finishSharing] does it.
     */
    fun itemIdsNeedingYou(): Set<String> =
        keyItemIdsLeftForPerson.intersect(itemIdsNeedingNewKey().toSet()) + shareItemIdsLeftForPerson + itemIdsWithLostAgentCopy()

    /**
     * The person's own items whose record, checked as theirs, says the agent may use them but for which the server holds no
     * copy for the agent: another device re-keyed the item without making one, or a restore dropped it. Nothing happens by
     * itself: the person finishes them ([finishSharing]), which makes the copy again. A card never counts.
     */
    fun itemIdsWithLostAgentCopy(): Set<String> {
        val current = currentOpener() ?: return emptySet()
        val held = loadedContents() ?: return emptySet()
        val itemIds = mutableSetOf<String>()
        for (view in held.items) {
            if (view.isSharedWithMe || view.deletedAt != null || view.isUsableByAgent || view.itemKind == PasswordsItemKind.Card.rawValue) continue
            val known = openedItems[view.id]
            val opened = if (known != null && known.revision == view.revision) known else runCatching { current.open(view) }.getOrNull() ?: continue
            if (opened.sharingCheck == PasswordsSharingCheck.Verified && opened.fields.access?.isUsableByAgent == true) itemIds.add(view.id)
        }
        return itemIds
    }

    /**
     * Gives a new key to every one of the person's items the server marked, after a sync, as far as a new key given by
     * itself may go: only for an item this device remembers from before this session or from something it did itself, only
     * to people this device sealed it for before who are still in the record it checked, at most once in ten minutes for an
     * item and [MOST_AUTOMATIC_REKEYS_PER_SESSION] in a session. Returns the ids of those that got one; an item that could
     * not is left marked for the next time ([itemIdsNeedingAttention] names the ones held back by the wait or the limit,
     * [itemIdsNeedingYou] the ones left for the person).
     */
    suspend fun giveNewKeysToMarkedItems(): List<String> = mutex.withLock {
        val givenItemIds = mutableListOf<String>()
        keyItemIdsLeftForPerson = mutableSetOf()
        for (itemId in itemIdsNeedingNewKey()) {
            val given = try {
                rekey(itemId, null, null, AccessEdit.None, RekeyOrigin.Automatic)
            } catch (_: Exception) {
                null
            }
            if (given != null) givenItemIds.add(itemId)
        }
        givenItemIds
    }

    /**
     * Gives the item a new key first when it was marked, as far as one given by itself may go. False when it may not (the
     * record is not the person's own, or one was given a moment ago), so the caller goes on with the key the item has; any
     * other failure is thrown.
     */
    private suspend fun rekeyIfAllowed(itemId: String): Boolean {
        try {
            rekey(itemId, null, null, AccessEdit.None, RekeyOrigin.Automatic)
            return true
        } catch (error: PasswordsError.ItemNeedsAttention) {
            return false
        } catch (error: PasswordsError.SharingCouldNotBeChecked) {
            return false
        } catch (error: PasswordsError.ItemNeedsYou) {
            return false
        }
    }

    /** What a re-key changes in the item's access record, on top of the fields it re-seals. */
    private sealed interface AccessEdit {
        data object None : AccessEdit

        /** The grantees the person now chose. */
        data class Grantees(val grantees: List<PasswordsGrantee>) : AccessEdit

        /** Whether the agent may use the item. */
        data class AgentUse(val isUsableByAgent: Boolean) : AccessEdit
    }

    /** Who asked for a new key, which decides who it is sealed for. */
    private enum class RekeyOrigin {
        /** The person chose who has access (they removed somebody, or turned the agent off): the key is sealed for everyone the new record reaches who has a key. */
        Choice,

        /** The person asked for a new key without choosing who (the button on the item, the retry after a removal): sealed for everyone the verified record reaches who has a key. */
        Request,

        /** Nobody asked (the server marked the item, a save found it marked): only for an item this device remembers, only for people this device sealed for before, and at most once in ten minutes for an item and 20 in a session. */
        Automatic,

        /** The person restored a deleted item, or an earlier version of one: sealed only for people and the agent this device sealed for before, with no wait and no limit. */
        Restore,
        ;

        /** Whether the new key reaches only the people and the agent this device itself sealed for. */
        val isLimitedToWhatThisDeviceSealed: Boolean get() = this == Automatic || this == Restore
    }

    /**
     * Makes a new item key for an item the person owns and seals everything with it: the fields (`replacing` when a save
     * brings new ones, else the item's own, with `accessEdit` applied to their access record and signed again), the
     * person's copy, a copy for each recipient the owner's record still reaches and who has a key, and the agent's copy when
     * the record says the agent may use the item.
     *
     * For the automatic origin the new key never reaches further than what this device did itself: the item has to be one
     * the device remembers, a recipient has to be somebody this device sealed the item for before
     * ([PasswordsItemMemory.sealedForUserIds]), and the agent copy is made only if this device made one
     * ([PasswordsItemMemory.didMakeAgentCopy]) and only from a record that is the person's own. Anybody else waits for the
     * person. One the record does not reach gets nothing, whatever the server's list of shares says. When the server says
     * the item changed on another device and the change was not based on a revision the caller gave, the copy is brought up
     * to date and this is tried once more. With a `plan` only the people it names get a key and the agent a copy only if it
     * says so, also on that second try.
     */
    private suspend fun rekey(
        itemId: String,
        replacing: PasswordsItemFields?,
        baseRevision: Long?,
        accessEdit: AccessEdit,
        origin: RekeyOrigin,
        plan: PasswordsFinishPlan.Item? = null,
    ): PasswordItem {
        if (origin == RekeyOrigin.Automatic) {
            // An item whose record is not the person's own is not given a key by itself, and does not use up a slot.
            if (requireOpener().open(ownedView(itemId)).sharingCheck != PasswordsSharingCheck.Verified) throw PasswordsError.SharingCouldNotBeChecked(itemId)
            // An item this device has no memory of is left for the person.
            if (!isRememberedForAutomaticAction(itemId)) {
                keyItemIdsLeftForPerson.add(itemId)
                throw PasswordsError.ItemNeedsYou(itemId)
            }
            reserveAutomaticRekey(itemId)
        }
        try {
            return rekeyOnce(itemId, replacing, baseRevision, accessEdit, origin, plan)
        } catch (error: PasswordsError.ItemChangedOnAnotherDevice) {
            if (baseRevision != null) throw error
            syncLocked()
            return rekeyOnce(itemId, null, null, accessEdit, origin, plan)
        }
    }

    /** Counts one new key given by itself, or refuses it: not for the same item within ten minutes, and not more than [MOST_AUTOMATIC_REKEYS_PER_SESSION] in all. A refused item is listed in [itemIdsNeedingAttention]. */
    private fun reserveAutomaticRekey(itemId: String) {
        val nowEpochMS = clock()
        val lastEpochMS = lastAutomaticRekeyAtEpochMS[itemId]
        if (lastEpochMS != null && nowEpochMS - lastEpochMS < AUTOMATIC_REKEY_COOLDOWN_MS) {
            heldBackItemIds.add(itemId)
            throw PasswordsError.ItemNeedsAttention(itemId)
        }
        if (automaticRekeyCount >= MOST_AUTOMATIC_REKEYS_PER_SESSION) {
            heldBackItemIds.add(itemId)
            throw PasswordsError.ItemNeedsAttention(itemId)
        }
        lastAutomaticRekeyAtEpochMS[itemId] = nowEpochMS
        automaticRekeyCount++
        heldBackItemIds.remove(itemId)
    }

    private suspend fun rekeyOnce(
        itemId: String,
        replacing: PasswordsItemFields?,
        baseRevision: Long?,
        accessEdit: AccessEdit,
        origin: RekeyOrigin,
        plan: PasswordsFinishPlan.Item?,
    ): PasswordItem {
        val current = requireOpener()
        val view = ownedView(itemId)
        if (view.deletedAt != null) throw PasswordsError.ItemNotFound
        val opened = current.open(view)
        // A new key given without the person choosing who has access builds on the record, so the record has to be the person's own.
        if (origin != RekeyOrigin.Choice && opened.sharingCheck != PasswordsSharingCheck.Verified) throw PasswordsError.SharingCouldNotBeChecked(itemId)
        var fields: PasswordsItemFields
        if (replacing != null) {
            if (view.itemKind != replacing.itemKind.rawValue) throw PasswordsError.ItemKindCannotChange
            fields = replacing
        } else {
            fields = opened.fields
        }
        // What the record is edited from is the record the person owns; a record that was not checked counts as one that
        // grants nobody and does not let the agent in.
        when (accessEdit) {
            is AccessEdit.None -> Unit
            is AccessEdit.Grantees -> {
                val edited = PasswordsItemAccess.accessWithGrantees(opened.verifiedAccess, accessEdit.grantees)
                fields = PasswordsCrypto.withSignedAccess(current.accountKey, itemId, opened.itemKind, fields, edited, highestRecordVersion(opened))
            }
            is AccessEdit.AgentUse -> {
                val edited = PasswordsItemAccess.accessWithAgentUse(opened.verifiedAccess, accessEdit.isUsableByAgent)
                fields = PasswordsCrypto.withSignedAccess(current.accountKey, itemId, opened.itemKind, fields, edited, highestRecordVersion(opened))
            }
        }
        val memory = currentPins().itemMemories[itemId] ?: PasswordsItemMemory()
        if (origin != RekeyOrigin.Automatic && origin != RekeyOrigin.Restore) itemIdsActedOn.add(itemId)
        // A restore on a device with no memory of the item starts from a record it cannot tell from an old one: that record is not remembered either.
        val isRemembering = origin != RekeyOrigin.Restore || memory.highestRecordVersion > 0
        val newItemKey = PasswordsCrypto.makeKey()
        val sealedFields = PasswordsCrypto.sealItemFields(fields, newItemKey, itemId)
        val sealedItemKey = PasswordsCrypto.seal(newItemKey, current.accountKey, PasswordsCrypto.itemKeyAssociatedData(itemId))
        val shareKeys = mutableListOf<PasswordItemShareKeyInput>()
        val sealedKeys = linkedMapOf<String, String>()
        val seenUserIds = mutableSetOf<String>()
        for (recipientView in view.shares.flatMap { share -> share.recipients }) {
            if (recipientView.userId == userId || !seenUserIds.add(recipientView.userId)) continue
            if (!PasswordsItemAccess.isReachedByAccess(fields.access, recipientView.userId, circleIdsListing(recipientView.userId, view))) continue
            if (origin.isLimitedToWhatThisDeviceSealed && !memory.sealedForUserIds.contains(recipientView.userId)) continue
            // A key sealed for a confirmed plan goes only to the people the plan names.
            if (plan != null && !plan.recipientUserIds.contains(recipientView.userId)) continue
            val recipient = recipientOf(recipientView, current)
            val publicKey = recipient.publicKey ?: continue
            sealedKeys[recipient.userId] = base64(publicKey)
            shareKeys.add(itemShareKey(newItemKey, itemId, current.keyPair, recipient, publicKey))
        }
        var agentCopyText: String? = null
        val isAgentCopyAllowed = (!origin.isLimitedToWhatThisDeviceSealed || memory.didMakeAgentCopy) && (plan?.isAgentIncluded ?: true)
        if (isAgentCopyAllowed && fields.access?.isUsableByAgent == true && view.isUsableByAgent && view.itemKind != PasswordsItemKind.Card.rawValue) {
            agentCopyText = agentCopy(newItemKey, itemId, current)
        }
        recordSealedKeys(sealedKeys)
        val input = PasswordItemRekeyInput(itemId, baseRevision ?: view.revision, sealedFields.jsonText, sealedItemKey.jsonText, shareKeys, agentCopyText)
        val answered = call { api.rekeyPasswordItem(input) }
        val kept = keep(answered, itemId, current, isRemembering)
        recordSealed(
            itemId,
            PasswordsSealedChange(
                userIds = sealedKeys.keys.toList(),
                isReplacing = true,
                didMakeAgentCopy = agentCopyText != null,
                circleShares = circleSharesFor(sealedKeys.keys.toList(), memory.sealedForUserIds.toSet(), view, fields.access),
            ),
        )
        keyItemIdsLeftForPerson.remove(itemId)
        return kept
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

        /** How long an item waits after a new key this session gave it by itself (the server marked it, or a save found it marked) before it gives another one. */
        const val AUTOMATIC_REKEY_COOLDOWN_MS = 600_000L

        /** The most new keys one session gives by itself. Past it, items are listed in [itemIdsNeedingAttention] and wait for the person. */
        const val MOST_AUTOMATIC_REKEYS_PER_SESSION = 20
    }
}
