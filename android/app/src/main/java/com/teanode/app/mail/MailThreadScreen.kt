package com.teanode.app.mail

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Reply
import androidx.compose.material.icons.automirrored.outlined.ReplyAll
import androidx.compose.material.icons.automirrored.outlined.Forward
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DeleteForever
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.DownloadForOffline
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.GppBad
import androidx.compose.material.icons.outlined.GppGood
import androidx.compose.material.icons.outlined.GppMaybe
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.InvertColors
import androidx.compose.material.icons.outlined.MarkEmailRead
import androidx.compose.material.icons.outlined.MarkEmailUnread
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PersonAdd
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.Receipt
import androidx.compose.material.icons.outlined.Report
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Sell
import androidx.compose.material.icons.outlined.Star
import androidx.compose.material.icons.outlined.StarBorder
import androidx.compose.material.icons.outlined.ThumbUp
import androidx.compose.material.icons.outlined.ViewList
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.Notes
import androidx.compose.material3.BottomAppBar
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.asReasonSentence
import com.teanode.kit.mail.MailContent
import com.teanode.kit.mail.MailFolder
import com.teanode.kit.mail.MailFolders
import com.teanode.kit.mail.MailInsight
import com.teanode.kit.mail.MailInvitation
import com.teanode.kit.mail.MailItem
import com.teanode.kit.mail.MailMessage
import com.teanode.kit.mail.MailProposal
import com.teanode.kit.mail.MailThreadView
import com.teanode.kit.mail.MailVerdict
import com.teanode.kit.mail.MailWords
import com.teanode.kit.mail.ServerTime
import com.teanode.kit.mail.addProposedEvent
import com.teanode.kit.contacts.addressBooks
import com.teanode.kit.mail.answerInvitation
import com.teanode.kit.mail.cancelAgentReply
import com.teanode.kit.mail.mailContent
import com.teanode.kit.mail.mailInvitation
import com.teanode.kit.mail.mailThread
import com.teanode.kit.mail.readReceipt
import com.teanode.kit.mail.readTaxDocument
import com.teanode.kit.mail.setMailFlags
import com.teanode.kit.mail.setProposalStatus
import com.teanode.kit.mail.showMailImages
import com.teanode.kit.mail.sortMail
import com.teanode.kit.contacts.contacts
import com.teanode.kit.contacts.saveContact
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.text.DecimalFormat

