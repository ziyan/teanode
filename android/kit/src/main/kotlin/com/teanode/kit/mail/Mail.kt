package com.teanode.kit.mail

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.net.URLEncoder
import java.time.LocalDate
import java.time.ZoneId

/** MailboxEntry is one of the person's mailboxes with its folders and the unread counts the dashboard's rail shows. */
@Serializable
data class MailboxEntry(
    val mailbox: Mailbox? = null,
    val folders: List<MailFolder?> = emptyList(),
    val unread: Long = 0,
    val starredUnread: Long = 0,
    val priorityUnread: Long = 0,
    /** The most a message may be, in bytes; zero when there is no limit. */
    val maxMessageSize: Long = 0,
) {
    val presentFolders: List<MailFolder> get() = folders.filterNotNull()
}

/** Mailbox is a mailbox: its name, signature, addresses, auto-reply and what its agent may do. */
@Serializable
data class Mailbox(
    val id: String,
    val name: String,
    val signatureText: String = "",
    val signatureHtml: String = "",
    val addresses: List<Address?> = emptyList(),
    val autoReply: AutoReply? = null,
    val agent: AgentAccess? = null,
    /** What sorts mail as it arrives, in the order the rules run. */
    val rules: List<MailRule>? = null,
) {
    @Serializable
    data class Address(val address: String, val domain: String = "", val domainId: String = "")

    @Serializable
    data class AutoReply(
        val enabled: Boolean,
        val from: String? = null,
        val until: String? = null,
        val subject: String = "",
        val text: String = "",
        val html: String = "",
        val sameDomainOnly: Boolean = false,
    )

    @Serializable
    data class AgentAccess(val granted: Boolean = false, val draftReplies: Boolean = false, val triage: Triage? = null) {
        @Serializable
        data class Triage(val enabled: Boolean)

        /** Whether the agent sorts this mailbox, so Sort as and the Priority view are offered. */
        val isSorting: Boolean get() = granted && triage?.enabled == true
    }

    val sendingAddresses: List<String> get() = addresses.mapNotNull { address -> address?.address }
}

/** MailFolder is a folder: a system one by its kind, or one of the owner's. */
@Serializable
data class MailFolder(
    val id: String,
    val mailboxId: String,
    val parentId: String = "",
    val name: String,
    /** inbox, drafts, sent, archive, junk, trash, or empty for the owner's. */
    val kind: String = "",
    val pinnedAt: String? = null,
    val unread: Long = 0,
    val total: Long = 0,
    val noteCount: Long = 0,
) {
    /** A folder that holds only notes, or an empty one named Notes, which the dashboard shows as notes. */
    val isNotes: Boolean
        get() = kind.isEmpty() && ((total > 0 && noteCount == total) || (total == 0L && name.lowercase() == "notes"))
}

/** One row of the folder tree: the folder and how deep it is. */
data class MailFolderRow(val folder: MailFolder, val depth: Int)

/** MailFolders orders folders as the dashboard's rail does. */
object MailFolders {
    private val systemOrder = listOf("inbox", "drafts", "sent", "archive", "junk", "trash")

    /**
     * The folders in tree order with their depth: the system folders in their order, then the owner's alphabetically,
     * each followed by its children.
     */
    fun rows(folders: List<MailFolder>): List<MailFolderRow> {
        val known = folders.map { folder -> folder.id }.toSet()
        val children = HashMap<String, MutableList<MailFolder>>()
        for (folder in folders) {
            val parent = if (known.contains(folder.parentId)) folder.parentId else ""
            children.getOrPut(parent) { mutableListOf() }.add(folder)
        }
        val rows = mutableListOf<MailFolderRow>()
        fun walk(parent: String, depth: Int) {
            val ordered = (children[parent] ?: emptyList<MailFolder>()).sortedWith(
                compareBy<MailFolder> { folder -> systemOrder.indexOf(folder.kind).let { rank -> if (rank < 0) systemOrder.size else rank } }
                    .thenComparator { left, right -> compareStandard(left.name, right.name) },
            )
            for (folder in ordered) {
                rows.add(MailFolderRow(folder, depth))
                walk(folder.id, depth + 1)
            }
        }
        walk("", 0)
        return rows
    }

    fun folder(kind: String, folders: List<MailFolder>): MailFolder? = folders.firstOrNull { folder -> folder.kind == kind }

