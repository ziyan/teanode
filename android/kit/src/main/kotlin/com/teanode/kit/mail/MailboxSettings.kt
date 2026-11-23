package com.teanode.kit.mail

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

/** MailAppPassword is a password a mail program uses for the mailbox. */
@Serializable
data class MailAppPassword(
    val mailboxId: String? = null,
    val id: String,
    val name: String = "",
    val createdAt: String = "",
    val lastUsedAt: String? = null,
)

/** MailProgramSettings are what a mail program needs to reach the server. */
@Serializable
data class MailProgramSettings(
    val imapHost: String,
    val imapPort: Int,
    val imapsPort: Int,
    val submissionHost: String,
    val submissionPort: Int,
    val davHost: String,
)

/** MailAutoReplyDraft is the auto-reply as its form holds it. */
data class MailAutoReplyDraft(
    val isEnabled: Boolean = false,
    val from: Instant? = null,
    val until: Instant? = null,
    val subject: String = "",
    val text: String = "",
    val isSameDomainOnly: Boolean = false,
) {
    /** As the server takes it: plain text, no HTML. */
    val variables: JsonObject
        get() = JsonObject(
            mapOf(
                "enabled" to JsonPrimitive(isEnabled), "subject" to JsonPrimitive(subject), "text" to JsonPrimitive(text),
                "html" to JsonPrimitive(""), "sameDomainOnly" to JsonPrimitive(isSameDomainOnly),
                "from" to (from?.let { moment -> JsonPrimitive(ServerTime.text(moment)) } ?: JsonNull),
                "until" to (until?.let { moment -> JsonPrimitive(ServerTime.text(moment)) } ?: JsonNull),
            ),
        )

    companion object {
        fun of(reply: Mailbox.AutoReply?): MailAutoReplyDraft {
            if (reply == null) return MailAutoReplyDraft()
            return MailAutoReplyDraft(
                isEnabled = reply.enabled, from = ServerTime.instant(reply.from), until = ServerTime.instant(reply.until),
                subject = reply.subject, text = reply.text, isSameDomainOnly = reply.sameDomainOnly,
            )
        }
    }
}

/**
 * MailRule is one of the rules that sort mail as it arrives: when all of its conditions match, its actions run, and a
 * rule that stops ends the run.
 */
@Serializable
data class MailRule(
    val name: String = "",
    val enabled: Boolean = true,
    val stop: Boolean = false,
    val conditions: List<Condition> = listOf(Condition()),
    val actions: List<Action> = listOf(Action()),
) {
    /** Condition is one test of a message. */
    @Serializable
    data class Condition(
        /** from, to, subject, header, score, sender-known, category, priority, needs-reply or any. */
        val field: String = "from",
        /** The header's name, when the field is header. */
        val header: String? = null,
        /** contains, equals, matches, above or below; empty for the fields that need no value. */
        @SerialName("operator") @Serializable(with = NullAsEmptyString::class) val comparisonOperator: String = "contains",
        val value: String? = "",
    ) {
        val isBare: Boolean get() = bareFields.contains(this.field)

        /** As the server takes it: the header's name only for a header, and no operator for a field that needs none. */
        val variables: JsonObject
            get() = JsonObject(
                mapOf(
                    "field" to JsonPrimitive(this.field),
                    "header" to JsonPrimitive(if (this.field == "header") header ?: "" else ""),
                    "operator" to JsonPrimitive(if (isBare) "" else comparisonOperator),
                    "value" to JsonPrimitive(value ?: ""),
                ),
            )

        companion object {
            /** The fields that need no value: the condition is the field. */
            val bareFields: Set<String> = setOf("sender-known", "needs-reply", "any")

            /** What a field can be compared with: a score is above or below a number, a priority is one of three, everything else matches text. */
            fun operators(field: String): List<String> = when {
                field == "score" -> listOf("above", "below")
                field == "priority" -> listOf("equals")
                bareFields.contains(field) -> listOf("")
                else -> listOf("contains", "equals", "matches")
            }
        }
    }

    /** Action is one thing done to a message a rule matched. */
    @Serializable
    data class Action(
        /** move, markRead, flag, forward or delete. */
        val kind: String = "move",
        val folderId: String? = "",
        val address: String? = null,
    )

    /** As the server takes it: each part only where it means something, so a header name left from a field changed since is not sent. */
    val variables: JsonObject
        get() = JsonObject(
            mapOf(
                "name" to JsonPrimitive(name), "enabled" to JsonPrimitive(enabled), "stop" to JsonPrimitive(stop),
                "conditions" to JsonArray(conditions.map { condition -> condition.variables }),
                "actions" to JsonArray(
                    actions.map { action ->
                        JsonObject(
                            mapOf(
                                "kind" to JsonPrimitive(action.kind),
                                "folderId" to JsonPrimitive(if (action.kind == "move") action.folderId ?: "" else ""),
                                "address" to JsonPrimitive(if (action.kind == "forward") action.address ?: "" else ""),
                            ),
                        )
                    },
                ),
            ),
        )

    companion object {
        val fields = listOf("from", "to", "subject", "header", "score", "sender-known", "category", "priority", "needs-reply", "any")
        val actionKinds = listOf("move", "markRead", "flag", "forward", "delete")
        val priorities = listOf("high", "normal", "low")
    }
}

