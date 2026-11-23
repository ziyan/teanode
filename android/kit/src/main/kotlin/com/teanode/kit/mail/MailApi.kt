package com.teanode.kit.mail

import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.Upload
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import java.time.ZoneId

@Serializable
private class MailboxesAnswer(@SerialName("ListMailboxes") val entries: List<MailboxEntry?>)

@Serializable
private class MailThreadsAnswer(@SerialName("ListMailboxThreads") val page: MailThreadPage)

@Serializable
private class MailThreadAnswer(@SerialName("GetMailboxThread") val view: MailThreadView)

@Serializable
private class MailItemAnswer(@SerialName("GetMailboxItem") val item: MailItem)

@Serializable
private class MailContentAnswer(@SerialName("GetMailContent") val content: MailContent)

@Serializable
private class MoveAnswer(@SerialName("MoveMailboxItems") val moved: List<MailMoved?>)

@Serializable
private class NewestItemsAnswer(@SerialName("ListMailboxItems") val page: NewestItemsPage? = null) {
    @Serializable
    class NewestItemsPage(val items: List<MailMoved?> = emptyList())
}

@Serializable
private class AgentCategoriesAnswer(@SerialName("ReadAgent") val agent: AgentCategories? = null) {
    @Serializable
    class AgentCategories(val categories: List<String>? = null)
}

@Serializable
private class MailDraftAnswer(@SerialName("GetMailboxDraft") val draft: MailDraft)

@Serializable
private class SaveDraftAnswer(@SerialName("SaveMailboxDraft") val saved: Saved) {
    @Serializable
    class Saved(val id: String)
}

@Serializable
private class DraftReplyAnswer(@SerialName("DraftReply") val drafted: Drafted) {
    @Serializable
    class Drafted(val text: String)
}

@Serializable
private class SubmissionAnswer(@SerialName("GetMailboxSubmission") val submission: Submission? = null) {
    @Serializable
    class Submission(val mailId: String = "", val sentItemId: String = "")
}

@Serializable
private class SendAnswer(@SerialName("SendMailboxMessage") val sent: Sent) {
    @Serializable
    class Sent(val item: SentItem? = null)

    @Serializable
    class SentItem(val id: String)
}

@Serializable
private class CancelSubmissionAnswer(@SerialName("CancelMailboxSubmission") val submission: Submission? = null) {
    @Serializable
    class Submission(val mailId: String = "")
}

@Serializable
private class SubscriptionsAnswer(@SerialName("ListMailboxSubscriptions") val page: MailSubscriptionPage)

@Serializable
private class SubscriptionReadAnswer(@SerialName("ReadMailboxSubscription") val view: View) {
    @Serializable
    class View(val threadId: String, val subject: String = "", val truncated: Boolean = false, val items: List<MailThreadView.Entry?> = emptyList())
}

@Serializable
private class InvitationAnswer(@SerialName("GetMailInvitation") val invitation: MailInvitation? = null)

@Serializable
private class CalendarRequestAnswer(@SerialName("GetCalendarRequest") val request: CalendarRequest? = null) {
    @Serializable
    class CalendarRequest(val requestId: String = "")
}

@Serializable
private class UploadedDraft(val itemId: String? = null, val attachments: List<MailContent.Attachment>? = null, val error: String? = null)

/** What attaching a file to a draft answered: the draft's item and its attachments as the server now numbers them. */
data class MailDraftAttachments(val itemId: String, val attachments: List<MailContent.Attachment>)

private fun numberOf(count: Int): JsonElement = JsonPrimitive(count)

private suspend fun Connection.mutate(document: String, variables: Map<String, JsonElement>) {
    execute<JsonElement>(document, variables)
}

suspend fun Connection.mailboxes(): List<MailboxEntry> =
    execute<MailboxesAnswer>(MailDocuments.MAILBOXES).entries.filterNotNull()