    /** Names compared the way a person sorts them: ignoring case, numbers by their value. */
    private fun compareStandard(left: String, right: String): Int {
        val leftParts = Regex("""\d+|\D+""").findAll(left.lowercase()).map { part -> part.value }.toList()
        val rightParts = Regex("""\d+|\D+""").findAll(right.lowercase()).map { part -> part.value }.toList()
        for (index in 0 until minOf(leftParts.size, rightParts.size)) {
            val leftPart = leftParts[index]
            val rightPart = rightParts[index]
            val leftNumber = leftPart.toBigIntegerOrNull()
            val rightNumber = rightPart.toBigIntegerOrNull()
            val order = if (leftNumber != null && rightNumber != null) leftNumber.compareTo(rightNumber) else leftPart.compareTo(rightPart)
            if (order != 0) return order
        }
        return leftParts.size.compareTo(rightParts.size)
    }
}

/** MailMessage is a message as the server keeps it, without its content. */
@Serializable
data class MailMessage(
    val id: String,
    val from: String = "",
    val fromName: String = "",
    val sender: String = "",
    val subject: String = "",
    val recipients: List<String> = emptyList(),
    val receivedAt: String = "",
    val size: Long = 0,
    /** How many attachments the message carries; null from a server that does not count them. */
    val attachmentCount: Int? = null,
    val messageId: String? = null,
    val listKey: String? = null,
    val listName: String? = null,
    val logoDomain: String = "",
    val authenticationResults: Verdicts? = null,
) {
    @Serializable
    data class Verdicts(
        val spf: Outcome? = null,
        val dkims: List<Outcome?>? = null,
        val dmarc: DMARCOutcome? = null,
        val spamFilter: Spam? = null,
    ) {
        @Serializable
        data class Outcome(val result: String)

        @Serializable
        data class Spam(val score: Double)

        /**
         * DMARCOutcome is DMARC's result here, and the forwarder whose ARC seal records the message passing there when it
         * failed here only because of the forwarding.
         */
        @Serializable
        data class DMARCOutcome(val result: String, val passedAtForwarderDomain: String? = null)

        /** Whether the message satisfies its sender's DMARC policy, here or at a trusted forwarder. */
        val isDMARCPassed: Boolean
            get() = dmarc?.result?.lowercase() == "pass" || !dmarc?.passedAtForwarderDomain.isNullOrEmpty()
    }

    /** Who it is from, in the words the list shows. */
    val shownSender: String
        get() = when {
            fromName.isNotEmpty() -> fromName
            from.isNotEmpty() -> from
            else -> sender
        }
}

/** MailVerdict is how far a message can be trusted to come from who it says, as the dashboard's shield shows it. */
enum class MailVerdict {
    Good, Warn, Bad, Unknown;

    companion object {
        fun of(results: MailMessage.Verdicts?): MailVerdict {
            if (results == null) return Unknown
            val dmarc = results.dmarc?.result?.lowercase() ?: ""
            val spf = results.spf?.result?.lowercase() ?: ""
            val dkims = (results.dkims ?: emptyList()).mapNotNull { outcome -> outcome?.result?.lowercase() }
            val isDKIMPassed = dkims.contains("pass")
            // Once DMARC is satisfied, an SPF or signature failure is what forwarding does to a message, not a sign of forgery.
            val isDMARCPassed = results.isDMARCPassed
            if (!isDMARCPassed && (dmarc == "fail" || spf == "fail" || spf == "hardfail" || (dkims.contains("fail") && !isDKIMPassed))) return Bad
            val score = results.spamFilter?.score
            if (score != null && score >= 5) return Warn
            if (isDMARCPassed || (spf == "pass" && isDKIMPassed)) return Good
            return Unknown
        }

        /** "SPF pass · DKIM pass · DMARC pass · spam 0.4". */
        fun detail(results: MailMessage.Verdicts?): String {
            if (results == null) return ""
            val parts = mutableListOf<String>()
            val spf = results.spf?.result
            if (!spf.isNullOrEmpty()) parts.add("SPF $spf")
            val dkims = (results.dkims ?: emptyList()).mapNotNull { outcome -> outcome?.result }.filter { text -> text.isNotEmpty() }
            if (dkims.isNotEmpty()) parts.add("DKIM ${dkims.joinToString("/")}")
            val forwarderDomain = results.dmarc?.passedAtForwarderDomain
            val dmarc = results.dmarc?.result
            if (!forwarderDomain.isNullOrEmpty()) {
                parts.add("DMARC pass at $forwarderDomain")
            } else if (!dmarc.isNullOrEmpty()) {
                parts.add("DMARC $dmarc")
            }
            val score = results.spamFilter?.score
            if (score != null) parts.add(String.format(java.util.Locale.US, "spam %.1f", score))
            return parts.joinToString(" · ")
        }
    }
}

