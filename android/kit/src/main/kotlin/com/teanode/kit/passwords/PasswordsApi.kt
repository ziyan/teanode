package com.teanode.kit.passwords

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.serializer

// What the phone sends the server about Passwords, and what it reads back. Every value that is "sealed" is text the
// device made (the JSON of PasswordsCrypto.kt); the server stores it and hands it back without reading it. The names are
// the server's (internal/api/v1api/apigraph/passwords.go) and the dashboard's. PasswordsSession.kt is the code that
// seals and opens them; this file is only the shapes and the calls.

/** PasswordsSetupView is whether the person has set Passwords up, and what a device needs to make the passphrase key again. */
@Serializable
data class PasswordsSetupView(
    val isSetUp: Boolean,
    /** The passphrase salt as standard base64; null until set up. */
    val passphraseSalt: String? = null,
    val passphraseIterationCount: Int? = null,
    /** The person's public key as standard base64 (65 bytes); null until set up. */
    val publicKey: String? = null,
    val lockAfterMinutes: Int,
    val itemCount: Int,
)

/** What a right proof opens: the account key sealed by the secret the proof came from, and the person's key pair. */
@Serializable
data class PasswordsUnlockView(
    val accountKeyWrapped: String,
    val privateKeyByAccountKey: String,
    val publicKey: String,
)

/** A person an item reaches (or could reach). */
@Serializable
data class PasswordRecipientView(
    val userId: String,
    val name: String,
    val username: String,
    /** Standard base64 of the 65 byte public key; null when the person has not set up Passwords. */
    val publicKey: String? = null,
    /** Whether the item already has a key sealed for this person's current public key. */
    val hasKey: Boolean,
    /** Whether the person already has the most the server lets others share with them. An older server does not send it: then false. */
    val hasTooMuchShared: Boolean = false,
)

/** One share of an item: with a person or with a circle. */
@Serializable
data class PasswordShareView(
    val id: String,
    val granteeUserId: String? = null,
    val granteeCircleId: String? = null,
    val granteeName: String,
    /** Everyone the share reaches. */
    val recipients: List<PasswordRecipientView>,
)

/**
 * An item as the server holds it: everything in it is sealed except who owns it, its kind and its times. `itemKey` is
 * the item key sealed for the owner with the account key, or for a recipient with their key pair (`isSharedWithMe`).
 * `ownerPublicKey` is the owner's current public key (standard base64, 65 bytes): a recipient opens a shared item key
 * only when it equals the key the device already knows for that owner.
 */
@Serializable
data class PasswordItemView(
    val id: String,
    val ownerUserId: String,
    val ownerName: String,
    /** A phone's copy written before the owner's key was part of an item has none: it reads as empty, which no shared item opens with. */
    val ownerPublicKey: String = "",
    val itemKind: String,
    val encryptedFields: String,
    val itemKey: String,
    val isSharedWithMe: Boolean,
    val revision: Long,
    val isUsableByAgent: Boolean,
    val lastUsedByAgentAt: String? = null,
    val deletedAt: String? = null,
    val createdAt: String,
    val updatedAt: String,
    /** The owner's shares; empty for a recipient. */
    val shares: List<PasswordShareView>,
    /** Set by the server when access to the item narrowed and the item still has the key they held. Only a re-key clears it. */
    val needsNewKey: Boolean = false,
)

/**
 * One earlier version of an item the person owns, as the server keeps it: the sealed fields and the item key (sealed
 * with the account key) the item had then. `isSavedByPerson` says whether the person's own tap saved it, rather than
 * another device or program (a token).
 */
@Serializable
data class PasswordItemVersionView(
    val versionId: String,
    val savedAt: String,
    val isSavedByPerson: Boolean,
    val encryptedFields: String,
    val itemKey: String,
    val itemKind: String,
)

/** A page of changes: the items whose revision is above the one asked for, and the id of every item the person can reach now. */
@Serializable
data class PasswordItemListView(
    val items: List<PasswordItemView>,
    val itemIds: List<String>,
    /** The highest revision among everything visible. */
    val revision: Long,
)

