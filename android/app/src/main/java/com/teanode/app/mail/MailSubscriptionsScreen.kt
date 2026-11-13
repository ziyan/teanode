package com.teanode.app.mail

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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.MarkEmailRead
import androidx.compose.material.icons.outlined.MarkEmailUnread
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Newspaper
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material.icons.outlined.Report
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.ThumbUp
import androidx.compose.material3.BottomAppBar
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
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
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.openWebPage
import com.teanode.kit.mail.MailFolders
import com.teanode.kit.mail.MailSubscription
import com.teanode.kit.mail.MailSubscriptionPage
import com.teanode.kit.mail.MailThreadView
import com.teanode.kit.mail.ServerTime
import com.teanode.kit.mail.mailSubscriptions
import com.teanode.kit.mail.muteSubscription
import com.teanode.kit.mail.readMailSubscription
import com.teanode.kit.mail.reportJunk
import com.teanode.kit.mail.setMailFlags
import com.teanode.kit.mail.showSubscriptionImages
import com.teanode.kit.mail.unsubscribe
import kotlinx.coroutines.launch

/** The mailing lists that write to the mailbox, those still subscribed to or those left, searched by name. */
@Composable
fun SubscriptionsScreen(mail: MailModel, onBack: () -> Unit, onOpen: (MailSubscription) -> Unit) {
    var subscriptions by remember { mutableStateOf(emptyList<MailSubscription>()) }
    var page by remember { mutableStateOf<MailSubscriptionPage?>(null) }
    var isLeft by remember { mutableStateOf(false) }
    var searchText by remember { mutableStateOf("") }
    var isLoaded by remember { mutableStateOf(false) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    var requestCount by remember { mutableStateOf(0) }

    suspend fun load(isMore: Boolean = false) {
        val mailboxId = mail.mailbox?.id ?: return
        try {
            val read = mail.connection.mailSubscriptions(mailboxId, isLeft, searchText.trim(), 50, if (isMore) subscriptions.size else 0)
            val fresh = read.subscriptions.filterNotNull()
            subscriptions = if (isMore) subscriptions + fresh.filter { row -> subscriptions.none { existing -> existing.id == row.id } } else fresh
            page = read
            isLoaded = true
            failure = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            if (isLoaded) mail.snackbar?.showError(failed) else failure = failed
        }
    }

    LaunchedEffect(mail.isLoaded, mail.mailbox?.id) { if (!mail.isLoaded) mail.load() }
    LaunchedEffect(isLeft, mail.mailbox?.id, requestCount) { if (mail.mailbox != null) load() }
    val listState = rememberLazyListState()
    val lastVisible = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0
    LaunchedEffect(lastVisible, subscriptions.size) {
        if (subscriptions.isNotEmpty() && lastVisible >= subscriptions.size - 3 && subscriptions.size < (page?.total ?: 0)) load(isMore = true)
    }

    TeaNodeScreen(title = stringResource(R.string.mail_subscriptions), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize()) {
            OutlinedTextField(
                searchText, { typed ->
                    searchText = typed
                    if (typed.isEmpty()) requestCount += 1
                },
                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true, leadingIcon = { Icon(Icons.Outlined.Search, null) },
                placeholder = { Text(stringResource(R.string.mail_search_lists)) }, shape = RoundedCornerShape(28.dp),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search), keyboardActions = KeyboardActions(onSearch = { requestCount += 1 }),
            )
            val shownPage = page
            if (shownPage != null && (shownPage.left > 0 || searchText.isNotEmpty())) {
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
                    SegmentedButton(!isLeft, { isLeft = false }, SegmentedButtonDefaults.itemShape(0, 2)) { Text(stringResource(R.string.mail_subscribed_count, shownPage.subscribed)) }
                    SegmentedButton(isLeft, { isLeft = true }, SegmentedButtonDefaults.itemShape(1, 2)) { Text(stringResource(R.string.mail_unsubscribed_count, shownPage.left)) }
                }
            }
            when {
                failure != null && !isLoaded -> ErrorState(failure?.message.orEmpty(), onRetry = { scope.launch { load() } })
                !isLoaded -> LoadingState()
                subscriptions.isEmpty() -> when {
                    searchText.isNotEmpty() -> EmptyState(Icons.Outlined.Search, stringResource(R.string.mail_nothing_matches))
                    isLeft -> EmptyState(Icons.Outlined.Newspaper, stringResource(R.string.mail_not_left_any_lists))
                    else -> EmptyState(Icons.Outlined.Newspaper, stringResource(R.string.mail_no_lists_yet))
                }
                else -> LazyColumn(state = listState, modifier = Modifier.fillMaxSize()) {
                    items(subscriptions, key = { subscription -> subscription.id }) { subscription ->
                        SubscriptionRow(subscription, Modifier.clickable { onOpen(subscription) })
                    }
                }
            }
        }
    }
}

