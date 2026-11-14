package com.teanode.kit.passwords

import com.teanode.kit.stringValue
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import java.util.Base64

// What is inside an item once it is decrypted. The names are the ones the plan fixes (docs/planning/passwords-execplan.md,
// "Item fields"); the Go, web, command line and iPhone code spell them the same way, because the fields travel as one
// JSON text sealed with the item key. Another client may leave out an empty field, so decoding fills every missing one
// with an empty value.

/** PasswordsItemKind is what an item is. It is also part of the associated data of the sealed fields. */
enum class PasswordsItemKind(val rawValue: String) {
    Login("login"),
    Card("card"),
    Note("note");

    companion object {
        fun fromRawValue(rawValue: String): PasswordsItemKind? = entries.firstOrNull { kind -> kind.rawValue == rawValue }
    }
}

/** PasswordsFieldsException is a sealed value that opened but is not the JSON the format describes. */
class PasswordsFieldsException(description: String) : Exception(description)

/** PasswordsGrantee is one person or one circle that may have an item: `{"userId": "..."}` or `{"circleId": "..."}`. */
sealed class PasswordsGrantee {
    data class Person(val userId: String) : PasswordsGrantee()

    data class Circle(val circleId: String) : PasswordsGrantee()

    fun toJson(): JsonElement = when (this) {
        is Person -> JsonObject(mapOf("userId" to JsonPrimitive(userId)))
        is Circle -> JsonObject(mapOf("circleId" to JsonPrimitive(circleId)))
    }

    companion object {
        fun fromJson(element: JsonElement): PasswordsGrantee {
            val entries = (element as? JsonObject)?.entries?.toList()
            if (entries == null || entries.size != 1) throw PasswordsFieldsException("A grantee needs exactly one of userId and circleId")
            val identifier = entries.first().value.stringValue ?: throw PasswordsFieldsException("A grantee needs a text id")
            if (identifier.isEmpty()) throw PasswordsFieldsException("A grantee needs a non-empty id")
            return when (entries.first().key) {
                "userId" -> Person(identifier)
                "circleId" -> Circle(identifier)
                else -> throw PasswordsFieldsException("A grantee needs exactly one of userId and circleId")
            }
        }
    }
}

/**
 * PasswordsItemAccess is who may have an item and whether the agent may use it. It is a key inside the sealed fields of
 * every kind of item, written only by the owner's device, so the server cannot widen it. An item with no record grants
 * nobody and is not usable by the agent. A recipient's copy may show the record but never acts on it.
 *
 * `recordVersion` and `recordMac` are its signature: the version counts the owner's changes, and the mac is the base64
 * of an HMAC-SHA256 under a key made from the account key, which only the owner's devices hold. A record may lack both,
 * as one written before they existed; [PasswordsCrypto.verifyAccess] does not trust such a record.
 *
 * Decoding accepts exactly this shape: an object with `isUsableByAgent` and `grantees`, and optionally `recordVersion`
 * (a positive integer) and `recordMac` (canonical base64 of 32 bytes), and nothing else.
 */
