package com.teanode.kit.manage

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull

/** What the outside world sees this server as. */
@Serializable
data class ServerAddresses(val ipv4: String? = null, val ipv6: String? = null, @SerialName("error") val errorMessage: String? = null) {
    val isKnown: Boolean get() = !ipv4.isNullOrEmpty() || !ipv6.isNullOrEmpty()
}

/** What a receiver sees when this server reaches out: where from, the reverse name, and the name it greets with. */
@Serializable
data class OutgoingIdentity(
    val address: String? = null,
    /** direct, proxy or relay. */
    val via: String = "direct",
    val reverseName: String? = null,
    val forwardAddresses: List<String>? = null,
    @SerialName("confirmed") val isConfirmed: Boolean = false,
    val helloName: String? = null,
    val helloAddresses: List<String>? = null,
    @SerialName("helloMatches") val doesHelloMatch: Boolean = false,
    /** Why there is nothing to check, such as a relay sending as itself. */
    @SerialName("unknown") val uncheckableReason: String? = null,
)

/** The address a mail client is told to send through: what is set, and what it comes to. */
@Serializable
data class SubmissionAccess(val host: String? = null, val port: Int? = null, val effectiveHost: String = "", val effectivePort: String = "")

/** The address a mail program is told to read mail at. */
@Serializable
data class MailProgramAccess(
    val host: String? = null,
    val port: Int? = null,
    val tlsPort: Int? = null,
    val effectiveHost: String = "",
    val effectivePort: String = "",
    val effectiveTlsPort: String = "",
) {
    /** Whether anything listens for mail programs at all. */
    val isListening: Boolean get() = effectivePort.isNotEmpty() || effectiveTlsPort.isNotEmpty()
}

/** The Setup tab: what an operator needs to finish setting the server up. */
data class ServerSetup(val addresses: ServerAddresses?, val outgoingIdentity: OutgoingIdentity?, val submission: SubmissionAccess?, val mailProgram: MailProgramAccess?)

/** The submission address as its form holds it. Both empty follows the server. */
data class SubmissionAccessDraft(val host: String = "", val portText: String = "") {
    constructor(submission: SubmissionAccess) : this(submission.host.orEmpty(), submission.port?.takeIf { port -> port > 0 }?.toString().orEmpty())

    val variables: JsonObject get() = JsonObject(mapOf("host" to JsonPrimitive(host.trim()), "port" to JsonPrimitive(digits(portText).toIntOrNull() ?: 0)))
}

/** The mail program address as its form holds it. All empty follows the server. */
data class MailProgramAccessDraft(val host: String = "", val portText: String = "", val tlsPortText: String = "") {
    constructor(mailProgram: MailProgramAccess) : this(
        mailProgram.host.orEmpty(), mailProgram.port?.takeIf { port -> port > 0 }?.toString().orEmpty(), mailProgram.tlsPort?.takeIf { port -> port > 0 }?.toString().orEmpty(),
    )

    val variables: JsonObject
        get() = JsonObject(mapOf("host" to JsonPrimitive(host.trim()), "port" to JsonPrimitive(digits(portText).toIntOrNull() ?: 0), "tlsPort" to JsonPrimitive(digits(tlsPortText).toIntOrNull() ?: 0)))
}

/** Only the digits of what was typed, for a port. */
fun digits(text: String): String = text.filter { character -> character in '0'..'9' }

/** The addresses in the words of the advice an operator needs, when a reverse name or greeting does not resolve back. */
object ServerSetupAdvice {
    /** The addresses a name resolves to, or null when it resolves to nothing. */
    fun resolves(addresses: List<String>?): String? = addresses.orEmpty().joinToString(", ").ifEmpty { null }

    /** Whether the reverse name needs to change, and what is wrong with it. */
    fun reverseProblem(identity: OutgoingIdentity): ReverseProblem? = when {
        identity.isConfirmed -> null
        identity.reverseName.isNullOrEmpty() -> ReverseProblem.NoReverseName
        else -> ReverseProblem.DoesNotResolveBack
    }

    enum class ReverseProblem { NoReverseName, DoesNotResolveBack }
}