/** A page of a folder's conversations, or of the whole mailbox's for Starred, Priority and searching everywhere. */
suspend fun Connection.mailThreads(
    folderId: String?,
    mailboxId: String?,
    filter: MailFilter,
    isPriority: Boolean,
    first: Int,
    offset: Int,
    zone: ZoneId = ZoneId.systemDefault(),
): MailThreadPage {
    val variables = linkedMapOf<String, JsonElement>("first" to numberOf(first), "offset" to numberOf(offset))
    if (folderId != null) variables["folderId"] = JsonPrimitive(folderId)
    if (mailboxId != null) variables["mailboxId"] = JsonPrimitive(mailboxId)
    if (filter.isUnreadOnly) variables["unread"] = JsonPrimitive(true)
    if (filter.isStarredOnly) variables["flagged"] = JsonPrimitive(true)
    if (isPriority) variables["priority"] = JsonPrimitive("high")
    for ((key, typed) in listOf("search" to filter.searchText, "from" to filter.from, "to" to filter.to, "subject" to filter.subject)) {
        if (typed.trim().isNotEmpty()) variables[key] = JsonPrimitive(typed.trim())
    }
    filter.since?.let { day -> variables["since"] = JsonPrimitive(ServerTime.startOfDayText(day, zone)) }
    filter.before?.let { day -> variables["before"] = JsonPrimitive(ServerTime.startOfDayText(day.plusDays(1), zone)) }
    filter.hasAttachment?.let { has -> variables["hasAttachment"] = JsonPrimitive(has) }
    return execute<MailThreadsAnswer>(MailDocuments.MAIL_THREADS, variables).page
}

suspend fun Connection.mailThread(itemId: String): MailThreadView =
    execute<MailThreadAnswer>(MailDocuments.MAIL_THREAD, mapOf("itemId" to JsonPrimitive(itemId))).view

suspend fun Connection.mailItem(itemId: String): MailItem =
    execute<MailItemAnswer>(MailDocuments.MAIL_ITEM, mapOf("itemId" to JsonPrimitive(itemId))).item

suspend fun Connection.mailContent(mailId: String): MailContent =
    execute<MailContentAnswer>(MailDocuments.MAIL_CONTENT, mapOf("mailId" to JsonPrimitive(mailId))).content

suspend fun Connection.setMailFlags(itemIds: List<String>, isSeen: Boolean? = null, isFlagged: Boolean? = null) {
    val variables = linkedMapOf<String, JsonElement>("itemIds" to strings(itemIds))
    if (isSeen != null) variables["seen"] = JsonPrimitive(isSeen)
    if (isFlagged != null) variables["flagged"] = JsonPrimitive(isFlagged)
    mutate(MailDocuments.SET_MAIL_FLAGS, variables)
}

/** Moves messages; each comes back as a new item in its new folder. */
suspend fun Connection.moveMail(itemIds: List<String>, folderId: String): List<MailMoved> =
    execute<MoveAnswer>(MailDocuments.MOVE_MAIL, mapOf("itemIds" to strings(itemIds), "folderId" to JsonPrimitive(folderId))).moved.filterNotNull()

/** Moves messages to Trash, or out of Trash for good. */
suspend fun Connection.deleteMail(itemIds: List<String>) {
    mutate(MailDocuments.DELETE_MAIL, mapOf("itemIds" to strings(itemIds)))
}

suspend fun Connection.reportJunk(itemIds: List<String>, isNotJunk: Boolean) {
    mutate(MailDocuments.REPORT_JUNK, mapOf("itemIds" to strings(itemIds), "notJunk" to JsonPrimitive(isNotJunk)))
}

suspend fun Connection.emptyTrash(mailboxId: String) {
    mutate(MailDocuments.EMPTY_TRASH, mapOf("mailboxId" to JsonPrimitive(mailboxId)))
}

/** The newest items of a folder, for finding what an action put there. */
suspend fun Connection.newestMailItems(folderId: String, first: Int = 200): List<MailMoved> =
    execute<NewestItemsAnswer>(MailDocuments.NEWEST_MAIL_ITEMS, mapOf("folderId" to JsonPrimitive(folderId), "first" to numberOf(first)))
        .page?.items?.filterNotNull() ?: emptyList()

suspend fun Connection.sortMail(itemIds: List<String>, category: String?, priority: String?, needsReply: Boolean?) {
    val variables = linkedMapOf<String, JsonElement>("itemIds" to strings(itemIds))
    if (category != null) variables["category"] = JsonPrimitive(category)
    if (priority != null) variables["priority"] = JsonPrimitive(priority)
    if (needsReply != null) variables["needsReply"] = JsonPrimitive(needsReply)
    mutate(MailDocuments.SORT_MAIL, variables)
}

suspend fun Connection.agentCategories(): List<String> =
    execute<AgentCategoriesAnswer>(MailDocuments.AGENT_CATEGORIES).agent?.categories ?: emptyList()

suspend fun Connection.showMailImages(itemIds: List<String>) {
    mutate(MailDocuments.SHOW_MAIL_IMAGES, mapOf("itemIds" to strings(itemIds), "show" to JsonPrimitive(true)))
}

suspend fun Connection.cancelAgentReply(replyId: String) {
    mutate(MailDocuments.CANCEL_AGENT_REPLY, mapOf("replyId" to JsonPrimitive(replyId)))
}