data class PasswordsItemAccess(
    val isUsableByAgent: Boolean = false,
    val grantees: List<PasswordsGrantee> = emptyList(),
    /** Null for a record that has none, else a positive integer. */
    val recordVersion: Long? = null,
    /** Null for a record that has none, else the canonical base64 of 32 bytes. */
    val recordMac: String? = null,
) {
    fun toJson(): JsonObject {
        val entries = mutableMapOf<String, JsonElement>(
            "isUsableByAgent" to JsonPrimitive(isUsableByAgent),
            "grantees" to JsonArray(grantees.map { grantee -> grantee.toJson() }),
        )
        recordVersion?.let { version -> entries["recordVersion"] = JsonPrimitive(version) }
        recordMac?.let { mac -> entries["recordMac"] = JsonPrimitive(mac) }
        return JsonObject(entries)
    }

    companion object {
        fun fromJson(element: JsonElement): PasswordsItemAccess {
            val entries = element as? JsonObject
                ?: throw PasswordsFieldsException("An access record is an object")
            val required = setOf("isUsableByAgent", "grantees")
            val allowed = required + setOf("recordVersion", "recordMac")
            if (!entries.keys.containsAll(required) || !allowed.containsAll(entries.keys)) {
                throw PasswordsFieldsException("An access record is isUsableByAgent and grantees, and may have recordVersion and recordMac")
            }
            val isUsableByAgent = (entries["isUsableByAgent"] as? JsonPrimitive)?.takeIf { primitive -> !primitive.isString }?.booleanOrNull
                ?: throw PasswordsFieldsException("isUsableByAgent is a boolean")
            val granteesArray = entries["grantees"] as? JsonArray ?: throw PasswordsFieldsException("grantees is a list")
            val grantees = granteesArray.map { grantee -> PasswordsGrantee.fromJson(grantee) }
            var recordVersion: Long? = null
            if (entries.containsKey("recordVersion")) {
                // A whole number may be written 1.0 or 1e0 (JSON numbers are not told apart by spelling), never as text, null or with a fraction.
                val version = (entries["recordVersion"] as? JsonPrimitive)?.takeIf { !it.isString && it !is JsonNull }?.let { number ->
                    runCatching { java.math.BigDecimal(number.content).toBigIntegerExact().takeIf { it.bitLength() < 63 }?.toLong() }.getOrNull()
                }
                if (version == null || version < 1 || version > PasswordsCrypto.MAXIMUM_RECORD_VERSION) {
                    throw PasswordsFieldsException("A record version is a positive integer")
                }
                recordVersion = version
            }
            var recordMac: String? = null
            if (entries.containsKey("recordMac")) {
                val text = entries["recordMac"]?.stringValue
                val bytes = text?.let { value -> runCatching { Base64.getDecoder().decode(value) }.getOrNull() }
                if (text == null || bytes == null || bytes.size != PasswordsCrypto.KEY_BYTE_COUNT ||
                    Base64.getEncoder().encodeToString(bytes) != text
                ) {
                    throw PasswordsFieldsException("A record mac is canonical base64 of 32 bytes")
                }
                recordMac = text
            }
            return PasswordsItemAccess(isUsableByAgent, grantees, recordVersion, recordMac)
        }

        /**
         * Whether the record grants an item to a recipient: a person grantee by user id, or a circle grantee when the
         * server lists the recipient as a member of that circle. The recipient also needs a key before anything is
         * sealed for them. No record reaches nobody.
         */
        fun isReachedByAccess(access: PasswordsItemAccess?, recipientUserId: String, circleIdsOfRecipient: List<String>): Boolean {
            if (access == null || recipientUserId.isEmpty()) return false
            return access.grantees.any { grantee ->
                when (grantee) {
                    is PasswordsGrantee.Person -> grantee.userId == recipientUserId
                    is PasswordsGrantee.Circle -> circleIdsOfRecipient.contains(grantee.circleId)
                }
            }
        }

        /** A record with these grantees and the same agent use, without a version or signature: sign it again before it is sealed. */
        fun accessWithGrantees(access: PasswordsItemAccess?, grantees: List<PasswordsGrantee>): PasswordsItemAccess =
            PasswordsItemAccess(isUsableByAgent = access?.isUsableByAgent ?: false, grantees = grantees)

        /** A record with agent use turned on or off and the same grantees, without a version or signature. */
        fun accessWithAgentUse(access: PasswordsItemAccess?, isUsableByAgent: Boolean): PasswordsItemAccess =
            PasswordsItemAccess(isUsableByAgent = isUsableByAgent, grantees = access?.grantees ?: emptyList())
    }
}

/**
 * PasswordsPasskeyFields is a passkey kept inside a login. `privateKey` is PKCS#8 DER (ES256) in base64; `credentialId`
 * and `userHandle` are base64url, as a website sends them; `createdAt` is an ISO 8601 time.
 */
data class PasswordsPasskeyFields(
    val credentialId: String = "",
    val relyingPartyId: String = "",
    val userHandle: String = "",
    val userName: String = "",
    val userDisplayName: String = "",
    val privateKey: String = "",
    val createdAt: String = "",
) {
    fun toJson(): JsonObject = JsonObject(
        mapOf(
            "credentialId" to JsonPrimitive(credentialId),
            "relyingPartyId" to JsonPrimitive(relyingPartyId),
            "userHandle" to JsonPrimitive(userHandle),
            "userName" to JsonPrimitive(userName),
            "userDisplayName" to JsonPrimitive(userDisplayName),
            "privateKey" to JsonPrimitive(privateKey),
            "createdAt" to JsonPrimitive(createdAt),
        ),
    )

    companion object {
        fun fromJson(element: JsonElement): PasswordsPasskeyFields = PasswordsPasskeyFields(
            credentialId = element.text("credentialId"),
            relyingPartyId = element.text("relyingPartyId"),
            userHandle = element.text("userHandle"),
            userName = element.text("userName"),
            userDisplayName = element.text("userDisplayName"),
            privateKey = element.text("privateKey"),
            createdAt = element.text("createdAt"),
        )
    }
}