/** MailThreadScreen is one conversation: its messages newest first, each folded to a line or opened to read, with what the agent made of it. */
@Composable
fun MailThreadScreen(
    mail: MailModel,
    itemId: String,
    folderId: String?,
    onBack: () -> Unit,
    onCompose: (ComposeKind, String?) -> Unit,
    startsWithEverythingOpen: Boolean = false,
) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    var thread by remember { mutableStateOf<MailThreadView?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    val expanded = remember { mutableStateListOf<String>() }
    var isSorting by remember { mutableStateOf(false) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var isMoveMenuOpen by remember { mutableStateOf(false) }
    var isReplyMenuOpen by remember { mutableStateOf(false) }

    val currentFolder = folderId?.let { id -> mail.folderById(id) }

    suspend fun load() {
        try {
            val read = mail.connection.mailThread(itemId)
            val isFirst = thread == null
            thread = read
            if (isFirst) {
                val items = read.entries.mapNotNull { entry -> entry.item }
                val open = items.filter { item -> !item.seen }.map { item -> item.id }.toMutableSet()
                items.firstOrNull { item -> !item.draft }?.let { newest -> open.add(newest.id) }
                open.add(itemId)
                if (startsWithEverythingOpen) open.addAll(items.map { item -> item.id })
                expanded.clear()
                expanded.addAll(open)
                val marking = read.entries.filter { entry ->
                    val item = entry.item
                    item != null && open.contains(item.id) && !item.seen && !item.draft && (item.id == itemId || folderId == null || entry.folderId == folderId)
                }.mapNotNull { entry -> entry.item?.id }
                if (marking.isNotEmpty()) {
                    scope.launch {
                        runCatching { mail.connection.setMailFlags(marking, isSeen = true) }
                        mail.load()
                    }
                }
                if (itemId == items.firstOrNull()?.id && items.firstOrNull()?.draft == true) onCompose(ComposeKind.Draft, itemId)
            }
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failure = failed
        }
    }

    LaunchedEffect(itemId) { load() }
    // The summary is written after the thread is read; it is asked for a few times until it is there.
    LaunchedEffect(thread?.summary?.pending) {
        if (thread?.summary?.pending != true) return@LaunchedEffect
        repeat(6) {
            delay(5000)
            runCatching { mail.connection.mailThread(itemId) }.getOrNull()?.let { read ->
                thread = read
                if (read.summary?.pending != true) return@LaunchedEffect
            }
        }
    }

    val entries = thread?.entries ?: emptyList()
    val targetItems = run {
        val inFolder = entries.filter { entry -> folderId != null && entry.folderId == folderId }.mapNotNull { entry -> entry.item }
        inFolder.ifEmpty { entries.mapNotNull { entry -> entry.item }.filter { item -> item.id == itemId } }
    }
    val newest = entries.mapNotNull { entry -> entry.item }.firstOrNull()
    val isAnyUnread = targetItems.any { item -> !item.seen }
    val isAnyUnstarred = targetItems.any { item -> !item.flagged }
    val deleteLabel = stringResource(if (currentFolder?.kind == "trash") R.string.mail_delete_forever else R.string.mail_delete)

    fun move(destination: MailFolder, said: String) {
        val target = MailTarget.of(targetItems)
        scope.launch { if (mail.move(target, destination.id, said)) onBack() }
    }

    TeaNodeScreen(
        title = "",
        onBack = onBack,
        actions = {
            IconButton(onClick = {
                val ids = targetItems.map { item -> item.id }
                scope.launch {
                    mail.act(context.getString(if (isAnyUnstarred) R.string.mail_starred_done else R.string.mail_unstarred_done)) { mail.connection.setMailFlags(ids, isFlagged = isAnyUnstarred) }
                    load()
                }
            }, enabled = targetItems.isNotEmpty()) {
                Icon(if (isAnyUnstarred) Icons.Outlined.StarBorder else Icons.Outlined.Star, stringResource(if (isAnyUnstarred) R.string.mail_star else R.string.mail_unstar))
            }
            Box {
                IconButton(onClick = { isMenuOpen = true }, enabled = targetItems.isNotEmpty()) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.mail_more)) }
                DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text(stringResource(if (isAnyUnread) R.string.mail_mark_read else R.string.mail_mark_unread)) },
                        leadingIcon = { Icon(if (isAnyUnread) Icons.Outlined.MarkEmailRead else Icons.Outlined.MarkEmailUnread, null) },
                        onClick = {
                            isMenuOpen = false
                            val ids = targetItems.map { item -> item.id }
                            scope.launch {
                                mail.act(context.getString(if (isAnyUnread) R.string.mail_marked_read else R.string.mail_marked_unread)) { mail.connection.setMailFlags(ids, isSeen = isAnyUnread) }
                                load()
                            }
                        },
                    )
                    if (mail.mailbox?.agent?.isSorting == true) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.mail_sort_as)) }, leadingIcon = { Icon(Icons.Outlined.Sell, null) }, onClick = { isMenuOpen = false; isSorting = true })
                    }
                    if (currentFolder?.kind == "junk") {
                        DropdownMenuItem(text = { Text(stringResource(R.string.mail_not_junk)) }, leadingIcon = { Icon(Icons.Outlined.ThumbUp, null) }, onClick = {
                            isMenuOpen = false
                            val target = MailTarget.of(targetItems)
                            scope.launch { if (mail.reportJunk(target, true, context.getString(R.string.mail_said_not_junk))) onBack() }
                        })
                    } else {
                        DropdownMenuItem(text = { Text(stringResource(R.string.mail_report_junk)) }, leadingIcon = { Icon(Icons.Outlined.Report, null) }, onClick = {
                            isMenuOpen = false
                            val target = MailTarget.of(targetItems)
                            scope.launch { if (mail.reportJunk(target, false, context.getString(R.string.mail_reported_junk))) onBack() }
                        })
                    }
                }
            }
        },
        bottomBar = {
            BottomAppBar {
                val archive = mail.folderOfKind("archive")
                if (archive != null && currentFolder?.id != archive.id) {
                    IconButton(onClick = { move(archive, context.getString(R.string.mail_conversation_archived)) }) { Icon(Icons.Outlined.Archive, stringResource(R.string.mail_archive_action)) }
                }
                Box {
                    IconButton(onClick = { isMoveMenuOpen = true }) { Icon(Icons.Outlined.Folder, stringResource(R.string.mail_move_to)) }
                    DropdownMenu(expanded = isMoveMenuOpen, onDismissRequest = { isMoveMenuOpen = false }) {
                        for (row in MailFolders.rows(mail.folders).filter { row -> row.folder.id != currentFolder?.id }) {
                            val name = MailNames.folder(row.folder)
                            DropdownMenuItem(
                                text = { Text(name, Modifier.padding(start = (row.depth.coerceAtMost(3) * 12).dp)) }, leadingIcon = { Icon(MailNames.icon(row.folder), null) },
                                onClick = { isMoveMenuOpen = false; move(row.folder, context.getString(R.string.mail_moved_to, name)) },
                            )
                        }
                    }
                }
                IconButton(onClick = {
                    val target = MailTarget.of(targetItems)
                    val isForever = currentFolder?.kind == "trash"
                    scope.launch {
                        if (mail.delete(target, isForever, context.getString(if (isForever) R.string.mail_deleted_for_good else R.string.mail_moved_to_trash))) onBack()
                    }
                }) { Icon(if (currentFolder?.kind == "trash") Icons.Outlined.DeleteForever else Icons.Outlined.Delete, deleteLabel, tint = MaterialTheme.colorScheme.error) }
                Spacer(Modifier.weight(1f))
                if (newest != null && !newest.draft) {
                    Box {
                        Button(onClick = { onCompose(ComposeKind.Reply, newest.id) }) {
                            Icon(Icons.AutoMirrored.Outlined.Reply, null, Modifier.size(18.dp))
                            Spacer(Modifier.width(8.dp))
                            Text(stringResource(R.string.mail_reply))
                        }
                    }
                    Box {
                        IconButton(onClick = { isReplyMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.mail_more_ways_to_answer)) }
                        DropdownMenu(expanded = isReplyMenuOpen, onDismissRequest = { isReplyMenuOpen = false }) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.mail_reply)) }, leadingIcon = { Icon(Icons.AutoMirrored.Outlined.Reply, null) }, onClick = { isReplyMenuOpen = false; onCompose(ComposeKind.Reply, newest.id) })
                            DropdownMenuItem(text = { Text(stringResource(R.string.mail_reply_all)) }, leadingIcon = { Icon(Icons.AutoMirrored.Outlined.ReplyAll, null) }, onClick = { isReplyMenuOpen = false; onCompose(ComposeKind.ReplyAll, newest.id) })
                            DropdownMenuItem(text = { Text(stringResource(R.string.mail_forward)) }, leadingIcon = { Icon(Icons.AutoMirrored.Outlined.Forward, null) }, onClick = { isReplyMenuOpen = false; onCompose(ComposeKind.Forward, newest.id) })
                        }
                    }
                }
            }
        },
    ) { contentPadding ->
        val shownThread = thread
        when {
            shownThread != null -> Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                SelectionContainer {
                    Text(
                        shownThread.subject.ifEmpty { stringResource(R.string.mail_no_subject) },
                        style = MaterialTheme.typography.titleLarge.copy(fontWeight = FontWeight.SemiBold),
                    )
                }
                if (shownThread.entries.size > 1) {
                    Text(
                        if (shownThread.truncated) stringResource(R.string.mail_only_newest_shown, shownThread.entries.size) else pluralStringResource(R.plurals.mail_messages_count, shownThread.entries.size, shownThread.entries.size),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                val held = shownThread.heldReply
                if (held != null && held.status == "held") HeldReplyBanner(mail, held, onEdit = { onCompose(ComposeKind.Draft, held.draftItemId) }, onCancelled = { scope.launch { load() } })
                val summary = shownThread.summary
                if (summary != null && (summary.summary.isNotEmpty() || summary.pending)) SummaryStrip(summary)
                for (entry in shownThread.entries) {
                    val item = entry.item ?: continue
                    MessageCard(
                        mail, entry, item, isOpen = expanded.contains(item.id), isOtherFolder = folderId != null && entry.folderId != folderId,
                        onToggle = {
                            if (item.draft) {
                                onCompose(ComposeKind.Draft, item.id)
                            } else if (expanded.contains(item.id)) {
                                expanded.remove(item.id)
                            } else {
                                expanded.add(item.id)
                                if (!item.seen) scope.launch { runCatching { mail.connection.setMailFlags(listOf(item.id), isSeen = true) } }
                            }
                        },
                    )
                }
            }
            failure != null -> ErrorState((failure?.message ?: "").asReasonSentence(), onRetry = { failure = null; scope.launch { load() } })
            else -> LoadingState(Modifier.padding(contentPadding))
        }
    }

    if (isSorting) {
        SortSheet(mail, targetItems.size, onDismiss = { isSorting = false }, onSort = { category, priority, needsReply ->
            isSorting = false
            val ids = targetItems.map { item -> item.id }
            scope.launch {
                mail.act(context.getString(R.string.mail_sorted)) { mail.connection.sortMail(ids, category, priority, needsReply) }
                load()
            }
        })
    }
}