/** MailRuleTrial is what the rules would do with one recent message: the places in the list of the rules that match it. */
@Serializable
data class MailRuleTrial(val matched: List<Int> = emptyList(), val item: Item) {
    @Serializable
    data class Item(val id: String, val mail: Mail? = null) {
        @Serializable
        data class Mail(val from: String? = null, val sender: String? = null, val subject: String? = null, val receivedAt: String? = null)
    }
}

@Serializable
private class TestRulesAnswer(@SerialName("TestMailboxRules") val trials: List<MailRuleTrial>)

@Serializable
private class AppPasswordsAnswer(
    @SerialName("ListMailboxAppPasswords") val passwords: List<MailAppPassword?>,
    @SerialName("GetMailProgramSettings") val settings: MailProgramSettings? = null,
)

@Serializable
private class CreatedPasswordAnswer(@SerialName("CreateMailboxAppPassword") val created: Created) {
    @Serializable
    class Created(val password: String, val username: String)
}

@Serializable
private class SetupProfileAnswer(@SerialName("CreateMailboxSetupProfile") val created: Created) {
    @Serializable
    class Created(val downloadLink: String)
}

/** The app passwords of a mailbox and what a mail program needs to reach the server. */
data class MailAppPasswords(val passwords: List<MailAppPassword>, val settings: MailProgramSettings?)

/** An app password just made: shown this once. */
data class MailAppPasswordCreated(val password: String, val username: String)

/** Changes a mailbox's name, signature or auto-reply; what is not given stays as it is. */
suspend fun Connection.updateMailbox(mailboxId: String, name: String? = null, signature: String? = null, autoReply: MailAutoReplyDraft? = null) {
    val variables = linkedMapOf<String, JsonElement>("mailboxId" to JsonPrimitive(mailboxId))
    if (name != null) variables["name"] = JsonPrimitive(name)
    if (signature != null) {
        variables["signatureText"] = JsonPrimitive(signature)
        variables["signatureHtml"] = JsonPrimitive("")
    }
    if (autoReply != null) variables["autoReply"] = autoReply.variables
    execute<JsonElement>(MailboxSettingsDocuments.UPDATE_MAILBOX, variables)
}

/** Replaces the mailbox's rules with these, in this order. */
suspend fun Connection.updateMailboxRules(mailboxId: String, rules: List<MailRule>) {
    execute<JsonElement>(
        MailboxSettingsDocuments.UPDATE_MAILBOX_RULES,
        mapOf("mailboxId" to JsonPrimitive(mailboxId), "rules" to JsonArray(rules.map { rule -> rule.variables })),
    )
}