/** A recipient of one of the caller's items who has set up Passwords but has no key for their current public key yet. */
@Serializable
data class PasswordWaitingShareView(
    val itemId: String,
    val recipient: PasswordRecipientView,
    /** Set on every entry when more are waiting than the server lists. An older server does not send it: then false. */
    val hasMoreWaiting: Boolean = false,
)

/** Everything a device makes when the person sets Passwords up. The salt is base64; the proof hashes are base64 of the SHA-256 of the proof bytes. */
@Serializable
data class PasswordsSetUpInput(
    val accountKeyByPassphrase: String,
    val passphraseSalt: String,
    val passphraseIterationCount: Int,
    val passphraseProofHash: String,
    val accountKeyByRecoveryKey: String,
    val recoveryKeyProofHash: String,
    val publicKey: String,
    val privateKeyByAccountKey: String,
)

/** Proves the old passphrase (or the recovery key) and carries the account key sealed by the new passphrase. */
@Serializable
data class PasswordsChangePassphraseInput(
    val proof: String,
    val isRecoveryKey: Boolean,
    val accountKeyByPassphrase: String,
    val passphraseSalt: String,
    val passphraseIterationCount: Int,
    val passphraseProofHash: String,
)

/** Proves the passphrase and carries the account key sealed by the new recovery key. */
@Serializable
data class PasswordsReplaceRecoveryKeyInput(
    val proof: String,
    val accountKeyByRecoveryKey: String,
    val recoveryKeyProofHash: String,
)

/** A new or changed item. The id is made by the device because the encryption names it. */
@Serializable
data class PasswordItemSaveInput(
    val id: String,
    val itemKind: String,
    val encryptedFields: String,
    val itemKey: String,
    /** The revision the change is based on; null for a new item. */
    val baseRevision: Long? = null,
)

/** Who an item is shared with: a person or a circle, one of the two. */
@Serializable
data class PasswordShareGranteeInput(
    val granteeUserId: String? = null,
    val granteeCircleId: String? = null,
)

/**
 * The item key sealed for one recipient. The recipient's public key is sent so the server can refuse a key made for a
 * key the person no longer has.
 */
@Serializable
data class PasswordShareKeyInput(
    val recipientUserId: String,
    val itemKeyByRecipientKey: String,
    val recipientPublicKey: String,
    /**
     * The item key sealed with the account key, exactly as the item showed it when this device opened it to seal this key.
     * The server refuses the key when the item has another key by now ("changed on another device").
     */
    val itemKey: String? = null,
)

/** A share key for one item, for adding keys to shares that already exist. */
@Serializable
data class PasswordItemShareKeyInput(
    val itemId: String,
    val recipientUserId: String,
    val itemKeyByRecipientKey: String,
    val recipientPublicKey: String,
    /** As in [PasswordShareKeyInput]. A re-key leaves it out: its keys are sealed from the new key in the same call. */
    val itemKey: String? = null,
)

/**
 * Gives an item a new item key when access narrows (the agent stops using it, or somebody is removed from its shares):
 * the fields sealed with the new key, the new key sealed for the owner, for each recipient who keeps access, and for the
 * agent if it still may use the item. The server drops every old share key.
 */
@Serializable
data class PasswordItemRekeyInput(
    val id: String,
    /** The revision the device read; a newer one is refused as "changed on another device". */
    val baseRevision: Long,
    val encryptedFields: String,
    /** The new item key sealed with the account key. */
    val itemKey: String,
    val shareKeys: List<PasswordItemShareKeyInput>,
    /** The new item key sealed for the agent key; null when the agent is not to use the item. */
    val itemKeyByAgentKey: String? = null,
)

/** The server's side of Passwords as calls. [ConnectionPasswordsApi] is the real one; the tests stand in a server of their own. */
interface PasswordsApi {
    suspend fun readPasswordsSetup(): PasswordsSetupView

