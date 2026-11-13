package com.teanode.kit.passwords

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.Serializable
import java.io.File

// The phone's copy of the person's Passwords: what the server sends, which is sealed text, plus the person's key pair
// (the private half sealed by the account key). Nothing in it opens without the account key, which lives in the Android
// Keystore behind the fingerprint (the app's PasswordsKeyStore). The wrapped account key that a passphrase or recovery
// key opens is not stored here.
//
// Unlike the iPhone's AutoFill extension, Android's autofill service runs inside the app's own process, so one store
// serves both and there are no per-item files, no file coordination and no memory-limited loading on demand: the copy is
// one document, written whole. The keys the device holds for other people are kept apart (PasswordsPins), because they
// must survive the copy being dropped.

/** PasswordsCacheException is why the cache could not be used. */
class PasswordsCacheException(val failure: PasswordsCacheFailure) : Exception(failure.name)

enum class PasswordsCacheFailure {
    /** The file is not a cache this build can read. */
    Unreadable,

    /** The file was written by a newer build. */
    UnsupportedVersion,
}

/** What a merge changed, for a screen that wants to say so and for the session to forget what it had opened. */
data class PasswordsMergeResult(
    /** Items that are new or whose server copy was replaced. */
    val changedItemIds: List<String> = emptyList(),
    /** Items the person can no longer reach, which the merge dropped. */
    val removedItemIds: List<String> = emptyList(),
)

/**
 * A person this device sealed an item key for because the item is shared with a circle they are in, and had never
 * sealed it for before. It is kept until the person dismisses the notice.
 */
@Serializable
data class PasswordsCircleShare(
    val recipientUserId: String,
    val recipientName: String,
    val circleName: String,
)

/**
 * What this device remembers about one item the person owns. It is filled only from what this device saw verified and
 * did itself, never from the server's lists of who holds a key: a device that has never seen an item cannot tell an old
 * signed record from the newest one, so it does nothing by itself for an item it has no memory of.
 */
@Serializable
data class PasswordsItemMemory(
    /** The highest `recordVersion` this device has seen on the item's access record with a valid signature. */
    val highestRecordVersion: Long = 0,
    /** The `recordMac` of the record at that version. A record with the same version and another mac is a replay as well. */
    val highestRecordMac: String? = null,
    /** The user ids this device itself sealed the item key for, the last time it sealed. */
    val sealedForUserIds: List<String> = emptyList(),
    /** Whether this device itself made the agent's copy of the item key, the last time it sealed. */
    val didMakeAgentCopy: Boolean = false,
    /** The people the person named directly the last time they set who the item is shared with on this device. */
    val chosenUserIds: List<String> = emptyList(),
    /** People this device sealed for through a circle for the first time, whose notice the person has not dismissed. */
    val circleShares: List<PasswordsCircleShare> = emptyList(),
) {
    /** The memory after a record with a valid signature was seen. The version only goes up. */
    fun seeingRecord(version: Long, mac: String?): PasswordsItemMemory = when {
        version > highestRecordVersion -> copy(highestRecordVersion = version, highestRecordMac = mac)
        version == highestRecordVersion && highestRecordMac == null -> copy(highestRecordMac = mac)
        else -> this
    }

    /** Whether a record with a valid signature is older than the newest this device saw, or has the same version and another mac. */
    fun isReplay(version: Long, mac: String?): Boolean {
        if (version < highestRecordVersion) return true
        val remembered = highestRecordMac ?: return false
        return version == highestRecordVersion && mac != remembered
    }

    companion object {
        const val MOST_SEALED_FOR_USER_IDS = 1000
        const val MOST_CIRCLE_SHARES = 100
    }
}

/**
 * The public keys this device holds for other people, and what it remembers about each item the person owns. They are
 * about other people and the server's past answers, not about the person's own setup, so they are kept in a store of
 * their own that survives the cache being dropped (a server that answers with another public key or an older record
 * must not be able to make the phone forget them). They go only when the person removes the account from the phone or
 * starts Passwords over, never when the server ends the sign-in.
 */