/** MailInsight is what the agent made of a message when it sorted it. */
@Serializable
data class MailInsight(
    val category: String = "",
    val priority: String = "",
    val needsReply: Boolean = false,
    val summary: String = "",
    val actionItems: List<String> = emptyList(),
    val notes: String? = null,
    val model: String? = null,
    val runId: String? = null,
    val proposals: List<MailProposal>? = null,
)

/** MailProposal is an event or a contact the agent found in a message and offers to add. */
@Serializable
data class MailProposal(
    val kind: String = "",
    val because: String = "",
    val status: String = "",
    val summary: String = "",
    val starts: String = "",
    val ends: String = "",
    val location: String = "",
    val allDay: Boolean = false,
    val name: String = "",
    val organization: String = "",
    val title: String = "",
    val emails: List<String> = emptyList(),
    val phones: List<String> = emptyList(),
    val note: String = "",
    val contactId: String = "",
)

/** MailItem is a message in a folder, with its flags. */
@Serializable
data class MailItem(
    val id: String,
    val folderId: String = "",
    val mailId: String = "",
    val seen: Boolean = false,
    val flagged: Boolean = false,
    val answered: Boolean = false,
    val forwarded: Boolean = false,
    val draft: Boolean = false,
    val addedAt: String = "",
    val subscriptionId: String? = null,
    val insight: MailInsight? = null,
    val mail: MailMessage? = null,
) {
    /** The item with its flags changed, as it reads once the server has them. */
    fun with(isSeen: Boolean? = null, isFlagged: Boolean? = null): MailItem =
        copy(seen = isSeen ?: seen, flagged = isFlagged ?: flagged)
}

/** MailThread is a conversation as one row of a folder. */
@Serializable
data class MailThread(
    val threadId: String,
    val count: Int = 1,
    val unread: Int = 0,
    val flagged: Boolean = false,
    val participants: List<String> = emptyList(),
    val itemIds: List<String> = emptyList(),
    val hasDraft: Boolean = false,
    val item: MailItem? = null,
) {
    val id: String get() = item?.id ?: threadId

    /** The row with its flags changed: read clears its unread count. */
    fun with(isSeen: Boolean? = null, isFlagged: Boolean? = null): MailThread = copy(
        unread = if (isSeen != null) (if (isSeen) 0 else maxOf(1, unread)) else unread,
        flagged = isFlagged ?: flagged,
        item = item?.with(isSeen, isFlagged),
    )
}

@Serializable
data class MailThreadPage(val total: Long = 0, val threads: List<MailThread?> = emptyList())

/** MailThreadView is a whole conversation, newest first, across folders. */
@Serializable
data class MailThreadView(
    val threadId: String,
    val subject: String = "",
    val truncated: Boolean = false,
    val summary: Summary? = null,
    val heldReply: HeldReply? = null,
    val items: List<Entry?> = emptyList(),
) {
    @Serializable
    data class Entry(val folderId: String = "", val folderName: String = "", val folderKind: String = "", val item: MailItem? = null)

    @Serializable
    data class Summary(val summary: String = "", val messageCount: Int = 0, val stale: Boolean = false, val pending: Boolean = false)

    /** A reply the agent is holding back to send. */
    @Serializable
    data class HeldReply(
        val id: String,
        val status: String = "",
        val subject: String = "",
        val to: String = "",
        val text: String = "",
        val sendAfter: String? = null,
        val draftItemId: String = "",
    )

    val entries: List<Entry> get() = items.filterNotNull()
}

/** MailContent is a message's text, its cleaned HTML, its headers and its attachments. */
@Serializable
data class MailContent(
    val mailId: String,
    val available: Boolean = true,
    val text: String = "",
    val html: String = "",
    val hasRemoteContent: Boolean? = null,
    val imagesAllowed: Boolean? = null,
    val rawHeaders: String? = null,
    val headers: List<Header?>? = null,
    val attachments: List<Attachment?> = emptyList(),
) {
    @Serializable
    data class Attachment(val index: Int, val filename: String = "", val contentType: String = "", val size: Int = 0, val inline: Boolean = false)

    @Serializable
    data class Header(val key: String, val value: String)

    val presentAttachments: List<Attachment> get() = attachments.filterNotNull()

    /** A header's value, by its name in any case. */
    fun header(name: String): String? = (headers ?: emptyList()).filterNotNull().firstOrNull { header -> header.key.lowercase() == name.lowercase() }?.value
}