/** A reply the agent is holding back to send, with the ways to read it, change it, or stop it. */
@Composable
fun HeldReplyBanner(mail: MailModel, reply: MailThreadView.HeldReply, onEdit: () -> Unit, onCancelled: () -> Unit) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    var isShowingText by remember { mutableStateOf(false) }
    Surface(shape = RoundedCornerShape(12.dp), color = MaterialTheme.colorScheme.surfaceVariant, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.Top) {
                Icon(Icons.Outlined.Schedule, null, Modifier.padding(end = 12.dp))
                val sendAfter = ServerTime.instant(reply.sendAfter)
                Text(
                    if (sendAfter != null) stringResource(R.string.mail_agent_will_reply_at, reply.to, MailTimes.timeOfDay(sendAfter)) else stringResource(R.string.mail_agent_will_reply, reply.to),
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            if (isShowingText) SelectionContainer { Text(reply.text, style = MaterialTheme.typography.bodySmall) }
            OutlinedButton(onClick = { isShowingText = !isShowingText }, modifier = Modifier.fillMaxWidth()) {
                Icon(if (isShowingText) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility, null, Modifier.size(18.dp))
                Spacer(Modifier.width(8.dp))
                Text(stringResource(if (isShowingText) R.string.mail_hide_reply else R.string.mail_show_reply))
            }
            if (reply.draftItemId.isNotEmpty()) {
                OutlinedButton(onClick = onEdit, modifier = Modifier.fillMaxWidth()) {
                    Icon(Icons.Outlined.Notes, null, Modifier.size(18.dp))
                    Spacer(Modifier.width(8.dp))
                    Text(stringResource(R.string.mail_edit_reply))
                }
            }
            OutlinedButton(onClick = {
                scope.launch {
                    mail.act(context.getString(R.string.mail_agent_reply_cancelled)) { mail.connection.cancelAgentReply(reply.id) }
                    onCancelled()
                }
            }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.mail_cancel_reply), color = MaterialTheme.colorScheme.error) }
        }
    }
}

