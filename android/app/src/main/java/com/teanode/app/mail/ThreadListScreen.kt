package com.teanode.app.mail

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Reply
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Clear
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DeleteForever
import androidx.compose.material.icons.outlined.Drafts
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.GppBad
import androidx.compose.material.icons.outlined.GppMaybe
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PriorityHigh
import androidx.compose.material.icons.outlined.Report
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Star
import androidx.compose.material.icons.outlined.StarBorder
import androidx.compose.material.icons.outlined.Sell
import androidx.compose.material.icons.outlined.ThumbUp
import androidx.compose.material.icons.outlined.AlternateEmail
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.MarkEmailRead
import androidx.compose.material.icons.outlined.MarkEmailUnread
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.snapshotFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.offset
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.IntOffset
import kotlin.math.roundToInt
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.mail.MailFilter
import com.teanode.kit.mail.MailFolders
import com.teanode.kit.mail.MailThread
import com.teanode.kit.mail.MailVerdict
import com.teanode.kit.mail.emptyTrash
import com.teanode.kit.mail.setMailFlags
import com.teanode.kit.mail.sortMail
import kotlinx.coroutines.launch

/**
 * ThreadListScreen is a folder, Starred or Priority: the conversations newest first, searched and narrowed, swiped,
 * chosen several at a time.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)
@Composable
fun ThreadListScreen(
    mail: MailModel,
    place: MailPlace,
    onBack: () -> Unit,
    onOpenThread: (itemId: String, folderId: String?) -> Unit,
    onCompose: () -> Unit,
    searchedWords: String? = null,
    startingFilter: MailFilter? = null,
    startsSelecting: Boolean = false,
) {
    val list = remember(place, searchedWords) {
        ThreadListModel(place, startingFilter ?: if (searchedWords != null) MailFilter(searchText = searchedWords, isEverywhere = true) else MailFilter())
    }
    val scope = rememberCoroutineScope()
    val selection = remember { mutableStateListOf<String>() }
    var isSelecting by remember { mutableStateOf(startsSelecting) }
    var isNarrowing by remember { mutableStateOf(false) }
    var isSorting by remember { mutableStateOf(false) }
    var isConfirmingEmpty by remember { mutableStateOf(false) }
    var isFilterMenuOpen by remember { mutableStateOf(false) }
    var isActionsMenuOpen by remember { mutableStateOf(false) }
    var searchText by remember(list) { mutableStateOf(list.filter.searchText) }

    val folder = (place as? MailPlace.Folder)?.let { shown -> mail.folderById(shown.folderId) }
    LaunchedEffect(mail) { if (!mail.isLoaded) mail.load() }
    LaunchedEffect(list, mail.isLoaded) { if (mail.isLoaded) list.load(mail) }
    LaunchedEffect(startsSelecting) {
        if (startsSelecting && selection.isEmpty()) list.threads.take(2).forEach { thread -> selection.add(thread.id) }
    }

    fun reload() {
        scope.launch { list.load(mail) }
    }

    fun finishSelecting() {
        isSelecting = false
        selection.clear()
    }

    // Back leaves choosing before it leaves the list.
    androidx.activity.compose.BackHandler(enabled = isSelecting) { finishSelecting() }

    fun chosen(threads: List<MailThread>? = null): List<MailThread> = threads ?: list.threads.filter { thread -> selection.contains(thread.id) }

    val words = MailListWords(mail, list, place, folder)
    fun setSeen(threads: List<MailThread>, isSeen: Boolean) {
        val target = list.target(threads) ?: return
        list.patch(threads.map { thread -> thread.id }.toSet(), isSeen = isSeen)
        scope.launch {
            mail.act(if (isSeen) words.markedRead else words.markedUnread) { mail.connection.setMailFlags(target.itemIds, isSeen = isSeen) }
            finishSelecting()
        }
    }

    fun setFlagged(threads: List<MailThread>, isFlagged: Boolean) {
        val target = list.target(threads) ?: return
        if (place == MailPlace.Starred && !isFlagged) list.remove(threads.map { thread -> thread.id }.toSet()) else list.patch(threads.map { thread -> thread.id }.toSet(), isFlagged = isFlagged)
        scope.launch {
            mail.act(if (isFlagged) words.starred else words.unstarred) { mail.connection.setMailFlags(target.itemIds, isFlagged = isFlagged) }
            finishSelecting()
        }
    }

    fun move(threads: List<MailThread>, destination: com.teanode.kit.mail.MailFolder, said: String) {
        val target = list.target(threads) ?: return
        list.remove(threads.map { thread -> thread.id }.toSet())
        finishSelecting()
        scope.launch {
            mail.move(target, destination.id, said)
            list.load(mail)
        }
    }

    fun delete(threads: List<MailThread>) {
        val target = list.target(threads) ?: return
        list.remove(threads.map { thread -> thread.id }.toSet())
        finishSelecting()
        val isForever = folder?.kind == "trash"
        scope.launch {
            mail.delete(target, isForever, if (isForever) words.deletedForGood else words.movedToTrash)
            list.load(mail)
        }
    }

    fun junk(threads: List<MailThread>, isNotJunk: Boolean) {
        val target = list.target(threads) ?: return
        list.remove(threads.map { thread -> thread.id }.toSet())
        finishSelecting()
        scope.launch {
            mail.reportJunk(target, isNotJunk, if (isNotJunk) words.saidNotJunk else words.reportedJunk)
            list.load(mail)
        }
    }

    val title = when (place) {
        is MailPlace.Folder -> folder?.let { shown -> MailNames.folder(shown) } ?: stringResource(R.string.mail_title)
        MailPlace.Starred -> stringResource(R.string.mail_starred)
        MailPlace.Priority -> stringResource(R.string.mail_priority)
        MailPlace.Subscriptions -> stringResource(R.string.mail_subscriptions)
    }

    TeaNodeScreen(
        title = if (isSelecting) pluralStringResource(R.plurals.mail_selected_count, selection.size, selection.size) else title,
        onBack = if (isSelecting) ({ finishSelecting() }) else onBack,
        actions = {
            if (isSelecting) {
                TextButton(onClick = {
                    if (selection.size == list.threads.size) selection.clear() else {
                        selection.clear()
                        selection.addAll(list.threads.map { thread -> thread.id })
                    }
                }) { Text(if (selection.size == list.threads.size) stringResource(R.string.mail_select_none) else stringResource(R.string.mail_select_all)) }
                Box {
                    IconButton(onClick = { isActionsMenuOpen = true }, enabled = selection.isNotEmpty()) {
                        Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.mail_actions))
                    }
                    DropdownMenu(expanded = isActionsMenuOpen, onDismissRequest = { isActionsMenuOpen = false }) {
                        RowMenuItems(mail, folder, chosen(), words, onDone = { isActionsMenuOpen = false },
                            onSeen = { threads, isSeen -> setSeen(threads, isSeen) }, onFlagged = { threads, isFlagged -> setFlagged(threads, isFlagged) },
                            onSort = { isSorting = true }, onMove = { threads, destination, said -> move(threads, destination, said) },
                            onJunk = { threads, isNotJunk -> junk(threads, isNotJunk) }, onDelete = { threads -> delete(threads) })
                    }
                }
                IconButton(onClick = { delete(chosen()) }, enabled = selection.isNotEmpty()) {
                    Icon(if (folder?.kind == "trash") Icons.Outlined.DeleteForever else Icons.Outlined.Delete, contentDescription = words.deleteLabel)
                }
            } else {
                Box {
                    IconButton(onClick = { isFilterMenuOpen = true }) {
                        Icon(Icons.Outlined.FilterList, contentDescription = stringResource(R.string.mail_filter), tint = if (list.filter.isNarrowing) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface)
                    }
                    DropdownMenu(expanded = isFilterMenuOpen, onDismissRequest = { isFilterMenuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.mail_unread_only)) },
                            leadingIcon = { Checkbox(checked = list.filter.isUnreadOnly, onCheckedChange = null) },
                            onClick = { isFilterMenuOpen = false; list.filter = list.filter.copy(isUnreadOnly = !list.filter.isUnreadOnly); reload() },
                        )
                        if (place != MailPlace.Starred) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.mail_starred_only)) },
                                leadingIcon = { Checkbox(checked = list.filter.isStarredOnly, onCheckedChange = null) },
                                onClick = { isFilterMenuOpen = false; list.filter = list.filter.copy(isStarredOnly = !list.filter.isStarredOnly); reload() },
                            )
                        }
                        DropdownMenuItem(text = { Text(stringResource(R.string.mail_narrow)) }, leadingIcon = { Icon(Icons.Outlined.FilterList, null) }, onClick = { isFilterMenuOpen = false; isNarrowing = true })
                        if (list.filter.isNarrowing) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.mail_show_everything)) },
                                leadingIcon = { Icon(Icons.Outlined.Clear, null) },
                                onClick = { isFilterMenuOpen = false; list.filter = MailFilter(); searchText = ""; reload() },
                            )
                        }
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.mail_select)) },
                            leadingIcon = { Icon(Icons.Outlined.CheckCircle, null) },
                            enabled = list.threads.isNotEmpty(),
                            onClick = { isFilterMenuOpen = false; isSelecting = true },
                        )
                        if (folder?.kind == "trash" && list.total > 0) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.mail_empty_trash), color = MaterialTheme.colorScheme.error) },
                                leadingIcon = { Icon(Icons.Outlined.DeleteForever, null, tint = MaterialTheme.colorScheme.error) },
                                onClick = { isFilterMenuOpen = false; isConfirmingEmpty = true },
                            )
                        }
                    }
                }
            }
        },
        floatingActionButton = {
            if (!isSelecting) {
                ExtendedFloatingActionButton(onClick = onCompose, icon = { Icon(Icons.Outlined.Edit, null) }, text = { Text(stringResource(R.string.mail_new_message)) })
            }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize()) {
            OutlinedTextField(
                value = searchText,
                onValueChange = { typed ->
                    searchText = typed
                    if (typed.isEmpty() && list.filter.searchText.isNotEmpty()) {
                        list.filter = list.filter.copy(searchText = "")
                        reload()
                    }
                },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                singleLine = true,
                leadingIcon = { Icon(Icons.Outlined.Search, null) },
                placeholder = { Text(stringResource(R.string.common_search)) },
                shape = RoundedCornerShape(28.dp),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = {
                    list.filter = list.filter.copy(searchText = searchText)
                    reload()
                }),
            )
            val failure = list.failure
            when {
                mail.loadFailure != null && !mail.isLoaded -> ErrorState(mail.loadFailure?.message.orEmpty(), onRetry = { mail.reload() })
                failure != null && !list.isLoaded -> ErrorState(failure.message.orEmpty(), onRetry = { reload() })
                !list.isLoaded -> LoadingState()
                list.threads.isEmpty() -> ThreadListEmpty(mail, place, folder, list)
                else -> ThreadRows(
                    mail, list, folder, selection, isSelecting, words,
                    onOpen = { thread -> onOpenThread(thread.item?.id ?: thread.threadId, if (list.isAcrossFolders) null else folder?.id) },
                    onSelect = { thread ->
                        isSelecting = true
                        if (!selection.contains(thread.id)) selection.add(thread.id)
                    },
                    onToggle = { thread -> if (selection.contains(thread.id)) selection.remove(thread.id) else selection.add(thread.id) },
                    onSeen = { thread, isSeen -> setSeen(listOf(thread), isSeen) },
                    onArchive = { thread -> mail.folderOfKind("archive")?.let { archive -> move(listOf(thread), archive, words.archived) } },
                    onDelete = { thread -> delete(listOf(thread)) },
                    onMore = { scope.launch { list.load(mail, isMore = true) } },
                )
            }
        }
    }

    if (isNarrowing) {
        NarrowSheet(list.filter, onDismiss = { isNarrowing = false }, onApply = { applied ->
            list.filter = applied
            searchText = applied.searchText
            isNarrowing = false
            reload()
        })
    }
    if (isSorting) {
        val sorted = chosen()
        SortSheet(mail, sorted.size, onDismiss = { isSorting = false }, onSort = { category, priority, needsReply ->
            isSorting = false
            val target = list.target(sorted)
            scope.launch {
                if (target != null) mail.act(words.sorted) { mail.connection.sortMail(target.itemIds, category, priority, needsReply) }
                finishSelecting()
                list.load(mail)
            }
        })
    }
    if (isConfirmingEmpty) {
        ConfirmationDialog(
            title = stringResource(R.string.mail_empty_trash_question),
            text = stringResource(R.string.mail_empty_trash_message),
            confirmLabel = stringResource(R.string.mail_empty_trash),
            isDestructive = true,
            onConfirm = {
                isConfirmingEmpty = false
                val mailboxId = mail.mailbox?.id
                if (mailboxId != null) scope.launch {
                    mail.act(words.trashEmptied) { mail.connection.emptyTrash(mailboxId) }
                    list.load(mail)
                }
            },
            onDismiss = { isConfirmingEmpty = false },
        )
    }
}

/** The words an action on rows says, read once where the screen is drawn. */
class MailListWords(mail: MailModel, list: ThreadListModel, place: MailPlace, folder: com.teanode.kit.mail.MailFolder?) {
    private val context = mail.context
    val markedRead: String = context.getString(R.string.mail_marked_read)
    val markedUnread: String = context.getString(R.string.mail_marked_unread)
    val starred: String = context.getString(R.string.mail_starred_done)
    val unstarred: String = context.getString(R.string.mail_unstarred_done)
    val archived: String = context.getString(R.string.mail_conversation_archived)
    val movedToTrash: String = context.getString(R.string.mail_moved_to_trash)
    val deletedForGood: String = context.getString(R.string.mail_deleted_for_good)
    val saidNotJunk: String = context.getString(R.string.mail_said_not_junk)
    val reportedJunk: String = context.getString(R.string.mail_reported_junk)
    val sorted: String = context.getString(R.string.mail_sorted)
    val trashEmptied: String = context.getString(R.string.mail_trash_emptied)
    val deleteLabel: String = context.getString(if (folder?.kind == "trash") R.string.mail_delete_forever else R.string.mail_delete)
    fun movedTo(name: String): String = context.getString(R.string.mail_moved_to, name)
}

