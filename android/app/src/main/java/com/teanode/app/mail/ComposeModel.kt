package com.teanode.app.mail

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.app.R
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.mail.MailContent
import com.teanode.kit.mail.MailOutgoing
import com.teanode.kit.mail.MailSending
import com.teanode.kit.mail.MailWords
import com.teanode.kit.mail.Mailbox
import com.teanode.kit.mail.ServerTime
import com.teanode.kit.mail.attachToDraft
import com.teanode.kit.mail.cancel
import com.teanode.kit.mail.deleteMail
import com.teanode.kit.mail.draftReply
import com.teanode.kit.mail.mailContent
import com.teanode.kit.mail.mailDraft
import com.teanode.kit.mail.mailItem
import com.teanode.kit.mail.saveMailDraft
import com.teanode.kit.mail.send
import java.time.Instant

/** A file to attach, read from the phone. */
class PendingFile(val name: String, val contentType: String, val data: ByteArray)

/** What a message being written starts from. */
data class ComposeStart(val kind: ComposeKind, val itemId: String? = null, val to: String? = null)

/**
 * ComposeModel is one message being written: its fields, the quote it carries, the attachments, the draft it keeps
 * saved on the server, and a send whose answer may have been lost, kept on the phone so it is asked about before it is
 * sent again.
 */
class ComposeModel(val mail: MailModel, val start: ComposeStart) {
    private val context: Context = mail.context
    private val preferences = context.getSharedPreferences("mail", Context.MODE_PRIVATE)
    private val pendingKey = "mail-pending-send.${mail.connection.account.username}"

    var mailboxId by mutableStateOf("")
    var from by mutableStateOf("")
    var to by mutableStateOf("")
    var cc by mutableStateOf("")
    var bcc by mutableStateOf("")
    var subject by mutableStateOf("")
    var body by mutableStateOf("")
        private set
    var isShowingCopies by mutableStateOf(false)
    var quoteHtml by mutableStateOf("")
        private set
    var quoteText by mutableStateOf("")
        private set
    var carried by mutableStateOf(emptyList<MailContent.Attachment>())
        private set
    private var inlineKept = emptyList<Int>()
    private var isForwardCarry = false
    var uploadingCount by mutableStateOf(0)
        private set
    var isLoaded by mutableStateOf(false)
        private set
    var isSaving by mutableStateOf(false)
        private set
    var isSending by mutableStateOf(false)
        private set

    /** Whether this message went out, after which nothing of it is kept as a draft. */
    private var isSent = false
    var savedAt by mutableStateOf<Instant?>(null)
        private set
    var warningText by mutableStateOf<String?>(null)
    var draftItemId by mutableStateOf<String?>(null)
        private set
    private var replyToItemId = ""
    private var forwardItemId = ""
    var isChanged = false
        private set
    var pending by mutableStateOf<MailSending?>(runCatching { preferences.getString(pendingKey, null)?.let { text -> TeaNodeJson.decodeFromString(MailSending.serializer(), text) } }.getOrNull())
        private set
    var agentInstructions by mutableStateOf("")
    var isDrafting by mutableStateOf(false)
        private set

    fun changeBody(text: String) {
        body = text
        isChanged = true
    }

    val mailbox: Mailbox? get() = mail.entries.firstOrNull { entry -> entry.mailbox?.id == mailboxId }?.mailbox ?: mail.mailbox

    val isReply: Boolean get() = start.kind == ComposeKind.Reply || start.kind == ComposeKind.ReplyAll

    private val recipients: List<String> get() = MailWords.addresses(to) + MailWords.addresses(cc) + MailWords.addresses(bcc)

    val canSend: Boolean
        get() = from.isNotEmpty() && recipients.isNotEmpty() && uploadingCount == 0 && !isSending && pending == null && (body.isNotBlank() || carried.isNotEmpty())

    suspend fun load() {
        if (isLoaded) return
        mailboxId = mail.mailbox?.id ?: ""
        try {
            when (start.kind) {
                ComposeKind.New -> {
                    to = start.to ?: ""
                    addSignature()
                }
                ComposeKind.Draft -> start.itemId?.let { itemId -> fillDraft(itemId) }
                ComposeKind.Reply, ComposeKind.ReplyAll, ComposeKind.Forward -> start.itemId?.let { itemId ->
                    fillAnswer(itemId)
                    addSignature()
                }
            }
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            mail.snackbar?.showError(failed)
        }
        if (from.isEmpty()) from = mailbox?.sendingAddresses?.firstOrNull() ?: ""
        if (pending != null) warningText = context.getString(R.string.mail_may_be_sent)
        isLoaded = true
        isChanged = false
    }