    suspend fun setUpPasswords(input: PasswordsSetUpInput): PasswordsSetupView

    /** A right proof opens the account key. A wrong one is refused with "That passphrase is not right." or "That recovery key is not right.". */
    suspend fun unlockPasswords(proof: String, isRecoveryKey: Boolean): PasswordsUnlockView

    suspend fun changePasswordsPassphrase(input: PasswordsChangePassphraseInput): PasswordsSetupView

    suspend fun replacePasswordsRecoveryKey(input: PasswordsReplaceRecoveryKeyInput): PasswordsSetupView

    suspend fun updatePasswordsSettings(lockAfterMinutes: Int): PasswordsSetupView

    suspend fun erasePasswords(): Boolean

    suspend fun listPasswordItems(sinceRevision: Long?): PasswordItemListView

    suspend fun listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?): List<PasswordRecipientView>

    suspend fun listWaitingPasswordShares(): List<PasswordWaitingShareView>

    /** The server agent key's public key, standard base64 of 65 bytes. */
    suspend fun readPasswordsAgentKey(): String

    suspend fun savePasswordItem(input: PasswordItemSaveInput): PasswordItemView

    suspend fun deletePasswordItem(id: String): PasswordItemView

    suspend fun restorePasswordItem(id: String): PasswordItemView

    /** Deletes an item the person deleted, and its old versions, for good. The server needs the person's own tap (the app's sign-in), not a token. */
    suspend fun purgePasswordItem(id: String): Boolean

    suspend fun setPasswordItemShares(itemId: String, grantees: List<PasswordShareGranteeInput>, keys: List<PasswordShareKeyInput>): PasswordItemView

    suspend fun addPasswordShareKeys(keys: List<PasswordItemShareKeyInput>): Int

    suspend fun setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Boolean, itemKeyByAgentKey: String?): PasswordItemView

    /** Gives the item a new item key. */
    suspend fun rekeyPasswordItem(input: PasswordItemRekeyInput): PasswordItemView

    /** The earlier versions of an item the person owns, newest first. The server needs the person's own tap, not a token. */
    suspend fun listPasswordItemVersions(itemId: String): List<PasswordItemVersionView>

    /**
     * Makes an earlier version's fields and item key the item's own. The server drops the item's share keys and agent copy
     * and marks it as needing a new key; the device gives it one at once. Needs the person's own tap.
     */
    suspend fun restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Long): PasswordItemView
}

/** The GraphQL documents of Passwords: the same text as the iPhone app's (ios/TeaNodeKit/Sources/TeaNodeKit/PasswordsAPI.swift). */
object PasswordsDocuments {
    // Each shared list of fields is written out in full, nested ones too: the server's test of the app's documents fills
    // in one level of them.
    private const val SETUP_FIELDS = "isSetUp passphraseSalt passphraseIterationCount publicKey lockAfterMinutes itemCount"
    private const val UNLOCK_FIELDS = "accountKeyWrapped privateKeyByAccountKey publicKey"
    private const val RECIPIENT_FIELDS = "userId name username publicKey hasKey hasTooMuchShared"
    private const val ITEM_FIELDS =
        "id ownerUserId ownerName ownerPublicKey itemKind encryptedFields itemKey isSharedWithMe revision isUsableByAgent lastUsedByAgentAt deletedAt createdAt updatedAt needsNewKey\n" +
            "shares { id granteeUserId granteeCircleId granteeName recipients { userId name username publicKey hasKey hasTooMuchShared } }"