/** The actions on rows, the same in the menu of the rows chosen and in the thread's own menu. */
@Composable
private fun RowMenuItems(
    mail: MailModel,
    folder: com.teanode.kit.mail.MailFolder?,
    threads: List<MailThread>,
    words: MailListWords,
    onDone: () -> Unit,
    onSeen: (List<MailThread>, Boolean) -> Unit,
    onFlagged: (List<MailThread>, Boolean) -> Unit,
    onSort: () -> Unit,
    onMove: (List<MailThread>, com.teanode.kit.mail.MailFolder, String) -> Unit,
    onJunk: (List<MailThread>, Boolean) -> Unit,
    onDelete: (List<MailThread>) -> Unit,
) {
    val isAnyUnread = threads.any { thread -> thread.unread > 0 }
    val isAnyUnstarred = threads.any { thread -> !thread.flagged }
    var isMoveOpen by remember { mutableStateOf(false) }
    DropdownMenuItem(
        text = { Text(stringResource(if (isAnyUnread) R.string.mail_mark_read else R.string.mail_mark_unread)) },
        leadingIcon = { Icon(if (isAnyUnread) Icons.Outlined.MarkEmailRead else Icons.Outlined.MarkEmailUnread, null) },
        onClick = { onDone(); onSeen(threads, isAnyUnread) },
    )
    DropdownMenuItem(
        text = { Text(stringResource(if (isAnyUnstarred) R.string.mail_star else R.string.mail_unstar)) },
        leadingIcon = { Icon(if (isAnyUnstarred) Icons.Outlined.Star else Icons.Outlined.StarBorder, null) },
        onClick = { onDone(); onFlagged(threads, isAnyUnstarred) },
    )
    if (mail.mailbox?.agent?.isSorting == true) {
        DropdownMenuItem(text = { Text(stringResource(R.string.mail_sort_as)) }, leadingIcon = { Icon(Icons.Outlined.Sell, null) }, onClick = { onDone(); onSort() })
    }
    val archive = mail.folderOfKind("archive")
    if (archive != null && folder?.id != archive.id) {
        DropdownMenuItem(text = { Text(stringResource(R.string.mail_archive_action)) }, leadingIcon = { Icon(Icons.Outlined.Archive, null) }, onClick = { onDone(); onMove(threads, archive, words.archived) })
    }
    if (folder?.kind == "junk") {
        DropdownMenuItem(text = { Text(stringResource(R.string.mail_not_junk)) }, leadingIcon = { Icon(Icons.Outlined.ThumbUp, null) }, onClick = { onDone(); onJunk(threads, true) })
    } else {
        DropdownMenuItem(text = { Text(stringResource(R.string.mail_report_junk)) }, leadingIcon = { Icon(Icons.Outlined.Report, null) }, onClick = { onDone(); onJunk(threads, false) })
    }
    DropdownMenuItem(text = { Text(stringResource(R.string.mail_move_to)) }, leadingIcon = { Icon(Icons.Outlined.Folder, null) }, onClick = { isMoveOpen = !isMoveOpen })
    if (isMoveOpen) {
        for (row in MailFolders.rows(mail.folders).filter { row -> row.folder.id != folder?.id }) {
            val name = MailNames.folder(row.folder)
            DropdownMenuItem(
                text = { Text(name, modifier = Modifier.padding(start = (row.depth.coerceAtMost(3) * 12).dp)) },
                leadingIcon = { Icon(MailNames.icon(row.folder), null) },
                onClick = { onDone(); onMove(threads, row.folder, words.movedTo(name)) },
            )
        }
    }
    DropdownMenuItem(
        text = { Text(words.deleteLabel, color = MaterialTheme.colorScheme.error) },
        leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) },
        onClick = { onDone(); onDelete(threads) },
    )
}