private fun parseSetup(answer: JsonObject): ServerSetup {
    fun <T> decode(element: JsonElement?, read: (JsonObject) -> T): T? = (element as? JsonObject)?.let(read)
    val json = com.teanode.kit.TeaNodeJson
    val settings = answer["GetSettings"] as? JsonObject
    return ServerSetup(
        addresses = decode(answer["GetServerAddresses"]) { value -> json.decodeFromJsonElement(ServerAddresses.serializer(), value) },
        outgoingIdentity = decode(answer["GetOutgoingIdentity"]) { value -> json.decodeFromJsonElement(OutgoingIdentity.serializer(), value) },
        submission = decode(settings?.get("submission")) { value -> json.decodeFromJsonElement(SubmissionAccess.serializer(), value) },
        mailProgram = decode(settings?.get("imap")) { value -> json.decodeFromJsonElement(MailProgramAccess.serializer(), value) },
    )
}

/** The Setup tab: the address seen from outside, how outgoing mail identifies itself, and what mail clients are told. */
suspend fun Connection.serverSetup(): ServerSetup = parseSetup(execute<JsonObject>(ManageServerDocuments.MANAGE_SERVER_SETUP))

suspend fun Connection.updateSubmissionAccess(draft: SubmissionAccessDraft) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_UPDATE_SUBMISSION, mapOf("submission" to draft.variables))
}

suspend fun Connection.updateMailProgramAccess(draft: MailProgramAccessDraft) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_UPDATE_MAIL_PROGRAM, mapOf("imap" to draft.variables))
}

/**
 * One identity provider as its form holds it. The client secret is written and never read back: blank keeps the one
 * stored.
 */
data class SsoProviderDraft(
    val key: Int,
    val providerId: String = "",
    val name: String = "",
    val issuer: String = "",
    val clientId: String = "",
    val clientSecret: String = "",
    val hasClientSecret: Boolean = false,
    val groupsClaim: String = "",
    val shouldCreateUsers: Boolean = false,
) {
    /** Where the provider sends a person back, to register at the provider. */
    fun redirectAddress(serverOrigin: String): String = "$serverOrigin/api/v1/sso/${providerId.trim().ifEmpty { "id" }}/callback"

    /** Whether the fields the server needs are filled in. */
    val isComplete: Boolean get() = listOf(providerId, name, issuer, clientId).all { text -> text.isNotBlank() }

    val parameters: JsonObject
        get() = JsonObject(
            mapOf(
                "id" to JsonPrimitive(providerId.trim()), "name" to JsonPrimitive(name.trim()), "issuer" to JsonPrimitive(issuer.trim()), "clientId" to JsonPrimitive(clientId.trim()),
                "clientSecret" to if (clientSecret.isEmpty()) JsonNull else JsonPrimitive(clientSecret),
                "groupsClaim" to if (groupsClaim.isBlank()) JsonNull else JsonPrimitive(groupsClaim.trim()), "createUsers" to JsonPrimitive(shouldCreateUsers),
            ),
        )
}

/** The identity providers as the form holds them: the list sent replaces the stored one. */
object SsoProviders {
    /** The providers of the `sso` section of `GetSettings`. */
    fun drafts(section: JsonObject?): List<SsoProviderDraft> =
        (section?.get("providers") as? JsonArray).orEmpty().mapIndexedNotNull { index, element ->
            val provider = element as? JsonObject ?: return@mapIndexedNotNull null
            fun text(name: String): String = (provider[name] as? JsonPrimitive)?.contentOrNull.orEmpty()
            SsoProviderDraft(
                key = index, providerId = text("id"), name = text("name"), issuer = text("issuer"), clientId = text("clientId"),
                hasClientSecret = (provider["hasClientSecret"] as? JsonPrimitive)?.booleanOrNull == true, groupsClaim = text("groupsClaim"),
                shouldCreateUsers = (provider["createUsers"] as? JsonPrimitive)?.booleanOrNull == true,
            )
        }

    fun canSave(providers: List<SsoProviderDraft>): Boolean = providers.all { provider -> provider.isComplete }

    /** What is sent: every provider, replacing the stored list. */
    fun changes(providers: List<SsoProviderDraft>): JsonObject = JsonObject(mapOf("providers" to JsonArray(providers.map { provider -> provider.parameters })))
}