    const val PASSWORDS_SETUP = "query { ReadPasswordsSetup { $SETUP_FIELDS } }"
    const val SET_UP_PASSWORDS =
        "mutation (\$accountKeyByPassphrase: String!, \$passphraseSalt: String!, \$passphraseIterationCount: Int!, \$passphraseProofHash: String!, \$accountKeyByRecoveryKey: String!, \$recoveryKeyProofHash: String!, \$publicKey: String!, \$privateKeyByAccountKey: String!) {\n" +
            "  SetUpPasswords(input: { accountKeyByPassphrase: \$accountKeyByPassphrase, passphraseSalt: \$passphraseSalt, passphraseIterationCount: \$passphraseIterationCount, passphraseProofHash: \$passphraseProofHash, accountKeyByRecoveryKey: \$accountKeyByRecoveryKey, recoveryKeyProofHash: \$recoveryKeyProofHash, publicKey: \$publicKey, privateKeyByAccountKey: \$privateKeyByAccountKey }) { $SETUP_FIELDS }\n}"
    const val UNLOCK_PASSWORDS =
        "mutation (\$proof: String!, \$isRecoveryKey: Boolean) {\n  UnlockPasswords(proof: \$proof, isRecoveryKey: \$isRecoveryKey) { $UNLOCK_FIELDS }\n}"
    const val CHANGE_PASSWORDS_PASSPHRASE =
        "mutation (\$proof: String!, \$isRecoveryKey: Boolean, \$accountKeyByPassphrase: String!, \$passphraseSalt: String!, \$passphraseIterationCount: Int!, \$passphraseProofHash: String!) {\n" +
            "  ChangePasswordsPassphrase(input: { proof: \$proof, isRecoveryKey: \$isRecoveryKey, accountKeyByPassphrase: \$accountKeyByPassphrase, passphraseSalt: \$passphraseSalt, passphraseIterationCount: \$passphraseIterationCount, passphraseProofHash: \$passphraseProofHash }) { $SETUP_FIELDS }\n}"
    const val REPLACE_PASSWORDS_RECOVERY_KEY =
        "mutation (\$proof: String!, \$accountKeyByRecoveryKey: String!, \$recoveryKeyProofHash: String!) {\n" +
            "  ReplacePasswordsRecoveryKey(input: { proof: \$proof, accountKeyByRecoveryKey: \$accountKeyByRecoveryKey, recoveryKeyProofHash: \$recoveryKeyProofHash }) { $SETUP_FIELDS }\n}"
    const val UPDATE_PASSWORDS_SETTINGS =
        "mutation (\$lockAfterMinutes: Int!) { UpdatePasswordsSettings(lockAfterMinutes: \$lockAfterMinutes) { $SETUP_FIELDS } }"
    const val ERASE_PASSWORDS = "mutation { ErasePasswords }"

    const val PASSWORD_ITEMS =
        "query (\$sinceRevision: Int64) {\n  ListPasswordItems(sinceRevision: \$sinceRevision) {\n    items { $ITEM_FIELDS }\n    itemIds\n    revision\n  }\n}"
    const val PASSWORD_RECIPIENTS =
        "query (\$granteeUserId: String, \$granteeCircleId: String) {\n  ListPasswordRecipients(granteeUserId: \$granteeUserId, granteeCircleId: \$granteeCircleId) { $RECIPIENT_FIELDS }\n}"
    const val WAITING_PASSWORD_SHARES =
        "query { ListWaitingPasswordShares { itemId hasMoreWaiting recipient { userId name username publicKey hasKey hasTooMuchShared } } }"
    const val PASSWORDS_AGENT_KEY = "query { ReadPasswordsAgentKey }"