@Composable
private fun ThreadListEmpty(mail: MailModel, place: MailPlace, folder: com.teanode.kit.mail.MailFolder?, list: ThreadListModel) {
    when {
        list.filter.isNarrowing -> EmptyState(Icons.Outlined.Search, stringResource(R.string.mail_nothing_matches))
        place == MailPlace.Starred -> EmptyState(Icons.Outlined.Star, stringResource(R.string.mail_nothing_starred), description = stringResource(R.string.mail_nothing_starred_description))
        place == MailPlace.Priority -> EmptyState(Icons.Outlined.ErrorOutline, stringResource(R.string.mail_nothing_important), description = stringResource(R.string.mail_nothing_important_description))
        folder?.kind == "inbox" && mail.mailbox?.sendingAddresses?.isEmpty() == true ->
            EmptyState(Icons.Outlined.AlternateEmail, stringResource(R.string.mail_no_address_yet), description = stringResource(R.string.mail_no_address_description))
        else -> EmptyState(Icons.Outlined.Inbox, stringResource(R.string.mail_nothing_here))
    }
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)
@Composable
private fun ThreadRows(
    mail: MailModel,
    list: ThreadListModel,
    folder: com.teanode.kit.mail.MailFolder?,
    selection: List<String>,
    isSelecting: Boolean,
    words: MailListWords,
    onOpen: (MailThread) -> Unit,
    onSelect: (MailThread) -> Unit,
    onToggle: (MailThread) -> Unit,
    onSeen: (MailThread, Boolean) -> Unit,
    onArchive: (MailThread) -> Unit,
    onDelete: (MailThread) -> Unit,
    onMore: () -> Unit,
) {
    val state = rememberLazyListState()
    val periods = MailTimes.periods(list.threads) { thread -> MailTimes.moment(thread.item?.mail?.receivedAt ?: thread.item?.addedAt) }
    // The next page is asked for when the end of the list comes near. It is watched outside composition: reading the
    // list's layout while composing makes the list compose again after every layout.
    LaunchedEffect(state, list) {
        snapshotFlow { state.layoutInfo.let { info -> info.totalItemsCount > 0 && (info.visibleItemsInfo.lastOrNull()?.index ?: 0) >= info.totalItemsCount - 4 } }
            .distinctUntilChanged()
            .collect { isNearEnd -> if (isNearEnd && list.threads.size < list.total && !list.isLoading) onMore() }
    }
    val archive = mail.folderOfKind("archive")
    LazyColumn(state = state, modifier = Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 88.dp)) {
        for (periodRows in periods) {
            item(key = "period-" + periodRows.period.toString()) { ListPeriodHeader(periodRows.period) }
            items(periodRows.rows, key = { thread -> thread.id }) { thread ->
                val isChosen = selection.contains(thread.id)
                val canArchive = archive != null && folder?.id != archive.id
                SwipeableRow(
                    isEnabled = !isSelecting,
                    startToEnd = SwipeAction(
                        if (thread.unread > 0) Icons.Outlined.MarkEmailRead else Icons.Outlined.MarkEmailUnread,
                        stringResource(if (thread.unread > 0) R.string.mail_mark_read else R.string.mail_mark_unread), MaterialTheme.colorScheme.primary,
                    ) { onSeen(thread, thread.unread > 0) },
                    endToStart = if (canArchive) {
                        SwipeAction(Icons.Outlined.Archive, stringResource(R.string.mail_archive_action), MaterialTheme.colorScheme.tertiary) { onArchive(thread) }
                    } else {
                        SwipeAction(Icons.Outlined.Delete, words.deleteLabel, MaterialTheme.colorScheme.error) { onDelete(thread) }
                    },
                ) {
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .background(if (isChosen) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface)
                            .combinedClickable(onClick = { if (isSelecting) onToggle(thread) else onOpen(thread) }, onLongClick = { onSelect(thread) })
                            .padding(horizontal = 16.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.Top,
                    ) {
                        if (isSelecting) Checkbox(checked = isChosen, onCheckedChange = null, modifier = Modifier.padding(end = 8.dp))
                        ThreadRow(thread, folderName = folderNameOf(mail, list, folder, thread))
                    }
                }
            }
        }
        if (list.isLoading) item(key = "loading") { Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { androidx.compose.material3.CircularProgressIndicator(Modifier.size(24.dp)) } }
    }
}