/** A login: a user name and password for some sites, with an optional one-time code secret and passkeys. */
data class PasswordsLoginFields(
    val name: String = "",
    val userName: String = "",
    val password: String = "",
    /** Web addresses the login is for, like `https://example.com/sign-in`. */
    val sites: List<String> = emptyList(),
    /** An `otpauth://totp/...` link, or empty. */
    val oneTimeCodeUri: String = "",
    val notes: String = "",
    val passkeys: List<PasswordsPasskeyFields> = emptyList(),
    val folder: String = "",
    /** Who may have this login and whether the agent may use it; null is nobody and not the agent. */
    val access: PasswordsItemAccess? = null,
) {
    fun toJson(): JsonObject {
        val entries = mutableMapOf<String, JsonElement>(
            "name" to JsonPrimitive(name),
            "userName" to JsonPrimitive(userName),
            "password" to JsonPrimitive(password),
            "sites" to JsonArray(sites.map { site -> JsonPrimitive(site) }),
            "oneTimeCodeUri" to JsonPrimitive(oneTimeCodeUri),
            "notes" to JsonPrimitive(notes),
            "passkeys" to JsonArray(passkeys.map { passkey -> passkey.toJson() }),
            "folder" to JsonPrimitive(folder),
        )
        access?.let { record -> entries["access"] = record.toJson() }
        return JsonObject(entries)
    }

    companion object {
        fun fromJson(element: JsonElement): PasswordsLoginFields = PasswordsLoginFields(
            name = element.text("name"),
            userName = element.text("userName"),
            password = element.text("password"),
            sites = element.texts("sites"),
            oneTimeCodeUri = element.text("oneTimeCodeUri"),
            notes = element.text("notes"),
            passkeys = (element.child("passkeys") as? JsonArray)?.map { passkey -> PasswordsPasskeyFields.fromJson(passkey) } ?: emptyList(),
            folder = element.text("folder"),
            access = element.child("access")?.let { record -> PasswordsItemAccess.fromJson(record) },
        )
    }
}

/** A payment card. Cards are kept, shown and copied; nothing fills them. */
data class PasswordsCardFields(
    val name: String = "",
    val cardholderName: String = "",
    val number: String = "",
    val expiryMonth: String = "",
    val expiryYear: String = "",
    val securityCode: String = "",
    val brand: String = "",
    val notes: String = "",
    val folder: String = "",
    /** Who may have this card; null is nobody. The agent never uses a card, whatever the record says. */
    val access: PasswordsItemAccess? = null,
) {
    fun toJson(): JsonObject {
        val entries = mutableMapOf<String, JsonElement>(
            "name" to JsonPrimitive(name),
            "cardholderName" to JsonPrimitive(cardholderName),
            "number" to JsonPrimitive(number),
            "expiryMonth" to JsonPrimitive(expiryMonth),
            "expiryYear" to JsonPrimitive(expiryYear),
            "securityCode" to JsonPrimitive(securityCode),
            "brand" to JsonPrimitive(brand),
            "notes" to JsonPrimitive(notes),
            "folder" to JsonPrimitive(folder),
        )
        access?.let { record -> entries["access"] = record.toJson() }
        return JsonObject(entries)
    }

    companion object {
        fun fromJson(element: JsonElement): PasswordsCardFields = PasswordsCardFields(
            name = element.text("name"),
            cardholderName = element.text("cardholderName"),
            number = element.text("number"),
            expiryMonth = element.text("expiryMonth"),
            expiryYear = element.text("expiryYear"),
            securityCode = element.text("securityCode"),
            brand = element.text("brand"),
            notes = element.text("notes"),
            folder = element.text("folder"),
            access = element.child("access")?.let { record -> PasswordsItemAccess.fromJson(record) },
        )
    }
}

/** A secure note. */
data class PasswordsNoteFields(
    val name: String = "",
    val text: String = "",
    val folder: String = "",
    /** Who may have this note and whether the agent may use it; null is nobody and not the agent. */
    val access: PasswordsItemAccess? = null,
) {
    fun toJson(): JsonObject {
        val entries = mutableMapOf<String, JsonElement>(
            "name" to JsonPrimitive(name),
            "text" to JsonPrimitive(text),
            "folder" to JsonPrimitive(folder),
        )
        access?.let { record -> entries["access"] = record.toJson() }
        return JsonObject(entries)
    }

    companion object {
        fun fromJson(element: JsonElement): PasswordsNoteFields = PasswordsNoteFields(
            name = element.text("name"),
            text = element.text("text"),
            folder = element.text("folder"),
            access = element.child("access")?.let { record -> PasswordsItemAccess.fromJson(record) },
        )
    }
}