    private fun owningMailbox(folderId: String) {
        mail.folderById(folderId)?.mailboxId?.let { owner -> mailboxId = owner }
    }

    private suspend fun fillDraft(itemId: String) {
        val draft = mail.connection.mailDraft(itemId)
        runCatching { mail.connection.mailItem(itemId) }.getOrNull()?.let { item -> owningMailbox(item.folderId) }
        draftItemId = draft.itemId
        from = draft.from
        to = draft.to.joinToString(", ")
        cc = draft.cc.joinToString(", ")
        bcc = draft.bcc.joinToString(", ")
        isShowingCopies = draft.cc.isNotEmpty() || draft.bcc.isNotEmpty()
        subject = draft.subject
        replyToItemId = draft.replyToItemId
        forwardItemId = draft.forwardItemId
        val attachments = draft.attachments.filterNotNull()
        carried = attachments.filter { file -> !file.inline }
        inlineKept = attachments.filter { file -> file.inline }.map { file -> file.index }
        val quoteStart = draft.html.indexOf("<div class=\"teanode-quote\"")
        if (quoteStart >= 0) quoteHtml = draft.html.substring(quoteStart)
        val split = Regex("""\n\n(On .+ wrote:\n> |---------- Forwarded message ----------)""").find(draft.text)
        if (split != null) {
            body = draft.text.substring(0, split.range.first)
            quoteText = draft.text.substring(split.range.first).trim('\n')
        } else {
            body = draft.text
        }
    }

    private suspend fun fillAnswer(itemId: String) {
        val item = mail.connection.mailItem(itemId)
        owningMailbox(item.folderId)
        val message = item.mail
        if (message == null) {
            warningText = context.getString(R.string.mail_answered_not_stored)
            return
        }
        val content = runCatching { mail.connection.mailContent(message.id) }.getOrNull()
        val original = content?.let { read -> read.html.ifEmpty { MailWords.html(read.text) } } ?: ""
        val originalText = content?.text ?: ""
        if (content?.available == false) warningText = context.getString(R.string.mail_answered_not_stored)
        val whenText = ServerTime.instant(message.receivedAt)?.let { moment -> MailTimes.dateAndTime(moment) } ?: message.receivedAt
        val sender = if (message.fromName.isEmpty()) message.from else "${message.fromName} <${message.from}>"
        if (start.kind == ComposeKind.Forward) {
            forwardItemId = item.id
            isForwardCarry = true
            subject = MailWords.forwardSubject(message.subject)
            carried = content?.presentAttachments?.filter { file -> !file.inline } ?: emptyList()
            val header = listOf(
                "---------- Forwarded message ----------", "From: $sender", "Date: $whenText", "To: ${message.recipients.joinToString(", ")}", "Subject: ${message.subject}",
            )
            quoteHtml = "<div class=\"teanode-quote\"><p>" + header.joinToString("<br>") { line -> MailWords.escaped(line) } + "</p>" + original + "</div>"
            quoteText = header.joinToString("\n") + "\n\n" + originalText
        } else {
            replyToItemId = item.id
            val own = mail.entries.flatMap { entry -> entry.mailbox?.sendingAddresses ?: emptyList() }
            val reply = MailWords.reply(message, content?.header("Reply-To"), own, start.kind == ComposeKind.ReplyAll)
            to = reply.to.joinToString(", ")
            cc = reply.cc.joinToString(", ")
            isShowingCopies = reply.cc.isNotEmpty()
            val address = reply.from
            if (address != null) {
                mailbox?.sendingAddresses?.firstOrNull { candidate -> candidate.lowercase() == address.lowercase() }?.let { matching -> from = matching }
            }
            subject = MailWords.replySubject(message.subject)
            val said = context.getString(R.string.mail_on_wrote, whenText, sender)
            quoteHtml = "<div class=\"teanode-quote\"><p>${MailWords.escaped(said)}</p><blockquote>$original</blockquote></div>"
            quoteText = said + "\n" + MailWords.quoted(originalText)
        }
    }

    private fun addSignature() {
        val signature = mailbox?.signatureText
        if (signature.isNullOrEmpty()) return
        body = "\n\n-- \n$signature"
    }