private fun folderNameOf(mail: MailModel, list: ThreadListModel, folder: com.teanode.kit.mail.MailFolder?, thread: MailThread): String? {
    if (!list.isAcrossFolders) return null
    val folderId = thread.item?.folderId ?: return null
    if (folderId == folder?.id) return null
    val shown = mail.folderById(folderId) ?: return null
    return MailNames.folder(shown, mail.context)
}

/** The row's period heading in a list: plain words, as a mail app draws them. */
@Composable
fun ListPeriodHeader(period: ListPeriod) {
    val title = when (period) {
        ListPeriod.Earlier -> stringResource(R.string.mail_period_earlier)
        ListPeriod.Today -> stringResource(R.string.mail_period_today)
        ListPeriod.Yesterday -> stringResource(R.string.mail_period_yesterday)
        ListPeriod.ThisWeek -> stringResource(R.string.mail_period_this_week)
        ListPeriod.LastWeek -> stringResource(R.string.mail_period_last_week)
        ListPeriod.ThisMonth -> stringResource(R.string.mail_period_this_month)
        is ListPeriod.Month -> MailTimes.monthTitle(period)
    }
    Text(title, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp))
}

/** What a swipe does: its symbol, its word and the color behind. */
class SwipeAction(val icon: ImageVector, val label: String, val color: Color, val perform: () -> Unit)