@Serializable
data class PasswordsPins(
    val formatVersion: Int = CURRENT_FORMAT_VERSION,
    /** The signed-in person's id, so one person's pins are never read as another's. */
    val userId: String,
    /** The one key this device knows for each person, by user id (standard base64, 65 bytes). */
    val knownPublicKeys: Map<String, String> = emptyMap(),
    /** The key the person compared safety codes for, by user id. It gates nothing. */
    val comparedPublicKeys: Map<String, String> = emptyMap(),
    /** For each person whose known key changed and whose notice the person has not dismissed, the key it replaced. */
    val changedPublicKeys: Map<String, String> = emptyMap(),
    /** What this device remembers about each item the person owns, by item id. */
    val itemMemories: Map<String, PasswordsItemMemory> = emptyMap(),
) {
    /** Whether the person compared safety codes for this person's key as it is known now. */
    fun isCompared(otherUserId: String): Boolean {
        val known = knownPublicKeys[otherUserId] ?: return false
        return comparedPublicKeys[otherUserId] == known
    }

    companion object {
        const val CURRENT_FORMAT_VERSION = 2
    }
}

/** The cache's header and, when it is loaded whole, its items. */
@Serializable
data class PasswordsCacheContents(
    val formatVersion: Int = CURRENT_FORMAT_VERSION,
    /** The signed-in person's id, which a shared item's key is opened with. */
    val userId: String,
    /** The person's public key, standard base64 (65 bytes). */
    val publicKey: String,
    /** The person's private key sealed with the account key. */
    val privateKeyByAccountKey: String,
    /** The highest revision the copy holds, asked for as `sinceRevision` next time. */
    val revision: Long = 0,
    val lockAfterMinutes: Int,
    /** Every item the person can reach, owned ones deleted in the last 30 days included. */
    val items: List<PasswordItemView> = emptyList(),
) {
    /** The item with this id, if the copy holds it. */
    fun item(id: String): PasswordItemView? = items.firstOrNull { item -> item.id == id }

    /**
     * Takes in a page of changes: the items it carries replace the copy's (or join it), and every item whose id the server
     * no longer lists is dropped. The revision never moves backwards, so an answer that raced a change cannot make the
     * next read ask for revisions the copy already has.
     */
    fun merge(list: PasswordItemListView): Pair<PasswordsCacheContents, PasswordsMergeResult> {
        val merged = items.toMutableList()
        val changedItemIds = mutableListOf<String>()
        for (item in list.items) {
            val index = merged.indexOfFirst { held -> held.id == item.id }
            if (index >= 0) merged[index] = item else merged.add(item)
            changedItemIds.add(item.id)
        }
        val reachableItemIds = list.itemIds.toSet()
        val removedItemIds = merged.map { held -> held.id }.filter { itemId -> !reachableItemIds.contains(itemId) }
        merged.removeAll { held -> !reachableItemIds.contains(held.id) }
        return copy(items = merged, revision = maxOf(revision, list.revision)) to PasswordsMergeResult(changedItemIds, removedItemIds)
    }

    /** Puts one item the server just answered with into the copy. The copy's revision stays where it was. */
    fun replacing(item: PasswordItemView): PasswordsCacheContents {
        val merged = items.toMutableList()
        val index = merged.indexOfFirst { held -> held.id == item.id }
        if (index >= 0) merged[index] = item else merged.add(item)
        return copy(items = merged)
    }

    companion object {
        const val CURRENT_FORMAT_VERSION = 2
    }
}

/** Where the phone's copy and the pins are kept. The app's is files; the tests' is memory. */
interface PasswordsStorage {
    fun readCache(): String?

    fun writeCache(text: String)

    fun deleteCache()

    fun readPins(): String?

    fun writePins(text: String)

    fun deletePins()
}

/** A [PasswordsStorage] that keeps everything in memory. */
class InMemoryPasswordsStorage : PasswordsStorage {
    private var cacheText: String? = null
    private var pinsText: String? = null

    override fun readCache(): String? = cacheText

    override fun writeCache(text: String) {
        cacheText = text
    }

    override fun deleteCache() {
        cacheText = null
    }

    override fun readPins(): String? = pinsText

    override fun writePins(text: String) {
        pinsText = text
    }

    override fun deletePins() {
        pinsText = null
    }
}

/** A [PasswordsStorage] in two files of a folder. The files hold only sealed text and public keys. */
class FilePasswordsStorage(private val directory: File) : PasswordsStorage {
    private val cacheFile get() = File(directory, "passwords.json")
    private val pinsFile get() = File(directory, "passwords.keys.json")

    private fun read(file: File): String? = if (file.exists()) file.readText() else null