/** PasswordsItemFields is the fields of one item, whichever kind it is. */
sealed class PasswordsItemFields {
    data class Login(val fields: PasswordsLoginFields) : PasswordsItemFields()

    data class Card(val fields: PasswordsCardFields) : PasswordsItemFields()

    data class Note(val fields: PasswordsNoteFields) : PasswordsItemFields()

    val itemKind: PasswordsItemKind
        get() = when (this) {
            is Login -> PasswordsItemKind.Login
            is Card -> PasswordsItemKind.Card
            is Note -> PasswordsItemKind.Note
        }

    /** The name every kind has, shown in lists. */
    val name: String
        get() = when (this) {
            is Login -> fields.name
            is Card -> fields.name
            is Note -> fields.name
        }

    /** The access record of any kind; null is nobody and not the agent. */
    val access: PasswordsItemAccess?
        get() = when (this) {
            is Login -> fields.access
            is Card -> fields.access
            is Note -> fields.access
        }

    /** The same fields with this access record, everything else unchanged. */
    fun withAccess(access: PasswordsItemAccess?): PasswordsItemFields = when (this) {
        is Login -> Login(fields.copy(access = access))
        is Card -> Card(fields.copy(access = access))
        is Note -> Note(fields.copy(access = access))
    }

    /** The fields as the JSON text that is sealed: keys in alphabetical order, no spaces. */
    fun jsonBytes(): ByteArray {
        val json = when (this) {
            is Login -> fields.toJson()
            is Card -> fields.toJson()
            is Note -> fields.toJson()
        }
        return sortedJsonText(json).toByteArray(Charsets.UTF_8)
    }

    companion object {
        /** Reads fields of the given kind from the JSON text that was sealed. */
        fun fromJsonBytes(itemKind: PasswordsItemKind, jsonBytes: ByteArray): PasswordsItemFields {
            val element = try {
                Json.parseToJsonElement(String(jsonBytes, Charsets.UTF_8))
            } catch (_: Exception) {
                throw PasswordsFieldsException("The fields are not JSON")
            }
            if (element !is JsonObject) throw PasswordsFieldsException("The fields are not an object")
            return when (itemKind) {
                PasswordsItemKind.Login -> Login(PasswordsLoginFields.fromJson(element))
                PasswordsItemKind.Card -> Card(PasswordsCardFields.fromJson(element))
                PasswordsItemKind.Note -> Note(PasswordsNoteFields.fromJson(element))
            }
        }
    }
}

private fun JsonElement.child(key: String): JsonElement? = (this as? JsonObject)?.get(key)?.takeIf { it !is JsonNull }

private fun JsonElement.text(key: String): String = child(key)?.stringValue ?: ""

private fun JsonElement.texts(key: String): List<String> =
    (child(key) as? JsonArray)?.mapNotNull { element -> element.stringValue } ?: emptyList()

/** JSON with every object's keys in alphabetical order and no spaces, as the iPhone app's encoder writes it. */
fun sortedJsonText(element: JsonElement): String {
    val builder = StringBuilder()
    writeSorted(element, builder)
    return builder.toString()
}

private fun writeSorted(element: JsonElement, builder: StringBuilder) {
    when (element) {
        is JsonObject -> {
            builder.append('{')
            element.entries.sortedBy { entry -> entry.key }.forEachIndexed { index, entry ->
                if (index > 0) builder.append(',')
                builder.append(quoted(entry.key)).append(':')
                writeSorted(entry.value, builder)
            }
            builder.append('}')
        }
        is JsonArray -> {
            builder.append('[')
            element.forEachIndexed { index, child ->
                if (index > 0) builder.append(',')
                writeSorted(child, builder)
            }
            builder.append(']')
        }
        is JsonPrimitive -> builder.append(if (element.isString) quoted(element.content) else element.content)
    }
}

private fun quoted(text: String): String {
    val builder = StringBuilder("\"")
    for (character in text) {
        when {
            character == '"' -> builder.append("\\\"")
            character == '\\' -> builder.append("\\\\")
            character == '\n' -> builder.append("\\n")
            character == '\r' -> builder.append("\\r")
            character == '\t' -> builder.append("\\t")
            character == '\b' -> builder.append("\\b")
            character == '\u000C' -> builder.append("\\f")
            character.code < 0x20 -> builder.append("\\u%04x".format(character.code))
            else -> builder.append(character)
        }
    }
    return builder.append('"').toString()
}