@Composable
fun SummaryStrip(summary: MailThreadView.Summary) {
    Surface(shape = RoundedCornerShape(12.dp), color = MaterialTheme.colorScheme.surfaceVariant, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.mail_summary), style = MaterialTheme.typography.titleSmall)
                if (summary.pending) {
                    Text(stringResource(if (summary.summary.isEmpty()) R.string.mail_summary_writing else R.string.mail_summary_catching_up), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                } else if (summary.stale) {
                    Text(stringResource(R.string.mail_summary_stale), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                }
            }
            if (summary.summary.isNotEmpty()) SelectionContainer { Text(summary.summary, style = MaterialTheme.typography.bodyMedium) }
        }
    }
}

/** One message of the conversation: its sender and time folded to a line, or opened to read with its actions, attachments and body. */
@Composable
fun MessageCard(mail: MailModel, entry: MailThreadView.Entry, item: MailItem, isOpen: Boolean, isOtherFolder: Boolean, onToggle: () -> Unit) {
    val message = item.mail
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var content by remember { mutableStateOf<MailContent?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var isShowingDetails by remember { mutableStateOf(false) }
    var isShowingHeaders by remember { mutableStateOf(false) }
    var isLoadingPictures by remember { mutableStateOf(false) }
    var isOriginalLook by remember { mutableStateOf(false) }
    var isMenuOpen by remember { mutableStateOf(false) }

    LaunchedEffect(isOpen) {
        val mailId = message?.id ?: return@LaunchedEffect
        if (!isOpen || content != null) return@LaunchedEffect
        try {
            content = mail.connection.mailContent(mailId)
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failure = failed
        }
    }

    Surface(shape = RoundedCornerShape(14.dp), color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f), modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(Modifier.fillMaxWidth().clickable(onClick = onToggle), verticalAlignment = Alignment.Top) {
                SenderLogo(message?.logoDomain.orEmpty(), message?.shownSender.orEmpty())
                Spacer(Modifier.width(10.dp))
                Column(Modifier.weight(1f)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            message?.shownSender?.ifEmpty { null } ?: stringResource(R.string.mail_unknown_sender),
                            style = MaterialTheme.typography.titleSmall.copy(fontWeight = if (item.seen) FontWeight.Normal else FontWeight.SemiBold), maxLines = 1, overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        if (item.draft) Text(" " + stringResource(R.string.mail_draft_marker), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.labelMedium)
                        VerdictShield(message?.authenticationResults)
                        val shortTime = MailTimes.short(MailTimes.moment(message?.receivedAt ?: item.addedAt))
                        Text(
                            when (shortTime) {
                                ShortTime.None -> ""
                                ShortTime.Yesterday -> stringResource(R.string.mail_period_yesterday)
                                is ShortTime.Text -> shortTime.text
                            },
                            style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 6.dp),
                        )
                        if (isOpen) {
                    Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.mail_message_actions)) }
                    DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                        DropdownMenuItem(text = { Text(stringResource(if (isShowingDetails) R.string.mail_hide_details else R.string.mail_show_details)) }, leadingIcon = { Icon(Icons.Outlined.Info, null) }, onClick = { isMenuOpen = false; isShowingDetails = !isShowingDetails })
                        DropdownMenuItem(
                            text = { Text(stringResource(if (isShowingHeaders) R.string.mail_hide_headers else R.string.mail_show_headers)) }, leadingIcon = { Icon(Icons.Outlined.ViewList, null) },
                            enabled = !content?.rawHeaders.isNullOrEmpty(), onClick = { isMenuOpen = false; isShowingHeaders = !isShowingHeaders },
                        )
                        if (message != null) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.mail_save_sender)) }, leadingIcon = { Icon(Icons.Outlined.PersonAdd, null) }, onClick = {
                                isMenuOpen = false
                                scope.launch { saveSender(mail, message) }
                            })
                            DropdownMenuItem(text = { Text(stringResource(R.string.mail_read_as_receipt)) }, leadingIcon = { Icon(Icons.Outlined.Receipt, null) }, onClick = {
                                isMenuOpen = false
                                scope.launch { mail.act(context.getString(R.string.mail_reading_receipt)) { mail.connection.readReceipt(item.id) } }
                            })
                            DropdownMenuItem(text = { Text(stringResource(R.string.mail_read_as_tax_document)) }, leadingIcon = { Icon(Icons.Outlined.Description, null) }, onClick = {
                                isMenuOpen = false
                                scope.launch { mail.act(context.getString(R.string.mail_reading_tax_document)) { mail.connection.readTaxDocument(item.id) } }
                            })
                            DropdownMenuItem(text = { Text(stringResource(R.string.mail_download_original)) }, leadingIcon = { Icon(Icons.Outlined.DownloadForOffline, null) }, onClick = {
                                isMenuOpen = false
                                scope.launch { runCatching { openServerFile(context, mail.connection, "/api/v1/mail/${message.id}/raw", "${message.id}.eml") }.onFailure { failed -> mail.snackbar?.showError(failed) } }
                            })
                        }
                        DropdownMenuItem(text = { Text(stringResource(if (isOriginalLook) R.string.mail_show_dark else R.string.mail_show_original_look)) }, leadingIcon = { Icon(Icons.Outlined.InvertColors, null) }, onClick = { isMenuOpen = false; isOriginalLook = !isOriginalLook })
                    }
                }
                        }
                    }
                    if (isOtherFolder) Text(entry.folderName, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    if (!isOpen) {
                        Text(
                            item.insight?.summary?.ifEmpty { null } ?: message?.subject.orEmpty(), style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis,
                        )
                    } else if (message != null && message.recipients.isNotEmpty()) {
                        Text(
                            stringResource(R.string.mail_to_recipients, message.recipients.joinToString(", ")), style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = if (isShowingDetails) Int.MAX_VALUE else 1, overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
            }
            if (isOpen) {
                if (isShowingDetails && message != null) MessageDetails(message, item.insight)
                val rawHeaders = content?.rawHeaders
                if (isShowingHeaders && !rawHeaders.isNullOrEmpty()) {
                    Box(Modifier.fillMaxWidth().padding(vertical = 4.dp).horizontalScroll(rememberScrollState())) {
                        SelectionContainer { Text(rawHeaders, style = MaterialTheme.typography.bodySmall.copy(fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace)) }
                    }
                }
                InvitationCard(mail, item.id)
                val offers = (item.insight?.proposals ?: emptyList()).withIndex().filter { offer -> offer.value.status.isEmpty() && entry.folderKind != "junk" && entry.folderKind != "trash" }
                for (offer in offers) ProposalCard(mail, item.id, offer.index, offer.value)
                val shownContent = content
                if (shownContent != null) {
                    if (!shownContent.available) Text(stringResource(R.string.mail_no_longer_stored), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    val files = shownContent.presentAttachments.filter { file -> !file.inline || !shownContent.html.contains("/attachment/${file.index}") }
                    for (file in files) {
                        TextButton(onClick = {
                            scope.launch {
                                runCatching { openServerFile(context, mail.connection, "/api/v1/mail/${shownContent.mailId}/attachment/${file.index}", file.filename.ifEmpty { "attachment" }) }
                                    .onFailure { failed -> mail.snackbar?.showError(failed) }
                            }
                        }) {
                            Icon(Icons.Outlined.AttachFile, null, Modifier.size(18.dp))
                            Spacer(Modifier.width(8.dp))
                            Text(file.filename.ifEmpty { stringResource(R.string.mail_attachment) } + " · " + fileSize(file.size.toLong()), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                    if (shownContent.hasRemoteContent == true && shownContent.imagesAllowed != true && !isLoadingPictures) {
                        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            Text(stringResource(R.string.mail_pictures_held_back), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            OutlinedButton(onClick = {
                                isLoadingPictures = true
                                scope.launch { runCatching { mail.connection.showMailImages(listOf(item.id)) } }
                            }) {
                                Icon(Icons.Outlined.Image, null, Modifier.size(18.dp))
                                Spacer(Modifier.width(8.dp))
                                Text(stringResource(R.string.mail_load_pictures))
                            }
                        }
                    }
                    if (shownContent.html.isNotEmpty()) {
                        MailBody(shownContent.html, shownContent.mailId, mail.connection, isLoadingPictures || shownContent.imagesAllowed == true, isOriginalLook)
                    } else {
                        SelectionContainer { Text(shownContent.text, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.fillMaxWidth()) }
                    }
                } else if (failure != null) {
                    Text((failure?.message ?: "").asReasonSentence(), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                } else {
                    Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { androidx.compose.material3.CircularProgressIndicator(Modifier.size(24.dp)) }
                }
            }
        }
    }
}

/** A size in bytes as a person reads it: 1.2 MB. */
fun fileSize(byteCount: Long): String {
    if (byteCount < 1000) return "$byteCount B"
    val units = listOf("KB", "MB", "GB", "TB")
    var size = byteCount.toDouble()
    var unit = -1
    while (size >= 1000 && unit < units.lastIndex) {
        size /= 1000
        unit += 1
    }
    return DecimalFormat(if (size >= 100) "0" else "0.#").format(size) + " " + units[unit]
}

/** Saves the sender of a message as a contact, unless the address book has them. */
private suspend fun saveSender(mail: MailModel, message: MailMessage) {
    val address = MailWords.bareAddress(message.from.ifEmpty { message.sender })
    if (address.isEmpty()) return
    val context = mail.context
    try {
        val book = mail.connection.addressBooks().firstOrNull() ?: return
        val known = mail.connection.contacts(book.id, address, 20)
        if (known.any { contact -> (contact.emails ?: emptyList()).any { email -> email.lowercase() == address.lowercase() } }) {
            mail.snackbar?.show(context.getString(R.string.mail_already_in_contacts))
            return
        }
        val name = message.fromName.ifEmpty { address }
        mail.act(context.getString(R.string.mail_saved_to_contacts)) {
            mail.connection.saveContact(book.id, null, name, "", listOf(address), emptyList(), "", emptyList())
        }
    } catch (failed: kotlinx.coroutines.CancellationException) {
        throw failed
    } catch (failed: Throwable) {
        mail.snackbar?.showError(failed)
    }
}

/** What the agent offers to add from a message: an event for the calendar, or a person for the contacts. */
@Composable
fun ProposalCard(mail: MailModel, itemId: String, index: Int, proposal: MailProposal) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    var isDone by remember { mutableStateOf(false) }
    if (isDone) return
    val isEvent = proposal.kind == "event"

    suspend fun settle(status: String, said: String) {
        val isSettled = mail.act(said) { mail.connection.setProposalStatus(itemId, index, status) }
        if (isSettled) isDone = true
    }

    Surface(shape = RoundedCornerShape(12.dp), color = MaterialTheme.colorScheme.surface, tonalElevation = 2.dp, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(if (isEvent) Icons.Outlined.CalendarMonth else Icons.Outlined.PersonAdd, null, Modifier.padding(end = 8.dp))
                Text(stringResource(if (isEvent) R.string.mail_add_to_calendar_question else R.string.mail_save_to_contacts_question), style = MaterialTheme.typography.titleSmall)
            }
            if (isEvent) {
                Text(proposal.summary, style = MaterialTheme.typography.titleMedium)
                Text(proposalWhen(proposal), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (proposal.location.isNotEmpty()) Text(proposal.location, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            } else {
                Text(proposal.name, style = MaterialTheme.typography.titleMedium)
                Text((listOf(proposal.organization) + proposal.emails + proposal.phones).filter { detail -> detail.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (proposal.because.isNotEmpty()) Text("“${proposal.because}”", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = {
                    scope.launch {
                        try {
                            if (isEvent) {
                                mail.connection.addProposedEvent(proposal)
                            } else {
                                val book = mail.connection.addressBooks().firstOrNull() ?: return@launch
                                mail.connection.saveContact(book.id, null, proposal.name, proposal.organization, proposal.emails, proposal.phones, proposal.note, emptyList())
                            }
                            settle("accepted", context.getString(if (isEvent) R.string.mail_added_to_calendar else R.string.mail_saved_to_contacts))
                        } catch (failed: kotlinx.coroutines.CancellationException) {
                            throw failed
                        } catch (failed: Throwable) {
                            mail.snackbar?.showError(failed)
                        }
                    }
                }, modifier = Modifier.weight(1f)) { Text(stringResource(if (isEvent) R.string.mail_add else R.string.mail_save)) }
                OutlinedButton(onClick = { scope.launch { settle("dismissed", context.getString(R.string.mail_dismissed)) } }, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.mail_dismiss)) }
            }
        }
    }
}

private fun proposalWhen(proposal: MailProposal): String {
    val start = ServerTime.instant(proposal.starts) ?: return proposal.starts
    if (proposal.allDay) return MailTimes.wholeDate(start, java.time.ZoneOffset.UTC)
    val end = ServerTime.instant(proposal.ends)
    return if (end != null) "${MailTimes.dateAndTime(start)} – ${MailTimes.timeOfDay(end)}" else MailTimes.dateAndTime(start)
}

/** The facts of a message: who, to whom, when, the checks, and what the agent made of it. */
@Composable
fun MessageDetails(message: MailMessage, insight: MailInsight?) {
    SelectionContainer {
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            DetailLine(stringResource(R.string.mail_from), if (message.fromName.isEmpty()) message.from else "${message.fromName} <${message.from}>")
            DetailLine(stringResource(R.string.mail_to), message.recipients.joinToString(", "))
            ServerTime.instant(message.receivedAt)?.let { received -> DetailLine(stringResource(R.string.mail_received), MailTimes.dateAndTime(received)) }
            val checks = MailVerdict.detail(message.authenticationResults)
            if (checks.isNotEmpty()) DetailLine(stringResource(R.string.mail_checks), checks)
            if (insight != null) {
                DetailLine(stringResource(R.string.mail_category), insight.category)
                DetailLine(stringResource(R.string.mail_priority_label), insight.priority)
                DetailLine(stringResource(R.string.mail_needs_reply), stringResource(if (insight.needsReply) R.string.mail_yes else R.string.mail_no))
                if (insight.summary.isNotEmpty()) Text(insight.summary, style = MaterialTheme.typography.bodySmall)
                for (action in insight.actionItems) Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Outlined.CheckCircle, null, Modifier.size(16.dp).padding(end = 4.dp))
                    Text(action, style = MaterialTheme.typography.bodySmall)
                }
                if (!insight.notes.isNullOrEmpty()) Text(insight.notes.orEmpty(), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun DetailLine(label: String, value: String) {
    Row {
        Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(88.dp))
        Text(value, style = MaterialTheme.typography.bodySmall)
    }
}

/** The calendar invitation a message carries, with the way to answer it. */
@Composable
fun InvitationCard(mail: MailModel, itemId: String) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    var invitation by remember { mutableStateOf<MailInvitation?>(null) }
    var isAnswering by remember { mutableStateOf(false) }
    LaunchedEffect(itemId) { invitation = runCatching { mail.connection.mailInvitation(itemId) }.getOrNull() }
    val shown = invitation ?: return

    fun send(answer: String) {
        val preferences = context.getSharedPreferences("mail", android.content.Context.MODE_PRIVATE)
        val key = "invitation-answer.$itemId"
        val kept = preferences.getString(key, null)
        val requestId = kept ?: java.util.UUID.randomUUID().toString().replace("-", "")
        preferences.edit().putString(key, requestId).apply()
        isAnswering = true
        scope.launch {
            val isAnswered = mail.act(context.getString(R.string.mail_answer_sent)) { mail.connection.answerInvitation(itemId, answer, requestId, isRetry = kept != null) }
            isAnswering = false
            if (isAnswered) {
                preferences.edit().remove(key).apply()
                invitation = runCatching { mail.connection.mailInvitation(itemId) }.getOrNull()
            }
        }
    }

    if (shown.method != "REQUEST") {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.CalendarMonth, null, Modifier.padding(end = 8.dp))
            Text(stringResource(if (shown.cancelled) R.string.mail_invitation_cancelled else R.string.mail_invitation_answered, shown.summary), style = MaterialTheme.typography.bodySmall)
        }
        return
    }
    Surface(shape = RoundedCornerShape(12.dp), color = MaterialTheme.colorScheme.surface, tonalElevation = 2.dp, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.CalendarMonth, null, Modifier.padding(end = 8.dp))
                Text(shown.summary, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f, fill = false))
                if (shown.cancelled) Text("  " + stringResource(R.string.mail_cancelled), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.labelMedium)
            }
            Text(invitationWhen(shown), style = MaterialTheme.typography.bodyMedium)
            if (shown.location.isNotEmpty()) Row(verticalAlignment = Alignment.CenterVertically) { Icon(Icons.Outlined.Place, null, Modifier.size(16.dp).padding(end = 4.dp)); Text(shown.location, style = MaterialTheme.typography.bodySmall) }
            if (shown.organizer.isNotEmpty()) Text(stringResource(R.string.mail_organized_by, shown.organizer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (!shown.cancelled) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    for ((answer, title) in listOf("ACCEPTED" to R.string.mail_accept, "TENTATIVE" to R.string.mail_maybe, "DECLINED" to R.string.mail_decline)) {
                        if (shown.participation.uppercase() == answer) {
                            Button(onClick = { send(answer) }, enabled = !isAnswering) { Text(stringResource(title)) }
                        } else {
                            OutlinedButton(onClick = { send(answer) }, enabled = !isAnswering) { Text(stringResource(title)) }
                        }
                    }
                }
            }
        }
    }
}