/**
 * A row that is dragged sideways to do something: far enough to the right does the first action, to the left the second,
 * and the row slides back. The color and symbol of the action show behind it while it is dragged.
 */
@Composable
private fun SwipeableRow(isEnabled: Boolean, startToEnd: SwipeAction, endToStart: SwipeAction, content: @Composable () -> Unit) {
    var offsetPixels by remember { mutableFloatStateOf(0f) }
    val thresholdPixels = with(LocalDensity.current) { 96.dp.toPx() }
    val action = when {
        offsetPixels > 0f -> startToEnd
        offsetPixels < 0f -> endToStart
        else -> null
    }
    Box(Modifier.fillMaxWidth()) {
        if (action != null) {
            Box(
                Modifier.matchParentSize().background(action.color).padding(horizontal = 24.dp),
                contentAlignment = if (offsetPixels > 0f) Alignment.CenterStart else Alignment.CenterEnd,
            ) { Icon(action.icon, contentDescription = action.label, tint = MaterialTheme.colorScheme.surface) }
        }
        Box(
            Modifier
                .offset { IntOffset(offsetPixels.roundToInt(), 0) }
                .pointerInput(isEnabled) {
                    if (!isEnabled) return@pointerInput
                    detectHorizontalDragGestures(
                        onDragEnd = {
                            val finished = offsetPixels
                            offsetPixels = 0f
                            if (finished > thresholdPixels) startToEnd.perform() else if (finished < -thresholdPixels) endToStart.perform()
                        },
                        onDragCancel = { offsetPixels = 0f },
                    ) { change, drag ->
                        change.consume()
                        offsetPixels += drag
                    }
                },
        ) { content() }
    }
}

