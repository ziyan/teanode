package com.teanode.kit.notifications

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

/** AgentPhone is this phone as the server lists it. */
@Serializable
data class AgentPhone(
    val id: String,
    /** The kinds it is told about; empty is every kind. */
    val notificationKinds: List<String> = emptyList(),
    val canBeNotified: Boolean = false,
) {
    companion object {
        /** Every kind, in the order a person chooses among them. */
        val allNotificationKinds: List<String> =
            listOf("message", "approval", "question", "answer", "circle", "chat", "skill_share_call", "photo", "location", "shopping")
    }
}

object PhoneDocuments {
    /** The same document as the iPhone app's, with the platform the phone runs added (the server's `phonePlatform`). */
    const val REGISTER_PHONE = """
        mutation (${'$'}phoneName: String!, ${'$'}phoneModel: String!, ${'$'}phonePlatform: String, ${'$'}pushRegistration: String, ${'$'}voipPushRegistration: String, ${'$'}payloadKey: String) {
          RegisterAgentPhone(phoneName: ${'$'}phoneName, phoneModel: ${'$'}phoneModel, phonePlatform: ${'$'}phonePlatform, pushRegistration: ${'$'}pushRegistration, voipPushRegistration: ${'$'}voipPushRegistration, payloadKey: ${'$'}payloadKey) { id notificationKinds canBeNotified }
        }
        """
    const val UPDATE_PHONE = """
        mutation (${'$'}phoneId: String!, ${'$'}notificationKinds: [String!]) {
          UpdateAgentPhone(phoneId: ${'$'}phoneId, notificationKinds: ${'$'}notificationKinds) { id notificationKinds canBeNotified }
        }
        """
}

@Serializable
private class RegisterAnswer(@SerialName("RegisterAgentPhone") val phone: AgentPhone)

@Serializable
private class UpdateAnswer(@SerialName("UpdateAgentPhone") val phone: AgentPhone)

/**
 * Registers this phone with the server; the push registrations and the key come once the person allows notifications.
 * The registration for ringing is made from the same Firebase token as the other, with push type voip.
 */
suspend fun Connection.registerPhone(
    phoneName: String,
    phoneModel: String,
    pushRegistration: String? = null,
    voipPushRegistration: String? = null,
    payloadKey: String? = null,
    phonePlatform: String = PushRelay.PLATFORM,
): AgentPhone {
    val variables = mutableMapOf<String, JsonElement>(
        "phoneName" to JsonPrimitive(phoneName), "phoneModel" to JsonPrimitive(phoneModel), "phonePlatform" to JsonPrimitive(phonePlatform),
    )
    if (pushRegistration != null) variables["pushRegistration"] = JsonPrimitive(pushRegistration)
    if (voipPushRegistration != null) variables["voipPushRegistration"] = JsonPrimitive(voipPushRegistration)
    if (payloadKey != null) variables["payloadKey"] = JsonPrimitive(payloadKey)
    return execute(PhoneDocuments.REGISTER_PHONE, RegisterAnswer.serializer(), variables).phone
}

/** Chooses which kinds of notification this phone gets; none is every kind. */
suspend fun Connection.updatePhone(phoneId: String, notificationKinds: List<String>): AgentPhone =
    execute(
        PhoneDocuments.UPDATE_PHONE, UpdateAnswer.serializer(),
        mapOf("phoneId" to JsonPrimitive(phoneId), "notificationKinds" to JsonArray(notificationKinds.map { kind -> JsonPrimitive(kind) })),
    ).phone
