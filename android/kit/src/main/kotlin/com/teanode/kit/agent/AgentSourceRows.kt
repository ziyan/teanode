package com.teanode.kit.agent

import com.teanode.kit.Connection
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull

/** The shape a stored source has. */
enum class SourceShape { Files, Journal, Records, Sent }

/** The shape a stored source has: sent mail by its kind, anything on a computer by its format, and a format this app does not know as files. */
val KnowledgeSourceRow.shape: SourceShape
    get() = when {
        kind == "sent" -> SourceShape.Sent
        specification.format == "journal" -> SourceShape.Journal
        specification.format == "records" -> SourceShape.Records
        else -> SourceShape.Files
    }

/** Whether the source is a bank or card linked on the Finance tab, which is listed, synced and removed there instead. */
val KnowledgeSourceRow.isFinance: Boolean get() = kind == "finance"

/**
 * The line under a source's name that says where it reads: the mailbox of sent mail by its name, the folder and the
 * computer, or for a type of commands what was filled in for it.
 */
fun KnowledgeSourceRow.whereItReads(mailboxNames: Map<String, String>): String {
    if (kind == "sent" && specification.mailboxId.isNotEmpty()) return mailboxNames[specification.mailboxId].orEmpty()
    var whereText = specification.path
    if (whereText.isEmpty() && specification.type.isNotEmpty()) {
        whereText = specification.storedSettings.entries.sortedBy { entry -> entry.key }.mapNotNull { entry ->
            val settingValue = entry.value
            val isEmpty = settingValue is JsonNull || (settingValue is JsonPrimitive && !settingValue.isString && settingValue.booleanOrNull == false) ||
                (settingValue is JsonPrimitive && settingValue.isString && settingValue.content.isEmpty()) || (settingValue is JsonArray && settingValue.isEmpty())
            if (isEmpty) null else "${KnowledgeSourceType.label(entry.key)}: ${KnowledgeSourceType.text(settingValue)}"
        }.joinToString(" · ")
    }
    return listOf(whereText, specification.computer).filter { text -> text.isNotEmpty() }.joinToString(" · ")
}

val KnowledgeSourceAttachments.hasAny: Boolean get() = undecided > 0 || declined > 0 || described > 0

/** One file a source's reading decided against opening, with the reason it gave. */
@Serializable
data class DeclinedAttachment(
    val documentId: String,
    val name: String = "",
    val contentType: String = "",
    val channel: String = "",
    val thread: String = "",
    /** Where the file's bytes are on the server, or empty where they are not kept. */
    val path: String = "",
    val declined: String = "",
) {
    /** The thread and the channel it came from, where it says. */
    val placeText: String get() = listOf(thread, channel).filter { text -> text.isNotBlank() }.joinToString(" · ")

    data class Group(val declinedReason: String, val files: List<DeclinedAttachment>)

    companion object {
        /** The files in the order they came, each run gathered under the reason it shares, so the reason is said once rather than once a file. */
        fun groupedByReason(files: List<DeclinedAttachment>): List<Group> {
            val reasons = mutableListOf<String>()
            val byReason = mutableMapOf<String, MutableList<DeclinedAttachment>>()
            files.forEach { file ->
                if (file.declined !in byReason) {
                    reasons += file.declined
                    byReason[file.declined] = mutableListOf()
                }
                byReason.getValue(file.declined) += file
            }
            return reasons.map { reason -> Group(reason, byReason.getValue(reason)) }
        }
    }
}

@Serializable private class ListDeclinedAttachmentsAnswer(val ListAgentDeclinedAttachments: List<DeclinedAttachment?>)

/** The files one source's reading decided against opening, and why. */
suspend fun Connection.declinedAttachments(sourceId: String): List<DeclinedAttachment> =
    execute<ListDeclinedAttachmentsAnswer>(AgentUpkeepDocuments.DECLINED_ATTACHMENTS, variablesOf("sourceId" to sourceId)).ListAgentDeclinedAttachments.filterNotNull()

@Serializable private class RereadAgentDocumentsAnswer(val RereadAgentDocuments: Int)

/** Puts back into the queue what a dream marked read in the last so many minutes, and returns how many documents that was. */
suspend fun Connection.rereadDocuments(minutes: Int): Int =
    execute<RereadAgentDocumentsAnswer>(AgentUpkeepDocuments.REREAD_DOCUMENTS, variablesOf("minutes" to minutes)).RereadAgentDocuments

/** How long the rest takes, in the unit it is said in. */
sealed class ReadingSpan {
    data class Minutes(val minutes: Int) : ReadingSpan()
    data class Hours(val hours: Int) : ReadingSpan()
    data class Days(val days: Int) : ReadingSpan()
}

val ReadingProgress.total: Int get() = read + waiting

/** The share read, rounded down, and never a hundred while something waits: a bar that says finished beside seven hundred left is the one a person believes. */
val ReadingProgress.readPercent: Int
    get() = when {
        total == 0 -> 0
        waiting == 0 -> 100
        else -> minOf(99, (read.toDouble() / total * 100).toInt())
    }

/** How long the rest takes at the measured pace, or null with nothing waiting or no pace yet. */
val ReadingProgress.timeLeft: ReadingSpan?
    get() = when {
        waiting <= 0 || perHour <= 0 -> null
        hoursLeft < 1 -> ReadingSpan.Minutes(maxOf(1, Math.round(hoursLeft * 60).toInt()))
        hoursLeft < 48 -> ReadingSpan.Hours(Math.round(hoursLeft).toInt())
        else -> ReadingSpan.Days(Math.round(hoursLeft / 24).toInt())
    }