/**
 * ThreadRow is a conversation in a list: who, the agent's marks, the subject, the agent's summary, the shield and when.
 * Every row is the same height: the summary always takes two lines, empty or not, and the marks are symbols on the first
 * line rather than a line of their own.
 */
@Composable
fun ThreadRow(thread: MailThread, folderName: String?, modifier: Modifier = Modifier) {
    val item = thread.item
    val message = item?.mail
    val isUnread = thread.unread > 0
    val insight = item?.insight
    val verdict = MailVerdict.of(message?.authenticationResults)
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.Top) {
        Box(Modifier.padding(top = 18.dp, end = 8.dp).size(8.dp).background(if (isUnread) MaterialTheme.colorScheme.primary else Color.Transparent, CircleShape).semantics { contentDescription = if (isUnread) "unread" else "" })
        SenderLogo(message?.logoDomain.orEmpty(), message?.shownSender.orEmpty(), 40)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    threadSenders(thread), style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold), maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Spacer(Modifier.width(4.dp))
                if (insight?.priority == "high") Icon(Icons.Outlined.PriorityHigh, stringResource(R.string.mail_important), Modifier.size(16.dp), tint = MaterialTheme.colorScheme.error)
                if (insight?.needsReply == true) Icon(Icons.AutoMirrored.Outlined.Reply, stringResource(R.string.mail_needs_reply), Modifier.size(16.dp), tint = MaterialTheme.colorScheme.primary)
                if (verdict == MailVerdict.Bad) Icon(Icons.Outlined.GppBad, stringResource(R.string.mail_not_verified), Modifier.size(16.dp), tint = MaterialTheme.colorScheme.error)
                else if (verdict == MailVerdict.Warn) Icon(Icons.Outlined.GppMaybe, stringResource(R.string.mail_looks_like_spam), Modifier.size(16.dp), tint = Color(0xFFE08A00))
                if (thread.flagged) Icon(Icons.Outlined.Star, stringResource(R.string.mail_starred), Modifier.size(16.dp), tint = Color(0xFFE08A00))
                Spacer(Modifier.width(4.dp))
                Text(MailTimes.label(MailTimes.moment(message?.receivedAt ?: item?.addedAt)), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (thread.hasDraft) Text(stringResource(R.string.mail_draft_marker) + " ", color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
                if (folderName != null) Text("$folderName ", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
                Text(
                    message?.subject?.takeIf { subject -> subject.isNotEmpty() } ?: stringResource(R.string.mail_no_subject),
                    style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
                )
                if ((message?.attachmentCount ?: 0) > 0) Icon(Icons.Outlined.AttachFile, stringResource(R.string.mail_has_attachments), Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                if (thread.count > 1) {
                    Spacer(Modifier.width(4.dp))
                    Text(
                        thread.count.toString(), style = MaterialTheme.typography.labelSmall,
                        color = if (isUnread) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.background(if (isUnread) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(5.dp)).padding(horizontal = 6.dp, vertical = 1.dp),
                    )
                }
            }
            // The summary always takes two lines, so every row is the same height.
            Text(
                insight?.summary ?: "", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, minLines = 2, maxLines = 2, overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun threadSenders(thread: MailThread): String {
    if (thread.participants.size > 1) return thread.participants.joinToString(", ")
    val sender = thread.item?.mail?.shownSender.orEmpty()
    return sender.ifEmpty { stringResource(R.string.mail_unknown_sender) }
}

/** SenderLogo is the sender's published logo when the server has one, and otherwise their initial. */
@Composable
fun SenderLogo(domain: String, name: String, sizeDp: Int = 36) {
    AvatarView(name = name, avatarUrl = if (domain.isEmpty()) null else "/api/v1/logo/$domain", colorIndex = null, size = sizeDp.dp)
}