/** What these rules would do with the newest messages in the Inbox; nothing is moved. */
suspend fun Connection.testMailboxRules(mailboxId: String, rules: List<MailRule>, first: Int = 20): List<MailRuleTrial> =
    execute<TestRulesAnswer>(
        MailboxSettingsDocuments.TEST_MAILBOX_RULES,
        mapOf("mailboxId" to JsonPrimitive(mailboxId), "rules" to JsonArray(rules.map { rule -> rule.variables }), "first" to JsonPrimitive(first)),
    ).trials

suspend fun Connection.createMailFolder(mailboxId: String, name: String, parentId: String?) {
    val variables = linkedMapOf<String, JsonElement>("mailboxId" to JsonPrimitive(mailboxId), "name" to JsonPrimitive(name))
    if (!parentId.isNullOrEmpty()) variables["parentId"] = JsonPrimitive(parentId)
    execute<JsonElement>(MailboxSettingsDocuments.CREATE_MAIL_FOLDER, variables)
}

suspend fun Connection.renameMailFolder(folderId: String, name: String) {
    execute<JsonElement>(MailboxSettingsDocuments.UPDATE_MAIL_FOLDER, mapOf("folderId" to JsonPrimitive(folderId), "name" to JsonPrimitive(name)))
}

suspend fun Connection.pinMailFolder(folderId: String, isPinned: Boolean) {
    execute<JsonElement>(MailboxSettingsDocuments.PIN_MAIL_FOLDER, mapOf("folderId" to JsonPrimitive(folderId), "pinned" to JsonPrimitive(isPinned)))
}

suspend fun Connection.deleteMailFolder(folderId: String) {
    execute<JsonElement>(MailboxSettingsDocuments.DELETE_MAIL_FOLDER, mapOf("folderId" to JsonPrimitive(folderId)))
}

suspend fun Connection.mailAppPasswords(mailboxId: String): MailAppPasswords {
    val answer = execute<AppPasswordsAnswer>(MailboxSettingsDocuments.MAIL_APP_PASSWORDS, mapOf("mailboxId" to JsonPrimitive(mailboxId)))
    return MailAppPasswords(answer.passwords.filterNotNull(), answer.settings)
}

/** Makes an app password; it is shown this once. */
suspend fun Connection.createMailAppPassword(mailboxId: String, name: String): MailAppPasswordCreated {
    val created = execute<CreatedPasswordAnswer>(
        MailboxSettingsDocuments.CREATE_MAIL_APP_PASSWORD, mapOf("mailboxId" to JsonPrimitive(mailboxId), "name" to JsonPrimitive(name)),
    ).created
    return MailAppPasswordCreated(created.password, created.username)
}

/** Makes a setup profile for this device, with an app password of its own, and returns the address it downloads from, once. */
suspend fun Connection.createSetupProfile(mailboxId: String, deviceName: String): String {
    val created = execute<SetupProfileAnswer>(
        MailboxSettingsDocuments.CREATE_SETUP_PROFILE, mapOf("mailboxId" to JsonPrimitive(mailboxId), "deviceName" to JsonPrimitive(deviceName)),
    ).created
    if (created.downloadLink.isEmpty()) throw TeaNodeError.UnreadableAnswer
    return created.downloadLink
}

suspend fun Connection.deleteMailAppPassword(appPasswordId: String) {
    execute<JsonElement>(MailboxSettingsDocuments.DELETE_MAIL_APP_PASSWORD, mapOf("appPasswordId" to JsonPrimitive(appPasswordId)))
}

/** Reads a text the server may send as null as an empty one, as the rule conditions that need no operator do. */
object NullAsEmptyString : KSerializer<String> {
    override val descriptor = PrimitiveSerialDescriptor("NullAsEmptyString", PrimitiveKind.STRING)

    override fun deserialize(decoder: Decoder): String = if (decoder.decodeNotNullMark()) decoder.decodeString() else decoder.decodeNull() ?: ""

    override fun serialize(encoder: Encoder, value: String) = encoder.encodeString(value)
}