    const val SAVE_PASSWORD_ITEM =
        "mutation (\$id: String!, \$itemKind: String!, \$encryptedFields: String!, \$itemKey: String!, \$baseRevision: Int64) {\n" +
            "  SavePasswordItem(input: { id: \$id, itemKind: \$itemKind, encryptedFields: \$encryptedFields, itemKey: \$itemKey, baseRevision: \$baseRevision }) { $ITEM_FIELDS }\n}"
    const val DELETE_PASSWORD_ITEM = "mutation (\$id: String!) { DeletePasswordItem(id: \$id) { $ITEM_FIELDS } }"
    const val RESTORE_PASSWORD_ITEM = "mutation (\$id: String!) { RestorePasswordItem(id: \$id) { $ITEM_FIELDS } }"
    const val PURGE_PASSWORD_ITEM = "mutation (\$id: String!) { PurgePasswordItem(id: \$id) }"
    const val SET_PASSWORD_ITEM_SHARES =
        "mutation (\$itemId: String!, \$grantees: [PasswordShareGranteeInput!]!, \$keys: [PasswordShareKeyInput!]!) {\n" +
            "  SetPasswordItemShares(input: { itemId: \$itemId, grantees: \$grantees, keys: \$keys }) { $ITEM_FIELDS }\n}"
    const val ADD_PASSWORD_SHARE_KEYS = "mutation (\$keys: [PasswordItemShareKeyInput!]!) { AddPasswordShareKeys(keys: \$keys) }"
    const val SET_PASSWORD_ITEM_USABLE_BY_AGENT =
        "mutation (\$itemId: String!, \$isUsableByAgent: Boolean!, \$itemKeyByAgentKey: String) {\n" +
            "  SetPasswordItemUsableByAgent(input: { itemId: \$itemId, isUsableByAgent: \$isUsableByAgent, itemKeyByAgentKey: \$itemKeyByAgentKey }) { $ITEM_FIELDS }\n}"
    const val REKEY_PASSWORD_ITEM =
        "mutation (\$id: String!, \$baseRevision: Int64!, \$encryptedFields: String!, \$itemKey: String!, \$shareKeys: [PasswordItemShareKeyInput!]!, \$itemKeyByAgentKey: String) {\n" +
            "  RekeyPasswordItem(input: { id: \$id, baseRevision: \$baseRevision, encryptedFields: \$encryptedFields, itemKey: \$itemKey, shareKeys: \$shareKeys, itemKeyByAgentKey: \$itemKeyByAgentKey }) { $ITEM_FIELDS }\n}"

    const val PASSWORD_ITEM_VERSIONS =
        "query (\$itemId: String!) {\n  ListPasswordItemVersions(itemId: \$itemId) { versionId savedAt isSavedByPerson encryptedFields itemKey itemKind }\n}"
    const val RESTORE_PASSWORD_ITEM_VERSION =
        "mutation (\$itemId: String!, \$versionId: String!, \$baseRevision: Int64!) {\n" +
            "  RestorePasswordItemVersion(itemId: \$itemId, versionId: \$versionId, baseRevision: \$baseRevision) { $ITEM_FIELDS }\n}"

    /** Every Passwords document, for a check against the server's schema. */
    val all: Map<String, String> = mapOf(
        "passwordsSetup" to PASSWORDS_SETUP, "setUpPasswords" to SET_UP_PASSWORDS, "unlockPasswords" to UNLOCK_PASSWORDS,
        "changePasswordsPassphrase" to CHANGE_PASSWORDS_PASSPHRASE, "replacePasswordsRecoveryKey" to REPLACE_PASSWORDS_RECOVERY_KEY,
        "updatePasswordsSettings" to UPDATE_PASSWORDS_SETTINGS, "erasePasswords" to ERASE_PASSWORDS, "passwordItems" to PASSWORD_ITEMS,
        "passwordRecipients" to PASSWORD_RECIPIENTS, "waitingPasswordShares" to WAITING_PASSWORD_SHARES,
        "passwordsAgentKey" to PASSWORDS_AGENT_KEY, "savePasswordItem" to SAVE_PASSWORD_ITEM, "deletePasswordItem" to DELETE_PASSWORD_ITEM,
        "restorePasswordItem" to RESTORE_PASSWORD_ITEM, "purgePasswordItem" to PURGE_PASSWORD_ITEM,
        "setPasswordItemShares" to SET_PASSWORD_ITEM_SHARES, "addPasswordShareKeys" to ADD_PASSWORD_SHARE_KEYS,
        "setPasswordItemUsableByAgent" to SET_PASSWORD_ITEM_USABLE_BY_AGENT, "rekeyPasswordItem" to REKEY_PASSWORD_ITEM,
        "passwordItemVersions" to PASSWORD_ITEM_VERSIONS, "restorePasswordItemVersion" to RESTORE_PASSWORD_ITEM_VERSION,
    )
}

