package com.teanode.kit.passwords

// What sharing shows and does, besides the session's calls: a person an item reaches with the code to compare, a
// recipient waiting for a key, a changed key, what finishing the sharing would do. The iPhone app's PasswordsSession.swift.

/** What a device knows about a recipient's public key. */
enum class PasswordsKeyTrust {
    /** The key is the one this device knows for the person. */
    Known,

    /** This device has never seen a key for the person: the key the server gives is sealed for at once and recorded as the known one. */
    New,

    /** The server now gives another key than the one this device knows: it is sealed for at once, recorded, and a notice is shown until dismissed. */
    Changed,

    /** The person has not set up Passwords, so there is no key. */
    NoKey,
}

/** A person an item reaches (or could reach), with the code the owner can compare with them to be sure of the key. */
class PasswordRecipient(
    view: PasswordRecipientView,
    knownPublicKey: String? = null,
    comparedPublicKey: String? = null,
    /** The circles the server listed this person as a member of when the recipients were read. */
    val circleIds: List<String> = emptyList(),
) {
    val userId: String = view.userId
    val name: String = view.name
    val username: String = view.username

    /** The person's public key (65 bytes); null when they have not set up Passwords. */
    val publicKey: ByteArray?

    /** Eight groups of four digits; null without a public key. */
    val safetyCode: String?

    /** Whether the item already has a key for this public key. */
    val hasKey: Boolean = view.hasKey

    /** Whether the person already has the most the server lets others share with them: no key is stored for them. */
    val hasTooMuchShared: Boolean = view.hasTooMuchShared
    val keyTrust: PasswordsKeyTrust

    /** The safety code of the key this device knew before, when [keyTrust] is [PasswordsKeyTrust.Changed]. */
    val knownSafetyCode: String?

    /** Whether the person compared safety codes for this key. It gates nothing. */
    val isCompared: Boolean

    init {
        val text = view.publicKey
        val data = text?.let { value -> runCatching { unbase64(value) }.getOrNull() }
        val code = data?.takeIf { bytes -> PasswordsCrypto.isPublicKeyValid(bytes) }?.let { bytes -> runCatching { PasswordsCrypto.safetyCode(bytes) }.getOrNull() }
        if (text != null && data != null && code != null) {
            publicKey = data
            safetyCode = code
            isCompared = comparedPublicKey == text
            if (knownPublicKey != null) {
                keyTrust = if (knownPublicKey == text) PasswordsKeyTrust.Known else PasswordsKeyTrust.Changed
                knownSafetyCode = if (knownPublicKey == text) null else PasswordsOpener.safetyCodeText(knownPublicKey)
            } else {
                keyTrust = PasswordsKeyTrust.New
                knownSafetyCode = null
            }
        } else {
            publicKey = null
            safetyCode = null
            isCompared = false
            keyTrust = PasswordsKeyTrust.NoKey
            knownSafetyCode = null
        }
    }
}

/** An item of the person's that a recipient cannot open yet, because nobody has sealed its key for their current public key. */
class PasswordWaitingShare(
    val itemId: String,
    /** The item's name, if the item is in the phone's copy. */
    val itemName: String?,
    val recipient: PasswordRecipient,
    /** Whether more are waiting than the server lists (the newest items first). */
    val hasMoreWaiting: Boolean,
)

/** A person whose key changed from the one this device knew: the key was accepted, and the screen shows a notice until dismissed. */
data class PasswordsKeyChange(
    val userId: String,
    /** The person's name when the phone's copy knows it; empty otherwise. */
    val name: String,
    val oldSafetyCode: String,
    val newSafetyCode: String,
    /** The key the person has now, standard base64: what `markCompared` is given. */
    val newPublicKey: String,
)

/** This device sealed an item's key for somebody the item reaches through a circle and had never sealed it for before. */
data class PasswordsCircleShareNotice(
    val itemId: String,
    val itemName: String,
    val recipientUserId: String,
    val recipientName: String,
    val circleName: String,
) {
    val id: String get() = "$itemId|$recipientUserId"
}

/** What `completeWaitingShares` did: how many keys it sealed for recipients who were waiting. */
data class PasswordsCompletedShares(
    val addedKeyCount: Int,
    /** The names of waiting people left out because they already have the most the server lets others share with them, sorted. */
    val tooMuchSharedNames: List<String> = emptyList(),
    /** Whether the server says more are waiting than it listed: the rest shows once these are done. */
    val hasMoreWaiting: Boolean = false,
)

/**
 * What "Finish sharing" would do for some items, worked out from the phone's copy before anything is sealed: who gets a key
 * for how many items, how many items get a new key, and for how many the agent's copy is made again. Finishing seals for
 * the people and the agent this plan names and for no one else.
 */
data class PasswordsFinishPlan(
    /** The items with something to finish. */
    val itemCount: Int,
    /** The people who would get keys, by name. */
    val people: List<Person>,
    /** The items that would be given a new key because the server marked them. */
    val newKeyItemCount: Int,
    /** The items whose record says the agent may use them but that have no copy for it. */
    val agentItemCount: Int,
    /** What is confirmed for each of those items. */
    val items: List<Item> = emptyList(),
) {
    /** A person who would get keys, and for how many of the items. */
    data class Person(val userId: String, val name: String, val itemCount: Int)

    /** What the person confirms for one item: the people (by user id) who would get a key and whether the agent would get a copy. */
    data class Item(val itemId: String, val recipientUserIds: List<String>, val isAgentIncluded: Boolean)

    val isEmpty: Boolean get() = itemCount == 0
}