private fun invitationWhen(invitation: MailInvitation): String {
    val start = ServerTime.instant(invitation.startsAt) ?: return ""
    if (invitation.allDay) return MailTimes.wholeDate(start, java.time.ZoneOffset.UTC)
    val end = ServerTime.instant(invitation.endsAt) ?: start
    return "${MailTimes.dateAndTime(start)} – ${MailTimes.timeOfDay(end)}"
}

/** Whether a message is from who it says: a green shield when checked, orange when it looks like spam, red when it failed. */
@Composable
fun VerdictShield(results: MailMessage.Verdicts?) {
    val detail = MailVerdict.detail(results)
    when (MailVerdict.of(results)) {
        MailVerdict.Good -> Icon(Icons.Outlined.GppGood, stringResource(R.string.mail_sender_checked, detail), Modifier.size(16.dp), tint = androidx.compose.ui.graphics.Color(0xFF2E7D32))
        MailVerdict.Warn -> Icon(Icons.Outlined.GppMaybe, stringResource(R.string.mail_looks_like_spam_detail, detail), Modifier.size(16.dp), tint = androidx.compose.ui.graphics.Color(0xFFE08A00))
        MailVerdict.Bad -> Icon(Icons.Outlined.GppBad, stringResource(R.string.mail_sender_not_verified, detail), Modifier.size(16.dp), tint = MaterialTheme.colorScheme.error)
        MailVerdict.Unknown -> Unit
    }
}