@Composable
fun SubscriptionRow(subscription: MailSubscription, modifier: Modifier = Modifier) {
    val displayName = subscription.name.ifEmpty { subscription.from }
    Row(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.Top) {
        SenderLogo(subscription.logoDomain, displayName, 32)
        Spacer(Modifier.width(10.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(displayName, style = MaterialTheme.typography.titleSmall.copy(fontWeight = if (subscription.unread > 0) FontWeight.SemiBold else FontWeight.Normal), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                val shortTime = MailTimes.short(MailTimes.moment(subscription.lastAt))
                Text(
                    when (shortTime) {
                        ShortTime.None -> ""
                        ShortTime.Yesterday -> stringResource(R.string.mail_period_yesterday)
                        is ShortTime.Text -> shortTime.text
                    },
                    style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            val counts = buildList {
                add(pluralStringResource(R.plurals.mail_messages_count, subscription.count, subscription.count))
                if (subscription.unread > 0) add(stringResource(R.string.mail_unread_count, subscription.unread))
                if (subscription.mutedAt != null) add(stringResource(R.string.mail_muted))
            }.joinToString(" · ")
            Text(counts, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            val requested = ServerTime.instant(subscription.requestedAt)
            if (requested != null) {
                val whenText = android.text.format.DateUtils.getRelativeTimeSpanString(requested.toEpochMilli(), System.currentTimeMillis(), android.text.format.DateUtils.MINUTE_IN_MILLIS).toString()
                if (subscription.failed) {
                    Text(stringResource(R.string.mail_unsubscribing_failed, whenText, subscription.error), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                } else {
                    Text(
                        when (subscription.method) {
                            "mail" -> stringResource(R.string.mail_unsubscribe_sent_by_mail, whenText)
                            "link" -> stringResource(R.string.mail_unsubscribed_on_senders_page, whenText)
                            else -> stringResource(R.string.mail_unsubscribed_when, whenText)
                        },
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

/** One list's messages from every folder, with what can be done about the list: mute it, leave it, sort what it sent. */
@Composable
fun SubscriptionReaderScreen(mail: MailModel, key: String, onBack: () -> Unit, onChanged: () -> Unit) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    var subscription by remember { mutableStateOf<MailSubscription?>(null) }
    var thread by remember { mutableStateOf<MailThreadView?>(null) }
    val expanded = remember { mutableStateListOf<String>() }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var isConfirmingLeave by remember { mutableStateOf(false) }
    var isConfirmingDelete by remember { mutableStateOf(false) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var isMoveOpen by remember { mutableStateOf(false) }

    suspend fun readSubscription() {
        val mailboxId = mail.mailbox?.id ?: return
        val found = runCatching { mail.connection.mailSubscriptions(mailboxId, false, "", 200, 0) }.getOrNull()?.subscriptions?.filterNotNull()?.firstOrNull { row -> row.key == key }
            ?: runCatching { mail.connection.mailSubscriptions(mailboxId, true, "", 200, 0) }.getOrNull()?.subscriptions?.filterNotNull()?.firstOrNull { row -> row.key == key }
        if (found != null) subscription = found
    }

    suspend fun load() {
        val mailboxId = mail.mailbox?.id ?: return
        try {
            val read = mail.connection.readMailSubscription(mailboxId, key)
            val isFirst = thread == null
            thread = read
            if (isFirst) {
                val all = read.entries.mapNotNull { entry -> entry.item }
                val open = all.filter { item -> !item.seen }.map { item -> item.id }.toMutableSet()
                all.firstOrNull()?.let { newest -> open.add(newest.id) }
                expanded.clear()
                expanded.addAll(open)
                val unread = all.filter { item -> !item.seen && open.contains(item.id) }.map { item -> item.id }
                if (unread.isNotEmpty()) {
                    runCatching { mail.connection.setMailFlags(unread, isSeen = true) }
                    onChanged()
                }
            }
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failure = failed
        }
    }

    suspend fun changed() {
        load()
        readSubscription()
        onChanged()
    }

    LaunchedEffect(key, mail.isLoaded) {
        if (!mail.isLoaded) mail.load()
        readSubscription()
        load()
    }

    val items = (thread?.entries ?: emptyList()).mapNotNull { entry -> entry.item }.filter { item -> !item.draft }
    val isAnyUnread = items.any { item -> !item.seen }
    val shown = subscription
    val displayName = shown?.name?.ifEmpty { shown.from } ?: ""

    TeaNodeScreen(
        title = displayName,
        onBack = onBack,
        actions = {
            Box {
                IconButton(onClick = { isMenuOpen = true }, enabled = items.isNotEmpty()) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.mail_actions)) }
                DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text(stringResource(if (isAnyUnread) R.string.mail_mark_all_read else R.string.mail_mark_all_unread)) },
                        leadingIcon = { Icon(if (isAnyUnread) Icons.Outlined.MarkEmailRead else Icons.Outlined.MarkEmailUnread, null) },
                        onClick = {
                            isMenuOpen = false
                            val ids = items.map { item -> item.id }
                            scope.launch {
                                mail.act(context.getString(if (isAnyUnread) R.string.mail_marked_read else R.string.mail_marked_unread)) { mail.connection.setMailFlags(ids, isSeen = isAnyUnread) }
                                changed()
                            }
                        },
                    )
                    if (shown != null) {
                        DropdownMenuItem(
                            text = { Text(stringResource(if (shown.imagesAt == null) R.string.mail_always_show_pictures else R.string.mail_ask_before_pictures)) },
                            leadingIcon = { Icon(Icons.Outlined.Image, null) },
                            onClick = {
                                isMenuOpen = false
                                val isShown = shown.imagesAt == null
                                val mailboxId = mail.mailbox?.id ?: return@DropdownMenuItem
                                scope.launch {
                                    mail.act(context.getString(if (isShown) R.string.mail_pictures_will_be_shown else R.string.mail_pictures_will_be_asked)) { mail.connection.showSubscriptionImages(mailboxId, key, isShown) }
                                    changed()
                                }
                            },
                        )
                        DropdownMenuItem(
                            text = { Text(stringResource(if (shown.mutedAt == null) R.string.mail_mute else R.string.mail_unmute)) },
                            leadingIcon = { Icon(if (shown.mutedAt == null) Icons.Outlined.NotificationsOff else Icons.Outlined.NotificationsActive, null) },
                            onClick = {
                                isMenuOpen = false
                                val isMuted = shown.mutedAt == null
                                val mailboxId = mail.mailbox?.id ?: return@DropdownMenuItem
                                scope.launch {
                                    mail.act(context.getString(if (isMuted) R.string.mail_kept_out_of_inbox else R.string.mail_back_in_inbox)) { mail.connection.muteSubscription(mailboxId, key, isMuted) }
                                    changed()
                                }
                            },
                        )
                    }
                    val isAllJunk = items.all { item -> mail.folderById(item.folderId)?.kind == "junk" }
                    DropdownMenuItem(
                        text = { Text(stringResource(if (isAllJunk) R.string.mail_not_junk else R.string.mail_report_junk)) },
                        leadingIcon = { Icon(if (isAllJunk) Icons.Outlined.ThumbUp else Icons.Outlined.Report, null) },
                        onClick = {
                            isMenuOpen = false
                            val ids = items.map { item -> item.id }
                            scope.launch {
                                mail.act(context.getString(if (isAllJunk) R.string.mail_said_not_junk else R.string.mail_reported_junk)) { mail.connection.reportJunk(ids, isAllJunk) }
                                changed()
                            }
                        },
                    )
                    DropdownMenuItem(text = { Text(stringResource(R.string.mail_move_to)) }, leadingIcon = { Icon(Icons.Outlined.Folder, null) }, onClick = { isMoveOpen = !isMoveOpen })
                    if (isMoveOpen) {
                        for (row in MailFolders.rows(mail.folders)) {
                            val name = MailNames.folder(row.folder)
                            DropdownMenuItem(text = { Text(name, Modifier.padding(start = (row.depth.coerceAtMost(3) * 12).dp)) }, leadingIcon = { Icon(MailNames.icon(row.folder), null) }, onClick = {
                                isMenuOpen = false
                                scope.launch {
                                    mail.move(MailTarget.of(items), row.folder.id, context.getString(R.string.mail_moved_to, name))
                                    changed()
                                }
                            })
                        }
                    }
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.mail_delete_ellipsis), color = MaterialTheme.colorScheme.error) },
                        leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) },
                        onClick = { isMenuOpen = false; isConfirmingDelete = true },
                    )
                }
            }
        },
        bottomBar = {
            BottomAppBar {
                val archive = mail.folderOfKind("archive")
                if (archive != null) {
                    TextButton(onClick = {
                        scope.launch {
                            mail.move(MailTarget.of(items.filter { item -> item.folderId != archive.id }), archive.id, context.getString(R.string.mail_archived))
                            changed()
                        }
                    }, enabled = items.isNotEmpty()) {
                        Icon(Icons.Outlined.Archive, null, Modifier.size(18.dp))
                        Spacer(Modifier.width(8.dp))
                        Text(stringResource(R.string.mail_archive_all))
                    }
                }
                Spacer(Modifier.weight(1f))
                TextButton(onClick = { isConfirmingLeave = true }, enabled = shown != null && (shown.requestedAt == null || shown.failed)) {
                    Text(stringResource(R.string.mail_unsubscribe), color = MaterialTheme.colorScheme.error)
                }
            }
        },
    ) { contentPadding ->
        val shownThread = thread
        if (shownThread == null) {
            if (failure != null) ErrorState(failure?.message.orEmpty(), onRetry = { failure = null; scope.launch { load() } }) else LoadingState(Modifier.padding(contentPadding))
        } else {
            Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    SenderLogo(shown?.logoDomain.orEmpty(), displayName, 44)
                    Spacer(Modifier.width(10.dp))
                    Column {
                        Text(displayName, style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.SemiBold))
                        Text(shown?.from.orEmpty(), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                if (shownThread.truncated) Text(stringResource(R.string.mail_only_newest_shown, shownThread.entries.size), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                for (entry in shownThread.entries) {
                    val item = entry.item ?: continue
                    MessageCard(mail, entry, item, isOpen = expanded.contains(item.id), isOtherFolder = true, onToggle = { if (expanded.contains(item.id)) expanded.remove(item.id) else expanded.add(item.id) })
                }
            }
        }
    }

    if (isConfirmingDelete) {
        ConfirmationDialog(
            title = stringResource(R.string.mail_delete_every_message_question), confirmLabel = pluralStringResource(R.plurals.mail_delete_messages, items.size, items.size), isDestructive = true,
            onConfirm = {
                isConfirmingDelete = false
                scope.launch {
                    mail.delete(MailTarget.of(items), false, context.getString(R.string.mail_moved_to_trash))
                    changed()
                }
            },
            onDismiss = { isConfirmingDelete = false },
        )
    }
    if (isConfirmingLeave && shown != null) {
        val leaveKind = shown.leaveKind
        ConfirmationDialog(
            title = if (leaveKind == "none") stringResource(R.string.mail_list_gives_no_way_out) else stringResource(R.string.mail_leave_list_question, shown.name),
            text = when (leaveKind) {
                "oneClick" -> stringResource(R.string.mail_leave_one_click)
                "mail" -> stringResource(R.string.mail_leave_by_mail)
                "link" -> stringResource(R.string.mail_leave_by_link)
                else -> stringResource(if (shown.stripped) R.string.mail_leave_none_stripped else R.string.mail_leave_none)
            },
            confirmLabel = if (leaveKind == "none") stringResource(R.string.mail_mute_instead) else stringResource(R.string.mail_unsubscribe),
            isDestructive = leaveKind != "none",
            onConfirm = {
                isConfirmingLeave = false
                val mailboxId = mail.mailbox?.id
                if (mailboxId != null) scope.launch {
                    if (leaveKind == "none") {
                        if (shown.mutedAt == null) mail.act(context.getString(R.string.mail_kept_out_of_inbox)) { mail.connection.muteSubscription(mailboxId, key, true) }
                    } else {
                        if (leaveKind == "link") shown.leavePage?.let { page -> openWebPage(context, page) }
                        mail.act(context.getString(R.string.mail_left_list, shown.name)) { mail.connection.unsubscribe(mailboxId, key) }
                    }
                    changed()
                }
            },
            onDismiss = { isConfirmingLeave = false },
        )
    }
}