    private fun write(file: File, text: String) {
        directory.mkdirs()
        // Written beside the file and moved over it, so a crash never leaves half a copy.
        val temporary = File(directory, file.name + ".tmp")
        temporary.writeText(text)
        if (!temporary.renameTo(file)) {
            file.writeText(text)
            temporary.delete()
        }
    }

    override fun readCache(): String? = read(cacheFile)

    override fun writeCache(text: String) = write(cacheFile, text)

    override fun deleteCache() {
        cacheFile.delete()
    }

    override fun readPins(): String? = read(pinsFile)

    override fun writePins(text: String) = write(pinsFile, text)

    override fun deletePins() {
        pinsFile.delete()
    }
}

/** PasswordsCache reads and writes the copy and the pins through a [PasswordsStorage]. */
class PasswordsCache(private val storage: PasswordsStorage) {
    /** The copy, or null when there is none. One that is damaged, or from a newer build, throws. */
    fun load(): PasswordsCacheContents? {
        val text = storage.readCache() ?: return null
        val contents = try {
            TeaNodeJson.decodeFromString(PasswordsCacheContents.serializer(), text)
        } catch (_: Exception) {
            throw PasswordsCacheException(PasswordsCacheFailure.Unreadable)
        }
        if (contents.formatVersion > PasswordsCacheContents.CURRENT_FORMAT_VERSION) {
            throw PasswordsCacheException(PasswordsCacheFailure.UnsupportedVersion)
        }
        return contents
    }

    fun save(contents: PasswordsCacheContents) {
        storage.writeCache(TeaNodeJson.encodeToString(PasswordsCacheContents.serializer(), contents))
    }

    /** The pins of this person; none when the store has none or has another person's. */
    fun loadPins(userId: String): PasswordsPins {
        val text = storage.readPins() ?: return PasswordsPins(userId = userId)
        val pins = try {
            TeaNodeJson.decodeFromString(PasswordsPins.serializer(), text)
        } catch (_: Exception) {
            throw PasswordsCacheException(PasswordsCacheFailure.Unreadable)
        }
        return if (pins.userId == userId) pins else PasswordsPins(userId = userId)
    }

    private fun updatePins(userId: String, change: (PasswordsPins) -> PasswordsPins) {
        val updated = change(loadPins(userId))
        storage.writePins(TeaNodeJson.encodeToString(PasswordsPins.serializer(), updated))
    }

    /** Records the key the server gave for a person when none was known. */
    fun recordKnownIfAbsent(userId: String, otherUserId: String, publicKey: String) {
        updatePins(userId) { pins ->
            if (pins.knownPublicKeys.containsKey(otherUserId)) pins
            else pins.copy(knownPublicKeys = pins.knownPublicKeys + (otherUserId to publicKey))
        }
    }

    /** Replaces the known key with another one the server gave, and keeps the one it replaced for the notice. */
    fun recordChangedKey(userId: String, otherUserId: String, publicKey: String) {
        updatePins(userId) { pins ->
            val known = pins.knownPublicKeys[otherUserId]
            if (known == null || known == publicKey) pins
            else pins.copy(
                knownPublicKeys = pins.knownPublicKeys + (otherUserId to publicKey),
                changedPublicKeys = pins.changedPublicKeys + (otherUserId to known),
            )
        }
    }

    /** Records that the person compared safety codes for this person's key. */
    fun recordCompared(userId: String, otherUserId: String, publicKey: String) {
        updatePins(userId) { pins -> pins.copy(comparedPublicKeys = pins.comparedPublicKeys + (otherUserId to publicKey)) }
    }

    /** Dismisses the notice that a person's key changed. */
    fun dismissChangedKey(userId: String, otherUserId: String) {
        updatePins(userId) { pins -> pins.copy(changedPublicKeys = pins.changedPublicKeys - otherUserId) }
    }

    /** Records the version and mac of an access record seen with a valid signature. The version only goes up. */
    fun recordSeenRecord(userId: String, itemId: String, version: Long, mac: String?) {
        updatePins(userId) { pins ->
            val current = pins.itemMemories[itemId] ?: PasswordsItemMemory()
            pins.copy(itemMemories = pins.itemMemories + (itemId to current.seeingRecord(version, mac)))
        }
    }

    /** Drops the copy and keeps the pins: a server's answer alone must not make the phone forget whose key it knew. */
    fun eraseKeepingPins() {
        storage.deleteCache()
    }

    /** Drops the copy and the pins: the person asked to start over. */
    fun erase() {
        storage.deleteCache()
        storage.deletePins()
    }
}