/** MailDraft is a draft whole, read to go on writing it. */
@Serializable
data class MailDraft(
    val itemId: String,
    val mailId: String = "",
    val from: String = "",
    val to: List<String> = emptyList(),
    val cc: List<String> = emptyList(),
    val bcc: List<String> = emptyList(),
    val subject: String = "",
    val html: String = "",
    val text: String = "",
    val replyToItemId: String = "",
    val forwardItemId: String = "",
    val attachments: List<MailContent.Attachment?> = emptyList(),
)

/** MailSubscription is a mailing list that writes to the mailbox. */
@Serializable
data class MailSubscription(
    val id: String,
    val key: String,
    val name: String = "",
    val from: String = "",
    val count: Int = 0,
    val unread: Int = 0,
    val lastAt: String = "",
    val lastItemId: String = "",
    val oneClick: Boolean = false,
    val unsubscribe: List<String> = emptyList(),
    val logoDomain: String = "",
    val requestedAt: String? = null,
    val method: String = "",
    val failed: Boolean = false,
    val error: String = "",
    val stripped: Boolean = false,
    val mutedAt: String? = null,
    val imagesAt: String? = null,
) {
    /** How the person can leave: one click, a mail, a page of the sender's, or no way at all. */
    val leaveKind: String
        get() = when {
            oneClick -> "oneClick"
            unsubscribe.any { address -> address.lowercase().startsWith("mailto:") } -> "mail"
            unsubscribe.any { address -> address.lowercase().startsWith("http") } -> "link"
            else -> "none"
        }

    val leavePage: String? get() = unsubscribe.firstOrNull { address -> address.lowercase().startsWith("http") }
}

@Serializable
data class MailSubscriptionPage(
    val total: Long = 0,
    val subscribed: Long = 0,
    val left: Long = 0,
    val subscriptions: List<MailSubscription?> = emptyList(),
)

/** MailInvitation is a calendar invitation a message carries. */
@Serializable
data class MailInvitation(
    val id: String,
    val method: String = "",
    val summary: String = "",
    val location: String = "",
    val startsAt: String = "",
    val endsAt: String = "",
    val allDay: Boolean = false,
    val cancelled: Boolean = false,
    val organizer: String = "",
    val participation: String = "",
    val attendees: List<Attendee?> = emptyList(),
) {
    @Serializable
    data class Attendee(val address: String, val name: String? = null, val participation: String? = null)
}

/** MailFilter is what a folder's list is narrowed to. */
data class MailFilter(
    val searchText: String = "",
    val isUnreadOnly: Boolean = false,
    val isStarredOnly: Boolean = false,
    val isEverywhere: Boolean = false,
    val from: String = "",
    val to: String = "",
    val subject: String = "",
    val since: LocalDate? = null,
    val before: LocalDate? = null,
    /** Null for any, true for with, false for without. */
    val hasAttachment: Boolean? = null,
) {
    /** Whether anything narrows the list. */
    val isNarrowing: Boolean
        get() = searchText.isNotEmpty() || isUnreadOnly || isStarredOnly || from.isNotEmpty() || to.isNotEmpty() || subject.isNotEmpty() ||
            since != null || before != null || hasAttachment != null
}

/** The place a reply is going: who to, who to copy, and which of the person's addresses it comes from. */
data class MailReplyAddresses(val to: List<String>, val cc: List<String>, val from: String?)

/** MailWords are the small rules for replies and forwards. */
object MailWords {
    /** "Re: " in front, unless it is there already. */
    fun replySubject(subject: String): String = if (subject.lowercase().startsWith("re:")) subject else "Re: $subject"

    fun forwardSubject(subject: String): String = if (subject.lowercase().startsWith("fwd:")) subject else "Fwd: $subject"

    /** The bare address in "Name <address>". */
    fun bareAddress(written: String): String {
        val open = written.lastIndexOf('<')
        val close = written.lastIndexOf('>')
        if (open >= 0 && close >= 0 && open < close) return written.substring(open + 1, close).trim()
        return written.trim()
    }

    /** Addresses typed into a field, split on commas, semicolons and lines. */
    fun addresses(typed: String): List<String> =
        typed.split(',', ';', '\n', '\r').map { address -> address.trim() }.filter { address -> address.isNotEmpty() }