/** The real [PasswordsApi]: GraphQL over a [Connection]. */
class ConnectionPasswordsApi(private val connection: Connection) : PasswordsApi {
    private suspend fun <Answer> call(
        document: String,
        field: String,
        deserializer: KSerializer<Answer>,
        variables: Map<String, JsonElement> = emptyMap(),
    ): Answer {
        val answer: JsonObject = connection.execute(document, variables)
        val element = answer[field] ?: throw com.teanode.kit.TeaNodeError.UnreadableAnswer
        return try {
            TeaNodeJson.decodeFromJsonElement(deserializer, element)
        } catch (_: Exception) {
            throw com.teanode.kit.TeaNodeError.UnreadableAnswer
        }
    }

    private fun <Input> variablesOf(serializer: KSerializer<Input>, input: Input): Map<String, JsonElement> =
        (TeaNodeJson.encodeToJsonElement(serializer, input) as JsonObject).toMap()

    override suspend fun readPasswordsSetup(): PasswordsSetupView =
        call(PasswordsDocuments.PASSWORDS_SETUP, "ReadPasswordsSetup", serializer())

    override suspend fun setUpPasswords(input: PasswordsSetUpInput): PasswordsSetupView =
        call(PasswordsDocuments.SET_UP_PASSWORDS, "SetUpPasswords", serializer(), variablesOf(serializer(), input))

    override suspend fun unlockPasswords(proof: String, isRecoveryKey: Boolean): PasswordsUnlockView = call(
        PasswordsDocuments.UNLOCK_PASSWORDS, "UnlockPasswords", serializer(),
        mapOf("proof" to JsonPrimitive(proof), "isRecoveryKey" to JsonPrimitive(isRecoveryKey)),
    )

    override suspend fun changePasswordsPassphrase(input: PasswordsChangePassphraseInput): PasswordsSetupView =
        call(PasswordsDocuments.CHANGE_PASSWORDS_PASSPHRASE, "ChangePasswordsPassphrase", serializer(), variablesOf(serializer(), input))

    override suspend fun replacePasswordsRecoveryKey(input: PasswordsReplaceRecoveryKeyInput): PasswordsSetupView =
        call(PasswordsDocuments.REPLACE_PASSWORDS_RECOVERY_KEY, "ReplacePasswordsRecoveryKey", serializer(), variablesOf(serializer(), input))

    override suspend fun updatePasswordsSettings(lockAfterMinutes: Int): PasswordsSetupView = call(
        PasswordsDocuments.UPDATE_PASSWORDS_SETTINGS, "UpdatePasswordsSettings", serializer(),
        mapOf("lockAfterMinutes" to JsonPrimitive(lockAfterMinutes)),
    )

    override suspend fun erasePasswords(): Boolean = call(PasswordsDocuments.ERASE_PASSWORDS, "ErasePasswords", serializer())

    override suspend fun listPasswordItems(sinceRevision: Long?): PasswordItemListView {
        val variables = if (sinceRevision != null) mapOf("sinceRevision" to JsonPrimitive(sinceRevision)) else emptyMap()
        return call(PasswordsDocuments.PASSWORD_ITEMS, "ListPasswordItems", serializer(), variables)
    }

