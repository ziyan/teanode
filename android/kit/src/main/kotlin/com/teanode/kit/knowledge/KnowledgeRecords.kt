package com.teanode.kit.knowledge

import com.teanode.kit.calendar.ServerTime
import kotlinx.serialization.Serializable
import java.net.URI
import java.net.URLDecoder
import java.time.Instant

/**
 * KnowledgeRecordLink joins a page of the agent's memory to one of the person's own records: a contact card, a mail, an
 * event or reminder, a message of a conversation, a finance transaction or receipt, a passage of a recording. Nothing
 * of the record is kept in the link: its title, date and where it opens are found when the link is read.
 */
@Serializable
data class KnowledgeRecordLink(
    val id: String,
    /** The page the link is on, filled for the pages that link to a record. */
    val nodePath: String,
    val nodeName: String,
    val nodeKind: String,
    val recordKind: String,
    val recordId: String,
    val recordRelation: String,
    /** "stated", or "proposed" where the agent only guessed. */
    val linkStatus: String,
    val passageStartMS: Long,
    val passageEndMS: Long,
    val transcriptRunId: String,
    /** How many passage links of one recording this row stands for; missing from a server that lists every passage on its own. */
    val passageCount: Int? = null,
    val note: String,
    val happenedAt: String? = null,
    val createdAt: String,
    val recordTitle: String,
    val recordHappenedAt: String? = null,
    /** Where the dashboard opens the record, such as /mailbox/starred/ID. */
    val dashboardPath: String,
    /** The record is gone: the link can only be removed. */
    val isUnavailable: Boolean,
) {
    /** How many passages of the recording the row covers, else zero. */
    val coveredPassageCount: Int get() = passageCount ?: 0

    /** The agent only guessed this link; the person confirms or removes it. */
    val isProposed: Boolean get() = linkStatus == "proposed"

    /** What a tap can open: the record is still there and has a place. */
    val canOpen: Boolean get() = !isUnavailable && RecordTarget.fromDashboardPath(dashboardPath) != null

    /** The date of the record, else of the link. */
    val date: Instant? get() = ServerTime.instant(recordHappenedAt) ?: ServerTime.instant(happenedAt)

    /** The record as the command line names it: kind:id, with the range of a passage of a recording. */
    val reference: String
        get() = if (recordKind == "recording_passage") "$recordKind:$recordId@$passageStartMS-$passageEndMS" else "$recordKind:$recordId"
}

/** KnowledgeEvidenceRecord is the record one piece of a fact's evidence names, found again so that the source of a fact opens it. */
@Serializable
data class KnowledgeEvidenceRecord(
    val factId: String,
    val evidenceIndex: Int,
    val recordKind: String,
    val recordId: String,
    val passageStartMS: Long,
    val passageEndMS: Long,
    val transcriptRunId: String,
    val recordTitle: String,
    val dashboardPath: String,
    val isUnavailable: Boolean,
) {
    val canOpen: Boolean get() = !isUnavailable && RecordTarget.fromDashboardPath(dashboardPath) != null
}

/** KnowledgeRecordGroup is the links of one kind of record. */
data class KnowledgeRecordGroup(val recordKind: String, val links: List<KnowledgeRecordLink>) {
    val id: String get() = recordKind
}

/** KnowledgeRecordKinds are the kinds of record, in the order a page lists them: the most personal first. */
object KnowledgeRecordKinds {
    val all: List<String> = listOf(
        "contact", "mail", "calendar_event", "reminder", "conversation_message", "finance_transaction", "finance_receipt", "recording_passage",
    )

    /** Groups links by kind in the order above, a kind this build does not know after them so a newer server's kind is listed and not dropped. */
    fun grouped(links: List<KnowledgeRecordLink>): List<KnowledgeRecordGroup> {
        val byKind = links.groupBy { link -> link.recordKind }
        val unknown = byKind.keys.filter { kind -> kind !in all }.sorted()
        return (all.filter { kind -> byKind.containsKey(kind) } + unknown).map { kind -> KnowledgeRecordGroup(kind, byKind[kind].orEmpty()) }
    }

    /** The pages that link to a record, each once: a statement is kept over a guess where a page links both ways. */
    fun pages(links: List<KnowledgeRecordLink>): List<KnowledgeRecordLink> {
        val kept = LinkedHashMap<String, KnowledgeRecordLink>()
        for (link in links) {
            val earlier = kept[link.nodePath]
            if (earlier == null) {
                kept[link.nodePath] = link
            } else if (earlier.isProposed && !link.isProposed) {
                kept[link.nodePath] = link
            }
        }
        return kept.values.toList()
    }
}

/** RecordTarget is where the app opens a record, read from the path the dashboard opens it at, which is what the server returns for every link. */
sealed class RecordTarget {
    data class Mail(val itemId: String) : RecordTarget()
    data class Contact(val id: String) : RecordTarget()
    data class CalendarEvent(val id: String) : RecordTarget()
    data class Reminder(val id: String) : RecordTarget()
    data class Conversation(val id: String) : RecordTarget()
    data class FinanceTransaction(val id: String) : RecordTarget()
    data class FinanceReceipt(val id: String) : RecordTarget()

    /** A recording, with its passage in the query as the dashboard writes it. */
    data class Recording(val path: String) : RecordTarget()

    companion object {
        fun fromDashboardPath(dashboardPath: String): RecordTarget? {
            if (!dashboardPath.startsWith("/") || dashboardPath.length > 600) return null
            val uri = try {
                URI(dashboardPath)
            } catch (_: Exception) {
                return null
            }
            val segments = uri.path.orEmpty().split("/").filter { segment -> segment.isNotEmpty() }
            val queryItems = uri.rawQuery.orEmpty().split("&").filter { item -> item.isNotEmpty() }.associate { item ->
                val name = item.substringBefore('=')
                name to URLDecoder.decode(item.substringAfter('=', ""), Charsets.UTF_8)
            }
            fun value(name: String): String? = queryItems[name]?.takeIf { found -> found.isNotEmpty() }
            return when (segments.firstOrNull().orEmpty()) {
                "mailbox" -> if (segments.size == 3 && segments[2].isNotEmpty()) Mail(segments[2]) else null
                "contacts" -> value("contact")?.let(::Contact)
                "calendar" -> value("event")?.let(::CalendarEvent)
                "reminders" -> value("reminder")?.let(::Reminder)
                "agent" -> value("conversation")?.let(::Conversation)
                "finance" -> when {
                    segments.size == 2 && segments[1] == "transactions" -> value("transaction")?.let(::FinanceTransaction)
                    segments.size == 2 && segments[1] == "receipts" -> value("receipt")?.let(::FinanceReceipt)
                    else -> null
                }
                "recordings" -> if (segments.size == 2) Recording(dashboardPath) else null
                else -> null
            }
        }
    }
}