    /**
     * Who a reply goes to and from: the Reply-To or the sender; the address of the person's that it was written to; and
     * for reply all, everyone else it went to.
     */
    fun reply(message: MailMessage, replyTo: String?, own: List<String>, isReplyAll: Boolean): MailReplyAddresses {
        val ownBare = own.map { address -> address.lowercase() }.toSet()
        val recipient = if (!replyTo.isNullOrEmpty()) replyTo else if (message.from.isEmpty()) message.sender else message.from
        val from = message.recipients.map { address -> bareAddress(address) }.firstOrNull { bare -> ownBare.contains(bare.lowercase()) }
        var copied = emptyList<String>()
        if (isReplyAll) {
            val toBare = bareAddress(recipient).lowercase()
            copied = message.recipients.filter { address ->
                val bare = bareAddress(address).lowercase()
                !ownBare.contains(bare) && bare != toBare
            }
        }
        return MailReplyAddresses(listOf(recipient), copied, from)
    }

    /** Plain text as HTML paragraphs, the way the dashboard writes it. */
    fun html(ofText: String): String =
        ofText.split("\n\n").joinToString("") { paragraph -> "<p>" + escaped(paragraph).replace("\n", "<br>") + "</p>" }

    fun escaped(text: String): String = text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace("\"", "&quot;")

    /** Lines with "> " in front, for a plain reply's quote. */
    fun quoted(text: String): String = text.split("\n").joinToString("\n") { line -> "> $line" }

    /**
     * The images a message's sender keeps on their own server, which the server sends held back (`data-blocked-src`),
     * pointed at this server's proxy instead, so they never load from the sender.
     */
    fun showingRemoteImages(html: String, mailId: String): String {
        val pattern = Regex("""data-blocked-src="([^"]*)"\s+src="[^"]*"""")
        return pattern.replace(html) { match ->
            val original = decodedAttribute(match.groupValues[1])
            "src=\"/api/v1/mail/$mailId/remote?url=${percentEncoded(original)}\""
        }
    }

    /** Percent-encoding that keeps letters, digits and -_.!~*'() as the browser's encodeURIComponent does. */
    private fun percentEncoded(text: String): String = URLEncoder.encode(text, "UTF-8").replace("+", "%20")
        .replace("%21", "!").replace("%7E", "~").replace("%27", "'").replace("%28", "(").replace("%29", ")").replace("%2A", "*")

    /** Undoes the server's attribute escaping, in the order the dashboard does: the named characters first, the ampersand last. */
    internal fun decodedAttribute(text: String): String = text.replace("&lt;", "<").replace("&gt;", ">").replace("&#34;", "\"")
        .replace("&#39;", "'").replace("&#13;", "\r").replace("&amp;", "&")
}

/** MailOutgoing is a message being written, as the server takes it. */
data class MailOutgoing(
    val from: String = "",
    val to: List<String> = emptyList(),
    val cc: List<String> = emptyList(),
    val bcc: List<String> = emptyList(),
    val subject: String = "",
    val htmlContent: String = "",
    val textContent: String = "",
    val replyToItemId: String = "",
    val forwardItemId: String = "",
    val forwardAttachments: List<Int> = emptyList(),
    val draftItemId: String = "",
    val keepAttachments: List<Int> = emptyList(),
) {
    val variables: JsonObject
        get() {
            val fields = linkedMapOf<String, JsonElement>(
                "from" to JsonPrimitive(from), "to" to strings(to), "cc" to strings(cc), "bcc" to strings(bcc),
                "subject" to JsonPrimitive(subject), "htmlContent" to JsonPrimitive(htmlContent), "textContent" to JsonPrimitive(textContent),
            )
            if (replyToItemId.isNotEmpty()) fields["replyToItemId"] = JsonPrimitive(replyToItemId)
            if (forwardItemId.isNotEmpty()) {
                fields["forwardItemId"] = JsonPrimitive(forwardItemId)
                fields["forwardAttachments"] = JsonArray(forwardAttachments.map { index -> JsonPrimitive(index) })
            }
            if (draftItemId.isNotEmpty()) {
                fields["draftItemId"] = JsonPrimitive(draftItemId)
                fields["keepAttachments"] = JsonArray(keepAttachments.map { index -> JsonPrimitive(index) })
            }
            return JsonObject(fields)
        }
}

internal fun strings(texts: List<String>): JsonArray = JsonArray(texts.map { text -> JsonPrimitive(text) })

/**
 * MailSending is a send whose answer may have been lost, kept on the phone with its own id, so it is asked about before
 * it is sent again.
 */
data class MailSending(
    val mailboxId: String,
    val message: MailOutgoing,
    val submissionId: String = java.util.UUID.randomUUID().toString().replace("-", ""),
)

/** MailMoved is where a message is now, after a move. */
@Serializable
data class MailMoved(val id: String, val folderId: String = "", val mailId: String = "", val addedAt: String? = null)