suspend fun Connection.mailDraft(itemId: String): MailDraft =
    execute<MailDraftAnswer>(MailDocuments.MAIL_DRAFT, mapOf("itemId" to JsonPrimitive(itemId))).draft

/** Saves a draft and returns its item id. */
suspend fun Connection.saveMailDraft(mailboxId: String, message: MailOutgoing): String =
    execute<SaveDraftAnswer>(MailDocuments.SAVE_MAIL_DRAFT, mapOf("mailboxId" to JsonPrimitive(mailboxId), "message" to message.variables)).saved.id

suspend fun Connection.draftReply(itemId: String, instructions: String): String {
    val variables = linkedMapOf<String, JsonElement>("itemId" to JsonPrimitive(itemId))
    if (instructions.isNotEmpty()) variables["instructions"] = JsonPrimitive(instructions)
    return execute<DraftReplyAnswer>(MailDocuments.DRAFT_REPLY, variables).drafted.text
}

/**
 * Sends a message. One sent before whose answer was lost is asked about first and sent again only if the server never
 * took it. Returns the sent item's id when the server says it.
 */
suspend fun Connection.send(sending: MailSending, isRetry: Boolean): String? {
    if (isRetry) {
        val taken = execute<SubmissionAnswer>(
            MailDocuments.MAIL_SUBMISSION,
            mapOf("mailboxId" to JsonPrimitive(sending.mailboxId), "submissionId" to JsonPrimitive(sending.submissionId)),
        ).submission
        if (taken != null) return taken.sentItemId
    }
    return execute<SendAnswer>(
        MailDocuments.SEND_MAIL,
        mapOf("mailboxId" to JsonPrimitive(sending.mailboxId), "submissionId" to JsonPrimitive(sending.submissionId), "message" to sending.message.variables),
    ).sent.item?.id
}

/** Stops a send; true when the server had already taken it. */
suspend fun Connection.cancel(sending: MailSending): Boolean {
    val answer = execute<CancelSubmissionAnswer>(
        MailDocuments.CANCEL_MAIL_SUBMISSION,
        mapOf("mailboxId" to JsonPrimitive(sending.mailboxId), "submissionId" to JsonPrimitive(sending.submissionId)),
    )
    return !answer.submission?.mailId.isNullOrEmpty()
}

suspend fun Connection.mailSubscriptions(mailboxId: String, isLeft: Boolean, matching: String, first: Int, offset: Int): MailSubscriptionPage {
    val variables = linkedMapOf<String, JsonElement>(
        "mailboxId" to JsonPrimitive(mailboxId), "first" to numberOf(first), "offset" to numberOf(offset), "left" to JsonPrimitive(isLeft),
    )
    if (matching.isNotEmpty()) variables["matching"] = JsonPrimitive(matching)
    return execute<SubscriptionsAnswer>(MailDocuments.MAIL_SUBSCRIPTIONS, variables).page
}

suspend fun Connection.readMailSubscription(mailboxId: String, key: String): MailThreadView {
    val view = execute<SubscriptionReadAnswer>(
        MailDocuments.READ_MAIL_SUBSCRIPTION, mapOf("mailboxId" to JsonPrimitive(mailboxId), "key" to JsonPrimitive(key)),
    ).view
    return MailThreadView(threadId = view.threadId, subject = view.subject, truncated = view.truncated, items = view.items)
}

suspend fun Connection.showSubscriptionImages(mailboxId: String, key: String, isShown: Boolean) {
    mutate(
        MailDocuments.SHOW_SUBSCRIPTION_IMAGES,
        mapOf("mailboxId" to JsonPrimitive(mailboxId), "key" to JsonPrimitive(key), "show" to JsonPrimitive(isShown)),
    )
}

suspend fun Connection.muteSubscription(mailboxId: String, key: String, isMuted: Boolean) {
    mutate(
        MailDocuments.MUTE_SUBSCRIPTION,
        mapOf("mailboxId" to JsonPrimitive(mailboxId), "key" to JsonPrimitive(key), "muted" to JsonPrimitive(isMuted)),
    )
}

/** Leaves a list. A failure is recorded on the list rather than thrown. */
suspend fun Connection.unsubscribe(mailboxId: String, key: String) {
    mutate(MailDocuments.UNSUBSCRIBE, mapOf("mailboxId" to JsonPrimitive(mailboxId), "key" to JsonPrimitive(key)))
}

suspend fun Connection.mailInvitation(itemId: String): MailInvitation? =
    execute<InvitationAnswer>(MailDocuments.MAIL_INVITATION, mapOf("itemId" to JsonPrimitive(itemId))).invitation