    override suspend fun listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?): List<PasswordRecipientView> {
        val variables = mutableMapOf<String, JsonElement>()
        granteeUserId?.let { value -> variables["granteeUserId"] = JsonPrimitive(value) }
        granteeCircleId?.let { value -> variables["granteeCircleId"] = JsonPrimitive(value) }
        return call(PasswordsDocuments.PASSWORD_RECIPIENTS, "ListPasswordRecipients", ListSerializer(serializer<PasswordRecipientView>()), variables)
    }

    override suspend fun listWaitingPasswordShares(): List<PasswordWaitingShareView> =
        call(PasswordsDocuments.WAITING_PASSWORD_SHARES, "ListWaitingPasswordShares", ListSerializer(serializer<PasswordWaitingShareView>()))

    override suspend fun readPasswordsAgentKey(): String = call(PasswordsDocuments.PASSWORDS_AGENT_KEY, "ReadPasswordsAgentKey", serializer())

    override suspend fun savePasswordItem(input: PasswordItemSaveInput): PasswordItemView =
        call(PasswordsDocuments.SAVE_PASSWORD_ITEM, "SavePasswordItem", serializer(), variablesOf(serializer(), input))

    override suspend fun deletePasswordItem(id: String): PasswordItemView =
        call(PasswordsDocuments.DELETE_PASSWORD_ITEM, "DeletePasswordItem", serializer(), mapOf("id" to JsonPrimitive(id)))

    override suspend fun restorePasswordItem(id: String): PasswordItemView =
        call(PasswordsDocuments.RESTORE_PASSWORD_ITEM, "RestorePasswordItem", serializer(), mapOf("id" to JsonPrimitive(id)))

    override suspend fun purgePasswordItem(id: String): Boolean =
        call(PasswordsDocuments.PURGE_PASSWORD_ITEM, "PurgePasswordItem", serializer(), mapOf("id" to JsonPrimitive(id)))

    override suspend fun setPasswordItemShares(
        itemId: String,
        grantees: List<PasswordShareGranteeInput>,
        keys: List<PasswordShareKeyInput>,
    ): PasswordItemView {
        val variables = mapOf(
            "itemId" to JsonPrimitive(itemId),
            "grantees" to TeaNodeJson.encodeToJsonElement(ListSerializer(serializer<PasswordShareGranteeInput>()), grantees),
            "keys" to TeaNodeJson.encodeToJsonElement(ListSerializer(serializer<PasswordShareKeyInput>()), keys),
        )
        return call(PasswordsDocuments.SET_PASSWORD_ITEM_SHARES, "SetPasswordItemShares", serializer(), variables)
    }

    override suspend fun addPasswordShareKeys(keys: List<PasswordItemShareKeyInput>): Int = call(
        PasswordsDocuments.ADD_PASSWORD_SHARE_KEYS, "AddPasswordShareKeys", serializer(),
        mapOf("keys" to TeaNodeJson.encodeToJsonElement(ListSerializer(serializer<PasswordItemShareKeyInput>()), keys)),
    )

    override suspend fun setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Boolean, itemKeyByAgentKey: String?): PasswordItemView {
        val variables = mutableMapOf<String, JsonElement>("itemId" to JsonPrimitive(itemId), "isUsableByAgent" to JsonPrimitive(isUsableByAgent))
        itemKeyByAgentKey?.let { value -> variables["itemKeyByAgentKey"] = JsonPrimitive(value) }
        return call(PasswordsDocuments.SET_PASSWORD_ITEM_USABLE_BY_AGENT, "SetPasswordItemUsableByAgent", serializer(), variables)
    }

    override suspend fun rekeyPasswordItem(input: PasswordItemRekeyInput): PasswordItemView =
        call(PasswordsDocuments.REKEY_PASSWORD_ITEM, "RekeyPasswordItem", serializer(), variablesOf(serializer(), input))

    override suspend fun listPasswordItemVersions(itemId: String): List<PasswordItemVersionView> = call(
        PasswordsDocuments.PASSWORD_ITEM_VERSIONS, "ListPasswordItemVersions", ListSerializer(serializer<PasswordItemVersionView>()),
        mapOf("itemId" to JsonPrimitive(itemId)),
    )

    override suspend fun restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Long): PasswordItemView = call(
        PasswordsDocuments.RESTORE_PASSWORD_ITEM_VERSION, "RestorePasswordItemVersion", serializer(),
        mapOf("itemId" to JsonPrimitive(itemId), "versionId" to JsonPrimitive(versionId), "baseRevision" to JsonPrimitive(baseRevision)),
    )
}