    suspend fun uploadFiles(files: List<PendingFile>) {
        val limit = mail.entries.firstOrNull { entry -> entry.mailbox?.id == mailboxId }?.maxMessageSize ?: 0
        val adding = files.sumOf { file -> file.data.size.toLong() } + carried.sumOf { file -> file.size.toLong() }
        if (limit > 0 && adding > limit) {
            mail.snackbar?.showProblem(context.getString(R.string.mail_files_too_large, fileSize(adding), fileSize(limit)))
            return
        }
        uploadingCount += files.size
        for (file in files) {
            try {
                if (draftItemId == null) save()
                val uploaded = mail.connection.attachToDraft(mailboxId, draftItemId, file.name, file.contentType, file.data)
                draftItemId = uploaded.itemId
                carried = uploaded.attachments.filter { attachment -> !attachment.inline }
                inlineKept = uploaded.attachments.filter { attachment -> attachment.inline }.map { attachment -> attachment.index }
                isForwardCarry = false
                isChanged = true
            } catch (failed: kotlinx.coroutines.CancellationException) {
                throw failed
            } catch (failed: Throwable) {
                mail.snackbar?.showError(failed)
            } finally {
                uploadingCount -= 1
            }
        }
    }

    fun remove(attachment: MailContent.Attachment) {
        carried = carried.filter { file -> file.index != attachment.index }
        isChanged = true
    }

    suspend fun draftWithAgent() {
        if (replyToItemId.isEmpty()) return
        isDrafting = true
        try {
            val drafted = mail.connection.draftReply(replyToItemId, agentInstructions)
            changeBody(drafted + (if (body.isEmpty()) "" else "\n\n$body"))
            agentInstructions = ""
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            mail.snackbar?.showError(failed)
        } finally {
            isDrafting = false
        }
    }

    val outgoing: MailOutgoing
        get() = MailOutgoing(
            from = from, to = MailWords.addresses(to), cc = MailWords.addresses(cc), bcc = MailWords.addresses(bcc), subject = subject,
            htmlContent = MailWords.html(body) + quoteHtml, textContent = if (quoteText.isEmpty()) body else body + "\n\n" + quoteText,
            replyToItemId = replyToItemId, forwardItemId = forwardItemId,
            forwardAttachments = if (isForwardCarry) carried.map { file -> file.index } else emptyList(),
            draftItemId = draftItemId ?: "", keepAttachments = if (draftItemId != null) carried.map { file -> file.index } + inlineKept else emptyList(),
        )

    suspend fun save(isForced: Boolean = false) {
        if (!isLoaded || isSaving || isSending || isSent || pending != null || !(isForced || isChanged || draftItemId == null)) return
        isSaving = true
        try {
            val saved = mail.connection.saveMailDraft(mailboxId, outgoing)
            draftItemId = saved
            savedAt = Instant.now()
            isChanged = false
            runCatching { mail.connection.mailDraft(saved) }.getOrNull()?.let { draft ->
                val attachments = draft.attachments.filterNotNull()
                carried = attachments.filter { file -> !file.inline }
                inlineKept = attachments.filter { file -> file.inline }.map { file -> file.index }
                isForwardCarry = false
            }
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            mail.snackbar?.showError(failed)
        } finally {
            isSaving = false
        }
    }

    suspend fun send(): Boolean {
        // A draft being saved at this moment would be made after the server dropped the draft this message replaces.
        while (isSaving) kotlinx.coroutines.delay(50)
        val sending = pending ?: MailSending(mailboxId, outgoing)
        val isRetry = pending != null
        keep(sending)
        isSending = true
        return try {
            mail.connection.send(sending, isRetry)
            keep(null)
            isSent = true
            mail.snackbar?.show(context.getString(R.string.mail_sent_done))
            mail.load()
            true
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            warningText = context.getString(R.string.mail_may_be_sent)
            mail.snackbar?.showError(failed)
            false
        } finally {
            isSending = false
        }
    }

    suspend fun returnToEditing(): Boolean {
        val kept = pending ?: return false
        try {
            val wasSent = mail.connection.cancel(kept)
            keep(null)
            warningText = null
            if (wasSent) {
                mail.snackbar?.show(context.getString(R.string.mail_sent_done))
                return true
            }
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            mail.snackbar?.showError(failed)
        }
        return false
    }

    suspend fun discard() {
        val itemId = draftItemId ?: return
        mail.act(context.getString(R.string.mail_draft_discarded)) { mail.connection.deleteMail(listOf(itemId)) }
    }

    private fun keep(sending: MailSending?) {
        pending = sending
        runCatching {
            if (sending != null) preferences.edit().putString(pendingKey, TeaNodeJson.encodeToString(MailSending.serializer(), sending)).apply() else preferences.edit().remove(pendingKey).apply()
        }
    }
}