/** Answers an invitation, with an id of its own so an answer whose reply was lost is not sent twice. */
suspend fun Connection.answerInvitation(itemId: String, answer: String, requestId: String, isRetry: Boolean) {
    if (isRetry) {
        val asked = execute<CalendarRequestAnswer>(MailDocuments.CALENDAR_REQUEST, mapOf("requestId" to JsonPrimitive(requestId)))
        if (asked.request != null) return
    }
    mutate(
        MailDocuments.ANSWER_INVITATION,
        mapOf("requestId" to JsonPrimitive(requestId), "itemId" to JsonPrimitive(itemId), "answer" to JsonPrimitive(answer)),
    )
}

suspend fun Connection.setProposalStatus(itemId: String, index: Int, status: String) {
    mutate(
        MailDocuments.SET_PROPOSAL_STATUS,
        mapOf("itemId" to JsonPrimitive(itemId), "index" to numberOf(index), "status" to JsonPrimitive(status)),
    )
}

/**
 * Adds a file to a draft: to the one given, or to a new draft made around it. Returns the draft's item id and its
 * attachments as the server now numbers them.
 */
suspend fun Connection.attachToDraft(
    mailboxId: String,
    draftItemId: String?,
    name: String,
    contentType: String,
    data: ByteArray,
): MailDraftAttachments {
    val boundary = "teanode-${java.util.UUID.randomUUID()}"
    val path = if (draftItemId != null) "/api/v1/mailbox/drafts/$draftItemId/attachments" else "/api/v1/mailbox/$mailboxId/drafts/attachments"
    val response = send(
        HTTPRequest(
            account.server.endpoint(path).toString(), if (draftItemId == null) "POST" else "PUT",
            mapOf("Content-Type" to "multipart/form-data; boundary=$boundary"),
            Upload.body(boundary, name, contentType, data),
        ),
    )
    val uploaded = try {
        TeaNodeJson.decodeFromString(UploadedDraft.serializer(), response.text)
    } catch (_: Exception) {
        null
    }
    val itemId = uploaded?.itemId
    if (response.statusCode != 200 || itemId == null) {
        if (response.statusCode == 413) throw TeaNodeError.Refused("That file is larger than a message may be.")
        throw TeaNodeError.Refused(uploaded?.error ?: response.text.take(300))
    }
    return MailDraftAttachments(itemId, uploaded.attachments ?: emptyList())
}

@Serializable
private class CalendarsAnswer(@SerialName("ListCalendars") val calendars: List<ProposalCalendar>) {
    @Serializable
    class ProposalCalendar(val id: String, val calendarKind: String? = null, val shareLevel: String? = null, val timezone: String? = null) {
        val isOwn: Boolean get() = shareLevel == null || shareLevel == "owner"
    }
}

/**
 * Adds the event the agent found in a message to the person's own calendar of events, as the calendar's change is made:
 * with an id of its own, so one whose answer was lost is not made twice.
 */
suspend fun Connection.addProposedEvent(proposal: MailProposal, requestId: String = java.util.UUID.randomUUID().toString().replace("-", "")) {
    val calendar = execute<CalendarsAnswer>(MailProposalDocuments.CALENDARS).calendars.firstOrNull { calendar -> calendar.calendarKind == "events" && calendar.isOwn }
        ?: throw TeaNodeError.Refused("There is no calendar to add it to.")
    execute<JsonElement>(
        MailProposalDocuments.SAVE_CALENDAR_EVENT,
        mapOf(
            "requestId" to JsonPrimitive(requestId), "calendarId" to JsonPrimitive(calendar.id), "summary" to JsonPrimitive(proposal.summary),
            "location" to JsonPrimitive(proposal.location), "startsAt" to JsonPrimitive(proposal.starts),
            "endsAt" to JsonPrimitive(proposal.ends.ifEmpty { proposal.starts }), "allDay" to JsonPrimitive(proposal.allDay),
            "timezone" to JsonPrimitive(if (proposal.allDay) "" else calendar.timezone ?: ""),
        ),
    )
}

/** Has the agent read a mail as a receipt. */
suspend fun Connection.readReceipt(mailboxItemId: String) {
    execute<JsonElement>(MailProposalDocuments.READ_RECEIPT, mapOf("mailboxItemId" to JsonPrimitive(mailboxItemId)))
}

/** Has the agent read each PDF and picture a message carries as a paystub or a tax form. */
suspend fun Connection.readTaxDocument(mailboxItemId: String) {
    execute<JsonElement>(MailProposalDocuments.READ_TAX_DOCUMENT, mapOf("mailboxItemId" to JsonPrimitive(mailboxItemId)))
}
